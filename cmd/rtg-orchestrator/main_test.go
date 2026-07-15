package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupTestHandler creates a handler with a temp data directory for testing.
func setupTestHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	handler := setupHandlers(dir, "/tmp/rtg-test-config")
	return handler, dir
}

func TestHealthEndpoint(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// No services yet
	resp, err := http.Get(srv.URL + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var health struct {
		Status        string `json:"status"`
		Uptime        string `json:"uptime"`
		ServicesCount int    `json:"services_count"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatalf("unmarshal health response: %v (body: %s)", err, string(body))
	}

	if health.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", health.Status)
	}
	if health.ServicesCount != 0 {
		t.Errorf("expected services_count 0, got %d", health.ServicesCount)
	}
	if health.Uptime == "" {
		t.Errorf("expected non-empty uptime")
	}
}

func TestHealthEndpointReturns200(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestHealthShowsServiceCount(t *testing.T) {
	handler, dataDir := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Register a service via the API
	regResp, err := http.Post(srv.URL+"/v1/services/register",
		"application/json",
		strings.NewReader(`{"host":"vps-b","port":8080,"proto":"tcp","ttl":30}`))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	regResp.Body.Close()

	if regResp.StatusCode != http.StatusOK {
		t.Fatalf("register status: %d", regResp.StatusCode)
	}

	// Health should now show services_count=1
	resp, err := http.Get(srv.URL + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	defer resp.Body.Close()

	var health struct {
		ServicesCount int `json:"services_count"`
	}
	json.NewDecoder(resp.Body).Decode(&health)
	if health.ServicesCount != 1 {
		t.Errorf("expected services_count=1, got %d", health.ServicesCount)
	}

	_ = dataDir // dataDir used via handler closure
}

// ─── Happy: register → list → service in list ───────────────────────────────

func TestRegisterAndListHTTP(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Register
	regResp, err := http.Post(srv.URL+"/v1/services/register",
		"application/json",
		strings.NewReader(`{"host":"vps-b","port":8080,"proto":"http","ttl":30}`))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer regResp.Body.Close()

	if regResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", regResp.StatusCode)
	}

	var regResult struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(regResp.Body).Decode(&regResult); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if regResult.ID != "vps-b-8080" {
		t.Errorf("expected id 'vps-b-8080', got %q", regResult.ID)
	}
	if regResult.Status != "registered" {
		t.Errorf("expected status 'registered', got %q", regResult.Status)
	}

	// List
	listResp, err := http.Get(srv.URL + "/v1/services")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}

	var services []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&services); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0]["host"] != "vps-b" {
		t.Errorf("expected host 'vps-b', got %v", services[0]["host"])
	}
	if services[0]["port"] != float64(8080) {
		t.Errorf("expected port 8080, got %v", services[0]["port"])
	}
}

// ─── Happy: delete → list → empty array ─────────────────────────────────────

func TestDeleteAndListHTTP(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Register
	http.Post(srv.URL+"/v1/services/register",
		"application/json",
		strings.NewReader(`{"host":"vps-b","port":8080,"proto":"tcp","ttl":30}`))

	// Delete
	req, _ := http.NewRequest("DELETE", srv.URL+"/v1/services/vps-b-8080", nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	delResp.Body.Close()

	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delResp.StatusCode)
	}

	// List should be empty
	listResp, _ := http.Get(srv.URL + "/v1/services")
	var services []map[string]any
	json.NewDecoder(listResp.Body).Decode(&services)
	listResp.Body.Close()

	if len(services) != 0 {
		t.Errorf("expected 0 services, got %d", len(services))
	}
}

// ─── Failure: DELETE non-existent → 404 ──────────────────────────────────────

func TestDeleteNonExistentHTTP(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, _ := http.NewRequest("DELETE", srv.URL+"/v1/services/nonexistent", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not found") {
		t.Errorf("expected error message containing 'not found', got: %s", string(body))
	}
}

// ─── Failure: register with port=0 → 400 + "port" in error ──────────────────

func TestRegisterPortZeroHTTP(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/services/register",
		"application/json",
		strings.NewReader(`{"host":"vps-b","port":0,"proto":"tcp"}`))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "port") {
		t.Errorf("expected error containing 'port', got: %s", string(body))
	}
}

// ─── Failure: register with invalid JSON → 400 ──────────────────────────────

func TestRegisterInvalidJSONHTTP(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/services/register",
		"application/json",
		strings.NewReader(`{invalid json`))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ─── Failure: register missing host → 400 ───────────────────────────────────

func TestRegisterMissingHostHTTP(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/services/register",
		"application/json",
		strings.NewReader(`{"port":8080,"proto":"tcp"}`))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ─── Happy: heartbeat via HTTP ──────────────────────────────────────────────

func TestHeartbeatHTTP(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Register
	http.Post(srv.URL+"/v1/services/register",
		"application/json",
		strings.NewReader(`{"host":"vps-b","port":8080,"proto":"tcp","ttl":30}`))

	time.Sleep(100 * time.Millisecond)

	// Heartbeat
	hbResp, err := http.Post(srv.URL+"/v1/services/heartbeat",
		"application/json",
		strings.NewReader(`{"host":"vps-b"}`))
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	defer hbResp.Body.Close()

	if hbResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", hbResp.StatusCode)
	}

	var hbResult struct {
		Status          string `json:"status"`
		ServicesUpdated int    `json:"services_updated"`
	}
	json.NewDecoder(hbResp.Body).Decode(&hbResult)
	if hbResult.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", hbResult.Status)
	}
	if hbResult.ServicesUpdated != 1 {
		t.Errorf("expected ServicesUpdated=1, got %d", hbResult.ServicesUpdated)
	}
}

// ─── List with status=alive filter via HTTP ──────────────────────────────────

func TestListAliveHTTP(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Register with 30s TTL
	http.Post(srv.URL+"/v1/services/register",
		"application/json",
		strings.NewReader(`{"host":"vps-b","port":8080,"proto":"tcp","ttl":30}`))

	// List alive
	listResp, err := http.Get(srv.URL + "/v1/services?status=alive")
	if err != nil {
		t.Fatalf("list alive: %v", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}

	var services []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&services); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(services) != 1 {
		t.Errorf("expected 1 alive service, got %d", len(services))
	}
}

// ─── Service file persists to disk ───────────────────────────────────────────

func TestServiceWrittenToDisk(t *testing.T) {
	handler, dataDir := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	http.Post(srv.URL+"/v1/services/register",
		"application/json",
		strings.NewReader(`{"host":"vps-b","port":8080,"proto":"tcp","ttl":30}`))

	// Verify file on disk
	svcFile := filepath.Join(dataDir, "services", "vps-b-8080.json")
	if _, err := os.Stat(svcFile); os.IsNotExist(err) {
		t.Fatal("service file was not written to disk")
	}
}

// ─── Heartbeat with empty host → 400 ────────────────────────────────────────

func TestHeartbeatMissingHostHTTP(t *testing.T) {
	handler, _ := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/services/heartbeat",
		"application/json",
		strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ─── Health with services via direct disk write ──────────────────────────────

func TestHealthReflectsDiskState(t *testing.T) {
	handler, dataDir := setupTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Write a service file directly to disk (simulate prior state)
	svcDir := filepath.Join(dataDir, "services")
	os.MkdirAll(svcDir, 0o755)
	os.WriteFile(filepath.Join(svcDir, "vps-x-9999.json"),
		[]byte(`{"host":"vps-x","port":9999,"proto":"tcp","ttl":30,"last_seen":"2026-01-01T00:00:00Z"}`),
		0o644)

	// Health should show 1 service
	resp, _ := http.Get(srv.URL + "/v1/health")
	var health struct {
		ServicesCount int `json:"services_count"`
	}
	json.NewDecoder(resp.Body).Decode(&health)
	resp.Body.Close()

	if health.ServicesCount != 1 {
		t.Errorf("expected services_count=1 (from disk), got %d", health.ServicesCount)
	}
}
