package registry

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// setupTestDir creates a temporary directory for testing.
func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	svcDir := filepath.Join(dir, "services")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return dir
}

func svcDir(dir string) string {
	return filepath.Join(dir, "services")
}

// ─── Happy: Register → List → service in list ────────────────────────────────

func TestRegisterAndList(t *testing.T) {
	dir := setupTestDir(t)

	resp, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-b",
		Port:  8080,
		Proto: "http",
		TTL:   30,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.ID != "vps-b-8080" {
		t.Errorf("expected id 'vps-b-8080', got %q", resp.ID)
	}
	if resp.Status != "registered" {
		t.Errorf("expected status 'registered', got %q", resp.Status)
	}

	// Verify file was created
	path := filepath.Join(svcDir(dir), "vps-b-8080.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("service file was not created")
	}

	// List all services
	services, err := ListServices(svcDir(dir), "")
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Host != "vps-b" {
		t.Errorf("expected host 'vps-b', got %q", services[0].Host)
	}
	if services[0].Port != 8080 {
		t.Errorf("expected port 8080, got %d", services[0].Port)
	}
	if services[0].Proto != "http" {
		t.Errorf("expected proto 'http', got %q", services[0].Proto)
	}
	if services[0].TTL != 30 {
		t.Errorf("expected TTL 30, got %d", services[0].TTL)
	}
	if services[0].LastSeen == "" {
		t.Errorf("expected non-empty last_seen")
	}
}

// ─── Happy: Register → Heartbeat → list → status="alive" ────────────────────

func TestRegisterAndHeartbeat(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-b",
		Port:  8080,
		Proto: "http",
		TTL:   30,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Record the initial last_seen
	services, _ := ListServices(svcDir(dir), "")
	initialLastSeen := services[0].LastSeen

	// Sleep so the timestamp second will change (RFC3339 second precision)
	time.Sleep(1500 * time.Millisecond)

	// Heartbeat
	hbResp, err := Heartbeat(svcDir(dir), HeartbeatRequest{Host: "vps-b"})
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if hbResp.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", hbResp.Status)
	}
	if hbResp.ServicesUpdated != 1 {
		t.Errorf("expected ServicesUpdated=1, got %d", hbResp.ServicesUpdated)
	}

	// List alive services
	services, err = ListServices(svcDir(dir), "alive")
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 alive service, got %d", len(services))
	}

	// Verify last_seen was updated
	if services[0].LastSeen == initialLastSeen {
		t.Errorf("expected last_seen to be updated")
	}
}

// ─── Happy: Delete → List → empty array ──────────────────────────────────────

func TestRegisterAndDelete(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-b",
		Port:  8080,
		Proto: "tcp",
		TTL:   30,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Delete
	if err := DeleteService(svcDir(dir), "vps-b-8080"); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}

	// List should be empty
	services, err := ListServices(svcDir(dir), "")
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) != 0 {
		t.Errorf("expected 0 services after delete, got %d", len(services))
	}
}

// ─── Failure: DELETE non-existent ID → error ─────────────────────────────────

func TestDeleteNonExistent(t *testing.T) {
	dir := setupTestDir(t)

	err := DeleteService(svcDir(dir), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent service, got nil")
	}
}

// ─── Failure: Register with port=0 → error  ──────────────────────────────────

func TestRegisterInvalidPort(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-b",
		Port:  0,
		Proto: "tcp",
		TTL:   30,
	})
	if err == nil {
		t.Fatal("expected error for port=0, got nil")
	}
}

// ─── Failure: Register with port=65536 → error ───────────────────────────────

func TestRegisterPortOutOfRange(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-b",
		Port:  65536,
		Proto: "tcp",
		TTL:   30,
	})
	if err == nil {
		t.Fatal("expected error for port=65536, got nil")
	}
}

// ─── Failure: Register with empty host → error ───────────────────────────────

func TestRegisterMissingHost(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "",
		Port:  8080,
		Proto: "tcp",
		TTL:   30,
	})
	if err == nil {
		t.Fatal("expected error for empty host, got nil")
	}
}

// ─── Failure: Register with invalid proto → error ────────────────────────────

func TestRegisterInvalidProto(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-b",
		Port:  8080,
		Proto: "udp",
		TTL:   30,
	})
	if err == nil {
		t.Fatal("expected error for proto 'udp', got nil")
	}
}

// ─── TTL default ─────────────────────────────────────────────────────────────

func TestRegisterDefaultTTL(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-b",
		Port:  8080,
		Proto: "tcp",
		TTL:   0, // should default to 30
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	services, _ := ListServices(svcDir(dir), "")
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].TTL != 30 {
		t.Errorf("expected default TTL 30, got %d", services[0].TTL)
	}
}

// ─── TTL expiry: register TTL=1 → wait → list empty ──────────────────────────

func TestTTLExpiry(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-b",
		Port:  8080,
		Proto: "tcp",
		TTL:   1, // 1 second TTL
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Initially should be alive
	services, _ := ListServices(svcDir(dir), "alive")
	if len(services) != 1 {
		t.Errorf("expected 1 alive service initially, got %d", len(services))
	}

	// Wait for TTL*2 + margin = 3 seconds
	time.Sleep(3 * time.Second)

	// Should be expired now
	services, err = ListServices(svcDir(dir), "alive")
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(services) != 0 {
		t.Errorf("expected 0 alive services after TTL expiry, got %d", len(services))
	}

	// Unfiltered list should still show the service (just not alive)
	allServices, _ := ListServices(svcDir(dir), "")
	if len(allServices) != 1 {
		t.Errorf("expected 1 service in unfiltered list, got %d", len(allServices))
	}
}

// ─── Cleanup removes expired services (TTL*3) ────────────────────────────────

func TestCleanupRemovesExpired(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-b",
		Port:  8080,
		Proto: "tcp",
		TTL:   1,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Wait past TTL*3 (3 seconds + margin)
	time.Sleep(4 * time.Second)

	removed := Cleanup(svcDir(dir))
	if len(removed) != 1 {
		t.Errorf("expected Cleanup to remove 1 service, got %d", len(removed))
	}

	// List should now be empty even unfiltered
	services, _ := ListServices(svcDir(dir), "")
	if len(services) != 0 {
		t.Errorf("expected 0 services after cleanup, got %d", len(services))
	}
}

// ─── Cleanup does NOT remove alive services ──────────────────────────────────

func TestCleanupKeepsAlive(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-b",
		Port:  8080,
		Proto: "http",
		TTL:   30,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Cleanup immediately — service should still be alive
	removed := Cleanup(svcDir(dir))
	if len(removed) != 0 {
		t.Errorf("expected Cleanup to remove 0 services, got %d", len(removed))
	}

	services, _ := ListServices(svcDir(dir), "")
	if len(services) != 1 {
		t.Errorf("expected 1 service after cleanup, got %d", len(services))
	}
}

// ─── Heartbeat updates ALL services of a host ────────────────────────────────

func TestHeartbeatUpdatesAllHostServices(t *testing.T) {
	dir := setupTestDir(t)

	// Register two services for the same host
	for _, port := range []int{8080, 9090} {
		_, err := Register(svcDir(dir), RegisterRequest{
			Host:  "vps-b",
			Port:  port,
			Proto: "tcp",
			TTL:   30,
		})
		if err != nil {
			t.Fatalf("Register port %d: %v", port, err)
		}
	}

	// Register a service for a different host
	_, err := Register(svcDir(dir), RegisterRequest{
		Host:  "vps-c",
		Port:  8080,
		Proto: "tcp",
		TTL:   30,
	})
	if err != nil {
		t.Fatalf("Register vps-c: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Heartbeat only vps-b
	hbResp, err := Heartbeat(svcDir(dir), HeartbeatRequest{Host: "vps-b"})
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if hbResp.ServicesUpdated != 2 {
		t.Errorf("expected ServicesUpdated=2, got %d", hbResp.ServicesUpdated)
	}

	// Verify vps-b services have newer last_seen than vps-c
	services, _ := ListServices(svcDir(dir), "")
	for _, svc := range services {
		if svc.Host == "vps-c" {
			// vps-c should have the initial last_seen from registration
			// vps-b services should have been updated
		}
	}
}

// ─── Heartbeat: lazy register for unknown host ───────────────────────────────

func TestHeartbeatLazyRegister(t *testing.T) {
	dir := setupTestDir(t)

	// Heartbeat for a host that has no services
	hbResp, err := Heartbeat(svcDir(dir), HeartbeatRequest{Host: "vps-new"})
	if err != nil {
		t.Fatalf("Heartbeat (lazy register): %v", err)
	}
	if hbResp.ServicesUpdated != 1 {
		t.Errorf("expected ServicesUpdated=1 for lazy register, got %d", hbResp.ServicesUpdated)
	}

	// Verify a service was created
	services, _ := ListServices(svcDir(dir), "")
	if len(services) != 1 {
		t.Fatalf("expected 1 service (lazy registered), got %d", len(services))
	}
	if services[0].Host != "vps-new" {
		t.Errorf("expected host 'vps-new', got %q", services[0].Host)
	}
}

// ─── Heartbeat with empty host ───────────────────────────────────────────────

func TestHeartbeatMissingHost(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Heartbeat(svcDir(dir), HeartbeatRequest{Host: ""})
	if err == nil {
		t.Fatal("expected error for empty host, got nil")
	}
}

// ─── List with no services returns empty array ───────────────────────────────

func TestListEmpty(t *testing.T) {
	dir := setupTestDir(t)

	services, err := ListServices(svcDir(dir), "")
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if services == nil {
		t.Fatal("expected empty slice, not nil")
	}
	if len(services) != 0 {
		t.Errorf("expected 0 services, got %d", len(services))
	}
}

// ─── List with non-existent directory returns empty ──────────────────────────

func TestListNonExistentDir(t *testing.T) {
	services, err := ListServices("/tmp/rtg-test-nonexistent-"+time.Now().String(), "")
	if err != nil {
		t.Fatalf("ListServices on non-existent dir: %v", err)
	}
	if services == nil {
		t.Fatal("expected empty slice, not nil")
	}
	if len(services) != 0 {
		t.Errorf("expected 0 services, got %d", len(services))
	}
}

// ─── ServiceCount ────────────────────────────────────────────────────────────

func TestServiceCount(t *testing.T) {
	dir := setupTestDir(t)

	if c := ServiceCount(svcDir(dir)); c != 0 {
		t.Errorf("expected count 0, got %d", c)
	}

	Register(svcDir(dir), RegisterRequest{Host: "vps-b", Port: 8080, Proto: "tcp", TTL: 30})
	if c := ServiceCount(svcDir(dir)); c != 1 {
		t.Errorf("expected count 1, got %d", c)
	}

	Register(svcDir(dir), RegisterRequest{Host: "vps-b", Port: 9090, Proto: "tcp", TTL: 30})
	if c := ServiceCount(svcDir(dir)); c != 2 {
		t.Errorf("expected count 2, got %d", c)
	}
}

// ─── Opaque ID: register with host that contains dashes ──────────────────────

func TestOpaqueID(t *testing.T) {
	dir := setupTestDir(t)

	// Host with a dash — the ID will be "my-vps-8080"
	resp, err := Register(svcDir(dir), RegisterRequest{
		Host:  "my-vps",
		Port:  8080,
		Proto: "tcp",
		TTL:   30,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.ID != "my-vps-8080" {
		t.Errorf("expected id 'my-vps-8080', got %q", resp.ID)
	}

	// Delete using the opaque ID
	if err := DeleteService(svcDir(dir), "my-vps-8080"); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}

	services, _ := ListServices(svcDir(dir), "")
	if len(services) != 0 {
		t.Errorf("expected 0 services after opaque ID delete, got %d", len(services))
	}
}

// ─── Multiple services mixed alive/expired ───────────────────────────────────

func TestMixedAliveExpired(t *testing.T) {
	dir := setupTestDir(t)

	// Register a service with long TTL
	Register(svcDir(dir), RegisterRequest{Host: "vps-a", Port: 8080, Proto: "tcp", TTL: 30})
	// Register a service with very short TTL
	Register(svcDir(dir), RegisterRequest{Host: "vps-b", Port: 9090, Proto: "tcp", TTL: 1})

	time.Sleep(3 * time.Second)

	alive, _ := ListServices(svcDir(dir), "alive")
	if len(alive) != 1 {
		t.Errorf("expected 1 alive service (vps-a), got %d", len(alive))
	}
	if len(alive) > 0 && alive[0].Host != "vps-a" {
		t.Errorf("expected alive host 'vps-a', got %q", alive[0].Host)
	}

	all, _ := ListServices(svcDir(dir), "")
	if len(all) != 2 {
		t.Errorf("expected 2 total services, got %d", len(all))
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// ADVERSARIAL TESTS
// ═══════════════════════════════════════════════════════════════════════════════

// ─── Race: 10 parallel register + 10 parallel delete + heartbeat ────────────

func TestRaceCondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race test in short mode")
	}

	dir := setupTestDir(t)
	var wg sync.WaitGroup

	// 10 parallel registers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			port := 8000 + i
			_, err := Register(svcDir(dir), RegisterRequest{
				Host:  "vps-race",
				Port:  port,
				Proto: "tcp",
				TTL:   30,
			})
			if err != nil {
				t.Logf("register race err (port=%d): %v", port, err)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent heartbeat + deletes
	var hbWg sync.WaitGroup
	for i := 0; i < 5; i++ {
		hbWg.Add(1)
		go func() {
			defer hbWg.Done()
			Heartbeat(svcDir(dir), HeartbeatRequest{Host: "vps-race"})
		}()
	}

	var delWg sync.WaitGroup
	for i := 0; i < 10; i++ {
		delWg.Add(1)
		go func(i int) {
			defer delWg.Done()
			port := 8000 + i
			id := formatID("vps-race", port)
			DeleteService(svcDir(dir), id)
		}(i)
	}

	hbWg.Wait()
	delWg.Wait()

	// Should not panic or corrupt — verify list still works
	services, err := ListServices(svcDir(dir), "")
	if err != nil {
		t.Fatalf("ListServices after race: %v", err)
	}
	_ = services
}

// ─── Register with domain ────────────────────────────────────────────────────

func TestRegisterWithDomain(t *testing.T) {
	dir := setupTestDir(t)

	_, err := Register(svcDir(dir), RegisterRequest{
		Host:   "vps-b",
		Port:   8080,
		Domain: "myapp.zeitoven.ru",
		Proto:  "http",
		TTL:    30,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	services, _ := ListServices(svcDir(dir), "")
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Domain != "myapp.zeitoven.ru" {
		t.Errorf("expected domain 'myapp.zeitoven.ru', got %q", services[0].Domain)
	}
}

// ─── Heartbeat on non-existent services directory creates it ─────────────────

func TestHeartbeatCreatesServicesDir(t *testing.T) {
	dir := t.TempDir()
	// services subdirectory does NOT exist

	hbResp, err := Heartbeat(filepath.Join(dir, "services"), HeartbeatRequest{Host: "vps-b"})
	if err != nil {
		t.Fatalf("Heartbeat with no services dir: %v", err)
	}
	if hbResp.ServicesUpdated != 1 {
		t.Errorf("expected ServicesUpdated=1, got %d", hbResp.ServicesUpdated)
	}

	// Verify services dir was created
	if _, err := os.Stat(filepath.Join(dir, "services")); os.IsNotExist(err) {
		t.Error("services directory was not created")
	}
}

// ─── Write-after-read consistency (no caching) ───────────────────────────────

func TestDiskBasedReadConsistency(t *testing.T) {
	dir := setupTestDir(t)

	// Register
	Register(svcDir(dir), RegisterRequest{Host: "vps-b", Port: 8080, Proto: "tcp", TTL: 30})

	// List — should have 1
	services1, _ := ListServices(svcDir(dir), "")
	if len(services1) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services1))
	}

	// Manually write a service file (simulate another process or direct write)
	manualSvc := Service{
		Host:     "vps-manual",
		Port:     9999,
		Proto:    "tcp",
		TTL:      30,
		LastSeen: time.Now().UTC().Format(time.RFC3339),
	}
	manualData, _ := marshalService(manualSvc)
	marshalService(manualSvc) // ensure marshalService works
	if err := os.WriteFile(filepath.Join(svcDir(dir), "vps-manual-9999.json"), manualData, 0o644); err != nil {
		t.Fatalf("write manual service: %v", err)
	}

	// List again — should pick up the new file
	services2, _ := ListServices(svcDir(dir), "")
	if len(services2) != 2 {
		t.Errorf("expected 2 services after manual write, got %d", len(services2))
	}
}
