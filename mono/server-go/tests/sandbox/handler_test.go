package sandbox_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/sandbox"
	"github.com/google/uuid"
)

// ── HandleResolveAndExecute ─────────────────────────────────────────────────

func TestHandleResolveAndExecute_Success(t *testing.T) {
	mock := sandbox.NewMockRuntime()
	mock.WaitResult = &sandbox.BuildResult{
		ExitCode: 0,
		Logs:     "BUILD OK",
		Duration: 3 * time.Second,
	}

	h := sandbox.NewHandler(mock, nil, nil)

	body, _ := json.Marshal(map[string]any{
		"task_id":      uuid.New().String(),
		"repo_id":      uuid.New().String(),
		"user_id":      uuid.New().String(),
		"build_system": "go",
		"command":      "go build ./...",
		"base_image":   "golang:1.22",
		"sandbox_mode": true,
	})

	req := httptest.NewRequest(http.MethodPost, "/sandbox/resolve-execute", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleResolveAndExecute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if ec, ok := resp["exit_code"].(float64); !ok || int(ec) != 0 {
		t.Errorf("exit_code = %v, want 0", resp["exit_code"])
	}
}

func TestHandleResolveAndExecute_MissingBaseImage(t *testing.T) {
	h := sandbox.NewHandler(sandbox.NewMockRuntime(), nil, nil)

	body, _ := json.Marshal(map[string]any{
		"task_id": uuid.New().String(),
		"repo_id": uuid.New().String(),
		"user_id": uuid.New().String(),
		"command": "make",
	})

	req := httptest.NewRequest(http.MethodPost, "/sandbox/resolve-execute", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleResolveAndExecute(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleResolveAndExecute_InvalidTaskID(t *testing.T) {
	h := sandbox.NewHandler(sandbox.NewMockRuntime(), nil, nil)

	body, _ := json.Marshal(map[string]string{
		"task_id": "not-a-uuid",
		"repo_id": uuid.New().String(),
		"user_id": uuid.New().String(),
	})

	req := httptest.NewRequest(http.MethodPost, "/sandbox/resolve-execute", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleResolveAndExecute(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleResolveAndExecute_NoKeychainSkipsSecrets(t *testing.T) {
	mock := sandbox.NewMockRuntime()
	h := sandbox.NewHandler(mock, nil, nil)

	body, _ := json.Marshal(map[string]any{
		"task_id":      uuid.New().String(),
		"repo_id":      uuid.New().String(),
		"user_id":      uuid.New().String(),
		"build_system": "node",
		"command":      "npm test",
		"base_image":   "node:20",
		"secret_refs":  []string{"NPM_TOKEN", "API_KEY"},
	})

	req := httptest.NewRequest(http.MethodPost, "/sandbox/resolve-execute", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleResolveAndExecute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// No keychain → no resolved secrets, no failed secrets.
	resolved, _ := resp["resolved_secrets"]
	if resolved != nil {
		t.Errorf("expected nil resolved_secrets without keychain, got %v", resolved)
	}
}

func TestHandleResolveAndExecute_ContainerCreateError(t *testing.T) {
	mock := sandbox.NewMockRuntime()
	mock.CreateErr = sandbox.ErrRuntimeUnavailable

	h := sandbox.NewHandler(mock, nil, nil)

	body, _ := json.Marshal(map[string]any{
		"task_id":      uuid.New().String(),
		"repo_id":      uuid.New().String(),
		"user_id":      uuid.New().String(),
		"build_system": "go",
		"command":      "go build ./...",
		"base_image":   "golang:1.22",
	})

	req := httptest.NewRequest(http.MethodPost, "/sandbox/resolve-execute", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleResolveAndExecute(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleResolveAndExecute_BuildFailed(t *testing.T) {
	mock := sandbox.NewMockRuntime()
	mock.WaitResult = &sandbox.BuildResult{
		ExitCode: 1,
		Logs:     "COMPILATION ERROR",
		Duration: 5 * time.Second,
	}

	h := sandbox.NewHandler(mock, nil, nil)

	body, _ := json.Marshal(map[string]any{
		"task_id":      uuid.New().String(),
		"repo_id":      uuid.New().String(),
		"user_id":      uuid.New().String(),
		"build_system": "cmake",
		"command":      "cmake --build .",
		"base_image":   "rtvortex/builder-cpp:latest",
	})

	req := httptest.NewRequest(http.MethodPost, "/sandbox/resolve-execute", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleResolveAndExecute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if ec, ok := resp["exit_code"].(float64); !ok || int(ec) != 1 {
		t.Errorf("exit_code = %v, want 1", resp["exit_code"])
	}
}

func TestHandleResolveAndExecute_Defaults(t *testing.T) {
	mock := sandbox.NewMockRuntime()
	h := sandbox.NewHandler(mock, nil, nil)

	body, _ := json.Marshal(map[string]any{
		"task_id":      uuid.New().String(),
		"repo_id":      uuid.New().String(),
		"user_id":      uuid.New().String(),
		"build_system": "go",
		"command":      "go test ./...",
		"base_image":   "golang:1.22",
	})

	req := httptest.NewRequest(http.MethodPost, "/sandbox/resolve-execute", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleResolveAndExecute(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if len(mock.CreatedPlans) != 1 {
		t.Fatalf("expected 1 created plan, got %d", len(mock.CreatedPlans))
	}

	plan := mock.CreatedPlans[0]
	if plan.MemoryLimit != sandbox.DefaultMemoryLimit {
		t.Errorf("MemoryLimit = %q, want %q", plan.MemoryLimit, sandbox.DefaultMemoryLimit)
	}
	if plan.CPULimit != sandbox.DefaultCPULimit {
		t.Errorf("CPULimit = %q, want %q", plan.CPULimit, sandbox.DefaultCPULimit)
	}
	if plan.Timeout != sandbox.DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", plan.Timeout, sandbox.DefaultTimeout)
	}
}
