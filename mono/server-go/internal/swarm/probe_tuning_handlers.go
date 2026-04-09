package swarm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ── Probe Tuning HTTP Handlers ──────────────────────────────────────────────
//
// GET  /internal/swarm/probe-config              — fetch best probe params
// POST /internal/swarm/probe-history             — record probe outcome
// GET  /api/v1/swarm/probe-configs               — list all configs (user)
// GET  /api/v1/swarm/probe-configs/{role}        — config detail for role
// PUT  /api/v1/swarm/probe-configs/{role}        — manually update a config
// GET  /api/v1/swarm/probe-stats/{role}          — provider stats for role
// GET  /api/v1/swarm/probe-history               — recent probe history

// HandleGetProbeConfig handles GET /internal/swarm/probe-config.
// Python agents call this before each multi-LLM probe to get adaptive parameters.
func (h *Handler) HandleGetProbeConfig(w http.ResponseWriter, r *http.Request) {
	if h.ProbeTuningSvc == nil {
		http.Error(w, `{"error":"probe tuning service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	role := r.URL.Query().Get("role")
	repoID := r.URL.Query().Get("repo_id")
	actionType := r.URL.Query().Get("action_type")
	complexityLabel := r.URL.Query().Get("complexity_label")
	tier := r.URL.Query().Get("tier")

	if role == "" {
		http.Error(w, `{"error":"role is required"}`, http.StatusBadRequest)
		return
	}

	cfg, err := h.ProbeTuningSvc.GetConfig(r.Context(), role, repoID, actionType)
	if err != nil {
		slog.Error("probe-config fetch failed", "role", role, "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Apply per-probe complexity/tier enhancements (not persisted).
	if complexityLabel != "" || tier != "" {
		cfg = h.ProbeTuningSvc.EnhanceForComplexity(cfg, complexityLabel, tier)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

// HandleRecordProbeHistory handles POST /internal/swarm/probe-history.
// Python agents call this after each probe to record the outcome.
func (h *Handler) HandleRecordProbeHistory(w http.ResponseWriter, r *http.Request) {
	if h.ProbeTuningSvc == nil {
		http.Error(w, `{"error":"probe tuning service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req ProbeHistoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.TaskID == "" || req.Role == "" {
		http.Error(w, `{"error":"task_id and role are required"}`, http.StatusBadRequest)
		return
	}

	taskID, err := uuid.Parse(req.TaskID)
	if err != nil {
		http.Error(w, `{"error":"invalid task_id"}`, http.StatusBadRequest)
		return
	}

	history := &ProbeHistory{
		ID:                  uuid.New(),
		TaskID:              taskID,
		Role:                req.Role,
		RepoID:              req.RepoID,
		ActionType:          req.ActionType,
		ProvidersQueried:    req.ProvidersQueried,
		ProvidersSucceeded:  req.ProvidersSucceeded,
		ProviderWinner:      req.ProviderWinner,
		StrategyUsed:        req.StrategyUsed,
		ConsensusConfidence: req.ConsensusConfidence,
		ProviderLatencies:   req.ProviderLatencies,
		ProviderTokens:      req.ProviderTokens,
		TotalMs:             req.TotalMs,
		TotalTokens:         req.TotalTokens,
		EstimatedCostUSD:    req.EstimatedCostUSD,
		Success:             req.Success,
		ErrorDetail:         req.ErrorDetail,
		ComplexityLabel:     req.ComplexityLabel,
		NumModelsUsed:       req.NumModelsUsed,
		TemperatureUsed:     req.TemperatureUsed,
		CreatedAt:           time.Now().UTC(),
	}

	if err := h.ProbeTuningSvc.RecordProbeOutcome(r.Context(), history); err != nil {
		slog.Error("probe-history record failed", "task_id", req.TaskID, "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Broadcast to WebSocket.
	if h.WS != nil {
		h.WS.BroadcastTaskEvent("probe_outcome", req.TaskID, map[string]interface{}{
			"role":                 req.Role,
			"providers_queried":    req.ProvidersQueried,
			"provider_winner":      req.ProviderWinner,
			"consensus_confidence": req.ConsensusConfidence,
			"total_ms":             req.TotalMs,
			"success":              req.Success,
			"complexity_label":     req.ComplexityLabel,
		})
	}

	slog.Info("probe outcome recorded",
		"task_id", req.TaskID,
		"role", req.Role,
		"winner", req.ProviderWinner,
		"confidence", req.ConsensusConfidence,
		"total_ms", req.TotalMs,
		"success", req.Success,
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "recorded"})
}

// HandleListProbeConfigs handles GET /api/v1/swarm/probe-configs.
// User-facing: lists all probe configs with optional role filter.
func (h *Handler) HandleListProbeConfigs(w http.ResponseWriter, r *http.Request) {
	if h.ProbeTuningSvc == nil {
		http.Error(w, `{"error":"probe tuning service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	role := r.URL.Query().Get("role")
	configs, err := h.ProbeTuningSvc.ListConfigs(r.Context(), role)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(configs)
}

// HandleGetProbeConfigByRole handles GET /api/v1/swarm/probe-configs/{role}.
// User-facing: get the config for a specific role.
func (h *Handler) HandleGetProbeConfigByRole(w http.ResponseWriter, r *http.Request) {
	if h.ProbeTuningSvc == nil {
		http.Error(w, `{"error":"probe tuning service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	role := chi.URLParam(r, "role")
	repoID := r.URL.Query().Get("repo_id")
	actionType := r.URL.Query().Get("action_type")

	cfg, err := h.ProbeTuningSvc.GetConfig(r.Context(), role, repoID, actionType)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

// HandleUpdateProbeConfig handles PUT /api/v1/swarm/probe-configs/{role}.
// User-facing: manually override a probe config.
func (h *Handler) HandleUpdateProbeConfig(w http.ResponseWriter, r *http.Request) {
	if h.ProbeTuningSvc == nil {
		http.Error(w, `{"error":"probe tuning service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	role := chi.URLParam(r, "role")

	var update struct {
		RepoID              string   `json:"repo_id"`
		ActionType          string   `json:"action_type"`
		NumModels           *int     `json:"num_models,omitempty"`
		PreferredProviders  []string `json:"preferred_providers,omitempty"`
		ExcludedProviders   []string `json:"excluded_providers,omitempty"`
		Temperature         *float64 `json:"temperature,omitempty"`
		MaxTokens           *int     `json:"max_tokens,omitempty"`
		TimeoutSeconds      *int     `json:"timeout_seconds,omitempty"`
		BudgetCapUSD        *float64 `json:"budget_cap_usd,omitempty"`
		Strategy            string   `json:"strategy,omitempty"`
		ConfidenceThreshold *float64 `json:"confidence_threshold,omitempty"`
		Retries             *int     `json:"retries,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Load existing or create new.
	cfg, err := h.ProbeTuningSvc.GetConfig(r.Context(), role, update.RepoID, update.ActionType)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Apply overrides.
	if update.NumModels != nil {
		cfg.NumModels = *update.NumModels
	}
	if update.PreferredProviders != nil {
		cfg.PreferredProviders = update.PreferredProviders
	}
	if update.ExcludedProviders != nil {
		cfg.ExcludedProviders = update.ExcludedProviders
	}
	if update.Temperature != nil {
		cfg.Temperature = *update.Temperature
	}
	if update.MaxTokens != nil {
		cfg.MaxTokens = *update.MaxTokens
	}
	if update.TimeoutSeconds != nil {
		cfg.TimeoutSeconds = *update.TimeoutSeconds
	}
	if update.BudgetCapUSD != nil {
		cfg.BudgetCapUSD = *update.BudgetCapUSD
	}
	if update.Strategy != "" {
		cfg.Strategy = update.Strategy
	}
	if update.ConfidenceThreshold != nil {
		cfg.ConfidenceThreshold = *update.ConfidenceThreshold
	}
	if update.Retries != nil {
		cfg.Retries = *update.Retries
	}
	cfg.Role = role
	cfg.RepoID = update.RepoID
	cfg.ActionType = update.ActionType
	cfg.Reasoning = "manual override"
	now := time.Now().UTC()
	cfg.LastTunedAt = &now
	cfg.UpdatedAt = now

	if err := h.ProbeTuningSvc.UpsertConfig(r.Context(), cfg); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	slog.Info("probe config updated manually", "role", role, "repo_id", update.RepoID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

// HandleGetProbeStats handles GET /api/v1/swarm/probe-stats/{role}.
// User-facing: aggregated provider stats for a role.
func (h *Handler) HandleGetProbeStats(w http.ResponseWriter, r *http.Request) {
	if h.ProbeTuningSvc == nil {
		http.Error(w, `{"error":"probe tuning service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	role := chi.URLParam(r, "role")
	repoID := r.URL.Query().Get("repo_id")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	stats, err := h.ProbeTuningSvc.ComputeProviderStats(r.Context(), role, repoID, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// HandleListProbeHistory handles GET /api/v1/swarm/probe-history.
// User-facing: recent probe outcomes.
func (h *Handler) HandleListProbeHistory(w http.ResponseWriter, r *http.Request) {
	if h.ProbeTuningSvc == nil {
		http.Error(w, `{"error":"probe tuning service not configured"}`, http.StatusServiceUnavailable)
		return
	}

	role := r.URL.Query().Get("role")
	repoID := r.URL.Query().Get("repo_id")
	limitStr := r.URL.Query().Get("limit")
	limit := 25
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}

	history, err := h.ProbeTuningSvc.GetRecentHistory(r.Context(), role, repoID, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(history)
}
