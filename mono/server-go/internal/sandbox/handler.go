package sandbox

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ── Handler ─────────────────────────────────────────────────────────────────

// Handler serves sandbox-related HTTP endpoints.
type Handler struct {
	Runtime ContainerRuntime
	Logger  *slog.Logger
}

// NewHandler creates a sandbox HTTP handler.
func NewHandler(runtime ContainerRuntime, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{Runtime: runtime, Logger: logger}
}

// ── POST /internal/swarm/sandbox/plan ────────────────────────────────────────

// HandleGeneratePlan creates a build plan from the request payload.
func (h *Handler) HandleGeneratePlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID       string   `json:"task_id"`
		RepoID       string   `json:"repo_id"`
		RepoFiles    []string `json:"repo_files"`
		ChangedFiles []string `json:"changed_files"`
		SecretNames  []string `json:"secret_names"`
		SandboxMode  bool     `json:"sandbox_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	taskID, err := uuid.Parse(req.TaskID)
	if err != nil {
		http.Error(w, `{"error":"invalid task_id"}`, http.StatusBadRequest)
		return
	}

	plan := GeneratePlan(r.Context(), PlanOptions{
		TaskID:       taskID,
		RepoID:       req.RepoID,
		RepoFiles:    req.RepoFiles,
		ChangedFiles: req.ChangedFiles,
		SecretNames:  req.SecretNames,
		SandboxMode:  req.SandboxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(plan)
}

// ── POST /internal/swarm/sandbox/execute ─────────────────────────────────────

// HandleExecute triggers a sandboxed build execution.
func (h *Handler) HandleExecute(w http.ResponseWriter, r *http.Request) {
	var plan BuildPlan
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	containerID, err := h.Runtime.Create(r.Context(), &plan)
	if err != nil {
		h.Logger.Error("sandbox: failed to create container", "error", err)
		http.Error(w, `{"error":"container creation failed"}`, http.StatusInternalServerError)
		return
	}

	if err := h.Runtime.Start(r.Context(), containerID); err != nil {
		h.Logger.Error("sandbox: failed to start container", "error", err, "container_id", containerID)
		_ = h.Runtime.Destroy(r.Context(), containerID)
		http.Error(w, `{"error":"container start failed"}`, http.StatusInternalServerError)
		return
	}

	result, err := h.Runtime.Wait(r.Context(), containerID, plan.Timeout)

	// Always destroy — even on error.
	destroyErr := h.Runtime.Destroy(r.Context(), containerID)
	if destroyErr != nil {
		h.Logger.Warn("sandbox: container cleanup failed", "error", destroyErr, "container_id", containerID)
	}

	// Zero out env vars from the plan after container is destroyed.
	// Secrets existed only as container env vars — wipe the Go-side copy.
	for k := range plan.EnvVars {
		plan.EnvVars[k] = ""
	}
	plan.EnvVars = nil

	if err != nil {
		h.Logger.Error("sandbox: build execution failed", "error", err)
		http.Error(w, `{"error":"build execution failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ── GET /internal/swarm/sandbox/status/{id} ──────────────────────────────────

// HandleStatus returns the status of a build.
// Placeholder — will query swarm_builds table in Phase 6.
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":     buildID,
		"status": "not_implemented",
	})
}

// ── GET /internal/swarm/sandbox/logs/{id} ────────────────────────────────────

// HandleLogs returns build logs.
// Placeholder — will query swarm_builds table in Phase 6.
func (h *Handler) HandleLogs(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":   buildID,
		"logs": "not_implemented",
	})
}
