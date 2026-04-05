package swarm

// ── Adaptive Probe Tuning ──────────────────────────────────────────
//
// The probe tuning engine dynamically adjusts multi-LLM probe parameters
// based on historical performance data.  It manages:
//
//   1. Per-(role, repo, action_type) probe configurations
//      (num_models, preferred/excluded providers, temperature, timeout)
//   2. A rolling history of probe outcomes (latency, cost, winner, confidence)
//   3. A periodic tuning loop that analyses the last N outcomes and adjusts
//      configs to optimise for quality, latency, and cost
//
// The Python swarm calls GET /internal/swarm/probe-config before each probe
// to fetch the best parameters.  After the probe completes, it calls
// POST /internal/swarm/probe-history to record the outcome.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Constants ───────────────────────────────────────────────────────────────

const (
	ProbeStrategyAdaptive   = "adaptive"
	ProbeStrategyStatic     = "static"
	ProbeStrategyAggressive = "aggressive"

	// Tuning engine parameters.
	probeTuneInterval      = 10 * time.Minute // how often to run the tuning loop
	probeTuneHistoryWindow = 50               // analyse last N outcomes per config
	probeTuneMinSamples    = 5                // minimum probes before adjusting

	// Cost estimates per 1K tokens (rough averages, updated by history).
	defaultCostPer1KPrompt     = 0.003
	defaultCostPer1KCompletion = 0.015
)

// ── Types ───────────────────────────────────────────────────────────────────

// ProbeConfig is a row in swarm_probe_configs.
type ProbeConfig struct {
	ID                  uuid.UUID  `json:"id"`
	Role                string     `json:"role"`
	RepoID              string     `json:"repo_id"`
	ActionType          string     `json:"action_type"`
	NumModels           int        `json:"num_models"`
	PreferredProviders  []string   `json:"preferred_providers"`
	ExcludedProviders   []string   `json:"excluded_providers"`
	Temperature         float64    `json:"temperature"`
	MaxTokens           int        `json:"max_tokens"`
	TimeoutSeconds      int        `json:"timeout_seconds"`
	BudgetCapUSD        float64    `json:"budget_cap_usd"`
	TokensSpent         int64      `json:"tokens_spent"`
	Strategy            string     `json:"strategy"`
	ConfidenceThreshold float64    `json:"confidence_threshold"`
	Retries             int        `json:"retries"`
	Reasoning           string     `json:"reasoning"`
	LastTunedAt         *time.Time `json:"last_tuned_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// ProbeHistory is a row in swarm_probe_history.
type ProbeHistory struct {
	ID                  uuid.UUID             `json:"id"`
	TaskID              uuid.UUID             `json:"task_id"`
	Role                string                `json:"role"`
	RepoID              string                `json:"repo_id"`
	ActionType          string                `json:"action_type"`
	ProvidersQueried    []string              `json:"providers_queried"`
	ProvidersSucceeded  []string              `json:"providers_succeeded"`
	ProviderWinner      string                `json:"provider_winner"`
	StrategyUsed        string                `json:"strategy_used"`
	ConsensusConfidence float64               `json:"consensus_confidence"`
	ProviderLatencies   map[string]int64      `json:"provider_latencies"`
	ProviderTokens      map[string]TokenUsage `json:"provider_tokens"`
	TotalMs             int                   `json:"total_ms"`
	TotalTokens         int                   `json:"total_tokens"`
	EstimatedCostUSD    float64               `json:"estimated_cost_usd"`
	Success             bool                  `json:"success"`
	ErrorDetail         string                `json:"error_detail,omitempty"`
	ComplexityLabel     string                `json:"complexity_label"`
	NumModelsUsed       int                   `json:"num_models_used"`
	TemperatureUsed     float64               `json:"temperature_used"`
	CreatedAt           time.Time             `json:"created_at"`
}

// TokenUsage is per-provider token breakdown.
type TokenUsage struct {
	Prompt     int `json:"prompt"`
	Completion int `json:"completion"`
}

// ProbeConfigRequest is the query from Python asking for best probe params.
type ProbeConfigRequest struct {
	Role            string `json:"role"`
	RepoID          string `json:"repo_id"`
	ActionType      string `json:"action_type"`
	ComplexityLabel string `json:"complexity_label"`
	Tier            string `json:"tier"` // current ELO tier for adjustments
}

// ProbeHistoryRequest is the payload Python sends after a probe.
type ProbeHistoryRequest struct {
	TaskID              string                `json:"task_id"`
	Role                string                `json:"role"`
	RepoID              string                `json:"repo_id"`
	ActionType          string                `json:"action_type"`
	ProvidersQueried    []string              `json:"providers_queried"`
	ProvidersSucceeded  []string              `json:"providers_succeeded"`
	ProviderWinner      string                `json:"provider_winner"`
	StrategyUsed        string                `json:"strategy_used"`
	ConsensusConfidence float64               `json:"consensus_confidence"`
	ProviderLatencies   map[string]int64      `json:"provider_latencies"`
	ProviderTokens      map[string]TokenUsage `json:"provider_tokens"`
	TotalMs             int                   `json:"total_ms"`
	TotalTokens         int                   `json:"total_tokens"`
	EstimatedCostUSD    float64               `json:"estimated_cost_usd"`
	Success             bool                  `json:"success"`
	ErrorDetail         string                `json:"error_detail"`
	ComplexityLabel     string                `json:"complexity_label"`
	NumModelsUsed       int                   `json:"num_models_used"`
	TemperatureUsed     float64               `json:"temperature_used"`
}

// ProviderStats aggregates historical provider performance.
type ProviderStats struct {
	Provider         string  `json:"provider"`
	TotalProbes      int     `json:"total_probes"`
	Successes        int     `json:"successes"`
	Wins             int     `json:"wins"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	AvgTokens        float64 `json:"avg_tokens"`
	SuccessRate      float64 `json:"success_rate"`
	WinRate          float64 `json:"win_rate"`
	ReliabilityScore float64 `json:"reliability_score"` // 0.0–1.0 composite
}

// TuningRecommendation is the adaptive engine's output per config.
type TuningRecommendation struct {
	ConfigID     uuid.UUID   `json:"config_id"`
	OldConfig    ProbeConfig `json:"old_config"`
	NewNumModels int         `json:"new_num_models"`
	NewPreferred []string    `json:"new_preferred"`
	NewExcluded  []string    `json:"new_excluded"`
	NewTemp      float64     `json:"new_temperature"`
	NewTimeout   int         `json:"new_timeout_seconds"`
	NewRetries   int         `json:"new_retries"`
	NewThreshold float64     `json:"new_confidence_threshold"`
	Reasoning    string      `json:"reasoning"`
	Applied      bool        `json:"applied"`
}

// ── Service ─────────────────────────────────────────────────────────────────

// ProbeTuningService manages probe configs and adaptive tuning.
type ProbeTuningService struct {
	db      *pgxpool.Pool
	roleELO *RoleELOService
	mu      sync.RWMutex
	// In-memory cache of configs, keyed by "role:repo_id:action_type".
	cache map[string]*ProbeConfig
}

// NewProbeTuningService creates a new instance.
func NewProbeTuningService(db *pgxpool.Pool, roleELO *RoleELOService) *ProbeTuningService {
	return &ProbeTuningService{
		db:      db,
		roleELO: roleELO,
		cache:   make(map[string]*ProbeConfig),
	}
}

// cacheKey builds the lookup key for the in-memory config cache.
func probeCacheKey(role, repoID, actionType string) string {
	return role + ":" + repoID + ":" + actionType
}

// ── Config CRUD ─────────────────────────────────────────────────────────────

// GetConfig returns the best-match probe config for (role, repo, action).
// Falls back through: exact → (role, repo, "") → (role, "", "") → defaults.
func (s *ProbeTuningService) GetConfig(
	ctx context.Context,
	role, repoID, actionType string,
) (*ProbeConfig, error) {
	// Try cache first.
	s.mu.RLock()
	for _, key := range []string{
		probeCacheKey(role, repoID, actionType),
		probeCacheKey(role, repoID, ""),
		probeCacheKey(role, "", ""),
	} {
		if cfg, ok := s.cache[key]; ok {
			s.mu.RUnlock()
			return cfg, nil
		}
	}
	s.mu.RUnlock()

	// Query DB with fallback.
	cfg, err := s.fetchConfigFallback(ctx, role, repoID, actionType)
	if err != nil {
		return nil, err
	}

	// Cache it.
	s.mu.Lock()
	key := probeCacheKey(cfg.Role, cfg.RepoID, cfg.ActionType)
	s.cache[key] = cfg
	s.mu.Unlock()

	return cfg, nil
}

// fetchConfigFallback queries the DB with cascading fallback.
func (s *ProbeTuningService) fetchConfigFallback(
	ctx context.Context,
	role, repoID, actionType string,
) (*ProbeConfig, error) {
	// Try exact, then (role, repo, ""), then (role, "", "").
	candidates := []struct{ role, repo, action string }{
		{role, repoID, actionType},
		{role, repoID, ""},
		{role, "", ""},
	}

	for _, c := range candidates {
		cfg, err := s.fetchConfig(ctx, c.role, c.repo, c.action)
		if err == nil {
			return cfg, nil
		}
		if err != pgx.ErrNoRows {
			return nil, err
		}
	}

	// No config found — return defaults.
	return s.defaultConfig(role, repoID, actionType), nil
}

// fetchConfig loads a single config row.
func (s *ProbeTuningService) fetchConfig(
	ctx context.Context,
	role, repoID, actionType string,
) (*ProbeConfig, error) {
	var cfg ProbeConfig
	err := s.db.QueryRow(ctx, `
		SELECT id, role, repo_id, action_type, num_models,
		       preferred_providers, excluded_providers,
		       temperature, max_tokens, timeout_seconds,
		       budget_cap_usd, tokens_spent, strategy,
		       confidence_threshold, retries, reasoning,
		       last_tuned_at, created_at, updated_at
		FROM swarm_probe_configs
		WHERE role = $1 AND repo_id = $2 AND action_type = $3`,
		role, repoID, actionType,
	).Scan(
		&cfg.ID, &cfg.Role, &cfg.RepoID, &cfg.ActionType, &cfg.NumModels,
		&cfg.PreferredProviders, &cfg.ExcludedProviders,
		&cfg.Temperature, &cfg.MaxTokens, &cfg.TimeoutSeconds,
		&cfg.BudgetCapUSD, &cfg.TokensSpent, &cfg.Strategy,
		&cfg.ConfidenceThreshold, &cfg.Retries, &cfg.Reasoning,
		&cfg.LastTunedAt, &cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// defaultConfig returns sensible defaults for a role.
func (s *ProbeTuningService) defaultConfig(role, repoID, actionType string) *ProbeConfig {
	numModels := 3
	temp := 0.7
	timeout := 120
	retries := 1
	threshold := 0.7

	// Role-specific defaults.
	switch role {
	case "orchestrator":
		numModels = 3
		temp = 0.5
		timeout = 180
	case "architect":
		numModels = 3
		temp = 0.6
	case "senior_dev":
		numModels = 2
		temp = 0.4
		timeout = 150
	case "qa", "security":
		numModels = 3
		temp = 0.3
		threshold = 0.8
	case "junior_dev":
		numModels = 2
		temp = 0.5
	case "docs":
		numModels = 2
		temp = 0.7
		timeout = 90
	}

	return &ProbeConfig{
		ID:                  uuid.New(),
		Role:                role,
		RepoID:              repoID,
		ActionType:          actionType,
		NumModels:           numModels,
		PreferredProviders:  []string{},
		ExcludedProviders:   []string{},
		Temperature:         temp,
		MaxTokens:           4096,
		TimeoutSeconds:      timeout,
		BudgetCapUSD:        0,
		TokensSpent:         0,
		Strategy:            ProbeStrategyAdaptive,
		ConfidenceThreshold: threshold,
		Retries:             retries,
		Reasoning:           "default config for " + role,
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	}
}

// UpsertConfig creates or updates a probe config.
func (s *ProbeTuningService) UpsertConfig(ctx context.Context, cfg *ProbeConfig) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO swarm_probe_configs (
			id, role, repo_id, action_type, num_models,
			preferred_providers, excluded_providers,
			temperature, max_tokens, timeout_seconds,
			budget_cap_usd, tokens_spent, strategy,
			confidence_threshold, retries, reasoning,
			last_tuned_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19
		)
		ON CONFLICT (role, repo_id, action_type) DO UPDATE SET
			num_models = EXCLUDED.num_models,
			preferred_providers = EXCLUDED.preferred_providers,
			excluded_providers = EXCLUDED.excluded_providers,
			temperature = EXCLUDED.temperature,
			max_tokens = EXCLUDED.max_tokens,
			timeout_seconds = EXCLUDED.timeout_seconds,
			budget_cap_usd = EXCLUDED.budget_cap_usd,
			tokens_spent = EXCLUDED.tokens_spent,
			strategy = EXCLUDED.strategy,
			confidence_threshold = EXCLUDED.confidence_threshold,
			retries = EXCLUDED.retries,
			reasoning = EXCLUDED.reasoning,
			last_tuned_at = EXCLUDED.last_tuned_at,
			updated_at = NOW()`,
		cfg.ID, cfg.Role, cfg.RepoID, cfg.ActionType, cfg.NumModels,
		cfg.PreferredProviders, cfg.ExcludedProviders,
		cfg.Temperature, cfg.MaxTokens, cfg.TimeoutSeconds,
		cfg.BudgetCapUSD, cfg.TokensSpent, cfg.Strategy,
		cfg.ConfidenceThreshold, cfg.Retries, cfg.Reasoning,
		cfg.LastTunedAt, cfg.CreatedAt, cfg.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upserting probe config: %w", err)
	}

	// Invalidate cache.
	s.mu.Lock()
	key := probeCacheKey(cfg.Role, cfg.RepoID, cfg.ActionType)
	s.cache[key] = cfg
	s.mu.Unlock()

	return nil
}

// ListConfigs returns all probe configs, optionally filtered by role.
func (s *ProbeTuningService) ListConfigs(ctx context.Context, filterRole string) ([]ProbeConfig, error) {
	var rows pgx.Rows
	var err error

	if filterRole != "" {
		rows, err = s.db.Query(ctx, `
			SELECT id, role, repo_id, action_type, num_models,
			       preferred_providers, excluded_providers,
			       temperature, max_tokens, timeout_seconds,
			       budget_cap_usd, tokens_spent, strategy,
			       confidence_threshold, retries, reasoning,
			       last_tuned_at, created_at, updated_at
			FROM swarm_probe_configs
			WHERE role = $1
			ORDER BY repo_id, action_type`, filterRole)
	} else {
		rows, err = s.db.Query(ctx, `
			SELECT id, role, repo_id, action_type, num_models,
			       preferred_providers, excluded_providers,
			       temperature, max_tokens, timeout_seconds,
			       budget_cap_usd, tokens_spent, strategy,
			       confidence_threshold, retries, reasoning,
			       last_tuned_at, created_at, updated_at
			FROM swarm_probe_configs
			ORDER BY role, repo_id, action_type`)
	}
	if err != nil {
		return nil, fmt.Errorf("listing probe configs: %w", err)
	}
	defer rows.Close()

	var configs []ProbeConfig
	for rows.Next() {
		var cfg ProbeConfig
		if err := rows.Scan(
			&cfg.ID, &cfg.Role, &cfg.RepoID, &cfg.ActionType, &cfg.NumModels,
			&cfg.PreferredProviders, &cfg.ExcludedProviders,
			&cfg.Temperature, &cfg.MaxTokens, &cfg.TimeoutSeconds,
			&cfg.BudgetCapUSD, &cfg.TokensSpent, &cfg.Strategy,
			&cfg.ConfidenceThreshold, &cfg.Retries, &cfg.Reasoning,
			&cfg.LastTunedAt, &cfg.CreatedAt, &cfg.UpdatedAt,
		); err != nil {
			continue
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

// ── History Recording ───────────────────────────────────────────────────────

// RecordProbeOutcome appends a probe outcome to the history table and
// updates the token spend counter on the matching config.
func (s *ProbeTuningService) RecordProbeOutcome(ctx context.Context, h *ProbeHistory) error {
	latenciesJSON, _ := json.Marshal(h.ProviderLatencies)
	tokensJSON, _ := json.Marshal(h.ProviderTokens)

	_, err := s.db.Exec(ctx, `
		INSERT INTO swarm_probe_history (
			id, task_id, role, repo_id, action_type,
			providers_queried, providers_succeeded, provider_winner,
			strategy_used, consensus_confidence,
			provider_latencies, provider_tokens,
			total_ms, total_tokens, estimated_cost_usd,
			success, error_detail, complexity_label,
			num_models_used, temperature_used, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
		)`,
		h.ID, h.TaskID, h.Role, h.RepoID, h.ActionType,
		h.ProvidersQueried, h.ProvidersSucceeded, h.ProviderWinner,
		h.StrategyUsed, h.ConsensusConfidence,
		latenciesJSON, tokensJSON,
		h.TotalMs, h.TotalTokens, h.EstimatedCostUSD,
		h.Success, h.ErrorDetail, h.ComplexityLabel,
		h.NumModelsUsed, h.TemperatureUsed, h.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("recording probe outcome: %w", err)
	}

	// Update token spend on the config (best-effort).
	_, _ = s.db.Exec(ctx, `
		UPDATE swarm_probe_configs
		SET tokens_spent = tokens_spent + $1, updated_at = NOW()
		WHERE role = $2 AND repo_id = $3 AND action_type = $4`,
		int64(h.TotalTokens), h.Role, h.RepoID, h.ActionType,
	)

	// Record Prometheus metric.
	status := "ok"
	if !h.Success {
		status = "error"
	}
	SwarmProbeTuningOutcomes.WithLabelValues(h.Role, h.ComplexityLabel, status).Inc()
	SwarmProbeTuningLatency.WithLabelValues(h.Role).Observe(float64(h.TotalMs) / 1000.0)
	SwarmProbeTuningTokens.WithLabelValues(h.Role).Add(float64(h.TotalTokens))
	if h.EstimatedCostUSD > 0 {
		SwarmProbeTuningCostUSD.WithLabelValues(h.Role).Add(h.EstimatedCostUSD)
	}

	return nil
}

// GetRecentHistory returns the last N probe outcomes for a (role, repo).
func (s *ProbeTuningService) GetRecentHistory(
	ctx context.Context,
	role, repoID string,
	limit int,
) ([]ProbeHistory, error) {
	if limit <= 0 {
		limit = probeTuneHistoryWindow
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, task_id, role, repo_id, action_type,
		       providers_queried, providers_succeeded, provider_winner,
		       strategy_used, consensus_confidence,
		       provider_latencies, provider_tokens,
		       total_ms, total_tokens, estimated_cost_usd,
		       success, error_detail, complexity_label,
		       num_models_used, temperature_used, created_at
		FROM swarm_probe_history
		WHERE role = $1 AND ($2 = '' OR repo_id = $2)
		ORDER BY created_at DESC
		LIMIT $3`,
		role, repoID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("fetching probe history: %w", err)
	}
	defer rows.Close()

	var history []ProbeHistory
	for rows.Next() {
		var h ProbeHistory
		var latenciesJSON, tokensJSON []byte
		if err := rows.Scan(
			&h.ID, &h.TaskID, &h.Role, &h.RepoID, &h.ActionType,
			&h.ProvidersQueried, &h.ProvidersSucceeded, &h.ProviderWinner,
			&h.StrategyUsed, &h.ConsensusConfidence,
			&latenciesJSON, &tokensJSON,
			&h.TotalMs, &h.TotalTokens, &h.EstimatedCostUSD,
			&h.Success, &h.ErrorDetail, &h.ComplexityLabel,
			&h.NumModelsUsed, &h.TemperatureUsed, &h.CreatedAt,
		); err != nil {
			continue
		}
		_ = json.Unmarshal(latenciesJSON, &h.ProviderLatencies)
		_ = json.Unmarshal(tokensJSON, &h.ProviderTokens)
		history = append(history, h)
	}
	return history, nil
}

// ── Provider Stats ──────────────────────────────────────────────────────────

// ComputeProviderStats aggregates provider performance from recent history.
func (s *ProbeTuningService) ComputeProviderStats(
	ctx context.Context,
	role, repoID string,
	limit int,
) ([]ProviderStats, error) {
	history, err := s.GetRecentHistory(ctx, role, repoID, limit)
	if err != nil {
		return nil, err
	}

	// Aggregate per provider.
	type agg struct {
		total      int
		successes  int
		wins       int
		latencySum int64
		tokenSum   int64
	}
	provAgg := make(map[string]*agg)

	for _, h := range history {
		for _, p := range h.ProvidersQueried {
			a, ok := provAgg[p]
			if !ok {
				a = &agg{}
				provAgg[p] = a
			}
			a.total++

			// Check success.
			for _, sp := range h.ProvidersSucceeded {
				if sp == p {
					a.successes++
					break
				}
			}

			// Check win.
			if h.ProviderWinner == p {
				a.wins++
			}

			// Latency.
			if lat, ok := h.ProviderLatencies[p]; ok {
				a.latencySum += lat
			}

			// Tokens.
			if tok, ok := h.ProviderTokens[p]; ok {
				a.tokenSum += int64(tok.Prompt + tok.Completion)
			}
		}
	}

	var stats []ProviderStats
	for provider, a := range provAgg {
		ps := ProviderStats{
			Provider:    provider,
			TotalProbes: a.total,
			Successes:   a.successes,
			Wins:        a.wins,
		}
		if a.total > 0 {
			ps.AvgLatencyMs = float64(a.latencySum) / float64(a.total)
			ps.AvgTokens = float64(a.tokenSum) / float64(a.total)
			ps.SuccessRate = float64(a.successes) / float64(a.total)
		}
		if a.successes > 0 {
			ps.WinRate = float64(a.wins) / float64(a.successes)
		}
		// Composite reliability: 60% success rate + 30% win rate + 10% latency factor.
		latencyFactor := 1.0
		if ps.AvgLatencyMs > 0 {
			latencyFactor = math.Min(1.0, 5000.0/ps.AvgLatencyMs) // faster = higher score
		}
		ps.ReliabilityScore = math.Round(
			(0.60*ps.SuccessRate+0.30*ps.WinRate+0.10*latencyFactor)*1000) / 1000

		stats = append(stats, ps)
	}

	// Sort by reliability descending.
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].ReliabilityScore > stats[j].ReliabilityScore
	})

	return stats, nil
}

// ── Adaptive Tuning Engine ──────────────────────────────────────────────────

// StartTuningLoop runs the periodic adaptive tuning goroutine.
func (s *ProbeTuningService) StartTuningLoop(ctx context.Context) {
	ticker := time.NewTicker(probeTuneInterval)
	defer ticker.Stop()

	// Run once immediately.
	s.runTuningCycle(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runTuningCycle(ctx)
		}
	}
}

// runTuningCycle analyses probe history and adjusts configs.
func (s *ProbeTuningService) runTuningCycle(ctx context.Context) {
	configs, err := s.ListConfigs(ctx, "")
	if err != nil {
		slog.Error("probe-tuning: failed to list configs", "error", err)
		return
	}

	var adjusted int
	for i := range configs {
		cfg := &configs[i]
		rec, err := s.tuneConfig(ctx, cfg)
		if err != nil {
			slog.Debug("probe-tuning: skip config", "role", cfg.Role, "error", err)
			continue
		}
		if rec == nil {
			continue // no changes needed
		}

		// Apply recommendation.
		cfg.NumModels = rec.NewNumModels
		cfg.PreferredProviders = rec.NewPreferred
		cfg.ExcludedProviders = rec.NewExcluded
		cfg.Temperature = rec.NewTemp
		cfg.TimeoutSeconds = rec.NewTimeout
		cfg.Retries = rec.NewRetries
		cfg.ConfidenceThreshold = rec.NewThreshold
		cfg.Reasoning = rec.Reasoning
		now := time.Now().UTC()
		cfg.LastTunedAt = &now
		cfg.UpdatedAt = now

		if err := s.UpsertConfig(ctx, cfg); err != nil {
			slog.Error("probe-tuning: failed to apply", "role", cfg.Role, "error", err)
			continue
		}

		adjusted++
		SwarmProbeTuningAdjustments.WithLabelValues(cfg.Role, cfg.Strategy).Inc()
		slog.Info("probe-tuning: config adjusted",
			"role", cfg.Role,
			"repo", cfg.RepoID,
			"num_models", cfg.NumModels,
			"preferred", cfg.PreferredProviders,
			"excluded", cfg.ExcludedProviders,
			"reasoning", cfg.Reasoning,
		)
	}

	if adjusted > 0 {
		slog.Info("probe-tuning: cycle complete", "adjusted", adjusted, "total", len(configs))
	}
}

// tuneConfig analyses recent history for a config and returns a recommendation.
// Returns nil if no changes are needed.
func (s *ProbeTuningService) tuneConfig(
	ctx context.Context,
	cfg *ProbeConfig,
) (*TuningRecommendation, error) {
	if cfg.Strategy == ProbeStrategyStatic {
		return nil, nil // static configs are never auto-adjusted
	}

	history, err := s.GetRecentHistory(ctx, cfg.Role, cfg.RepoID, probeTuneHistoryWindow)
	if err != nil {
		return nil, err
	}

	// Filter to matching action_type if set.
	if cfg.ActionType != "" {
		filtered := make([]ProbeHistory, 0, len(history))
		for _, h := range history {
			if h.ActionType == cfg.ActionType {
				filtered = append(filtered, h)
			}
		}
		history = filtered
	}

	if len(history) < probeTuneMinSamples {
		return nil, nil // not enough data
	}

	// Compute provider stats from this slice.
	type provAgg struct {
		total, successes, wins int
		latencySum             int64
		failStreak             int
	}
	provMap := make(map[string]*provAgg)
	var totalSuccessRate float64
	var avgConfidence float64
	var highLatencyCount int

	for _, h := range history {
		if h.Success {
			totalSuccessRate++
		}
		avgConfidence += h.ConsensusConfidence

		for _, p := range h.ProvidersQueried {
			a, ok := provMap[p]
			if !ok {
				a = &provAgg{}
				provMap[p] = a
			}
			a.total++
			succeeded := false
			for _, sp := range h.ProvidersSucceeded {
				if sp == p {
					a.successes++
					succeeded = true
					break
				}
			}
			if !succeeded {
				a.failStreak++
			} else {
				a.failStreak = 0
			}
			if h.ProviderWinner == p {
				a.wins++
			}
			if lat, ok := h.ProviderLatencies[p]; ok {
				a.latencySum += lat
				if lat > int64(cfg.TimeoutSeconds)*800 { // 80% of timeout
					highLatencyCount++
				}
			}
		}
	}

	n := float64(len(history))
	totalSuccessRate /= n
	avgConfidence /= n

	// Build recommendation.
	rec := &TuningRecommendation{
		ConfigID:     cfg.ID,
		OldConfig:    *cfg,
		NewNumModels: cfg.NumModels,
		NewPreferred: append([]string{}, cfg.PreferredProviders...),
		NewExcluded:  append([]string{}, cfg.ExcludedProviders...),
		NewTemp:      cfg.Temperature,
		NewTimeout:   cfg.TimeoutSeconds,
		NewRetries:   cfg.Retries,
		NewThreshold: cfg.ConfidenceThreshold,
	}
	var reasons []string
	changed := false

	// ── Rule 1: Exclude consistently failing providers ──────────────
	for prov, a := range provMap {
		if a.total >= 3 && float64(a.successes)/float64(a.total) < 0.3 {
			if !probeStringSliceContains(rec.NewExcluded, prov) {
				rec.NewExcluded = append(rec.NewExcluded, prov)
				reasons = append(reasons, fmt.Sprintf("excluded %s (%.0f%% success rate)", prov, 100*float64(a.successes)/float64(a.total)))
				changed = true
			}
		}
	}

	// ── Rule 2: Prefer high-reliability providers ───────────────────
	type provScore struct {
		name  string
		score float64
	}
	var scored []provScore
	for prov, a := range provMap {
		if probeStringSliceContains(rec.NewExcluded, prov) {
			continue
		}
		successRate := float64(a.successes) / math.Max(float64(a.total), 1)
		winRate := float64(a.wins) / math.Max(float64(a.successes), 1)
		score := 0.5*successRate + 0.5*winRate
		scored = append(scored, provScore{prov, score})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })

	if len(scored) >= 2 {
		newPreferred := make([]string, 0, len(scored))
		for _, ps := range scored {
			newPreferred = append(newPreferred, ps.name)
		}
		if !probeStringSliceEqual(newPreferred, cfg.PreferredProviders) {
			rec.NewPreferred = newPreferred
			reasons = append(reasons, fmt.Sprintf("reranked providers: %s", strings.Join(newPreferred, " > ")))
			changed = true
		}
	}

	// ── Rule 3: Adjust num_models based on confidence ───────────────
	if avgConfidence > 0.85 && cfg.NumModels > 2 {
		rec.NewNumModels = cfg.NumModels - 1
		reasons = append(reasons, fmt.Sprintf("reduced probes %d→%d (avg confidence %.2f)", cfg.NumModels, rec.NewNumModels, avgConfidence))
		changed = true
	} else if avgConfidence < 0.5 && cfg.NumModels < 5 {
		rec.NewNumModels = cfg.NumModels + 1
		reasons = append(reasons, fmt.Sprintf("increased probes %d→%d (avg confidence %.2f)", cfg.NumModels, rec.NewNumModels, avgConfidence))
		changed = true
	}

	// ── Rule 4: Adjust temperature based on success rate ────────────
	if totalSuccessRate < 0.6 && cfg.Temperature > 0.2 {
		rec.NewTemp = math.Max(0.1, cfg.Temperature-0.1)
		reasons = append(reasons, fmt.Sprintf("lowered temperature %.1f→%.1f (success %.0f%%)", cfg.Temperature, rec.NewTemp, totalSuccessRate*100))
		changed = true
	} else if totalSuccessRate > 0.95 && cfg.Temperature < 0.9 {
		rec.NewTemp = math.Min(1.0, cfg.Temperature+0.05)
		reasons = append(reasons, fmt.Sprintf("raised temperature %.2f→%.2f (success %.0f%%)", cfg.Temperature, rec.NewTemp, totalSuccessRate*100))
		changed = true
	}

	// ── Rule 5: Adjust timeout if many probes are slow ──────────────
	if highLatencyCount > len(history)/3 && cfg.TimeoutSeconds < 300 {
		rec.NewTimeout = cfg.TimeoutSeconds + 30
		reasons = append(reasons, fmt.Sprintf("extended timeout %ds→%ds (%d/%d slow probes)", cfg.TimeoutSeconds, rec.NewTimeout, highLatencyCount, len(history)))
		changed = true
	}

	// ── Rule 6: Adjust confidence threshold ─────────────────────────
	if avgConfidence > 0.9 && cfg.ConfidenceThreshold < 0.85 {
		rec.NewThreshold = math.Min(0.9, cfg.ConfidenceThreshold+0.05)
		reasons = append(reasons, fmt.Sprintf("raised threshold %.2f→%.2f", cfg.ConfidenceThreshold, rec.NewThreshold))
		changed = true
	} else if avgConfidence < 0.5 && cfg.ConfidenceThreshold > 0.5 {
		rec.NewThreshold = math.Max(0.4, cfg.ConfidenceThreshold-0.1)
		reasons = append(reasons, fmt.Sprintf("lowered threshold %.2f→%.2f", cfg.ConfidenceThreshold, rec.NewThreshold))
		changed = true
	}

	if !changed {
		return nil, nil
	}

	rec.Reasoning = strings.Join(reasons, "; ")
	rec.Applied = true
	return rec, nil
}

// ── Complexity-Aware Config Enhancement ─────────────────────────────────────

// EnhanceForComplexity returns a copy of the config adjusted for task complexity.
// This is called per-probe (not persisted), giving tasks harder tasks more probes.
func (s *ProbeTuningService) EnhanceForComplexity(
	cfg *ProbeConfig,
	complexityLabel, tier string,
) *ProbeConfig {
	enhanced := *cfg // copy

	// Adjust num_models based on complexity.
	switch complexityLabel {
	case FormationComplexityTrivial:
		enhanced.NumModels = maxInt(1, cfg.NumModels-1)
	case formationLabelSmall:
		// keep as-is
	case formationLabelMedium:
		// keep as-is
	case formationLabelLarge:
		enhanced.NumModels = minInt(cfg.NumModels+1, 6)
	case FormationComplexityCritical:
		enhanced.NumModels = minInt(cfg.NumModels+2, 7)
		enhanced.Temperature = math.Max(0.1, cfg.Temperature-0.1) // more deterministic
		enhanced.ConfidenceThreshold = math.Min(0.95, cfg.ConfidenceThreshold+0.1)
	}

	// Adjust for ELO tier.
	switch tier {
	case TierRestricted:
		enhanced.NumModels = maxInt(enhanced.NumModels, 4) // restricted roles need more oversight
		enhanced.Retries = maxInt(cfg.Retries, 2)
	case TierExpert:
		enhanced.NumModels = maxInt(1, enhanced.NumModels-1) // trusted, fewer probes
	}

	return &enhanced
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func probeStringSliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func probeStringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
