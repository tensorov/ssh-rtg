package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tensorov/reverse-ssh-gateway/internal/registry"
)

var (
	startTime = time.Now()
)

type healthResponse struct {
	Status        string `json:"status"`
	Uptime        string `json:"uptime"`
	ServicesCount int    `json:"services_count"`
}

// loggingMiddleware logs each HTTP request with method, path, duration, and remote addr.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", duration.String(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// setupHandlers creates the HTTP handler mux with all routes.
func setupHandlers(dataDir, configDir string) http.Handler {
	servicesDir := filepath.Join(dataDir, "services")

	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		uptime := time.Since(startTime)
		count := registry.ServiceCount(servicesDir)
		resp := healthResponse{
			Status:        "ok",
			Uptime:        uptime.String(),
			ServicesCount: count,
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("POST /v1/services/register", func(w http.ResponseWriter, r *http.Request) {
		var req registry.RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
			return
		}

		resp, err := registry.Register(servicesDir, req)
		if err != nil {
			errMsg := err.Error()
			status := http.StatusBadRequest
			if strings.Contains(errMsg, "host is required") ||
				strings.Contains(errMsg, "port must be") ||
				strings.Contains(errMsg, "proto must be") {
				status = http.StatusBadRequest
			} else {
				status = http.StatusInternalServerError
			}
			writeError(w, status, errMsg)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("POST /v1/services/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var req registry.HeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
			return
		}

		resp, err := registry.Heartbeat(servicesDir, req)
		if err != nil {
			errMsg := err.Error()
			status := http.StatusBadRequest
			if strings.Contains(errMsg, "host is required") {
				status = http.StatusBadRequest
			} else {
				status = http.StatusInternalServerError
			}
			writeError(w, status, errMsg)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("GET /v1/services", func(w http.ResponseWriter, r *http.Request) {
		statusFilter := r.URL.Query().Get("status")
		services, err := registry.ListServices(servicesDir, statusFilter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, services)
	})

	mux.HandleFunc("DELETE /v1/services/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id is required")
			return
		}

		if err := registry.DeleteService(servicesDir, id); err != nil {
			if strings.Contains(err.Error(), "service not found") {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	_ = configDir // used in later tasks

	return loggingMiddleware(mux)
}

func main() {
	port := flag.Int("port", 8443, "HTTP server port")
	configDir := flag.String("config-dir", "/etc/traefik/dynamic/tunnels/", "Traefik dynamic config directory")
	dataDir := flag.String("data-dir", "/var/lib/rtg-orchestrator/", "Service registry data directory")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	servicesDir := filepath.Join(*dataDir, "services")
	handler := setupHandlers(*dataDir, *configDir)

	addr := fmt.Sprintf(":%d", *port)
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Ensure data directories exist on startup
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		slog.Error("failed to create data directory", "path", *dataDir, "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		slog.Error("failed to create services directory", "path", servicesDir, "error", err)
		os.Exit(1)
	}

	// Background service cleanup every 30 seconds
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				removed := registry.Cleanup(servicesDir)
				if len(removed) > 0 {
					slog.Info("cleanup removed expired services", "count", len(removed), "services", removed)
				}
			}
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		slog.Info("starting server", "addr", addr, "config_dir", *configDir, "data_dir", *dataDir)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	slog.Info("shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
