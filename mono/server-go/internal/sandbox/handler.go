package sandbox

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/vault/keychain"
)

// ── Handler ─────────────────────────────────────────────────────────────────

// SandboxLimits holds config-driven limits applied to every build.
type SandboxLimits struct {
	MaxTimeoutSec int
	MaxMemoryMB   int
	MaxCPU        int
	MaxRetries    int
	DefaultSandbox bool
}

// Handler serves sandbox-related HTTP endpoints.
type Handler struct {
	Runtime         ContainerRuntime
	KeychainService *keychain.Service
	Store           *BuildStore
	Logger          *slog.Logger
	Limits          *SandboxLimits
}

// NewHandler creates a sandbox HTTP handler.
func NewHandler(runtime ContainerRuntime, keychainSvc *keychain.Service, buildStore *BuildStore, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{Runtime: runtime, KeychainService: keychainSvc, Store: buildStore, Logger: logger}
}

// applyLimits clamps request values to server-configured maximums.
func (h *Handler) applyLimits(timeoutSec *int, memoryLimit *string, cpuLimit *string, sandboxMode *bool) bool {
	if h.Limits == nil {
		return false
	}
	applied := false

	if h.Limits.MaxTimeoutSec > 0 && *timeoutSec > h.Limits.MaxTimeoutSec {
		*timeoutSec = h.Limits.MaxTimeoutSec
		applied = true
	}

	if h.Limits.MaxMemoryMB > 0 {
		maxMem := memoryLimitString(h.Limits.MaxMemoryMB)
		if parseMemoryMB(*memoryLimit) > h.Limits.MaxMemoryMB {
			*memoryLimit = maxMem
			applied = true
		}
	}

	if h.Limits.MaxCPU > 0 {
		if parseCPU(*cpuLimit) > h.Limits.MaxCPU {
			*cpuLimit = intToStr(h.Limits.MaxCPU)
			applied = true
		}
	}

	if h.Limits.DefaultSandbox && !*sandboxMode {
		*sandboxMode = true
		applied = true
	}

	if applied {
		BuildConfigLimitsApplied.Inc()
	}
	return applied
}

// ApplyLimits is the exported version of applyLimits for testing.
func (h *Handler) ApplyLimits(timeoutSec int, memoryLimit, cpuLimit string, sandboxMode bool) (int, string, string, bool) {
	h.applyLimits(&timeoutSec, &memoryLimit, &cpuLimit, &sandboxMode)
	return timeoutSec, memoryLimit, cpuLimit, sandboxMode
}

func parseMemoryMB(s string) int {
	if s == "" {
		return 0
	}
	s = strings.TrimSpace(s)
	multiplier := 1
	if strings.HasSuffix(s, "g") || strings.HasSuffix(s, "G") {
		multiplier = 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "m") || strings.HasSuffix(s, "M") {
		s = s[:len(s)-1]
	}
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n * multiplier
}

func parseCPU(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func memoryLimitString(mb int) string {
	if mb >= 1024 && mb%1024 == 0 {
		return intToStr(mb/1024) + "g"
	}
	return intToStr(mb) + "m"
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
	TaskID           string            `json:"task_id"`
	RepoID           string            `json:"repo_id"`
	UserID           string            `json:"user_id"`
	BuildSystem      string            `json:"build_system"`
	Command          string            `json:"command"`
	PreCommands      []string          `json:"pre_commands"`
	SecretRefs       []string          `json:"secret_refs"`
	BaseImage        string            `json:"base_image"`
	SandboxMode      bool              `json:"sandbox_mode"`
	TimeoutSec       int               `json:"timeout_sec"`
	MemoryLimit      string            `json:"memory_limit"`
	CPULimit         string            `json:"cpu_limit"`
	ChangedFiles     []string          `json:"changed_files"`
	SkipCache        bool              `json:"skip_cache"`
	WorkspaceFiles   map[string]string `json:"workspace_files"`
	ArtifactPaths    []string          `json:"artifact_paths"`
	CollectArtifacts bool              `json:"collect_artifacts"`
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

	// Smart skip: if all changed files are non-code, skip the build entirely.
	if len(req.ChangedFiles) > 0 && CanSkipBuild(req.ChangedFiles) {
		BuildSkipped.Inc()
		BuildTotal.WithLabelValues("skipped").Inc()
		h.Logger.Info("sandbox: build skipped — non-code changes only",
			"task_id", taskID, "changed_files", len(req.ChangedFiles))

		buildID := uuid.New()
		if h.Store != nil {
			rec := &BuildRecord{
				ID:          buildID,
				TaskID:      taskID,
				RepoID:      req.RepoID,
				UserID:      &userID,
				BuildSystem: req.BuildSystem,
				Command:     req.Command,
				BaseImage:   req.BaseImage,
				Status:      "skipped",
				SecretNames: req.SecretRefs,
				SandboxMode: req.SandboxMode,
			}
			_ = h.Store.InsertBuild(r.Context(), rec)
			_ = h.Store.CompleteBuild(r.Context(), buildID, "skipped", 0,
				SkipReason(req.ChangedFiles), 0)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"build_id":  buildID,
			"exit_code": 0,
			"logs":      SkipReason(req.ChangedFiles),
			"duration":  "0s",
			"skipped":   true,
			"reason":    SkipReason(req.ChangedFiles),
		})
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
		WorkspaceFS: req.WorkspaceFiles,
	}

	// Prepare workspace: validate size, extract to temp dir.
	if len(plan.WorkspaceFS) > 0 {
		if WorkspaceSize(plan.WorkspaceFS) > MaxWorkspaceBytes {
			http.Error(w, `{"error":"workspace changeset exceeds size limit"}`, http.StatusBadRequest)
			return
		}
		if err := PrepareWorkspace(plan); err != nil {
			h.Logger.Error("sandbox: workspace preparation failed", "error", err)
			http.Error(w, `{"error":"workspace preparation failed"}`, http.StatusInternalServerError)
			return
		}
		defer CleanupWorkspace(plan)
		WorkspaceInjections.Inc()
		h.Logger.Info("sandbox: workspace injected",
			"files", len(plan.WorkspaceFS), "task_id", taskID)
	}

	// Artifact collection configuration.
	if req.CollectArtifacts {
		plan.ArtifactCfg = &ArtifactCollectorConfig{Paths: req.ArtifactPaths}
	}

	// Resolve dependency cache.
	if !req.SkipCache {
		plan.Cache = ResolveCacheConfig(req.RepoID, req.BuildSystem)
		if plan.Cache != nil {
			BuildCacheHits.Inc()
			h.Logger.Info("sandbox: cache enabled",
				"volume", plan.Cache.VolumeName, "path", plan.Cache.ContainerPath)
		}
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

	// Apply config-driven limits from rtserverprops.xml <sandbox> element.
	timeoutSec := int(plan.Timeout.Seconds())
	h.applyLimits(&timeoutSec, &plan.MemoryLimit, &plan.CPULimit, &plan.SandboxMode)
	plan.Timeout = time.Duration(timeoutSec) * time.Second

	// Build fingerprinting: compare build-config file hashes to detect
	// whether dependency installation can be skipped (fast path).
	var fingerprint *BuildFingerprint
	fastPath := false
	if len(plan.WorkspaceFS) > 0 {
		fingerprint = ComputeBuildFingerprint(req.BuildSystem, plan.WorkspaceFS)
		if fingerprint.Hash != "" && h.Store != nil {
			prev, err := h.Store.GetLatestFingerprint(r.Context(), req.RepoID, req.BuildSystem)
			if err == nil && prev != nil {
				AnnotateFastPath(fingerprint, prev)
				fastPath = fingerprint.FastPath
			}
		}
		if fastPath {
			plan.Command = FastPathCommand(req.BuildSystem, plan.Command, true)
			BuildFastPathTotal.Inc()
			BuildFingerprintHits.Inc()
			h.Logger.Info("sandbox: fast path — deps unchanged, skipping install",
				"build_system", req.BuildSystem, "fingerprint", fingerprint.Hash[:12])
		}
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

	// Collect build artifacts from logs.
	var artifactSummaries []map[string]any
	if plan.ArtifactCfg != nil || req.CollectArtifacts {
		logArtifacts := ParseArtifactsFromLogs(result.Logs, buildID)
		for _, a := range logArtifacts {
			if h.Store != nil {
				_ = h.Store.InsertArtifact(r.Context(), a)
			}
			ArtifactsCollected.Inc()
			ArtifactBytes.Add(float64(a.SizeBytes))
			artifactSummaries = append(artifactSummaries, map[string]any{
				"id":         a.ID,
				"kind":       a.Kind,
				"path":       a.Path,
				"size_bytes": a.SizeBytes,
			})
		}
		h.Logger.Info("sandbox: artifacts collected",
			"count", len(logArtifacts), "build_id", buildID)
	}

	// Compute build complexity scoring.
	result.Duration = duration
	complexity := ComputeBuildComplexity(plan, result)
	BuildComplexityDistribution.WithLabelValues(string(complexity.Label)).Inc()
	BuildComplexityScore.Observe(complexity.Score)
	BuildFailureProbability.Observe(complexity.FailureProbablity)

	if h.Store != nil {
		if err := h.Store.UpdateBuildComplexity(r.Context(), buildID, complexity); err != nil {
			h.Logger.Warn("sandbox: failed to persist complexity", "error", err)
		}
	}

	// Persist build fingerprint for future fast-path detection.
	if fingerprint != nil && fingerprint.Hash != "" && h.Store != nil {
		if err := h.Store.UpdateBuildFingerprint(r.Context(), buildID, fingerprint); err != nil {
			h.Logger.Warn("sandbox: failed to persist fingerprint", "error", err)
		}
	}

	h.Logger.Info("sandbox: "+BuildComplexitySummary(complexity),
		"build_id", buildID, "fast_path", fastPath)

	// Enrich response with resolution metadata.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"build_id":           buildID,
		"exit_code":          result.ExitCode,
		"logs":               result.Logs,
		"duration":           result.Duration.String(),
		"secret_refs":        result.SecretRefs,
		"resolved_secrets":   resolved,
		"failed_secrets":     failed,
		"artifacts":          artifactSummaries,
		"workspace_injected": len(plan.WorkspaceFS) > 0,
		"complexity":         complexity,
		"fingerprint":        fingerprint,
		"fast_path":          fastPath,
		"image_tag":          ImageTag(req.RepoID, fingerprint),
	})
}

// ── POST /internal/swarm/sandbox/retry ───────────────────────────────────────

// HandleRetry re-executes a failed or errored build, up to MaxRetries times.
// It reads the original build record, increments retry_count, and re-runs
// the container with the same plan parameters.
func (h *Handler) HandleRetry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BuildID string `json:"build_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	buildID, err := uuid.Parse(req.BuildID)
	if err != nil {
		http.Error(w, `{"error":"invalid build_id"}`, http.StatusBadRequest)
		return
	}

	if h.Store == nil {
		http.Error(w, `{"error":"build store not configured"}`, http.StatusServiceUnavailable)
		return
	}

	// Fetch the original build record.
	rec, err := h.Store.GetBuild(r.Context(), buildID)
	if err != nil {
		h.Logger.Warn("sandbox: build not found for retry", "id", buildID, "error", err)
		http.Error(w, `{"error":"build not found"}`, http.StatusNotFound)
		return
	}

	// Only failed or errored builds can be retried.
	if rec.Status != "failed" && rec.Status != "error" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"error":       "build is not retryable",
			"status":      rec.Status,
			"retry_count": rec.RetryCount,
		})
		return
	}

	if rec.RetryCount >= MaxRetries {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"error":       "max retries exceeded",
			"max_retries": MaxRetries,
			"retry_count": rec.RetryCount,
		})
		return
	}

	// Increment retry count.
	if err := h.Store.IncrementRetry(r.Context(), buildID); err != nil {
		h.Logger.Warn("sandbox: failed to increment retry", "id", buildID, "error", err)
	}
	BuildRetries.Inc()

	// Rebuild the plan from the stored record.
	var userID *uuid.UUID
	if rec.UserID != nil {
		userID = rec.UserID
	}

	plan := &BuildPlan{
		ID:          uuid.New(),
		TaskID:      rec.TaskID,
		RepoID:      rec.RepoID,
		BuildSystem: rec.BuildSystem,
		Command:     rec.Command,
		BaseImage:   rec.BaseImage,
		SandboxMode: rec.SandboxMode,
		SecretRefs:  rec.SecretNames,
		Timeout:     DefaultTimeout,
		MemoryLimit: DefaultMemoryLimit,
		CPULimit:    DefaultCPULimit,
		EnvVars:     make(map[string]string),
		Cache:       ResolveCacheConfig(rec.RepoID, rec.BuildSystem),
	}

	// Resolve secrets again.
	var resolved, failed []string
	if h.KeychainService != nil && userID != nil && len(rec.SecretNames) > 0 {
		repoUUID, rerr := uuid.Parse(rec.RepoID)
		if rerr == nil {
			for _, name := range rec.SecretNames {
				val, kerr := h.KeychainService.GetBuildSecret(r.Context(), *userID, repoUUID, name)
				if kerr != nil {
					failed = append(failed, name)
					continue
				}
				plan.EnvVars[name] = string(val)
				resolved = append(resolved, name)
			}
			BuildSecretInjects.Add(float64(len(resolved)))
		}
	}

	// Persist a new build record for the retry.
	retryBuildID := plan.ID
	retryRec := &BuildRecord{
		ID:          retryBuildID,
		TaskID:      rec.TaskID,
		RepoID:      rec.RepoID,
		UserID:      userID,
		BuildSystem: rec.BuildSystem,
		Command:     rec.Command,
		BaseImage:   rec.BaseImage,
		Status:      "running",
		SecretNames: rec.SecretNames,
		SandboxMode: rec.SandboxMode,
		RetryCount:  rec.RetryCount + 1,
	}
	if err := h.Store.InsertBuild(r.Context(), retryRec); err != nil {
		h.Logger.Warn("sandbox: failed to persist retry build record", "error", err)
	}

	h.Logger.Info("sandbox: retrying build",
		"original_id", buildID, "retry_id", retryBuildID,
		"retry_count", rec.RetryCount+1, "max", MaxRetries)

	// Execute the container.
	BuildContainersActive.Inc()
	defer BuildContainersActive.Dec()

	containerID, cerr := h.Runtime.Create(r.Context(), plan)
	if cerr != nil {
		h.Logger.Error("sandbox: retry container create failed", "error", cerr)
		if h.Store != nil {
			_ = h.Store.CompleteBuild(r.Context(), retryBuildID, "error", -1, cerr.Error(), 0)
		}
		http.Error(w, `{"error":"container creation failed"}`, http.StatusInternalServerError)
		return
	}

	if serr := h.Runtime.Start(r.Context(), containerID); serr != nil {
		h.Logger.Error("sandbox: retry container start failed", "error", serr)
		_ = h.Runtime.Destroy(r.Context(), containerID)
		if h.Store != nil {
			_ = h.Store.CompleteBuild(r.Context(), retryBuildID, "error", -1, serr.Error(), 0)
		}
		http.Error(w, `{"error":"container start failed"}`, http.StatusInternalServerError)
		return
	}

	start := time.Now()
	result, waitErr := h.Runtime.Wait(r.Context(), containerID, plan.Timeout)

	destroyErr := h.Runtime.Destroy(r.Context(), containerID)
	if destroyErr != nil {
		h.Logger.Warn("sandbox: retry container cleanup failed", "error", destroyErr)
	}

	// Zero secrets.
	for k := range plan.EnvVars {
		plan.EnvVars[k] = ""
	}
	plan.EnvVars = nil

	if waitErr != nil && result == nil {
		if h.Store != nil {
			_ = h.Store.CompleteBuild(r.Context(), retryBuildID, "error", -1, waitErr.Error(), int(time.Since(start).Milliseconds()))
		}
		http.Error(w, `{"error":"build execution failed"}`, http.StatusInternalServerError)
		return
	}

	duration := time.Since(start)
	BuildDuration.Observe(duration.Seconds())

	status := "success"
	if result.ExitCode != 0 {
		status = "failed"
	}
	BuildTotal.WithLabelValues(status).Inc()

	if h.Store != nil {
		logSummary := result.Logs
		if len(logSummary) > 8192 {
			logSummary = logSummary[:8192] + "\n... [truncated for DB]"
		}
		_ = h.Store.CompleteBuild(r.Context(), retryBuildID, status, result.ExitCode, logSummary, int(duration.Milliseconds()))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"build_id":         retryBuildID,
		"original_build":   buildID,
		"retry_count":      rec.RetryCount + 1,
		"exit_code":        result.ExitCode,
		"logs":             result.Logs,
		"duration":         result.Duration.String(),
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

// ── GET /internal/swarm/sandbox/artifacts/{id} ───────────────────────────────

// HandleListArtifacts returns the list of build artifacts for a build ID.
func (h *Handler) HandleListArtifacts(w http.ResponseWriter, r *http.Request) {
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

	artifacts, err := h.Store.ListArtifacts(r.Context(), id)
	if err != nil {
		h.Logger.Warn("sandbox: failed to list artifacts", "build_id", buildID, "error", err)
		http.Error(w, `{"error":"failed to list artifacts"}`, http.StatusInternalServerError)
		return
	}

	items := make([]map[string]any, 0, len(artifacts))
	for _, a := range artifacts {
		items = append(items, map[string]any{
			"id":         a.ID,
			"build_id":   a.BuildID,
			"kind":       a.Kind,
			"path":       a.Path,
			"size_bytes": a.SizeBytes,
			"created_at": a.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"build_id":  id,
		"artifacts": items,
		"count":     len(items),
	})
}

// ── GET /internal/swarm/sandbox/complexity/{repo_id} ─────────────────────────

// HandleBuildComplexity returns historical build complexity stats for a repo.
func (h *Handler) HandleBuildComplexity(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "repo_id")

	if h.Store == nil {
		http.Error(w, `{"error":"build store not configured"}`, http.StatusServiceUnavailable)
		return
	}

	if repoID == "" {
		http.Error(w, `{"error":"repo_id is required"}`, http.StatusBadRequest)
		return
	}

	records, err := h.Store.ListBuildsByRepo(r.Context(), repoID, 50)
	if err != nil {
		h.Logger.Warn("sandbox: failed to list builds for complexity",
			"repo_id", repoID, "error", err)
		http.Error(w, `{"error":"failed to list builds"}`, http.StatusInternalServerError)
		return
	}

	stats := ComputeHistoricalStats(records)
	baseHints := ResourceHints{
		TimeoutSec:  int(DefaultTimeout.Seconds()),
		MemoryLimit: DefaultMemoryLimit,
		CPULimit:    DefaultCPULimit,
	}
	refined := RefineResourceHints(baseHints, stats)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"repo_id":          repoID,
		"historical_stats": stats,
		"resource_hints":   refined,
		"build_count":      len(records),
	})
}

// ── GET /internal/swarm/sandbox/health ───────────────────────────────────────

// HandleHealth returns the sandbox runtime health status and current config.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	runtimeHealthy := false
	runtimeErr := ""
	if h.Runtime != nil {
		if err := h.Runtime.HealthCheck(r.Context()); err != nil {
			runtimeErr = err.Error()
		} else {
			runtimeHealthy = true
		}
	}

	storeHealthy := h.Store != nil

	resp := map[string]any{
		"runtime_healthy": runtimeHealthy,
		"store_healthy":   storeHealthy,
		"defaults": map[string]any{
			"timeout_sec":  int(DefaultTimeout.Seconds()),
			"memory_limit": DefaultMemoryLimit,
			"cpu_limit":    DefaultCPULimit,
			"max_retries":  MaxRetries,
		},
	}

	if runtimeErr != "" {
		resp["runtime_error"] = runtimeErr
	}

	if h.Limits != nil {
		resp["config_limits"] = map[string]any{
			"max_timeout_sec": h.Limits.MaxTimeoutSec,
			"max_memory_mb":   h.Limits.MaxMemoryMB,
			"max_cpu":         h.Limits.MaxCPU,
			"max_retries":     h.Limits.MaxRetries,
			"default_sandbox": h.Limits.DefaultSandbox,
		}
	}

	status := http.StatusOK
	if !runtimeHealthy {
		status = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}
