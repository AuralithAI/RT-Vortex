// Package api provides HTTP handlers for the RTVortex REST API.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/session"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// HealthHandler provides health and readiness endpoints.
type HealthHandler struct {
	db         *store.DB
	redis      *session.RedisClient
	enginePool *engine.Pool
	version    string
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(db *store.DB, redis *session.RedisClient, enginePool *engine.Pool, version string) *HealthHandler {
	return &HealthHandler{
		db:         db,
		redis:      redis,
		enginePool: enginePool,
		version:    version,
	}
}

// Health returns basic liveness status.
// GET /health
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// Ready returns detailed readiness status including dependency checks.
// GET /ready
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	checks := map[string]string{}
	ready := true

	// PostgreSQL
	if err := h.db.Pool.Ping(ctx); err != nil {
		checks["postgres"] = "unhealthy: " + err.Error()
		ready = false
	} else {
		checks["postgres"] = "ok"
	}

	// Redis
	if err := h.redis.Client().Ping(ctx).Err(); err != nil {
		checks["redis"] = "unhealthy: " + err.Error()
		ready = false
	} else {
		checks["redis"] = "ok"
	}

	// Engine gRPC
	if h.enginePool.Healthy() {
		checks["engine"] = "ok"
	} else {
		checks["engine"] = "unhealthy"
		ready = false
	}

	status := http.StatusOK
	statusStr := "ready"
	if !ready {
		status = http.StatusServiceUnavailable
		statusStr = "not_ready"
	}

	writeJSON(w, status, map[string]interface{}{
		"status": statusStr,
		"checks": checks,
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// Version returns build information.
// GET /version
func (h *HealthHandler) Version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version": h.version,
		"service": "rtvortex-api",
	})
}

// ─── JSON response helpers ──────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error":   http.StatusText(status),
		"message": message,
		"status":  status,
	})
}

// writeValidationError writes a structured 422 response with field-level errors.
func writeValidationError(w http.ResponseWriter, ve interface{ StatusCode() int }) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(ve)
}

func readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
