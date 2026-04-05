package swarm

// Self-Healing Pipeline.
//
// Provides automatic resilience for the swarm:
//   - Per-provider circuit breakers (closed → open → half-open → closed)
//   - Stuck-task detection and auto-recovery
//   - Provider failover tracking
//   - Self-heal event audit log
//
// The SelfHealService runs a background goroutine that periodically:
//   1. Scans for stuck tasks (in-progress beyond timeout threshold)
//   2. Probes half-open circuit breakers to see if a provider recovered
//   3. Garbage-collects old self-heal events
//
// Agents report provider successes/failures through the Go endpoints;
// the service aggregates these to trip/reset circuit breakers.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Circuit Breaker States ──────────────────────────────────────────────────

const (
	CircuitClosed   = "closed"
	CircuitOpen     = "open"
	CircuitHalfOpen = "half_open"
)

// ── Self-Heal Event Types ───────────────────────────────────────────────────

const (
	SHEventCircuitOpened      = "circuit_opened"
	SHEventCircuitClosed      = "circuit_closed"
	SHEventCircuitHalfOpen    = "circuit_half_open"
	SHEventTaskRetry          = "task_retry"
	SHEventTaskTimeoutRecover = "task_timeout_recovery"
	SHEventProviderFailover   = "provider_failover"
	SHEventAgentRestarted     = "agent_restarted"
	SHEventStuckTask          = "stuck_task_detected"
)

// ── Severity Levels ─────────────────────────────────────────────────────────

const (
	SeverityInfo     = "info"
	SeverityWarn     = "warn"
	SeverityCritical = "critical"
)

// ── Configuration ───────────────────────────────────────────────────────────

const (
	// selfHealLoopInterval is how often the background loop runs.
	selfHealLoopInterval = 30 * time.Second

	// circuitFailureThreshold is consecutive failures before opening a circuit.
	circuitFailureThreshold = 5

	// circuitOpenDuration is how long a circuit stays open before trying half-open.
	circuitOpenDuration = 2 * time.Minute

	// halfOpenMaxProbes is how many successes in half-open state reset the breaker.
	halfOpenMaxProbes = 3

	// stuckTaskThreshold is how long a task can be in-progress before being considered stuck.
	stuckTaskThreshold = 45 * time.Minute

	// eventRetentionDays is how many days to keep resolved self-heal events.
	eventRetentionDays = 30
)

// ── Data Types ──────────────────────────────────────────────────────────────

// ProviderCircuitState represents the current circuit-breaker state for a provider.
type ProviderCircuitState struct {
	ID                  uuid.UUID  `json:"id"`
	Provider            string     `json:"provider"`
	State               string     `json:"state"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	TotalFailures       int64      `json:"total_failures"`
	TotalSuccesses      int64      `json:"total_successes"`
	LastFailureAt       *time.Time `json:"last_failure_at,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	OpenUntil           *time.Time `json:"open_until,omitempty"`
	HalfOpenProbes      int        `json:"half_open_probes"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// SelfHealEvent is a single event in the self-heal audit log.
type SelfHealEvent struct {
	ID         uuid.UUID       `json:"id"`
	EventType  string          `json:"event_type"`
	Provider   string          `json:"provider,omitempty"`
	TaskID     *uuid.UUID      `json:"task_id,omitempty"`
	AgentID    *uuid.UUID      `json:"agent_id,omitempty"`
	Details    json.RawMessage `json:"details"`
	Severity   string          `json:"severity"`
	Resolved   bool            `json:"resolved"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

// SelfHealSummary is returned by the overview endpoint.
type SelfHealSummary struct {
	TotalEvents      int                    `json:"total_events"`
	UnresolvedEvents int                    `json:"unresolved_events"`
	OpenCircuits     int                    `json:"open_circuits"`
	HalfOpenCircuits int                    `json:"half_open_circuits"`
	ClosedCircuits   int                    `json:"closed_circuits"`
	Providers        []ProviderCircuitState `json:"providers"`
	RecentEvents     []SelfHealEvent        `json:"recent_events"`
}

// ProviderOutcomeReport is the body agents send to report a provider success/failure.
type ProviderOutcomeReport struct {
	Provider  string  `json:"provider"`
	Success   bool    `json:"success"`
	LatencyMs float64 `json:"latency_ms,omitempty"`
	ErrorMsg  string  `json:"error_msg,omitempty"`
	TaskID    string  `json:"task_id,omitempty"`
	AgentID   string  `json:"agent_id,omitempty"`
}

// ── SelfHealService ─────────────────────────────────────────────────────────

// SelfHealService manages circuit breakers and automatic recovery.
type SelfHealService struct {
	db      *pgxpool.Pool
	taskMgr *TaskManager
	wsHub   *WSHub

	// In-memory circuit breaker cache (authoritative; DB is for persistence/UI).
	mu       sync.RWMutex
	circuits map[string]*providerCircuit // keyed by provider name
}

type providerCircuit struct {
	state               string
	consecutiveFailures int
	totalFailures       int64
	totalSuccesses      int64
	lastFailureAt       *time.Time
	lastSuccessAt       *time.Time
	openUntil           *time.Time
	halfOpenProbes      int
}

// NewSelfHealService creates the self-heal service.
func NewSelfHealService(db *pgxpool.Pool, taskMgr *TaskManager, wsHub *WSHub) *SelfHealService {
	return &SelfHealService{
		db:       db,
		taskMgr:  taskMgr,
		wsHub:    wsHub,
		circuits: make(map[string]*providerCircuit),
	}
}

// ── Background Loop ─────────────────────────────────────────────────────────

// Start launches the self-heal background loop.  Blocks until ctx is cancelled.
func (s *SelfHealService) Start(ctx context.Context) {
	slog.Info("self-heal: background loop started",
		"interval", selfHealLoopInterval,
		"stuck_threshold", stuckTaskThreshold,
	)

	// Hydrate in-memory circuits from DB on startup.
	s.hydrateCircuits(ctx)

	ticker := time.NewTicker(selfHealLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("self-heal: background loop stopped")
			return
		case <-ticker.C:
			s.cycle(ctx)
		}
	}
}

func (s *SelfHealService) cycle(ctx context.Context) {
	start := time.Now()
	defer func() {
		SwarmSelfHealCycleDuration.Observe(time.Since(start).Seconds())
	}()

	s.detectStuckTasks(ctx)
	s.probeHalfOpenCircuits(ctx)
	s.gcOldEvents(ctx)
}

// ── Stuck Task Detection ────────────────────────────────────────────────────

func (s *SelfHealService) detectStuckTasks(ctx context.Context) {
	threshold := time.Now().Add(-stuckTaskThreshold)
	rows, err := s.db.Query(ctx, `
		SELECT id, status, assigned_team_id, retry_count, failure_reason
		FROM swarm_tasks
		WHERE status NOT IN ('completed', 'failed', 'cancelled', 'timed_out')
		  AND created_at < $1
		ORDER BY created_at ASC
		LIMIT 20
	`, threshold)
	if err != nil {
		slog.Warn("self-heal: failed to query stuck tasks", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var (
			taskID        uuid.UUID
			status        string
			teamID        *uuid.UUID
			retryCount    int
			failureReason string
		)
		if err := rows.Scan(&taskID, &status, &teamID, &retryCount, &failureReason); err != nil {
			continue
		}

		slog.Warn("self-heal: stuck task detected",
			"task_id", taskID, "status", status, "age_threshold", stuckTaskThreshold,
		)
		SwarmSelfHealEventsTotal.WithLabelValues(SHEventStuckTask).Inc()

		details := map[string]interface{}{
			"status":         status,
			"retry_count":    retryCount,
			"failure_reason": failureReason,
		}
		detailsJSON, _ := json.Marshal(details)

		s.recordEvent(ctx, SHEventStuckTask, "", &taskID, nil, detailsJSON, SeverityWarn)

		// Auto-recover: if retries remain, mark failed so the assign loop retries it.
		if retryCount < MaxRetries {
			s.recoverStuckTask(ctx, taskID, retryCount)
		} else {
			// Exhausted retries — force fail.
			_, _ = s.db.Exec(ctx, `
				UPDATE swarm_tasks SET status = 'timed_out', failure_reason = 'self-heal: stuck task exhausted retries',
				                       completed_at = now()
				WHERE id = $1`, taskID)
			SwarmTasksTotal.WithLabelValues("timed_out").Inc()
		}
	}
}

func (s *SelfHealService) recoverStuckTask(ctx context.Context, taskID uuid.UUID, retryCount int) {
	_, err := s.db.Exec(ctx, `
		UPDATE swarm_tasks
		SET status = 'submitted',
		    retry_count = retry_count + 1,
		    failure_reason = 'self-heal: automatic retry after stuck detection',
		    assigned_team_id = NULL,
		    timeout_at = NULL
		WHERE id = $1`, taskID)
	if err != nil {
		slog.Error("self-heal: failed to recover stuck task", "task_id", taskID, "error", err)
		return
	}

	SwarmSelfHealEventsTotal.WithLabelValues(SHEventTaskTimeoutRecover).Inc()
	SwarmTaskRetriesTotal.Inc()

	details, _ := json.Marshal(map[string]interface{}{
		"retry_count": retryCount + 1,
		"max_retries": MaxRetries,
		"action":      "resubmitted",
	})
	s.recordEvent(ctx, SHEventTaskTimeoutRecover, "", &taskID, nil, details, SeverityInfo)

	slog.Info("self-heal: stuck task resubmitted", "task_id", taskID, "retry", retryCount+1)

	// Notify WebSocket.
	if s.wsHub != nil {
		s.wsHub.BroadcastTaskEvent("self_heal_retry", taskID.String(), map[string]interface{}{
			"task_id":     taskID.String(),
			"retry_count": retryCount + 1,
		})
	}
}

// ── Half-Open Circuit Probing ───────────────────────────────────────────────

func (s *SelfHealService) probeHalfOpenCircuits(ctx context.Context) {
	s.mu.RLock()
	var halfOpen []string
	for provider, c := range s.circuits {
		if c.state == CircuitOpen && c.openUntil != nil && time.Now().After(*c.openUntil) {
			halfOpen = append(halfOpen, provider)
		}
	}
	s.mu.RUnlock()

	for _, provider := range halfOpen {
		s.transitionCircuit(ctx, provider, CircuitHalfOpen)
	}
}

// ── Circuit Breaker Operations ──────────────────────────────────────────────

// RecordProviderOutcome processes a provider success/failure report.
func (s *SelfHealService) RecordProviderOutcome(ctx context.Context, report ProviderOutcomeReport) error {
	s.mu.Lock()

	c, ok := s.circuits[report.Provider]
	if !ok {
		c = &providerCircuit{state: CircuitClosed}
		s.circuits[report.Provider] = c
	}

	now := time.Now()

	if report.Success {
		c.totalSuccesses++
		c.consecutiveFailures = 0
		c.lastSuccessAt = &now

		if c.state == CircuitHalfOpen {
			c.halfOpenProbes++
			if c.halfOpenProbes >= halfOpenMaxProbes {
				s.mu.Unlock()
				s.transitionCircuit(ctx, report.Provider, CircuitClosed)
				s.persistCircuit(ctx, report.Provider)
				return nil
			}
		}
	} else {
		c.totalFailures++
		c.consecutiveFailures++
		c.lastFailureAt = &now

		SwarmSelfHealProviderFailures.WithLabelValues(report.Provider).Inc()

		if c.consecutiveFailures >= circuitFailureThreshold && c.state == CircuitClosed {
			openUntil := now.Add(circuitOpenDuration)
			c.openUntil = &openUntil
			c.state = CircuitOpen
			s.mu.Unlock()
			s.transitionCircuit(ctx, report.Provider, CircuitOpen)
			s.persistCircuit(ctx, report.Provider)

			// Record failover event if there's a task context.
			if report.TaskID != "" {
				tid, _ := uuid.Parse(report.TaskID)
				details, _ := json.Marshal(map[string]interface{}{
					"error": report.ErrorMsg,
				})
				s.recordEvent(ctx, SHEventProviderFailover, report.Provider, &tid, nil, details, SeverityWarn)
			}
			return nil
		}
	}

	s.mu.Unlock()
	s.persistCircuit(ctx, report.Provider)
	return nil
}

// IsProviderAvailable checks if a provider's circuit breaker allows traffic.
func (s *SelfHealService) IsProviderAvailable(provider string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.circuits[provider]
	if !ok {
		return true // unknown provider = assume healthy
	}
	switch c.state {
	case CircuitOpen:
		return false
	case CircuitHalfOpen:
		return true // allow limited traffic
	default:
		return true
	}
}

// GetProviderState returns the circuit state for a provider.
func (s *SelfHealService) GetProviderState(provider string) *ProviderCircuitState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.circuits[provider]
	if !ok {
		return nil
	}
	return &ProviderCircuitState{
		Provider:            provider,
		State:               c.state,
		ConsecutiveFailures: c.consecutiveFailures,
		TotalFailures:       c.totalFailures,
		TotalSuccesses:      c.totalSuccesses,
		LastFailureAt:       c.lastFailureAt,
		LastSuccessAt:       c.lastSuccessAt,
		OpenUntil:           c.openUntil,
		HalfOpenProbes:      c.halfOpenProbes,
	}
}

// ── Summary / Dashboard ─────────────────────────────────────────────────────

// GetSummary builds a self-heal overview.
func (s *SelfHealService) GetSummary(ctx context.Context, limit int) (*SelfHealSummary, error) {
	if limit <= 0 {
		limit = 25
	}

	// Provider circuit states from DB.
	providers, err := s.listCircuitStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("self-heal summary: circuits: %w", err)
	}

	// Recent events.
	events, err := s.listRecentEvents(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("self-heal summary: events: %w", err)
	}

	// Counts.
	var totalEv, unresolvedEv int
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM swarm_self_heal_events`).Scan(&totalEv)
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM swarm_self_heal_events WHERE resolved = false`).Scan(&unresolvedEv)

	var openC, halfC, closedC int
	for _, p := range providers {
		switch p.State {
		case CircuitOpen:
			openC++
		case CircuitHalfOpen:
			halfC++
		default:
			closedC++
		}
	}

	return &SelfHealSummary{
		TotalEvents:      totalEv,
		UnresolvedEvents: unresolvedEv,
		OpenCircuits:     openC,
		HalfOpenCircuits: halfC,
		ClosedCircuits:   closedC,
		Providers:        providers,
		RecentEvents:     events,
	}, nil
}

// GetEvents returns self-heal events with filtering.
func (s *SelfHealService) GetEvents(ctx context.Context, eventType, severity, provider string, limit, offset int) ([]SelfHealEvent, int, error) {
	if limit <= 0 {
		limit = 50
	}

	where := "WHERE 1=1"
	args := []interface{}{}
	argIdx := 1

	if eventType != "" {
		where += fmt.Sprintf(" AND event_type = $%d", argIdx)
		args = append(args, eventType)
		argIdx++
	}
	if severity != "" {
		where += fmt.Sprintf(" AND severity = $%d", argIdx)
		args = append(args, severity)
		argIdx++
	}
	if provider != "" {
		where += fmt.Sprintf(" AND provider = $%d", argIdx)
		args = append(args, provider)
		argIdx++
	}

	var total int
	countQ := "SELECT count(*) FROM swarm_self_heal_events " + where
	err := s.db.QueryRow(ctx, countQ, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("self-heal events count: %w", err)
	}

	q := fmt.Sprintf(`
		SELECT id, event_type, COALESCE(provider,''), task_id, agent_id,
		       details, severity, resolved, resolved_at, created_at
		FROM swarm_self_heal_events %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("self-heal events: %w", err)
	}
	defer rows.Close()

	var events []SelfHealEvent
	for rows.Next() {
		var ev SelfHealEvent
		if err := rows.Scan(
			&ev.ID, &ev.EventType, &ev.Provider, &ev.TaskID, &ev.AgentID,
			&ev.Details, &ev.Severity, &ev.Resolved, &ev.ResolvedAt, &ev.CreatedAt,
		); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events, total, nil
}

// ResolveEvent marks an event as resolved.
func (s *SelfHealService) ResolveEvent(ctx context.Context, eventID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE swarm_self_heal_events
		SET resolved = true, resolved_at = now()
		WHERE id = $1 AND resolved = false
	`, eventID)
	if err != nil {
		return fmt.Errorf("resolve event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("event %s not found or already resolved", eventID)
	}
	return nil
}

// ResetCircuit manually resets a provider's circuit breaker to closed.
func (s *SelfHealService) ResetCircuit(ctx context.Context, provider string) error {
	s.mu.Lock()
	c, ok := s.circuits[provider]
	if ok {
		c.state = CircuitClosed
		c.consecutiveFailures = 0
		c.halfOpenProbes = 0
		c.openUntil = nil
	}
	s.mu.Unlock()

	_, err := s.db.Exec(ctx, `
		UPDATE swarm_provider_circuit_state
		SET state = 'closed', consecutive_failures = 0, half_open_probes = 0,
		    open_until = NULL, updated_at = now()
		WHERE provider = $1
	`, provider)
	if err != nil {
		return fmt.Errorf("reset circuit: %w", err)
	}

	details, _ := json.Marshal(map[string]interface{}{"action": "manual_reset"})
	s.recordEvent(ctx, SHEventCircuitClosed, provider, nil, nil, details, SeverityInfo)

	SwarmSelfHealCircuitTransitions.WithLabelValues(provider, CircuitClosed).Inc()

	slog.Info("self-heal: circuit manually reset", "provider", provider)
	return nil
}

// ── Internal Helpers ────────────────────────────────────────────────────────

func (s *SelfHealService) transitionCircuit(ctx context.Context, provider, newState string) {
	s.mu.Lock()
	c, ok := s.circuits[provider]
	if !ok {
		c = &providerCircuit{state: CircuitClosed}
		s.circuits[provider] = c
	}
	oldState := c.state
	c.state = newState

	if newState == CircuitHalfOpen {
		c.halfOpenProbes = 0
	}
	if newState == CircuitClosed {
		c.consecutiveFailures = 0
		c.halfOpenProbes = 0
		c.openUntil = nil
	}
	if newState == CircuitOpen {
		openUntil := time.Now().Add(circuitOpenDuration)
		c.openUntil = &openUntil
	}
	s.mu.Unlock()

	SwarmSelfHealCircuitTransitions.WithLabelValues(provider, newState).Inc()

	var sev string
	var evType string
	switch newState {
	case CircuitOpen:
		evType = SHEventCircuitOpened
		sev = SeverityCritical
	case CircuitHalfOpen:
		evType = SHEventCircuitHalfOpen
		sev = SeverityWarn
	case CircuitClosed:
		evType = SHEventCircuitClosed
		sev = SeverityInfo
	}

	details, _ := json.Marshal(map[string]interface{}{
		"old_state": oldState,
		"new_state": newState,
	})
	s.recordEvent(ctx, evType, provider, nil, nil, details, sev)

	s.persistCircuit(ctx, provider)

	slog.Info("self-heal: circuit transition",
		"provider", provider, "old", oldState, "new", newState,
	)

	if s.wsHub != nil {
		s.wsHub.BroadcastTaskEvent("self_heal_circuit", "", map[string]interface{}{
			"provider":  provider,
			"old_state": oldState,
			"new_state": newState,
		})
	}
}

func (s *SelfHealService) persistCircuit(ctx context.Context, provider string) {
	s.mu.RLock()
	c, ok := s.circuits[provider]
	if !ok {
		s.mu.RUnlock()
		return
	}
	// Copy values under lock.
	state := c.state
	cf := c.consecutiveFailures
	tf := c.totalFailures
	ts := c.totalSuccesses
	lf := c.lastFailureAt
	ls := c.lastSuccessAt
	ou := c.openUntil
	hp := c.halfOpenProbes
	s.mu.RUnlock()

	_, err := s.db.Exec(ctx, `
		INSERT INTO swarm_provider_circuit_state
			(provider, state, consecutive_failures, total_failures, total_successes,
			 last_failure_at, last_success_at, open_until, half_open_probes, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (provider)
		DO UPDATE SET
			state = EXCLUDED.state,
			consecutive_failures = EXCLUDED.consecutive_failures,
			total_failures = EXCLUDED.total_failures,
			total_successes = EXCLUDED.total_successes,
			last_failure_at = EXCLUDED.last_failure_at,
			last_success_at = EXCLUDED.last_success_at,
			open_until = EXCLUDED.open_until,
			half_open_probes = EXCLUDED.half_open_probes,
			updated_at = now()
	`, provider, state, cf, tf, ts, lf, ls, ou, hp)
	if err != nil {
		slog.Warn("self-heal: failed to persist circuit state", "provider", provider, "error", err)
	}
}

func (s *SelfHealService) recordEvent(
	ctx context.Context,
	eventType, provider string,
	taskID, agentID *uuid.UUID,
	details json.RawMessage,
	severity string,
) {
	if details == nil {
		details = json.RawMessage("{}")
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO swarm_self_heal_events
			(event_type, provider, task_id, agent_id, details, severity)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, eventType, provider, taskID, agentID, details, severity)
	if err != nil {
		slog.Warn("self-heal: failed to record event", "type", eventType, "error", err)
	}
}

func (s *SelfHealService) hydrateCircuits(ctx context.Context) {
	rows, err := s.db.Query(ctx, `
		SELECT provider, state, consecutive_failures, total_failures, total_successes,
		       last_failure_at, last_success_at, open_until, half_open_probes
		FROM swarm_provider_circuit_state
	`)
	if err != nil {
		slog.Warn("self-heal: failed to hydrate circuit state from DB", "error", err)
		return
	}
	defer rows.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for rows.Next() {
		var (
			provider string
			state    string
			cf       int
			tf       int64
			ts       int64
			lf       *time.Time
			ls       *time.Time
			ou       *time.Time
			hp       int
		)
		if err := rows.Scan(&provider, &state, &cf, &tf, &ts, &lf, &ls, &ou, &hp); err != nil {
			continue
		}
		s.circuits[provider] = &providerCircuit{
			state:               state,
			consecutiveFailures: cf,
			totalFailures:       tf,
			totalSuccesses:      ts,
			lastFailureAt:       lf,
			lastSuccessAt:       ls,
			openUntil:           ou,
			halfOpenProbes:      hp,
		}
		count++
	}
	slog.Info("self-heal: hydrated circuit state", "providers", count)
}

func (s *SelfHealService) listCircuitStates(ctx context.Context) ([]ProviderCircuitState, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, provider, state, consecutive_failures, total_failures, total_successes,
		       last_failure_at, last_success_at, open_until, half_open_probes,
		       created_at, updated_at
		FROM swarm_provider_circuit_state
		ORDER BY provider
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProviderCircuitState
	for rows.Next() {
		var p ProviderCircuitState
		if err := rows.Scan(
			&p.ID, &p.Provider, &p.State, &p.ConsecutiveFailures,
			&p.TotalFailures, &p.TotalSuccesses,
			&p.LastFailureAt, &p.LastSuccessAt, &p.OpenUntil, &p.HalfOpenProbes,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *SelfHealService) listRecentEvents(ctx context.Context, limit int) ([]SelfHealEvent, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, event_type, COALESCE(provider,''), task_id, agent_id,
		       details, severity, resolved, resolved_at, created_at
		FROM swarm_self_heal_events
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SelfHealEvent
	for rows.Next() {
		var ev SelfHealEvent
		if err := rows.Scan(
			&ev.ID, &ev.EventType, &ev.Provider, &ev.TaskID, &ev.AgentID,
			&ev.Details, &ev.Severity, &ev.Resolved, &ev.ResolvedAt, &ev.CreatedAt,
		); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

func (s *SelfHealService) gcOldEvents(ctx context.Context) {
	cutoff := time.Now().AddDate(0, 0, -eventRetentionDays)
	tag, err := s.db.Exec(ctx, `
		DELETE FROM swarm_self_heal_events
		WHERE resolved = true AND created_at < $1
	`, cutoff)
	if err != nil {
		slog.Warn("self-heal: gc failed", "error", err)
		return
	}
	if tag.RowsAffected() > 0 {
		slog.Info("self-heal: garbage collected old events", "count", tag.RowsAffected())
	}
}
