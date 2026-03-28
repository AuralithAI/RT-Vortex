package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/api"
)

// ── Health endpoint ─────────────────────────────────────────────────────────

func TestHealthHandler_Health(t *testing.T) {
	// NewHealthHandler accepts (db, redis, enginePool, version) — pass nil for deps.
	h := api.NewHealthHandler(nil, nil, nil, "1.0.0")
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	h.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", body["status"])
	}
}

// ── Version endpoint ────────────────────────────────────────────────────────

func TestHealthHandler_Version(t *testing.T) {
	h := api.NewHealthHandler(nil, nil, nil, "1.2.3")
	req := httptest.NewRequest("GET", "/version", nil)
	rr := httptest.NewRecorder()

	h.Version(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["version"] != "1.2.3" {
		t.Errorf("expected version '1.2.3', got %v", body["version"])
	}
	if body["service"] != "rtvortex-api" {
		t.Errorf("expected service 'rtvortex-api', got %v", body["service"])
	}
}

// ── Health response Content-Type ────────────────────────────────────────────

func TestHealthHandler_ContentType(t *testing.T) {
	h := api.NewHealthHandler(nil, nil, nil, "1.0.0")
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	h.Health(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
}
