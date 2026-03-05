package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/api"
)

// ── Pagination Helper Tests ─────────────────────────────────────────────────

func TestParsePagination_Defaults(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/repos", nil)
	limit, offset := api.ParsePaginationExported(req)
	if limit != 20 {
		t.Errorf("default limit = %d, want 20", limit)
	}
	if offset != 0 {
		t.Errorf("default offset = %d, want 0", offset)
	}
}

func TestParsePagination_CustomValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/repos?limit=50&offset=10", nil)
	limit, offset := api.ParsePaginationExported(req)
	if limit != 50 {
		t.Errorf("limit = %d, want 50", limit)
	}
	if offset != 10 {
		t.Errorf("offset = %d, want 10", offset)
	}
}

func TestParsePagination_MaxLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/repos?limit=200", nil)
	limit, _ := api.ParsePaginationExported(req)
	if limit != 20 {
		t.Errorf("limit above 100 should default to 20, got %d", limit)
	}
}

func TestParsePagination_ZeroLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/repos?limit=0", nil)
	limit, _ := api.ParsePaginationExported(req)
	if limit != 20 {
		t.Errorf("zero limit should default to 20, got %d", limit)
	}
}

func TestParsePagination_NegativeLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/repos?limit=-5", nil)
	limit, _ := api.ParsePaginationExported(req)
	if limit != 20 {
		t.Errorf("negative limit should default to 20, got %d", limit)
	}
}

func TestParsePagination_NegativeOffset(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/repos?offset=-5", nil)
	_, offset := api.ParsePaginationExported(req)
	if offset != 0 {
		t.Errorf("negative offset should default to 0, got %d", offset)
	}
}

func TestParsePagination_InvalidValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/repos?limit=abc&offset=xyz", nil)
	limit, offset := api.ParsePaginationExported(req)
	if limit != 20 {
		t.Errorf("invalid limit should default to 20, got %d", limit)
	}
	if offset != 0 {
		t.Errorf("invalid offset should default to 0, got %d", offset)
	}
}

func TestParsePagination_BoundaryValues(t *testing.T) {
	// Exactly 100 (max) should work.
	req := httptest.NewRequest("GET", "/api/v1/repos?limit=100", nil)
	limit, _ := api.ParsePaginationExported(req)
	if limit != 100 {
		t.Errorf("limit 100 should be accepted, got %d", limit)
	}

	// 101 should fall back to default.
	req = httptest.NewRequest("GET", "/api/v1/repos?limit=101", nil)
	limit, _ = api.ParsePaginationExported(req)
	if limit != 20 {
		t.Errorf("limit 101 should default to 20, got %d", limit)
	}

	// 1 (minimum valid) should work.
	req = httptest.NewRequest("GET", "/api/v1/repos?limit=1", nil)
	limit, _ = api.ParsePaginationExported(req)
	if limit != 1 {
		t.Errorf("limit 1 should be accepted, got %d", limit)
	}
}

// ── WriteValidationError Tests ──────────────────────────────────────────────

func TestWriteValidationError_Returns422(t *testing.T) {
	rr := httptest.NewRecorder()
	ve := &mockValidationError{statusCode: http.StatusUnprocessableEntity}
	api.WriteValidationErrorExported(rr, ve)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rr.Code)
	}
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
}

type mockValidationError struct {
	statusCode int
}

func (m *mockValidationError) StatusCode() int { return m.statusCode }
