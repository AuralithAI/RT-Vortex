package swarm

// Observability Dashboard Service.
//
// Provides aggregated, time-series observability data for the swarm:
//   - Periodic metric snapshots (tasks, agents, LLM usage, costs)
//   - Per-provider performance logs (latency, error rate, consensus wins)
//   - System health scoring (composite 0-100 score)
//   - Cost tracking with monthly budgets
//
// The service runs a background goroutine that periodically:
//   1. Samples current swarm counters/gauges from the DB
//   2. Estimates LLM cost from token counters
//   3. Computes a composite health score
//   4. Persists snapshots to swarm_metrics_snapshots
//   5. Persists per-provider perf logs to swarm_provider_perf_log
//   6. Garbage-collects old snapshots beyond the retention window

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Configuration ───────────────────────────────────────────────────────────

const (
	// observabilitySnapshotInterval is how often metric snapshots are taken.
	observabilitySnapshotInterval = 60 * time.Second

	// observabilityRetentionDays is how many days to keep metric snapshots.
	observabilityRetentionDays = 90

	// providerPerfRetentionDays is how many days to keep provider perf logs.
	providerPerfRetentionDays = 90

	// healthScoreMax is the maximum health score.
	healthScoreMax = 100
)

// ── Cost Estimation ─────────────────────────────────────────────────────────

// providerCostPer1KTokens maps provider names to estimated USD cost per 1K tokens.
// These are blended input/output averages — adjust for your tier/model.
var providerCostPer1KTokens = map[string]float64{
	"openai":    0.005,
	"anthropic": 0.008,
	"google":    0.004,
	"mistral":   0.003,
	"groq":      0.001,
	"deepseek":  0.002,
	"cohere":    0.003,
}

// defaultCostPer1KTokens is used for unknown providers.
const defaultCostPer1KTokens = 0.005

// ── Data Types ──────────────────────────────────────────────────────────────

// MetricsSnapshot is one point in the time-series.
type MetricsSnapshot struct {
	ID               uuid.UUID `json:"id"`
	ActiveTasks      int       `json:"active_tasks"`
	PendingTasks     int       `json:"pending_tasks"`
	CompletedTasks   int64     `json:"completed_tasks"`
	FailedTasks      int64     `json:"failed_tasks"`
	OnlineAgents     int       `json:"online_agents"`
	BusyAgents       int       `json:"busy_agents"`
	ActiveTeams      int       `json:"active_teams"`
	BusyTeams        int       `json:"busy_teams"`
	LLMCalls         int64     `json:"llm_calls"`
	LLMTokens        int64     `json:"llm_tokens"`
	LLMAvgLatencyMs  float64   `json:"llm_avg_latency_ms"`
	LLMErrorRate     float64   `json:"llm_error_rate"`
	ProbeCalls       int64     `json:"probe_calls"`
	ConsensusRuns    int64     `json:"consensus_runs"`
	ConsensusAvgConf float64   `json:"consensus_avg_confidence"`
	OpenCircuits     int       `json:"open_circuits"`
	HealEvents       int       `json:"heal_events"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd"`
	QueueDepth       int       `json:"queue_depth"`
	AgentUtilisation float64   `json:"agent_utilisation"`
	HealthScore      int       `json:"health_score"`
	CreatedAt        time.Time `json:"created_at"`
}

// ProviderPerfSnapshot is a per-provider performance data point.
type ProviderPerfSnapshot struct {
	ID               uuid.UUID `json:"id"`
	Provider         string    `json:"provider"`
	Calls            int       `json:"calls"`
	Successes        int       `json:"successes"`
	Failures         int       `json:"failures"`
	TokensUsed       int64     `json:"tokens_used"`
	AvgLatencyMs     float64   `json:"avg_latency_ms"`
	P95LatencyMs     float64   `json:"p95_latency_ms"`
	P99LatencyMs     float64   `json:"p99_latency_ms"`
	ErrorRate        float64   `json:"error_rate"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd"`
	ConsensusWins    int       `json:"consensus_wins"`
	ConsensusTotal   int       `json:"consensus_total"`
	CreatedAt        time.Time `json:"created_at"`
}

// CostBudget represents a monthly cost budget.
type CostBudget struct {
	ID             uuid.UUID `json:"id"`
	Scope          string    `json:"scope"`
	Month          time.Time `json:"month"`
	BudgetUSD      float64   `json:"budget_usd"`
	SpentUSD       float64   `json:"spent_usd"`
	AlertThreshold float64   `json:"alert_threshold"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ObservabilityDashboard is the full dashboard response.
type ObservabilityDashboard struct {
	// Current snapshot
	Current *MetricsSnapshot `json:"current"`
	// Time-series (most recent N snapshots)
	TimeSeries []MetricsSnapshot `json:"time_series"`
	// Provider performance (most recent per-provider data points)
	ProviderPerf []ProviderPerfSnapshot `json:"provider_perf"`
	// Provider performance time-series (for charts)
	ProviderTimeSeries map[string][]ProviderPerfSnapshot `json:"provider_time_series"`
	// Health breakdown
	HealthBreakdown *HealthBreakdown `json:"health_breakdown"`
	// Cost summary
	CostSummary *CostSummary `json:"cost_summary"`
	// System uptime
	UptimeSeconds float64 `json:"uptime_seconds"`
}

// HealthBreakdown explains the health score components.
type HealthBreakdown struct {
	Score             int     `json:"score"`
	TaskHealthPct     float64 `json:"task_health_pct"`
	AgentHealthPct    float64 `json:"agent_health_pct"`
	ProviderHealthPct float64 `json:"provider_health_pct"`
	QueueHealthPct    float64 `json:"queue_health_pct"`
	ErrorRatePct      float64 `json:"error_rate_pct"`
	Details           string  `json:"details"`
}

// CostSummary aggregates cost data.
type CostSummary struct {
	TodayUSD     float64            `json:"today_usd"`
	ThisWeekUSD  float64            `json:"this_week_usd"`
	ThisMonthUSD float64            `json:"this_month_usd"`
	ByProvider   map[string]float64 `json:"by_provider"`
	Budget       *CostBudget        `json:"budget,omitempty"`
}

// ── ObservabilityService ────────────────────────────────────────────────────

// ObservabilityService manages metric snapshots and dashboard data.
type ObservabilityService struct {
	db          *pgxpool.Pool
	selfHealSvc *SelfHealService
	startedAt   time.Time

	mu              sync.RWMutex
	latestSnapshot  *MetricsSnapshot
	providerLatency map[string]*latencyAccumulator // per-provider latency tracker
}

// latencyAccumulator tracks running latency stats between snapshots.
type latencyAccumulator struct {
	count   int64
	sumMs   float64
	samples []float64 // for percentile computation (capped at 1000)
}

// NewObservabilityService creates the observability service.
func NewObservabilityService(db *pgxpool.Pool, selfHealSvc *SelfHealService) *ObservabilityService {
	return &ObservabilityService{
		db:              db,
		selfHealSvc:     selfHealSvc,
		startedAt:       time.Now(),
		providerLatency: make(map[string]*latencyAccumulator),
	}
}

// ── Background Loop ─────────────────────────────────────────────────────────

// Start launches the observability background loop. Blocks until ctx is cancelled.
func (o *ObservabilityService) Start(ctx context.Context) {
	slog.Info("observability: background loop started",
		"interval", observabilitySnapshotInterval,
		"retention_days", observabilityRetentionDays,
	)

	// Take an initial snapshot immediately.
	o.takeSnapshot(ctx)

	ticker := time.NewTicker(observabilitySnapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("observability: background loop stopped")
			return
		case <-ticker.C:
			o.takeSnapshot(ctx)
		}
	}
}

// ── Snapshot Logic ──────────────────────────────────────────────────────────

func (o *ObservabilityService) takeSnapshot(ctx context.Context) {
	start := time.Now()
	defer func() {
		SwarmObservabilityCycleDuration.Observe(time.Since(start).Seconds())
	}()

	snap := o.collectMetrics(ctx)
	snap.HealthScore = o.computeHealthScore(snap)

	// Persist snapshot.
	_, err := o.db.Exec(ctx, `
		INSERT INTO swarm_metrics_snapshots
			(active_tasks, pending_tasks, completed_tasks, failed_tasks,
			 online_agents, busy_agents, active_teams, busy_teams,
			 llm_calls, llm_tokens, llm_avg_latency_ms, llm_error_rate,
			 probe_calls, consensus_runs, consensus_avg_confidence,
			 open_circuits, heal_events, estimated_cost_usd,
			 queue_depth, agent_utilisation, health_score)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
	`,
		snap.ActiveTasks, snap.PendingTasks, snap.CompletedTasks, snap.FailedTasks,
		snap.OnlineAgents, snap.BusyAgents, snap.ActiveTeams, snap.BusyTeams,
		snap.LLMCalls, snap.LLMTokens, snap.LLMAvgLatencyMs, snap.LLMErrorRate,
		snap.ProbeCalls, snap.ConsensusRuns, snap.ConsensusAvgConf,
		snap.OpenCircuits, snap.HealEvents, snap.EstimatedCostUSD,
		snap.QueueDepth, snap.AgentUtilisation, snap.HealthScore,
	)
	if err != nil {
		slog.Warn("observability: failed to persist snapshot", "error", err)
	}

	// Persist per-provider performance.
	o.persistProviderPerf(ctx)

	// Update in-memory latest.
	o.mu.Lock()
	o.latestSnapshot = snap
	o.mu.Unlock()

	SwarmObservabilityHealthScore.Set(float64(snap.HealthScore))
	SwarmObservabilitySnapshotsTotal.Inc()

	// GC old data.
	o.gcOldData(ctx)
}

func (o *ObservabilityService) collectMetrics(ctx context.Context) *MetricsSnapshot {
	snap := &MetricsSnapshot{}

	// Task counts.
	_ = o.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_tasks
		WHERE status NOT IN ('completed','failed','cancelled','timed_out')
	`).Scan(&snap.ActiveTasks)

	_ = o.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_tasks WHERE status = 'submitted'
	`).Scan(&snap.PendingTasks)

	_ = o.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_tasks WHERE status = 'completed'
	`).Scan(&snap.CompletedTasks)

	_ = o.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_tasks WHERE status IN ('failed','timed_out')
	`).Scan(&snap.FailedTasks)

	// Agent / team counts.
	_ = o.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_agents WHERE status != 'offline'
	`).Scan(&snap.OnlineAgents)

	_ = o.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_agents WHERE status = 'busy'
	`).Scan(&snap.BusyAgents)

	_ = o.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_teams WHERE status != 'offline'
	`).Scan(&snap.ActiveTeams)

	_ = o.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_teams WHERE status = 'busy'
	`).Scan(&snap.BusyTeams)

	// Utilisation.
	if snap.OnlineAgents > 0 {
		snap.AgentUtilisation = float64(snap.BusyAgents) / float64(snap.OnlineAgents)
	}

	// Queue depth = pending tasks.
	snap.QueueDepth = snap.PendingTasks

	// LLM usage from probe history (last snapshot interval).
	cutoff := time.Now().Add(-observabilitySnapshotInterval)
	_ = o.db.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*),0),
		       COALESCE(SUM(total_tokens),0),
		       COALESCE(AVG(latency_ms),0)
		FROM swarm_probe_history WHERE created_at >= $1
	`, cutoff).Scan(&snap.LLMCalls, &snap.LLMTokens, &snap.LLMAvgLatencyMs)

	// LLM error rate from probe history.
	var totalProbes, failedProbes int64
	_ = o.db.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*),0),
		       COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),0)
		FROM swarm_probe_history WHERE created_at >= $1
	`, cutoff).Scan(&totalProbes, &failedProbes)
	if totalProbes > 0 {
		snap.LLMErrorRate = float64(failedProbes) / float64(totalProbes)
	}

	// Probes and consensus from all-time.
	_ = o.db.QueryRow(ctx, `SELECT COALESCE(COUNT(*),0) FROM swarm_probe_history`).Scan(&snap.ProbeCalls)
	_ = o.db.QueryRow(ctx, `SELECT COALESCE(COUNT(*),0) FROM swarm_consensus_insights`).Scan(&snap.ConsensusRuns)

	// Consensus avg confidence.
	_ = o.db.QueryRow(ctx, `
		SELECT COALESCE(AVG(confidence),0) FROM swarm_consensus_insights
	`).Scan(&snap.ConsensusAvgConf)

	// Self-heal: open circuits.
	_ = o.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_provider_circuit_state WHERE state = 'open'
	`).Scan(&snap.OpenCircuits)

	// Self-heal: recent events (last interval).
	_ = o.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_self_heal_events WHERE created_at >= $1
	`, cutoff).Scan(&snap.HealEvents)

	// Estimated cost from probe history tokens.
	snap.EstimatedCostUSD = o.estimateCost(ctx, cutoff)

	return snap
}

func (o *ObservabilityService) estimateCost(ctx context.Context, since time.Time) float64 {
	rows, err := o.db.Query(ctx, `
		SELECT COALESCE(provider,'unknown'), COALESCE(SUM(total_tokens),0)
		FROM swarm_probe_history
		WHERE created_at >= $1
		GROUP BY provider
	`, since)
	if err != nil {
		return 0
	}
	defer rows.Close()

	var totalCost float64
	for rows.Next() {
		var provider string
		var tokens int64
		if err := rows.Scan(&provider, &tokens); err != nil {
			continue
		}
		costPer1K, ok := providerCostPer1KTokens[provider]
		if !ok {
			costPer1K = defaultCostPer1KTokens
		}
		totalCost += float64(tokens) / 1000.0 * costPer1K
	}
	return math.Round(totalCost*10000) / 10000 // 4 decimal places
}

func (o *ObservabilityService) persistProviderPerf(ctx context.Context) {
	cutoff := time.Now().Add(-observabilitySnapshotInterval)

	rows, err := o.db.Query(ctx, `
		SELECT provider,
		       COUNT(*) AS calls,
		       SUM(CASE WHEN status = 'ok' THEN 1 ELSE 0 END) AS successes,
		       SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) AS failures,
		       COALESCE(SUM(total_tokens),0) AS tokens,
		       COALESCE(AVG(latency_ms),0) AS avg_lat,
		       COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY latency_ms),0) AS p95,
		       COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY latency_ms),0) AS p99
		FROM swarm_probe_history
		WHERE created_at >= $1 AND provider IS NOT NULL AND provider != ''
		GROUP BY provider
	`, cutoff)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p ProviderPerfSnapshot
		if err := rows.Scan(&p.Provider, &p.Calls, &p.Successes, &p.Failures,
			&p.TokensUsed, &p.AvgLatencyMs, &p.P95LatencyMs, &p.P99LatencyMs,
		); err != nil {
			continue
		}
		if p.Calls > 0 {
			p.ErrorRate = float64(p.Failures) / float64(p.Calls)
		}
		costPer1K, ok := providerCostPer1KTokens[p.Provider]
		if !ok {
			costPer1K = defaultCostPer1KTokens
		}
		p.EstimatedCostUSD = float64(p.TokensUsed) / 1000.0 * costPer1K

		// Consensus wins for this provider.
		_ = o.db.QueryRow(ctx, `
			SELECT COALESCE(COUNT(*),0),
			       COALESCE(SUM(CASE WHEN provider = $1 THEN 1 ELSE 0 END),0)
			FROM swarm_consensus_insights
			WHERE created_at >= $2
		`, p.Provider, cutoff).Scan(&p.ConsensusTotal, &p.ConsensusWins)

		_, err := o.db.Exec(ctx, `
			INSERT INTO swarm_provider_perf_log
				(provider, calls, successes, failures, tokens_used,
				 avg_latency_ms, p95_latency_ms, p99_latency_ms,
				 error_rate, estimated_cost_usd, consensus_wins, consensus_total)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		`, p.Provider, p.Calls, p.Successes, p.Failures, p.TokensUsed,
			p.AvgLatencyMs, p.P95LatencyMs, p.P99LatencyMs,
			p.ErrorRate, p.EstimatedCostUSD, p.ConsensusWins, p.ConsensusTotal,
		)
		if err != nil {
			slog.Warn("observability: failed to persist provider perf", "provider", p.Provider, "error", err)
		}
	}
}

// ── Health Score ─────────────────────────────────────────────────────────────

func (o *ObservabilityService) computeHealthScore(snap *MetricsSnapshot) int {
	// Composite score from 5 dimensions, each contributing 0-20 points.
	score := 0.0

	// 1. Task health (20 pts): penalise high failure rate.
	total := snap.CompletedTasks + snap.FailedTasks
	if total > 0 {
		successRate := float64(snap.CompletedTasks) / float64(total)
		score += successRate * 20
	} else {
		score += 20 // no tasks = healthy
	}

	// 2. Agent health (20 pts): penalise if no agents online or low utilisation.
	if snap.OnlineAgents > 0 {
		score += 20 // agents are online
	}

	// 3. Provider health (20 pts): penalise open circuits.
	if snap.OpenCircuits == 0 {
		score += 20
	} else if snap.OpenCircuits == 1 {
		score += 10
	} else if snap.OpenCircuits == 2 {
		score += 5
	}

	// 4. Queue health (20 pts): penalise deep queues.
	switch {
	case snap.QueueDepth == 0:
		score += 20
	case snap.QueueDepth <= 5:
		score += 15
	case snap.QueueDepth <= 20:
		score += 10
	case snap.QueueDepth <= 50:
		score += 5
	}

	// 5. Error rate (20 pts): penalise high LLM error rate.
	switch {
	case snap.LLMErrorRate < 0.01:
		score += 20
	case snap.LLMErrorRate < 0.05:
		score += 15
	case snap.LLMErrorRate < 0.1:
		score += 10
	case snap.LLMErrorRate < 0.2:
		score += 5
	}

	result := int(math.Round(score))
	if result > healthScoreMax {
		result = healthScoreMax
	}
	return result
}

func (o *ObservabilityService) buildHealthBreakdown(snap *MetricsSnapshot) *HealthBreakdown {
	hb := &HealthBreakdown{Score: snap.HealthScore}

	total := snap.CompletedTasks + snap.FailedTasks
	if total > 0 {
		hb.TaskHealthPct = float64(snap.CompletedTasks) / float64(total) * 100
	} else {
		hb.TaskHealthPct = 100
	}

	if snap.OnlineAgents > 0 {
		hb.AgentHealthPct = 100
	}

	if snap.OpenCircuits == 0 {
		hb.ProviderHealthPct = 100
	} else {
		// Each open circuit reduces provider health.
		total := snap.OpenCircuits + 1 // +1 to avoid div by zero edge
		hb.ProviderHealthPct = math.Max(0, float64(total-snap.OpenCircuits)/float64(total)*100)
	}

	if snap.QueueDepth == 0 {
		hb.QueueHealthPct = 100
	} else if snap.QueueDepth <= 5 {
		hb.QueueHealthPct = 75
	} else if snap.QueueDepth <= 20 {
		hb.QueueHealthPct = 50
	} else {
		hb.QueueHealthPct = 25
	}

	hb.ErrorRatePct = snap.LLMErrorRate * 100

	// Summary detail.
	if snap.HealthScore >= 90 {
		hb.Details = "All systems operating normally."
	} else if snap.HealthScore >= 70 {
		hb.Details = "Minor degradation detected — check provider health."
	} else if snap.HealthScore >= 50 {
		hb.Details = "Significant issues — review circuit breakers and error rates."
	} else {
		hb.Details = "System degraded — immediate attention required."
	}

	return hb
}

// ── Dashboard Data ──────────────────────────────────────────────────────────

// GetDashboard builds the full observability dashboard.
func (o *ObservabilityService) GetDashboard(ctx context.Context, hours int) (*ObservabilityDashboard, error) {
	if hours <= 0 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	dash := &ObservabilityDashboard{
		UptimeSeconds: time.Since(o.startedAt).Seconds(),
	}

	// Current snapshot (latest in-memory or latest from DB).
	o.mu.RLock()
	dash.Current = o.latestSnapshot
	o.mu.RUnlock()

	if dash.Current == nil {
		// Fall back to DB.
		snap := &MetricsSnapshot{}
		err := o.db.QueryRow(ctx, `
			SELECT id, active_tasks, pending_tasks, completed_tasks, failed_tasks,
			       online_agents, busy_agents, active_teams, busy_teams,
			       llm_calls, llm_tokens, llm_avg_latency_ms, llm_error_rate,
			       probe_calls, consensus_runs, consensus_avg_confidence,
			       open_circuits, heal_events, estimated_cost_usd,
			       queue_depth, agent_utilisation, health_score, created_at
			FROM swarm_metrics_snapshots
			ORDER BY created_at DESC LIMIT 1
		`).Scan(
			&snap.ID, &snap.ActiveTasks, &snap.PendingTasks, &snap.CompletedTasks, &snap.FailedTasks,
			&snap.OnlineAgents, &snap.BusyAgents, &snap.ActiveTeams, &snap.BusyTeams,
			&snap.LLMCalls, &snap.LLMTokens, &snap.LLMAvgLatencyMs, &snap.LLMErrorRate,
			&snap.ProbeCalls, &snap.ConsensusRuns, &snap.ConsensusAvgConf,
			&snap.OpenCircuits, &snap.HealEvents, &snap.EstimatedCostUSD,
			&snap.QueueDepth, &snap.AgentUtilisation, &snap.HealthScore, &snap.CreatedAt,
		)
		if err == nil {
			dash.Current = snap
		}
	}

	// Time-series.
	timeSeries, err := o.getTimeSeries(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("observability: time-series: %w", err)
	}
	dash.TimeSeries = timeSeries

	// Provider performance (latest per provider).
	provPerf, err := o.getLatestProviderPerf(ctx)
	if err != nil {
		return nil, fmt.Errorf("observability: provider-perf: %w", err)
	}
	dash.ProviderPerf = provPerf

	// Provider time-series (for charts).
	provTS, err := o.getProviderTimeSeries(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("observability: provider-ts: %w", err)
	}
	dash.ProviderTimeSeries = provTS

	// Health breakdown.
	if dash.Current != nil {
		dash.HealthBreakdown = o.buildHealthBreakdown(dash.Current)
	}

	// Cost summary.
	costSummary, err := o.getCostSummary(ctx)
	if err != nil {
		slog.Warn("observability: cost summary error", "error", err)
	} else {
		dash.CostSummary = costSummary
	}

	return dash, nil
}

// GetTimeSeries returns metric snapshots for a time range.
func (o *ObservabilityService) GetTimeSeries(ctx context.Context, hours int) ([]MetricsSnapshot, error) {
	if hours <= 0 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	return o.getTimeSeries(ctx, since)
}

// GetProviderPerf returns per-provider performance data.
func (o *ObservabilityService) GetProviderPerf(ctx context.Context, provider string, hours int) ([]ProviderPerfSnapshot, error) {
	if hours <= 0 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	q := `
		SELECT id, provider, calls, successes, failures, tokens_used,
		       avg_latency_ms, p95_latency_ms, p99_latency_ms,
		       error_rate, estimated_cost_usd, consensus_wins, consensus_total,
		       created_at
		FROM swarm_provider_perf_log
		WHERE created_at >= $1
	`
	args := []interface{}{since}
	if provider != "" {
		q += " AND provider = $2"
		args = append(args, provider)
	}
	q += " ORDER BY created_at ASC"

	rows, err := o.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProviderPerfSnapshot
	for rows.Next() {
		var p ProviderPerfSnapshot
		if err := rows.Scan(
			&p.ID, &p.Provider, &p.Calls, &p.Successes, &p.Failures, &p.TokensUsed,
			&p.AvgLatencyMs, &p.P95LatencyMs, &p.P99LatencyMs,
			&p.ErrorRate, &p.EstimatedCostUSD, &p.ConsensusWins, &p.ConsensusTotal,
			&p.CreatedAt,
		); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// GetCostBudget returns the current month's cost budget.
func (o *ObservabilityService) GetCostBudget(ctx context.Context) (*CostBudget, error) {
	now := time.Now()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	var b CostBudget
	err := o.db.QueryRow(ctx, `
		SELECT id, scope, month, budget_usd, spent_usd, alert_threshold, created_at, updated_at
		FROM swarm_cost_budget
		WHERE scope = 'global' AND month = $1
	`, month).Scan(&b.ID, &b.Scope, &b.Month, &b.BudgetUSD, &b.SpentUSD, &b.AlertThreshold, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// SetCostBudget upserts the global cost budget for the current month.
func (o *ObservabilityService) SetCostBudget(ctx context.Context, budgetUSD, alertThreshold float64) error {
	now := time.Now()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	if alertThreshold <= 0 || alertThreshold > 1 {
		alertThreshold = 0.8
	}

	_, err := o.db.Exec(ctx, `
		INSERT INTO swarm_cost_budget (scope, month, budget_usd, alert_threshold)
		VALUES ('global', $1, $2, $3)
		ON CONFLICT (scope, month)
		DO UPDATE SET budget_usd = EXCLUDED.budget_usd,
		              alert_threshold = EXCLUDED.alert_threshold,
		              updated_at = now()
	`, month, budgetUSD, alertThreshold)
	return err
}

// ── Internal Queries ────────────────────────────────────────────────────────

func (o *ObservabilityService) getTimeSeries(ctx context.Context, since time.Time) ([]MetricsSnapshot, error) {
	rows, err := o.db.Query(ctx, `
		SELECT id, active_tasks, pending_tasks, completed_tasks, failed_tasks,
		       online_agents, busy_agents, active_teams, busy_teams,
		       llm_calls, llm_tokens, llm_avg_latency_ms, llm_error_rate,
		       probe_calls, consensus_runs, consensus_avg_confidence,
		       open_circuits, heal_events, estimated_cost_usd,
		       queue_depth, agent_utilisation, health_score, created_at
		FROM swarm_metrics_snapshots
		WHERE created_at >= $1
		ORDER BY created_at ASC
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MetricsSnapshot
	for rows.Next() {
		var s MetricsSnapshot
		if err := rows.Scan(
			&s.ID, &s.ActiveTasks, &s.PendingTasks, &s.CompletedTasks, &s.FailedTasks,
			&s.OnlineAgents, &s.BusyAgents, &s.ActiveTeams, &s.BusyTeams,
			&s.LLMCalls, &s.LLMTokens, &s.LLMAvgLatencyMs, &s.LLMErrorRate,
			&s.ProbeCalls, &s.ConsensusRuns, &s.ConsensusAvgConf,
			&s.OpenCircuits, &s.HealEvents, &s.EstimatedCostUSD,
			&s.QueueDepth, &s.AgentUtilisation, &s.HealthScore, &s.CreatedAt,
		); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (o *ObservabilityService) getLatestProviderPerf(ctx context.Context) ([]ProviderPerfSnapshot, error) {
	rows, err := o.db.Query(ctx, `
		SELECT DISTINCT ON (provider)
		       id, provider, calls, successes, failures, tokens_used,
		       avg_latency_ms, p95_latency_ms, p99_latency_ms,
		       error_rate, estimated_cost_usd, consensus_wins, consensus_total,
		       created_at
		FROM swarm_provider_perf_log
		ORDER BY provider, created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProviderPerfSnapshot
	for rows.Next() {
		var p ProviderPerfSnapshot
		if err := rows.Scan(
			&p.ID, &p.Provider, &p.Calls, &p.Successes, &p.Failures, &p.TokensUsed,
			&p.AvgLatencyMs, &p.P95LatencyMs, &p.P99LatencyMs,
			&p.ErrorRate, &p.EstimatedCostUSD, &p.ConsensusWins, &p.ConsensusTotal,
			&p.CreatedAt,
		); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (o *ObservabilityService) getProviderTimeSeries(ctx context.Context, since time.Time) (map[string][]ProviderPerfSnapshot, error) {
	rows, err := o.db.Query(ctx, `
		SELECT id, provider, calls, successes, failures, tokens_used,
		       avg_latency_ms, p95_latency_ms, p99_latency_ms,
		       error_rate, estimated_cost_usd, consensus_wins, consensus_total,
		       created_at
		FROM swarm_provider_perf_log
		WHERE created_at >= $1
		ORDER BY provider, created_at ASC
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]ProviderPerfSnapshot)
	for rows.Next() {
		var p ProviderPerfSnapshot
		if err := rows.Scan(
			&p.ID, &p.Provider, &p.Calls, &p.Successes, &p.Failures, &p.TokensUsed,
			&p.AvgLatencyMs, &p.P95LatencyMs, &p.P99LatencyMs,
			&p.ErrorRate, &p.EstimatedCostUSD, &p.ConsensusWins, &p.ConsensusTotal,
			&p.CreatedAt,
		); err != nil {
			continue
		}
		out[p.Provider] = append(out[p.Provider], p)
	}
	return out, nil
}

func (o *ObservabilityService) getCostSummary(ctx context.Context) (*CostSummary, error) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	weekStart := todayStart.AddDate(0, 0, -int(now.Weekday()))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	cs := &CostSummary{
		ByProvider: make(map[string]float64),
	}

	// Today.
	_ = o.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(estimated_cost_usd),0) FROM swarm_provider_perf_log WHERE created_at >= $1
	`, todayStart).Scan(&cs.TodayUSD)

	// This week.
	_ = o.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(estimated_cost_usd),0) FROM swarm_provider_perf_log WHERE created_at >= $1
	`, weekStart).Scan(&cs.ThisWeekUSD)

	// This month.
	_ = o.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(estimated_cost_usd),0) FROM swarm_provider_perf_log WHERE created_at >= $1
	`, monthStart).Scan(&cs.ThisMonthUSD)

	// By provider (this month).
	rows, err := o.db.Query(ctx, `
		SELECT provider, COALESCE(SUM(estimated_cost_usd),0)
		FROM swarm_provider_perf_log
		WHERE created_at >= $1
		GROUP BY provider
	`, monthStart)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var prov string
			var cost float64
			if rows.Scan(&prov, &cost) == nil {
				cs.ByProvider[prov] = math.Round(cost*10000) / 10000
			}
		}
	}

	// Budget.
	budget, err := o.GetCostBudget(ctx)
	if err == nil {
		budget.SpentUSD = cs.ThisMonthUSD
		cs.Budget = budget
	}

	return cs, nil
}

// ── Garbage Collection ──────────────────────────────────────────────────────

func (o *ObservabilityService) gcOldData(ctx context.Context) {
	snapCutoff := time.Now().AddDate(0, 0, -observabilityRetentionDays)
	tag, err := o.db.Exec(ctx, `
		DELETE FROM swarm_metrics_snapshots WHERE created_at < $1
	`, snapCutoff)
	if err != nil {
		slog.Warn("observability: gc snapshots failed", "error", err)
	} else if tag.RowsAffected() > 0 {
		slog.Info("observability: gc'd old snapshots", "count", tag.RowsAffected())
	}

	perfCutoff := time.Now().AddDate(0, 0, -providerPerfRetentionDays)
	tag, err = o.db.Exec(ctx, `
		DELETE FROM swarm_provider_perf_log WHERE created_at < $1
	`, perfCutoff)
	if err != nil {
		slog.Warn("observability: gc provider perf failed", "error", err)
	} else if tag.RowsAffected() > 0 {
		slog.Info("observability: gc'd old provider perf", "count", tag.RowsAffected())
	}
}
