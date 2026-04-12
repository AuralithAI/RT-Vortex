package sandbox

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/vault/keychain"
)

// ── Handler ─────────────────────────────────────────────────────────────────

// Handler serves sandbox-related HTTP endpoints.
type Handler struct {
	Runtime         ContainerRuntime
	KeychainService *keychain.Service
	Store           *BuildStore
	Logger          *slog.Logger
}

// NewHandler creates a sandbox HTTP handler.
func NewHandler(runtime ContainerRuntime, keychainSvc *keychain.Service, buildStore *BuildStore, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{Runtime: runtime, KeychainService: keychainSvc, Store: buildStore, Logger: logger}
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

// HandleStatus returns the status of a build from the swarm_builds table.
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")

	if h.Store == nil {
		http.Error(w, `{"error":"build store not configured"}`, http.StatusServiceUnavailable)
		return
	}

	id, err := uuid.Parse(buildID)
	if err != nil {
		http.Error(w, `{"error":"invalid build id"}`, http.StatusBadRequest)
		return
	}

	rec, err := h.Store.GetBuild(r.Context(), id)
	if err != nil {
		h.Logger.Warn("sandbox: build not found", "id", buildID, "error", err)
		http.Error(w, `{"error":"build not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":           rec.ID,
		"task_id":      rec.TaskID,
		"repo_id":      rec.RepoID,
		"build_system": rec.BuildSystem,
		"status":       rec.Status,
		"exit_code":    rec.ExitCode,
		"retry_count":  rec.RetryCount,
		"duration_ms":  rec.DurationMS,
		"sandbox_mode": rec.SandboxMode,
		"created_at":   rec.CreatedAt,
		"completed_at": rec.CompletedAt,
	})
}

// ── POST /internal/swarm/sandbox/resolve-execute ─────────────────────────────

// resolveExecuteRequest is the payload for HandleResolveAndExecute.
type resolveExecuteRequest struct {
	TaskID      string   `json:"task_id"`
	RepoID      string   `json:"repo_id"`
	UserID      string   `json:"user_id"`
	BuildSystem string   `json:"build_system"`
	Command     string   `json:"command"`
	PreCommands []string `json:"pre_commands"`
	SecretRefs  []string `json:"secret_refs"`
	BaseImage   string   `json:"base_image"`
	SandboxMode bool     `json:"sandbox_mode"`
	TimeoutSec  int      `json:"timeout_sec"`
	MemoryLimit string   `json:"memory_limit"`
	CPULimit    string   `json:"cpu_limit"`
}

// HandleResolveAndExecute resolves secret values from the keychain, populates
// the build plan, executes the container, and zeroes secret memory.
// This is the Phase 4 endpoint that agents call after HITL confirmation.
func (h *Handler) HandleResolveAndExecute(w http.ResponseWriter, r *http.Request) {
	var req resolveExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	taskID, err := uuid.Parse(req.TaskID)
	if err != nil {
		http.Error(w, `{"error":"invalid task_id"}`, http.StatusBadRequest)
		return
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		http.Error(w, `{"error":"invalid user_id"}`, http.StatusBadRequest)
		return
	}
	repoID, err := uuid.Parse(req.RepoID)
	if err != nil {
		http.Error(w, `{"error":"invalid repo_id"}`, http.StatusBadRequest)
		return
	}

	if req.BaseImage == "" || req.Command == "" {
		http.Error(w, `{"error":"base_image and command are required"}`, http.StatusBadRequest)
		return
	}

	// Build the plan.
	plan := &BuildPlan{
		ID:          uuid.New(),
		TaskID:      taskID,
		RepoID:      req.RepoID,
		BuildSystem: req.BuildSystem,
		Command:     req.Command,
		PreCommands: req.PreCommands,
		SecretRefs:  req.SecretRefs,
		BaseImage:   req.BaseImage,
		SandboxMode: req.SandboxMode,
		MemoryLimit: req.MemoryLimit,
		CPULimit:    req.CPULimit,
		EnvVars:     make(map[string]string),
	}
	if req.TimeoutSec > 0 {
		plan.Timeout = time.Duration(req.TimeoutSec) * time.Second
	} else {
		plan.Timeout = DefaultTimeout
	}
	if plan.MemoryLimit == "" {
		plan.MemoryLimit = DefaultMemoryLimit
	}
	if plan.CPULimit == "" {
		plan.CPULimit = DefaultCPULimit
	}

	// Resolve secrets from the keychain.
	var resolved, failed []string
	if h.KeychainService != nil && len(req.SecretRefs) > 0 {
		for _, name := range req.SecretRefs {
			val, kerr := h.KeychainService.GetBuildSecret(r.Context(), userID, repoID, name)
			if kerr != nil {
				h.Logger.Warn("sandbox: could not resolve secret",
					"name", name, "user_id", userID, "repo_id", repoID, "error", kerr)
				failed = append(failed, name)
				continue
			}
			plan.EnvVars[name] = string(val)
			resolved = append(resolved, name)
		}
		BuildSecretInjects.Add(float64(len(resolved)))
	}

	h.Logger.Info("sandbox: secrets resolved",
		"resolved", len(resolved), "failed", len(failed),
		"task_id", taskID, "repo_id", repoID)

	// Persist build record before execution.
	buildID := plan.ID
	if h.Store != nil {
		rec := &BuildRecord{
			ID:          buildID,
			TaskID:      taskID,
			RepoID:      req.RepoID,
			UserID:      &userID,
			BuildSystem: req.BuildSystem,
			Command:     req.Command,
			BaseImage:   req.BaseImage,
			Status:      "running",
			SecretNames: req.SecretRefs,
			SandboxMode: req.SandboxMode,
		}
		if err := h.Store.InsertBuild(r.Context(), rec); err != nil {
			h.Logger.Warn("sandbox: failed to persist build record", "error", err)
			// Non-fatal — proceed with execution.
		}
	}

	// Execute the container.
	BuildContainersActive.Inc()
	defer BuildContainersActive.Dec()

	containerID, err := h.Runtime.Create(r.Context(), plan)
	if err != nil {
		h.Logger.Error("sandbox: failed to create container", "error", err)
		if h.Store != nil {
			_ = h.Store.CompleteBuild(r.Context(), buildID, "error", -1, err.Error(), 0)
		}
		http.Error(w, `{"error":"container creation failed"}`, http.StatusInternalServerError)
		return
	}

	if err := h.Runtime.Start(r.Context(), containerID); err != nil {
		h.Logger.Error("sandbox: failed to start container", "error", err, "container_id", containerID)
		_ = h.Runtime.Destroy(r.Context(), containerID)
		if h.Store != nil {
			_ = h.Store.CompleteBuild(r.Context(), buildID, "error", -1, err.Error(), 0)
		}
		http.Error(w, `{"error":"container start failed"}`, http.StatusInternalServerError)
		return
	}

	start := time.Now()
	result, waitErr := h.Runtime.Wait(r.Context(), containerID, plan.Timeout)

	// Always destroy.
	destroyErr := h.Runtime.Destroy(r.Context(), containerID)
	if destroyErr != nil {
		h.Logger.Warn("sandbox: container cleanup failed", "error", destroyErr, "container_id", containerID)
	}

	// Zero secret values from memory.
	for k := range plan.EnvVars {
		plan.EnvVars[k] = ""
	}
	plan.EnvVars = nil

	if waitErr != nil {
		h.Logger.Error("sandbox: build execution failed", "error", waitErr)
		if result == nil {
			if h.Store != nil {
				_ = h.Store.CompleteBuild(r.Context(), buildID, "error", -1, waitErr.Error(), int(time.Since(start).Milliseconds()))
			}
			http.Error(w, `{"error":"build execution failed"}`, http.StatusInternalServerError)
			return
		}
	}

	duration := time.Since(start)
	BuildDuration.Observe(duration.Seconds())

	status := "success"
	if result.ExitCode != 0 {
		status = "failed"
	}
	BuildTotal.WithLabelValues(status).Inc()

	// Persist build completion.
	if h.Store != nil {
		logSummary := result.Logs
		if len(logSummary) > 8192 {
			logSummary = logSummary[:8192] + "\n... [truncated for DB]"
		}
		if err := h.Store.CompleteBuild(r.Context(), buildID, status, result.ExitCode, logSummary, int(duration.Milliseconds())); err != nil {
			h.Logger.Warn("sandbox: failed to persist build completion", "error", err)
		}
	}

	// Enrich response with resolution metadata.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"build_id":         buildID,
		"exit_code":        result.ExitCode,
		"logs":             result.Logs,
		"duration":         result.Duration.String(),
		"secret_refs":      result.SecretRefs,
		"resolved_secrets": resolved,
		"failed_secrets":   failed,
	})
}

// ── GET /internal/swarm/sandbox/logs/{id} ────────────────────────────────────

// HandleLogs returns build logs from the swarm_builds table.
func (h *Handler) HandleLogs(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")

	if h.Store == nil {
		http.Error(w, `{"error":"build store not configured"}`, http.StatusServiceUnavailable)
		return
	}

	id, err := uuid.Parse(buildID)
	if err != nil {
		http.Error(w, `{"error":"invalid build id"}`, http.StatusBadRequest)
		return
	}

	rec, err := h.Store.GetBuild(r.Context(), id)
	if err != nil {
		h.Logger.Warn("sandbox: build not found for logs", "id", buildID, "error", err)
		http.Error(w, `{"error":"build not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":         rec.ID,
		"task_id":    rec.TaskID,
		"status":     rec.Status,
		"exit_code":  rec.ExitCode,
		"logs":       rec.LogSummary,
		"created_at": rec.CreatedAt,
	})
}

// ── GET /internal/swarm/sandbox/secrets ──────────────────────────────────────

// HandleListBuildSecrets returns the secret names (never values) for a repo+user.
func (h *Handler) HandleListBuildSecrets(w http.ResponseWriter, r *http.Request) {
	if h.KeychainService == nil {
		http.Error(w, `{"error":"keychain service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	repoIDStr := r.URL.Query().Get("repo_id")
	userIDStr := r.URL.Query().Get("user_id")
	if repoIDStr == "" || userIDStr == "" {
		http.Error(w, `{"error":"repo_id and user_id query params required"}`, http.StatusBadRequest)
		return
	}

	repoID, err := uuid.Parse(repoIDStr)
	if err != nil {
		http.Error(w, `{"error":"invalid repo_id"}`, http.StatusBadRequest)
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, `{"error":"invalid user_id"}`, http.StatusBadRequest)
		return
	}

	versions, err := h.KeychainService.ListBuildSecretNames(r.Context(), userID, repoID)
	if err != nil {
		h.Logger.Error("sandbox: failed to list build secrets", "error", err, "repo_id", repoID, "user_id", userID)
		http.Error(w, `{"error":"failed to list secrets"}`, http.StatusInternalServerError)
		return
	}

	names := make([]string, 0, len(versions))
	for _, v := range versions {
		names = append(names, v.Name)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"repo_id": repoID,
		"user_id": userID,
		"secrets": names,
	})
}

// ── POST /internal/swarm/sandbox/probe ───────────────────────────────────────

// HandleProbeEnv runs the pre-build environment probe.
func (h *Handler) HandleProbeEnv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID       string            `json:"task_id"`
		RepoID       string            `json:"repo_id"`
		UserID       string            `json:"user_id"`
		RepoFiles    []string          `json:"repo_files"`
		ChangedFiles []string          `json:"changed_files"`
		FileContents map[string]string `json:"file_contents"`
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

	// Fetch secret names for this repo+user if keychain is available.
	var secretNames []string
	if h.KeychainService != nil && req.UserID != "" && req.RepoID != "" {
		userID, uerr := uuid.Parse(req.UserID)
		repoID, rerr := uuid.Parse(req.RepoID)
		if uerr == nil && rerr == nil {
			versions, kerr := h.KeychainService.ListBuildSecretNames(r.Context(), userID, repoID)
			if kerr == nil {
				for _, v := range versions {
					secretNames = append(secretNames, v.Name)
				}
			} else {
				h.Logger.Warn("sandbox: probe could not fetch secrets", "error", kerr)
			}
		}
	}

	ProbeTotal.Inc()

	result := RunProbe(r.Context(), ProbeOptions{
		TaskID:       taskID,
		RepoID:       req.RepoID,
		RepoFiles:    req.RepoFiles,
		ChangedFiles: req.ChangedFiles,
		SecretNames:  secretNames,
		FileContents: req.FileContents,
	})

	if len(result.MissingSecrets) > 0 {
		ProbeMissingSecrets.Add(float64(len(result.MissingSecrets)))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
