package sandbox_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/sandbox"
)

func TestHandleAuditEvents_NoAudit(t *testing.T) {
	h := sandbox.NewHandler(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/sandbox/audit", nil)
	w := httptest.NewRecorder()
	h.HandleAuditEvents(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleAuditEvents_NilPool(t *testing.T) {
	h := sandbox.NewHandler(nil, nil, nil, nil)
	h.Audit = sandbox.NewAuditLogger(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/sandbox/audit", nil)
	w := httptest.NewRecorder()
	h.HandleAuditEvents(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with nil pool, got %d", w.Code)
	}
}

func TestHandleAuditEvents_LimitParam(t *testing.T) {
	h := sandbox.NewHandler(nil, nil, nil, nil)
	h.Audit = sandbox.NewAuditLogger(nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/sandbox/audit?limit=999", nil)
	w := httptest.NewRecorder()
	h.HandleAuditEvents(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with nil pool, got %d", w.Code)
	}
}

func TestAuditQuery_DefaultLimit(t *testing.T) {
	q := sandbox.AuditQuery{}
	if q.Limit != 0 {
		t.Errorf("zero-value limit expected 0, got %d", q.Limit)
	}
}

func TestAuditQuery_Fields(t *testing.T) {
	q := sandbox.AuditQuery{
		BuildID: "build-1",
		UserID:  "user-1",
		Action:  "secret_access",
		Limit:   25,
	}
	if q.BuildID != "build-1" {
		t.Error("BuildID mismatch")
	}
	if q.UserID != "user-1" {
		t.Error("UserID mismatch")
	}
	if q.Action != "secret_access" {
		t.Error("Action mismatch")
	}
	if q.Limit != 25 {
		t.Errorf("Limit = %d, want 25", q.Limit)
	}
}

func TestAuditLogger_QueryNoPool(t *testing.T) {
	a := sandbox.NewAuditLogger(nil, nil)
	_, err := a.Query(nil, sandbox.AuditQuery{Limit: 10})
	if err == nil {
		t.Error("expected error with nil pool")
	}
}

func TestAuditEvent_AllActions(t *testing.T) {
	actions := []sandbox.AuditAction{
		sandbox.AuditSecretAccess,
		sandbox.AuditSecretDenied,
		sandbox.AuditContainerCreated,
		sandbox.AuditContainerDestroy,
		sandbox.AuditLogRedacted,
		sandbox.AuditWorkspaceScrub,
		sandbox.AuditAccessDenied,
		sandbox.AuditDataExport,
		sandbox.AuditConfigChange,
		sandbox.AuditOwnershipCheck,
	}
	for _, a := range actions {
		data, err := json.Marshal(a)
		if err != nil {
			t.Errorf("action %q: marshal error: %v", a, err)
		}
		var decoded sandbox.AuditAction
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("action %q: unmarshal error: %v", a, err)
		}
		if decoded != a {
			t.Errorf("action round-trip: got %q, want %q", decoded, a)
		}
	}
}
