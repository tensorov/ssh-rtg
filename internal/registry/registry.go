// Package registry provides a disk-based service registry for rtg-orchestrator.
//
// All state is stored as JSON files on disk — no in-memory caching.
// Service IDs are opaque strings derived from {host}-{port}.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ─── Types ───────────────────────────────────────────────────────────────────

// Service represents a registered tunnel service.
type Service struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Domain   string `json:"domain,omitempty"`
	Proto    string `json:"proto"`
	TTL      int    `json:"ttl"`
	LastSeen string `json:"last_seen"`
}

// RegisterRequest is the expected body for POST /v1/services/register.
type RegisterRequest struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Domain string `json:"domain,omitempty"`
	Proto  string `json:"proto"`
	TTL    int    `json:"ttl"`
}

// RegisterResponse is returned on successful registration.
type RegisterResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// HeartbeatRequest is the expected body for POST /v1/services/heartbeat.
type HeartbeatRequest struct {
	Host string `json:"host"`
}

// HeartbeatResponse is returned on successful heartbeat.
type HeartbeatResponse struct {
	Status          string `json:"status"`
	ServicesUpdated int    `json:"services_updated"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// formatID creates an opaque service ID from host and port.
// The ID is NOT parsed back — it's opaque for file naming.
func formatID(host string, port int) string {
	return fmt.Sprintf("%s-%d", host, port)
}

// servicePath returns the full file path for a service given its ID.
func servicePath(servicesDir, id string) string {
	return filepath.Join(servicesDir, id+".json")
}

// marshalService serializes a Service to JSON.
func marshalService(svc Service) ([]byte, error) {
	return json.Marshal(svc)
}

// unmarshalService deserializes JSON into a Service.
func unmarshalService(data []byte) (Service, error) {
	var svc Service
	err := json.Unmarshal(data, &svc)
	return svc, err
}

// ensureDir creates the services directory if it doesn't exist.
func ensureDir(servicesDir string) error {
	return os.MkdirAll(servicesDir, 0o755)
}

// readAllServices reads all .json files from the services directory.
// Errors on individual files are silently skipped.
func readAllServices(servicesDir string) ([]string, error) {
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read services dir: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		files = append(files, entry.Name())
	}
	return files, nil
}

// parseServiceFromFile reads and unmarshals a single service file.
func parseServiceFromFile(servicesDir, fileName string) (Service, bool) {
	filePath := filepath.Join(servicesDir, fileName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Service{}, false
	}
	svc, err := unmarshalService(data)
	if err != nil {
		return Service{}, false
	}
	return svc, true
}

// ─── Core Operations ─────────────────────────────────────────────────────────

// Register creates a new service registration.
// Validates inputs, writes the service file to disk, and returns an opaque ID.
func Register(servicesDir string, req RegisterRequest) (RegisterResponse, error) {
	// Validation
	if req.Host == "" {
		return RegisterResponse{}, fmt.Errorf("host is required")
	}
	if req.Port < 1 || req.Port > 65535 {
		return RegisterResponse{}, fmt.Errorf("port must be between 1 and 65535")
	}
	if req.Proto != "http" && req.Proto != "tcp" {
		return RegisterResponse{}, fmt.Errorf("proto must be \"http\" or \"tcp\"")
	}
	if req.TTL <= 0 {
		req.TTL = 30
	}

	id := formatID(req.Host, req.Port)

	svc := Service{
		Host:     req.Host,
		Port:     req.Port,
		Domain:   req.Domain,
		Proto:    req.Proto,
		TTL:      req.TTL,
		LastSeen: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := marshalService(svc)
	if err != nil {
		return RegisterResponse{}, fmt.Errorf("marshal service: %w", err)
	}

	if err := ensureDir(servicesDir); err != nil {
		return RegisterResponse{}, fmt.Errorf("create services dir: %w", err)
	}

	filePath := servicePath(servicesDir, id)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return RegisterResponse{}, fmt.Errorf("write service file: %w", err)
	}

	return RegisterResponse{ID: id, Status: "registered"}, nil
}

// Heartbeat updates the last_seen timestamp for ALL services of a given host.
// If the host has no services registered, it performs a lazy register (creates
// a minimal service entry with port=0). Heartbeat does NOT trigger Traefik
// config regeneration.
func Heartbeat(servicesDir string, req HeartbeatRequest) (HeartbeatResponse, error) {
	if req.Host == "" {
		return HeartbeatResponse{}, fmt.Errorf("host is required")
	}

	if err := ensureDir(servicesDir); err != nil {
		return HeartbeatResponse{}, fmt.Errorf("create services dir: %w", err)
	}

	files, err := readAllServices(servicesDir)
	if err != nil {
		return HeartbeatResponse{}, fmt.Errorf("read services: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	updated := 0

	for _, fileName := range files {
		svc, ok := parseServiceFromFile(servicesDir, fileName)
		if !ok {
			continue
		}
		if svc.Host == req.Host {
			svc.LastSeen = now
			data, err := marshalService(svc)
			if err != nil {
				continue
			}
			filePath := filepath.Join(servicesDir, fileName)
			if err := os.WriteFile(filePath, data, 0o644); err != nil {
				continue
			}
			updated++
		}
	}

	if updated == 0 {
		// Lazy register: create a minimal service for this host
		id := formatID(req.Host, 0)
		svc := Service{
			Host:     req.Host,
			Port:     0,
			Proto:    "tcp",
			TTL:      30,
			LastSeen: now,
		}
		data, err := marshalService(svc)
		if err != nil {
			return HeartbeatResponse{}, fmt.Errorf("marshal lazy service: %w", err)
		}
		filePath := servicePath(servicesDir, id)
		if err := os.WriteFile(filePath, data, 0o644); err != nil {
			return HeartbeatResponse{}, fmt.Errorf("write lazy service: %w", err)
		}
		return HeartbeatResponse{Status: "ok", ServicesUpdated: 1}, nil
	}

	return HeartbeatResponse{Status: "ok", ServicesUpdated: updated}, nil
}

// ListServices returns all service registrations from disk.
// If statusFilter is "alive", only services whose last_seen is within
// TTL*2 seconds are returned.
func ListServices(servicesDir string, statusFilter string) ([]Service, error) {
	files, err := readAllServices(servicesDir)
	if err != nil {
		return []Service{}, nil
	}
	if files == nil {
		return []Service{}, nil
	}

	now := time.Now()
	var services []Service

	for _, fileName := range files {
		svc, ok := parseServiceFromFile(servicesDir, fileName)
		if !ok {
			continue
		}

		// Compute alive status
		status := computeStatus(svc, now)
		if statusFilter == "alive" && status != "alive" {
			continue
		}

		services = append(services, svc)
	}

	if services == nil {
		services = []Service{}
	}
	return services, nil
}

// computeStatus returns "alive" or "expired" based on last_seen and TTL.
// A service is "alive" if last_seen is within TTL*2 seconds.
func computeStatus(svc Service, now time.Time) string {
	lastSeen, err := time.Parse(time.RFC3339, svc.LastSeen)
	if err != nil {
		return "unknown"
	}
	deadline := lastSeen.Add(time.Duration(svc.TTL*2) * time.Second)
	if now.After(deadline) {
		return "expired"
	}
	return "alive"
}

// DeleteService removes a service registration by its opaque ID.
// The ID is NOT parsed — the file is deleted directly.
func DeleteService(servicesDir string, id string) error {
	filePath := servicePath(servicesDir, id)
	err := os.Remove(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service not found: %s", id)
		}
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

// ServiceCount returns the number of service files on disk.
func ServiceCount(servicesDir string) int {
	files, err := readAllServices(servicesDir)
	if err != nil || files == nil {
		return 0
	}
	return len(files)
}

// Cleanup removes all services whose last_seen is older than TTL*3 seconds.
// Returns the list of removed file names. Safe to call in a background loop.
func Cleanup(servicesDir string) []string {
	files, err := readAllServices(servicesDir)
	if err != nil || files == nil {
		return nil
	}

	now := time.Now()
	var removed []string

	for _, fileName := range files {
		svc, ok := parseServiceFromFile(servicesDir, fileName)
		if !ok {
			continue
		}

		lastSeen, err := time.Parse(time.RFC3339, svc.LastSeen)
		if err != nil {
			continue
		}

		deadline := lastSeen.Add(time.Duration(svc.TTL*3) * time.Second)
		if now.After(deadline) {
			filePath := filepath.Join(servicesDir, fileName)
			if err := os.Remove(filePath); err == nil {
				removed = append(removed, fileName)
			}
		}
	}

	return removed
}
