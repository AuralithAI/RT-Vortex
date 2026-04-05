package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

// ── Automatic CI Signal Ingestion ────────────────────────────────
//
// CISignalPoller is a background service that periodically checks
// completed swarm tasks that have open PRs. For each task it:
//
//  1. Polls the VCS platform for the PR merge state (open → merged / closed).
//  2. Polls the commit status / check-runs for the PR head SHA.
//  3. Records the results in swarm_ci_signals.
//  4. Once the PR is finalized (merged or closed) and CI is settled,
//     feeds the signals into the role-based ELO system.
//
// This closes the loop between "agent produced a PR" and "did that PR
// actually pass CI and get merged?", enabling fully automatic reward
// signals for the composite ELO formula.

// ── Constants ───────────────────────────────────────────────────────────────

const (
	CISignalPollInterval    = 2 * time.Minute  // how often the poller ticks
	CISignalMaxPollAge      = 7 * 24 * time.Hour // stop polling after 7 days
	CISignalMaxPolls        = 500                 // safety cap on total polls per signal
	CISignalBatchSize       = 50                  // max tasks to check per cycle
	CISignalCooldown        = 5 * time.Minute     // minimum time between polls of same task
)

// CISignal represents a row in swarm_ci_signals.
type CISignal struct {
	ID            uuid.UUID       `json:"id"`
	TaskID        uuid.UUID       `json:"task_id"`
	RepoID        string          `json:"repo_id"`
	PRNumber      int             `json:"pr_number"`
	PRState       string          `json:"pr_state"`       // open, merged, closed, unknown
	PRMerged      bool            `json:"pr_merged"`
	CIState       string          `json:"ci_state"`       // pending, success, failure, error, unknown
	CITotal       int             `json:"ci_total"`
	CIPassed      int             `json:"ci_passed"`
	CIFailed      int             `json:"ci_failed"`
	CIPending     int             `json:"ci_pending"`
	CIDetails     json.RawMessage `json:"ci_details"`
	ELOIngested   bool            `json:"elo_ingested"`
	ELOIngestedAt *time.Time      `json:"elo_ingested_at,omitempty"`
	PollCount     int             `json:"poll_count"`
	LastPolledAt  *time.Time      `json:"last_polled_at,omitempty"`
	Finalized     bool            `json:"finalized"`
	FinalizedAt   *time.Time      `json:"finalized_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// CISignalSummary is a lightweight view for the UI.
type CISignalSummary struct {
	TaskID    uuid.UUID `json:"task_id"`
	PRState   string    `json:"pr_state"`
	PRMerged  bool      `json:"pr_merged"`
	CIState   string    `json:"ci_state"`
	CITotal   int       `json:"ci_total"`
	CIPassed  int       `json:"ci_passed"`
	CIFailed  int       `json:"ci_failed"`
	CIPending int       `json:"ci_pending"`
	Finalized bool      `json:"finalized"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ── CISignalPoller ──────────────────────────────────────────────────────────

// CISignalPoller runs the background CI signal ingestion loop.
type CISignalPoller struct {
	db          *pgxpool.Pool
	vcsResolver *vcs.Resolver
	roleELO     *RoleELOService
	wsHub       *WSHub
	interval    time.Duration
}

// NewCISignalPoller creates a new CI signal poller.
func NewCISignalPoller(
	db *pgxpool.Pool,
	vcsResolver *vcs.Resolver,
	roleELO *RoleELOService,
	wsHub *WSHub,
) *CISignalPoller {
	return &CISignalPoller{
		db:          db,
		vcsResolver: vcsResolver,
		roleELO:     roleELO,
		wsHub:       wsHub,
		interval:    CISignalPollInterval,
	}
}

// Start begins the periodic CI signal polling loop.
func (p *CISignalPoller) Start(ctx context.Context) {
	slog.Info("CI signal poller started", "interval", p.interval)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// Run once immediately on startup.
	p.runCycle(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("CI signal poller stopped")
			return
		case <-ticker.C:
			p.runCycle(ctx)
		}
	}
}

// runCycle performs a single polling cycle.
func (p *CISignalPoller) runCycle(ctx context.Context) {
	start := time.Now()

	// 1. Seed new CI signal records for completed tasks that have PRs but no signal row yet.
	seeded, err := p.seedNewSignals(ctx)
	if err != nil {
		slog.Error("CI signal poller: seed failed", "error", err)
	}

	// 2. Poll unfinalized signals.
	polled, ingested, finalized, err := p.pollSignals(ctx)
	if err != nil {
		slog.Error("CI signal poller: poll failed", "error", err)
	}

	duration := time.Since(start)
	total := seeded + polled + ingested + finalized
	if total > 0 {
		slog.Info("CI signal poller cycle",
			"duration_ms", duration.Milliseconds(),
			"seeded", seeded,
			"polled", polled,
			"ingested", ingested,
			"finalized", finalized,
		)
	}

	// Prometheus metrics.
	SwarmCISignalPollCycles.Inc()
	SwarmCISignalSeeded.Add(float64(seeded))
	SwarmCISignalPolled.Add(float64(polled))
	SwarmCISignalIngested.Add(float64(ingested))
	SwarmCISignalFinalized.Add(float64(finalized))
	SwarmCISignalCycleDuration.Observe(duration.Seconds())
}

// ── Seed ────────────────────────────────────────────────────────────────────

// seedNewSignals creates swarm_ci_signals rows for completed tasks that have
// a PR but don't yet have a signal tracking row.
func (p *CISignalPoller) seedNewSignals(ctx context.Context) (int, error) {
	tag, err := p.db.Exec(ctx, `
		INSERT INTO swarm_ci_signals (task_id, repo_id, pr_number)
		SELECT t.id, t.repo_id, t.pr_number
		FROM swarm_tasks t
		WHERE t.status IN ('completed', 'pr_creating')
		  AND t.pr_number > 0
		  AND t.pr_url IS NOT NULL
		  AND NOT EXISTS (
			SELECT 1 FROM swarm_ci_signals s WHERE s.task_id = t.id
		  )
		LIMIT $1
		ON CONFLICT (task_id) DO NOTHING`,
		CISignalBatchSize,
	)
	if err != nil {
		return 0, fmt.Errorf("seeding CI signals: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ── Poll ────────────────────────────────────────────────────────────────────

// pollSignals checks unfinalized CI signal records against VCS platforms.
func (p *CISignalPoller) pollSignals(ctx context.Context) (polled, ingested, finalized int, err error) {
	cooldownCutoff := time.Now().UTC().Add(-CISignalCooldown)

	rows, err := p.db.Query(ctx, `
		SELECT s.id, s.task_id, s.repo_id, s.pr_number, s.poll_count, s.created_at
		FROM swarm_ci_signals s
		WHERE NOT s.finalized
		  AND (s.last_polled_at IS NULL OR s.last_polled_at < $1)
		ORDER BY s.last_polled_at ASC NULLS FIRST
		LIMIT $2`,
		cooldownCutoff, CISignalBatchSize,
	)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("querying unfinalized CI signals: %w", err)
	}
	defer rows.Close()

	type signalJob struct {
		id        uuid.UUID
		taskID    uuid.UUID
		repoID    string
		prNumber  int
		pollCount int
		createdAt time.Time
	}

	var jobs []signalJob
	for rows.Next() {
		var j signalJob
		if err := rows.Scan(&j.id, &j.taskID, &j.repoID, &j.prNumber, &j.pollCount, &j.createdAt); err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	rows.Close()

	for _, j := range jobs {
		// Safety: stop polling after max age or max polls.
		if time.Since(j.createdAt) > CISignalMaxPollAge || j.pollCount >= CISignalMaxPolls {
			if err := p.finalizeSignal(ctx, j.id, "timeout"); err != nil {
				slog.Error("CI signal finalize (timeout) failed", "id", j.id, "error", err)
			}
			finalized++
			continue
		}

		didPoll, didIngest, didFinalize := p.pollSingleSignal(ctx, j.id, j.taskID, j.repoID, j.prNumber)
		if didPoll {
			polled++
		}
		if didIngest {
			ingested++
		}
		if didFinalize {
			finalized++
		}
	}

	return polled, ingested, finalized, nil
}

// pollSingleSignal polls the VCS for a single task's PR state and CI status.
func (p *CISignalPoller) pollSingleSignal(
	ctx context.Context,
	signalID, taskID uuid.UUID,
	repoID string,
	prNumber int,
) (polled, ingested, finalized bool) {
	repoUUID, err := uuid.Parse(repoID)
	if err != nil {
		slog.Error("CI signal: invalid repo_id", "repo_id", repoID, "error", err)
		_ = p.finalizeSignal(ctx, signalID, "invalid_repo_id")
		return false, false, true
	}

	platform, err := p.vcsResolver.ForRepo(ctx, repoUUID)
	if err != nil {
		slog.Warn("CI signal: cannot resolve VCS platform", "repo_id", repoID, "error", err)
		// Don't finalize — might be transient.
		return false, false, false
	}

	// Get repo owner/name.
	owner, repoName, err := p.getRepoInfo(ctx, repoUUID)
	if err != nil {
		slog.Warn("CI signal: cannot get repo info", "repo_id", repoID, "error", err)
		return false, false, false
	}

	// 1. Poll PR state.
	pr, err := platform.GetPullRequest(ctx, owner, repoName, prNumber)
	if err != nil {
		slog.Warn("CI signal: GetPullRequest failed",
			"repo", owner+"/"+repoName, "pr", prNumber, "error", err)
		_ = p.bumpPollCount(ctx, signalID)
		return true, false, false
	}

	prState := pr.State   // "open", "closed", "merged"
	prMerged := prState == "merged"
	headSHA := pr.HeadSHA

	// 2. Poll CI status on the head SHA.
	var ciState string = "unknown"
	var ciTotal, ciPassed, ciFailed, ciPending int
	var ciDetails json.RawMessage

	if headSHA != "" {
		combined, err := platform.GetCombinedStatus(ctx, owner, repoName, headSHA)
		if err != nil {
			slog.Warn("CI signal: GetCombinedStatus failed",
				"repo", owner+"/"+repoName, "sha", headSHA, "error", err)
		} else if combined != nil {
			ciState = string(combined.State)
			ciTotal = combined.Total
			ciPassed = combined.Passed
			ciFailed = combined.Failed
			ciPending = combined.Pending
			ciDetails, _ = json.Marshal(combined.Statuses)
		}
	}

	// 3. Update the signal record.
	now := time.Now().UTC()
	_, err = p.db.Exec(ctx, `
		UPDATE swarm_ci_signals SET
			pr_state       = $1,
			pr_merged      = $2,
			ci_state       = $3,
			ci_total       = $4,
			ci_passed      = $5,
			ci_failed      = $6,
			ci_pending     = $7,
			ci_details     = $8,
			poll_count     = poll_count + 1,
			last_polled_at = $9,
			updated_at     = $9
		WHERE id = $10`,
		prState, prMerged, ciState, ciTotal, ciPassed, ciFailed, ciPending,
		ciDetails, now, signalID,
	)
	if err != nil {
		slog.Error("CI signal: update failed", "id", signalID, "error", err)
		return true, false, false
	}

	polled = true

	// 4. Broadcast update via WebSocket.
	p.broadcastSignalUpdate(taskID, prState, prMerged, ciState, ciTotal, ciPassed, ciFailed)

	// 5. Check if we should finalize + ingest into ELO.
	// Finalize when: PR is merged or closed, AND CI is not pending.
	prSettled := prState == "merged" || prState == "closed"
	ciSettled := ciState != "pending" && ciState != "unknown"

	if prSettled && ciSettled {
		// Ingest into role ELO.
		if p.roleELO != nil {
			if err := p.ingestIntoELO(ctx, signalID, taskID, repoID, prMerged, ciState); err != nil {
				slog.Error("CI signal: ELO ingestion failed", "task_id", taskID, "error", err)
			} else {
				ingested = true
			}
		}

		// Mark finalized.
		if err := p.finalizeSignal(ctx, signalID, "settled"); err != nil {
			slog.Error("CI signal: finalize failed", "id", signalID, "error", err)
		} else {
			finalized = true
		}
	}

	return polled, ingested, finalized
}

// ── ELO Ingestion ───────────────────────────────────────────────────────────

// ingestIntoELO feeds CI signals into the role-based ELO system for every
// agent role that participated in the task.
func (p *CISignalPoller) ingestIntoELO(
	ctx context.Context,
	signalID, taskID uuid.UUID,
	repoID string,
	prMerged bool,
	ciState string,
) error {
	// Get the roles that participated in this task.
	roles, err := p.getTaskRoles(ctx, taskID)
	if err != nil {
		return fmt.Errorf("getting task roles: %w", err)
	}

	if len(roles) == 0 {
		slog.Debug("CI signal: no roles found for task, skipping ELO", "task_id", taskID)
		return nil
	}

	outcome := TaskOutcome{
		TaskID:       taskID.String(),
		PRAccepted:   prMerged,
		BuildSuccess: ciState == string(vcs.CommitStatusSuccess),
		TestsPassed:  ciState == string(vcs.CommitStatusSuccess),
	}

	for _, role := range roles {
		if err := p.roleELO.RecordRoleOutcome(ctx, role, repoID, outcome); err != nil {
			slog.Error("CI signal: RecordRoleOutcome failed",
				"role", role, "repo_id", repoID, "task_id", taskID, "error", err)
			continue
		}
		SwarmCISignalELOUpdates.WithLabelValues(role, ciState).Inc()
	}

	// Mark as ingested.
	now := time.Now().UTC()
	_, err = p.db.Exec(ctx, `
		UPDATE swarm_ci_signals SET
			elo_ingested    = TRUE,
			elo_ingested_at = $1,
			updated_at      = $1
		WHERE id = $2`,
		now, signalID,
	)
	return err
}

// ── Finalize ────────────────────────────────────────────────────────────────

func (p *CISignalPoller) finalizeSignal(ctx context.Context, signalID uuid.UUID, reason string) error {
	now := time.Now().UTC()
	_, err := p.db.Exec(ctx, `
		UPDATE swarm_ci_signals SET
			finalized    = TRUE,
			finalized_at = $1,
			updated_at   = $1
		WHERE id = $2`,
		now, signalID,
	)
	if err == nil {
		slog.Debug("CI signal finalized", "id", signalID, "reason", reason)
	}
	return err
}

func (p *CISignalPoller) bumpPollCount(ctx context.Context, signalID uuid.UUID) error {
	now := time.Now().UTC()
	_, err := p.db.Exec(ctx, `
		UPDATE swarm_ci_signals SET
			poll_count     = poll_count + 1,
			last_polled_at = $1,
			updated_at     = $1
		WHERE id = $2`,
		now, signalID,
	)
	return err
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// getRepoInfo looks up the owner and repo name from the repositories table.
func (p *CISignalPoller) getRepoInfo(ctx context.Context, repoID uuid.UUID) (owner, repoName string, err error) {
	var fullName string
	err = p.db.QueryRow(ctx,
		`SELECT full_name FROM repositories WHERE id = $1`, repoID,
	).Scan(&fullName)
	if err != nil {
		return "", "", fmt.Errorf("querying repo %s: %w", repoID, err)
	}
	parts := splitRepoFullName(fullName)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo full_name %q (expected owner/repo)", fullName)
	}
	return parts[0], parts[1], nil
}

// splitRepoFullName splits "owner/repo" into ["owner", "repo"].
func splitRepoFullName(fullName string) []string {
	idx := -1
	for i := 0; i < len(fullName); i++ {
		if fullName[i] == '/' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return []string{fullName}
	}
	return []string{fullName[:idx], fullName[idx+1:]}
}

// getTaskRoles returns the distinct agent roles for a task.
func (p *CISignalPoller) getTaskRoles(ctx context.Context, taskID uuid.UUID) ([]string, error) {
	rows, err := p.db.Query(ctx, `
		SELECT DISTINCT a.role
		FROM swarm_agents a
		JOIN swarm_tasks t ON a.id = ANY(t.assigned_agents)
		WHERE t.id = $1 AND a.role != ''`,
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			continue
		}
		roles = append(roles, role)
	}
	return roles, nil
}

// broadcastSignalUpdate sends a WebSocket event for CI signal updates.
func (p *CISignalPoller) broadcastSignalUpdate(
	taskID uuid.UUID,
	prState string, prMerged bool,
	ciState string, ciTotal, ciPassed, ciFailed int,
) {
	if p.wsHub == nil {
		return
	}

	p.wsHub.BroadcastTaskEvent("ci_signal_updated", taskID.String(), map[string]interface{}{
		"task_id":   taskID.String(),
		"pr_state":  prState,
		"pr_merged": prMerged,
		"ci_state":  ciState,
		"ci_total":  ciTotal,
		"ci_passed": ciPassed,
		"ci_failed": ciFailed,
	})
}

// ── Query Methods (used by handlers) ────────────────────────────────────────

// GetCISignal returns the CI signal record for a task (if any).
func GetCISignal(ctx context.Context, db *pgxpool.Pool, taskID uuid.UUID) (*CISignal, error) {
	var s CISignal
	err := db.QueryRow(ctx, `
		SELECT id, task_id, repo_id, pr_number, pr_state, pr_merged,
		       ci_state, ci_total, ci_passed, ci_failed, ci_pending,
		       ci_details, elo_ingested, elo_ingested_at,
		       poll_count, last_polled_at, finalized, finalized_at,
		       created_at, updated_at
		FROM swarm_ci_signals
		WHERE task_id = $1`,
		taskID,
	).Scan(
		&s.ID, &s.TaskID, &s.RepoID, &s.PRNumber, &s.PRState, &s.PRMerged,
		&s.CIState, &s.CITotal, &s.CIPassed, &s.CIFailed, &s.CIPending,
		&s.CIDetails, &s.ELOIngested, &s.ELOIngestedAt,
		&s.PollCount, &s.LastPolledAt, &s.Finalized, &s.FinalizedAt,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("getting CI signal for task %s: %w", taskID, err)
	}
	return &s, nil
}

// ListCISignals returns CI signal summaries for a repo.
func ListCISignals(ctx context.Context, db *pgxpool.Pool, repoID string, limit int) ([]CISignalSummary, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := db.Query(ctx, `
		SELECT task_id, pr_state, pr_merged, ci_state,
		       ci_total, ci_passed, ci_failed, ci_pending,
		       finalized, updated_at
		FROM swarm_ci_signals
		WHERE repo_id = $1
		ORDER BY updated_at DESC
		LIMIT $2`,
		repoID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing CI signals for repo %s: %w", repoID, err)
	}
	defer rows.Close()

	var results []CISignalSummary
	for rows.Next() {
		var s CISignalSummary
		if err := rows.Scan(
			&s.TaskID, &s.PRState, &s.PRMerged, &s.CIState,
			&s.CITotal, &s.CIPassed, &s.CIFailed, &s.CIPending,
			&s.Finalized, &s.UpdatedAt,
		); err != nil {
			continue
		}
		results = append(results, s)
	}
	return results, nil
}
