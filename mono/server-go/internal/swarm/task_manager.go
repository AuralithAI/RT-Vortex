package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	swarmauth "github.com/AuralithAI/rtvortex-server/internal/swarm/auth"
)

// ── Task Statuses ───────────────────────────────────────────────────────────

const (
	StatusSubmitted    = "submitted"
	StatusPlanning     = "planning"
	StatusPlanReview   = "plan_review"
	StatusImplementing = "implementing"
	StatusSelfReview   = "self_review"
	StatusDiffReview   = "diff_review"
	StatusPRCreating   = "pr_creating"
	StatusCompleted    = "completed"
	StatusCancelled    = "cancelled"
	StatusFailed       = "failed"
	StatusTimedOut     = "timed_out"
)

// TaskTimeout is the maximum time a task can run before being marked timed_out.
const TaskTimeout = 30 * time.Minute

// AssignLoopInterval is how often the assign loop checks for pending tasks.
const AssignLoopInterval = 1 * time.Second

// HeartbeatCheckInterval is how often the heartbeat monitor runs.
const HeartbeatCheckInterval = 15 * time.Second

// HeartbeatTimeout is how long an agent can go without a heartbeat before
// being marked offline.
const HeartbeatTimeout = 60 * time.Second

// StaleReapInterval is how often offline agents/teams are purged from the DB.
const StaleReapInterval = 30 * time.Second

// StaleOfflineThreshold is how long a row must be offline before deletion.
const StaleOfflineThreshold = 10 * time.Minute

// MaxRetries is the maximum number of times a failed/timed-out task can be retried.
const MaxRetries = 3

// MetricsRefreshInterval is how often swarm gauges are recomputed from the DB.
const MetricsRefreshInterval = 10 * time.Second

// Redis stream name for pending tasks.
const streamPending = "swarm:tasks:pending"

// Redis consumer group.
const consumerGroup = "swarm-controller"

// Redis stream for team-create events consumed by the Python swarm process.
const streamTeamCreate = "swarm:events:team:create"
const teamCreateConsumerGroup = "swarm-python"

// ── Task ────────────────────────────────────────────────────────────────────

// Task represents a swarm task from the database.
type Task struct {
	ID             uuid.UUID       `json:"id"`
	RepoID         string          `json:"repo_id"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Status         string          `json:"status"`
	PlanDocument   json.RawMessage `json:"plan_document,omitempty"`
	AssignedTeamID *uuid.UUID      `json:"assigned_team_id,omitempty"`
	AssignedAgents []uuid.UUID     `json:"assigned_agents,omitempty"`
	PRUrl          string          `json:"pr_url,omitempty"`
	PRNumber       int             `json:"pr_number,omitempty"`
	HumanRating    *int            `json:"human_rating,omitempty"`
	HumanComment   string          `json:"human_comment,omitempty"`
	SubmittedBy    *uuid.UUID      `json:"submitted_by,omitempty"`
	RetryCount     int             `json:"retry_count"`
	FailureReason  string          `json:"failure_reason,omitempty"`
	TeamFormation  json.RawMessage `json:"team_formation,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	TimeoutAt      *time.Time      `json:"timeout_at,omitempty"`
}

// TaskSummary is a lightweight projection for history / listing views.
type TaskSummary struct {
	ID          uuid.UUID  `json:"id"`
	RepoID      string     `json:"repo_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	RetryCount  int        `json:"retry_count"`
	PRUrl       string     `json:"pr_url,omitempty"`
	PRNumber    int        `json:"pr_number,omitempty"`
	HumanRating *int       `json:"human_rating,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	DiffCount   int        `json:"diff_count"`
	AgentCount  int        `json:"agent_count"`
	DurationSec *float64   `json:"duration_sec,omitempty"`
}

// ── TaskManager ─────────────────────────────────────────────────────────────

// TaskManager handles task lifecycle: creation, assignment, state transitions,
// and the assignLoop that matches pending tasks to idle teams.
type TaskManager struct {
	db      *pgxpool.Pool
	redis   *redis.Client
	tm      *TeamManager
	authSvc *swarmauth.Service
	wsHub   *WSHub

	cancel context.CancelFunc
}

// NewTaskManager creates a TaskManager wired to the database and Redis.
func NewTaskManager(db *pgxpool.Pool, redisClient *redis.Client, teamMgr *TeamManager) *TaskManager {
	return &TaskManager{
		db:    db,
		redis: redisClient,
		tm:    teamMgr,
	}
}

// SetWSHub sets the WebSocket hub for broadcasting events.
func (m *TaskManager) SetWSHub(hub *WSHub) {
	m.wsHub = hub
}

// SetAuthService sets the swarm auth service for token revocation on cleanup.
func (m *TaskManager) SetAuthService(svc *swarmauth.Service) {
	m.authSvc = svc
}

// Start launches the assignLoop, heartbeat monitor, and metrics refresh goroutines.
func (m *TaskManager) Start(ctx context.Context) {
	ctx, m.cancel = context.WithCancel(ctx)

	// Ensure consumer groups exist.
	_ = m.redis.XGroupCreateMkStream(ctx, streamPending, consumerGroup, "0").Err()
	_ = m.redis.XGroupCreateMkStream(ctx, streamTeamCreate, teamCreateConsumerGroup, "0").Err()

	// On startup, reconcile DB state with reality: any agent whose
	// heartbeat key is missing from Redis (server was down, worker
	// restarted, etc.) gets marked offline immediately so stale idle
	// teams don't get assigned new tasks.
	m.reconcileOnStartup(ctx)

	go m.assignLoop(ctx)
	go m.heartbeatMonitor(ctx)
	go m.staleReaper(ctx)
	go m.metricsRefreshLoop(ctx)
	slog.Info("swarm task_manager started",
		"assign_interval", AssignLoopInterval,
		"heartbeat_check", HeartbeatCheckInterval,
		"heartbeat_timeout", HeartbeatTimeout,
		"max_retries", MaxRetries,
	)
}

// Stop cancels the assignLoop.
func (m *TaskManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	slog.Info("swarm task_manager stopped")
}

// ── Task CRUD ───────────────────────────────────────────────────────────────

// CreateTask inserts a new task and publishes it to the Redis stream.
func (m *TaskManager) CreateTask(ctx context.Context, repoID, title, description string, submittedBy uuid.UUID) (*Task, error) {
	// If repoID looks like a slug ("owner/name"), resolve it to the
	// repository UUID.  The engine indexes by UUID, so all downstream
	// consumers (Python swarm, engine gRPC calls) need the UUID.
	if strings.Contains(repoID, "/") {
		parts := strings.SplitN(repoID, "/", 2)
		var resolved uuid.UUID
		err := m.db.QueryRow(ctx,
			`SELECT id FROM repositories WHERE owner = $1 AND name = $2`,
			parts[0], parts[1],
		).Scan(&resolved)
		if err == nil {
			slog.Debug("swarm CreateTask: resolved repo slug to UUID",
				"slug", repoID, "uuid", resolved)
			repoID = resolved.String()
		} else {
			slog.Warn("swarm CreateTask: could not resolve repo slug, using as-is",
				"slug", repoID, "error", err)
		}
	}

	task := &Task{
		ID:          uuid.New(),
		RepoID:      repoID,
		Title:       title,
		Description: description,
		Status:      StatusSubmitted,
		SubmittedBy: &submittedBy,
		CreatedAt:   time.Now().UTC(),
	}

	_, err := m.db.Exec(ctx, `
		INSERT INTO swarm_tasks (id, repo_id, title, description, status, submitted_by, created_at, retry_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0)`,
		task.ID, task.RepoID, task.Title, task.Description, task.Status, task.SubmittedBy, task.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting task: %w", err)
	}

	// Publish to Redis stream for the assignLoop to pick up.
	if err := m.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: streamPending,
		Values: map[string]interface{}{
			"task_id": task.ID.String(),
			"repo_id": task.RepoID,
		},
	}).Err(); err != nil {
		slog.Error("failed to publish task to Redis stream", "task_id", task.ID, "error", err)
		// Task is in DB — assignLoop can still pick it up via a fallback query.
	}

	SwarmTasksActive.Inc()
	slog.Info("swarm task created", "task_id", task.ID, "repo_id", repoID)
	return task, nil
}

// GetTask returns a single task by ID.
func (m *TaskManager) GetTask(ctx context.Context, taskID uuid.UUID) (*Task, error) {
	row := m.db.QueryRow(ctx, `
		SELECT id, repo_id, COALESCE(title, ''), description, status, plan_document,
		       assigned_team_id, assigned_agents,
		       COALESCE(pr_url, ''), COALESCE(pr_number, 0),
		       human_rating, COALESCE(human_comment, ''), submitted_by,
		       COALESCE(retry_count, 0), COALESCE(failure_reason, ''),
		       team_formation,
		       created_at, completed_at, timeout_at
		FROM swarm_tasks WHERE id = $1`, taskID)

	var t Task
	err := row.Scan(
		&t.ID, &t.RepoID, &t.Title, &t.Description, &t.Status, &t.PlanDocument,
		&t.AssignedTeamID, &t.AssignedAgents, &t.PRUrl, &t.PRNumber,
		&t.HumanRating, &t.HumanComment, &t.SubmittedBy,
		&t.RetryCount, &t.FailureReason,
		&t.TeamFormation,
		&t.CreatedAt, &t.CompletedAt, &t.TimeoutAt,
	)
	if err != nil {
		return nil, fmt.Errorf("querying task %s: %w", taskID, err)
	}
	return &t, nil
}

// ListTasks returns tasks filtered by optional repo_id and status.
func (m *TaskManager) ListTasks(ctx context.Context, repoID, status string, limit int) ([]Task, error) {
	query := `SELECT id, repo_id, COALESCE(title, ''), description, status, plan_document,
	                 assigned_team_id, assigned_agents,
	                 COALESCE(pr_url, ''), COALESCE(pr_number, 0),
	                 human_rating, COALESCE(human_comment, ''), submitted_by,
	                 COALESCE(retry_count, 0), COALESCE(failure_reason, ''),
	                 team_formation,
	                 created_at, completed_at, timeout_at
	          FROM swarm_tasks WHERE 1=1`
	args := []interface{}{}
	argIdx := 1

	if repoID != "" {
		query += fmt.Sprintf(" AND repo_id = $%d", argIdx)
		args = append(args, repoID)
		argIdx++
	}
	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	if limit <= 0 {
		limit = 50
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := m.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(
			&t.ID, &t.RepoID, &t.Title, &t.Description, &t.Status, &t.PlanDocument,
			&t.AssignedTeamID, &t.AssignedAgents, &t.PRUrl, &t.PRNumber,
			&t.HumanRating, &t.HumanComment, &t.SubmittedBy,
			&t.RetryCount, &t.FailureReason,
			&t.TeamFormation,
			&t.CreatedAt, &t.CompletedAt, &t.TimeoutAt,
		); err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// UpdateStatus transitions a task to a new status.
func (m *TaskManager) UpdateStatus(ctx context.Context, taskID uuid.UUID, newStatus string) error {
	tag, err := m.db.Exec(ctx, `UPDATE swarm_tasks SET status = $1 WHERE id = $2`, newStatus, taskID)
	if err != nil {
		return fmt.Errorf("updating task status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}
	slog.Info("swarm task status updated", "task_id", taskID, "status", newStatus)
	return nil
}

// GetTaskAgents returns all agents assigned to a task's team with their roles and statuses.
func (m *TaskManager) GetTaskAgents(ctx context.Context, taskID uuid.UUID) ([]AgentSnapshot, error) {
	rows, err := m.db.Query(ctx, `
		SELECT a.id, a.role, a.status, COALESCE(a.team_id::text, '')
		FROM swarm_agents a
		JOIN swarm_tasks t ON a.team_id = t.assigned_team_id
		WHERE t.id = $1
		ORDER BY a.registered_at ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("querying task agents: %w", err)
	}
	defer rows.Close()

	var agents []AgentSnapshot
	for rows.Next() {
		var a AgentSnapshot
		var tid string
		if err := rows.Scan(&a.ID, &a.Role, &a.Status, &tid); err != nil {
			continue
		}
		a.TeamID = tid
		agents = append(agents, a)
	}
	return agents, nil
}

// DeleteTask removes a task and all its dependent rows (cascaded via FK).
func (m *TaskManager) DeleteTask(ctx context.Context, taskID uuid.UUID) error {
	tag, err := m.db.Exec(ctx, `DELETE FROM swarm_tasks WHERE id = $1`, taskID)
	if err != nil {
		return fmt.Errorf("deleting task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found", taskID)
	}
	SwarmTasksActive.Dec()
	slog.Info("swarm task deleted", "task_id", taskID)
	return nil
}

// SetPlan stores the plan document for a task and moves it to plan_review.
func (m *TaskManager) SetPlan(ctx context.Context, taskID uuid.UUID, plan json.RawMessage) error {
	_, err := m.db.Exec(ctx, `
		UPDATE swarm_tasks SET plan_document = $1, status = $2 WHERE id = $3`,
		plan, StatusPlanReview, taskID,
	)
	if err != nil {
		return fmt.Errorf("setting plan: %w", err)
	}
	slog.Info("swarm task plan submitted", "task_id", taskID)
	return nil
}

// revokeTeamTokens revokes JWT tokens and deletes heartbeat keys for all
// agents on a team. Called on task completion/failure to prevent ghost tokens
// lingering until the 3h Redis TTL.
func (m *TaskManager) revokeTeamTokens(ctx context.Context, teamID uuid.UUID) {
	if m.authSvc == nil {
		return
	}
	rows, err := m.db.Query(ctx,
		`SELECT id FROM swarm_agents WHERE team_id = $1`, teamID)
	if err != nil {
		slog.Warn("revokeTeamTokens: query failed", "team_id", teamID, "error", err)
		return
	}
	defer rows.Close()

	var revoked int
	for rows.Next() {
		var agentID uuid.UUID
		if err := rows.Scan(&agentID); err != nil {
			continue
		}
		_ = m.authSvc.Revoke(ctx, agentID.String())
		m.redis.Del(ctx, fmt.Sprintf("swarm:agent:heartbeat:%s", agentID))
		revoked++
	}
	if revoked > 0 {
		slog.Debug("revokeTeamTokens: revoked", "team_id", teamID, "count", revoked)
	}
}

// CompleteTask marks a task as completed.
func (m *TaskManager) CompleteTask(ctx context.Context, taskID uuid.UUID) error {
	now := time.Now().UTC()
	_, err := m.db.Exec(ctx, `
		UPDATE swarm_tasks SET status = $1, completed_at = $2 WHERE id = $3`,
		StatusCompleted, now, taskID,
	)
	if err != nil {
		return fmt.Errorf("completing task: %w", err)
	}

	// Record metrics.
	SwarmTasksTotal.WithLabelValues(StatusCompleted).Inc()
	SwarmTasksActive.Dec()

	// Record duration.
	task, tErr := m.GetTask(ctx, taskID)
	if tErr == nil {
		duration := now.Sub(task.CreatedAt).Seconds()
		SwarmTaskDuration.Observe(duration)
	}

	// Revoke agent tokens and release the team.
	if task != nil && task.AssignedTeamID != nil {
		m.revokeTeamTokens(ctx, *task.AssignedTeamID)
		m.tm.ReleaseTeam(ctx, *task.AssignedTeamID)
	}

	slog.Info("swarm task completed", "task_id", taskID)
	return nil
}

// ── Diffs ───────────────────────────────────────────────────────────────────

// TaskDiff represents a file diff produced by an agent.
type TaskDiff struct {
	ID          uuid.UUID  `json:"id"`
	TaskID      uuid.UUID  `json:"task_id"`
	FilePath    string     `json:"file_path"`
	ChangeType  string     `json:"change_type"`
	Original    string     `json:"original"`
	Proposed    string     `json:"proposed"`
	UnifiedDiff string     `json:"unified_diff"`
	AgentID     *uuid.UUID `json:"agent_id,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
}

// DiffMeta is a lightweight diff projection without file content.
type DiffMeta struct {
	ID         uuid.UUID  `json:"id"`
	TaskID     uuid.UUID  `json:"task_id"`
	FilePath   string     `json:"file_path"`
	ChangeType string     `json:"change_type"`
	AgentID    *uuid.UUID `json:"agent_id,omitempty"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	LineCount  int        `json:"line_count"`
}

// AddDiff inserts a new file diff for a task.
func (m *TaskManager) AddDiff(ctx context.Context, taskID uuid.UUID, diff TaskDiff) (*TaskDiff, error) {
	diff.ID = uuid.New()
	diff.TaskID = taskID
	diff.Status = "pending"
	diff.CreatedAt = time.Now().UTC()

	_, err := m.db.Exec(ctx, `
		INSERT INTO swarm_task_diffs
			(id, task_id, file_path, change_type, original, proposed, unified_diff, agent_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		diff.ID, diff.TaskID, diff.FilePath, diff.ChangeType,
		diff.Original, diff.Proposed, diff.UnifiedDiff,
		diff.AgentID, diff.Status, diff.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting diff: %w", err)
	}
	return &diff, nil
}

// ListDiffs returns all diffs for a given task.
func (m *TaskManager) ListDiffs(ctx context.Context, taskID uuid.UUID) ([]TaskDiff, error) {
	rows, err := m.db.Query(ctx, `
		SELECT id, task_id, file_path, change_type, original, proposed,
		       unified_diff, agent_id, status, created_at
		FROM swarm_task_diffs WHERE task_id = $1 ORDER BY created_at`, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing diffs: %w", err)
	}
	defer rows.Close()

	var diffs []TaskDiff
	for rows.Next() {
		var d TaskDiff
		if err := rows.Scan(
			&d.ID, &d.TaskID, &d.FilePath, &d.ChangeType, &d.Original, &d.Proposed,
			&d.UnifiedDiff, &d.AgentID, &d.Status, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning diff: %w", err)
		}
		diffs = append(diffs, d)
	}
	return diffs, nil
}

// ListDiffsMeta returns lightweight diff metadata without file content.
func (m *TaskManager) ListDiffsMeta(ctx context.Context, taskID uuid.UUID) ([]DiffMeta, error) {
	rows, err := m.db.Query(ctx, `
		SELECT id, task_id, file_path, change_type, agent_id, status, created_at,
		       COALESCE(array_length(string_to_array(unified_diff, E'\n'), 1), 0)
		FROM swarm_task_diffs WHERE task_id = $1 ORDER BY created_at`, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing diff meta: %w", err)
	}
	defer rows.Close()

	var metas []DiffMeta
	for rows.Next() {
		var d DiffMeta
		if err := rows.Scan(
			&d.ID, &d.TaskID, &d.FilePath, &d.ChangeType,
			&d.AgentID, &d.Status, &d.CreatedAt, &d.LineCount,
		); err != nil {
			return nil, fmt.Errorf("scanning diff meta: %w", err)
		}
		metas = append(metas, d)
	}
	return metas, nil
}

// GetDiff returns a single diff by ID including full content.
func (m *TaskManager) GetDiff(ctx context.Context, diffID uuid.UUID) (*TaskDiff, error) {
	row := m.db.QueryRow(ctx, `
		SELECT id, task_id, file_path, change_type, original, proposed,
		       unified_diff, agent_id, status, created_at
		FROM swarm_task_diffs WHERE id = $1`, diffID)

	var d TaskDiff
	err := row.Scan(
		&d.ID, &d.TaskID, &d.FilePath, &d.ChangeType, &d.Original, &d.Proposed,
		&d.UnifiedDiff, &d.AgentID, &d.Status, &d.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("querying diff %s: %w", diffID, err)
	}
	return &d, nil
}

// ── Diff Comments ───────────────────────────────────────────────────────────

// DiffComment represents a comment on a diff line.
type DiffComment struct {
	ID         uuid.UUID `json:"id"`
	DiffID     uuid.UUID `json:"diff_id"`
	AuthorType string    `json:"author_type"` // agent | user
	AuthorID   string    `json:"author_id"`
	LineNumber int       `json:"line_number"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

// AddDiffComment inserts a comment on a diff.
func (m *TaskManager) AddDiffComment(ctx context.Context, diffID uuid.UUID, c DiffComment) (*DiffComment, error) {
	c.ID = uuid.New()
	c.DiffID = diffID
	c.CreatedAt = time.Now().UTC()

	_, err := m.db.Exec(ctx, `
		INSERT INTO swarm_diff_comments (id, diff_id, author_type, author_id, line_number, content, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		c.ID, c.DiffID, c.AuthorType, c.AuthorID, c.LineNumber, c.Content, c.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting diff comment: %w", err)
	}
	return &c, nil
}

// ── Heartbeat ───────────────────────────────────────────────────────────────

// Heartbeat updates the agent's last-seen timestamp.
// Returns error if the agent is not found.
func (m *TaskManager) Heartbeat(ctx context.Context, agentID uuid.UUID) error {
	tag, err := m.db.Exec(ctx, `
		UPDATE swarm_agents SET status = CASE WHEN status = 'offline' THEN 'idle' ELSE status END
		WHERE id = $1`, agentID)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("agent %s not found", agentID)
	}

	// Store last heartbeat timestamp in Redis for the heartbeat monitor.
	hbKey := fmt.Sprintf("swarm:agent:heartbeat:%s", agentID)
	m.redis.Set(ctx, hbKey, time.Now().UTC().Unix(), HeartbeatTimeout*2)

	return nil
}

// ── Rate Task ───────────────────────────────────────────────────────────────

// RateTask records a human rating for a completed task.
func (m *TaskManager) RateTask(ctx context.Context, taskID uuid.UUID, rating int, comment string) error {
	_, err := m.db.Exec(ctx, `
		UPDATE swarm_tasks SET human_rating = $1, human_comment = $2 WHERE id = $3`,
		rating, comment, taskID,
	)
	return err
}

// AgentsForTask returns the unique agent IDs that submitted diffs for a task.
func (m *TaskManager) AgentsForTask(ctx context.Context, taskID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := m.db.Query(ctx, `
		SELECT DISTINCT agent_id FROM swarm_task_diffs
		WHERE task_id = $1 AND agent_id IS NOT NULL`, taskID)
	if err != nil {
		return nil, fmt.Errorf("agents for task: %w", err)
	}
	defer rows.Close()

	var agents []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		agents = append(agents, id)
	}
	return agents, nil
}

// ── Assign Loop ─────────────────────────────────────────────────────────────

// assignLoop runs every AssignLoopInterval, reading from the Redis stream and
// assigning tasks to idle teams.
func (m *TaskManager) assignLoop(ctx context.Context) {
	ticker := time.NewTicker(AssignLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.processAssignments(ctx)
			m.checkTimeouts(ctx)
		}
	}
}

// cleanupInconsistentAgents finds agents marked busy on idle/offline teams
// and sets them offline. This catches orphaned state from Python pod crashes
// or partial pipeline failures. Cost: 1 DB query per call.
func (m *TaskManager) cleanupInconsistentAgents(ctx context.Context) {
	rows, err := m.db.Query(ctx, `
		SELECT a.id FROM swarm_agents a
		JOIN swarm_teams t ON a.team_id = t.id
		WHERE t.status IN ('idle', 'offline') AND a.status = 'busy'`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var agentID uuid.UUID
		if rows.Scan(&agentID) != nil {
			continue
		}
		if m.authSvc != nil {
			_ = m.authSvc.Revoke(ctx, agentID.String())
		}
		m.redis.Del(ctx, fmt.Sprintf("swarm:agent:heartbeat:%s", agentID))
		_, _ = m.db.Exec(ctx, `UPDATE swarm_agents SET status = 'offline' WHERE id = $1`, agentID)
	}
}

func (m *TaskManager) processAssignments(ctx context.Context) {
	// Fix inconsistent state: agents marked busy on teams that are idle or
	// offline (crash recovery, partial pipeline failures).
	m.cleanupInconsistentAgents(ctx)

	// First, re-process any previously delivered but un-ACKed (pending) messages.
	// These arise when a team:create was published but the task wasn't assigned
	// because no idle team existed yet at that time.
	m.processPendingAssignments(ctx)

	// Then read new messages (non-blocking — use short timeout so we don't
	// starve the pending re-processing on the next tick).
	result, err := m.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    consumerGroup,
		Consumer: "go-controller",
		Streams:  []string{streamPending, ">"},
		Count:    10,
		Block:    500 * time.Millisecond,
	}).Result()
	if err != nil {
		if err != redis.Nil && ctx.Err() == nil {
			slog.Error("swarm assignLoop: XReadGroup failed", "error", err)
		}
		return
	}

	for _, stream := range result {
		m.handleStreamMessages(ctx, stream.Messages)
	}
}

// processPendingAssignments reads messages that were previously delivered to
// this consumer but never ACKed (e.g. because no idle team was available).
func (m *TaskManager) processPendingAssignments(ctx context.Context) {
	result, err := m.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    consumerGroup,
		Consumer: "go-controller",
		Streams:  []string{streamPending, "0"},
		Count:    10,
		Block:    100 * time.Millisecond,
	}).Result()
	if err != nil {
		if err != redis.Nil && ctx.Err() == nil {
			slog.Error("swarm assignLoop: XReadGroup pending failed", "error", err)
		}
		return
	}

	for _, stream := range result {
		if len(stream.Messages) == 0 {
			return // No pending messages.
		}
		m.handleStreamMessages(ctx, stream.Messages)
	}
}

func (m *TaskManager) handleStreamMessages(ctx context.Context, messages []redis.XMessage) {
	for _, msg := range messages {
		taskIDStr, ok := msg.Values["task_id"].(string)
		if !ok {
			slog.Warn("swarm assignLoop: missing task_id in stream message", "msg_id", msg.ID)
			m.redis.XAck(ctx, streamPending, consumerGroup, msg.ID)
			continue
		}

		taskID, err := uuid.Parse(taskIDStr)
		if err != nil {
			slog.Warn("swarm assignLoop: invalid task_id", "task_id", taskIDStr)
			m.redis.XAck(ctx, streamPending, consumerGroup, msg.ID)
			continue
		}

		// Check if this task is already assigned (e.g. via DeclareTeamSize auto-assign).
		task, err := m.GetTask(ctx, taskID)
		if err != nil {
			slog.Warn("swarm assignLoop: task not found", "task_id", taskID)
			m.redis.XAck(ctx, streamPending, consumerGroup, msg.ID)
			continue
		}
		if task.AssignedTeamID != nil {
			// Already assigned — ACK and move on.
			m.redis.XAck(ctx, streamPending, consumerGroup, msg.ID)
			continue
		}

		// ACK tasks in terminal states — no point re-queuing them.
		switch task.Status {
		case StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut:
			slog.Debug("swarm assignLoop: task in terminal state, ACKing",
				"task_id", taskID, "status", task.Status)
			m.redis.XAck(ctx, streamPending, consumerGroup, msg.ID)
			continue
		}

		// Try to assign to an idle team.
		team := m.tm.GetIdleTeam(ctx)
		if team != nil {
			if err := m.assignTask(ctx, taskID, team.ID); err != nil {
				slog.Error("swarm assignLoop: failed to assign task", "task_id", taskID, "team_id", team.ID, "error", err)
				continue // Don't ACK — retry next iteration.
			}
			m.redis.XAck(ctx, streamPending, consumerGroup, msg.ID)
			continue
		}

		// Task carving: for small tasks, borrow agents from an existing
		// idle team with enough capacity instead of creating a new team.
		complexity := classifyTaskComplexity(task.Description)
		if complexity == ComplexitySmall {
			lender := m.tm.FindIdleTeamWithCapacity(ctx, 3)
			if lender != nil {
				if err := m.assignTask(ctx, taskID, lender.ID); err != nil {
					slog.Error("swarm assignLoop: carve-assign failed",
						"task_id", taskID, "lender_team", lender.ID, "error", err)
					continue
				}
				slog.Info("swarm assignLoop: carved small task into existing team",
					"task_id", taskID, "team_id", lender.ID)
				m.redis.XAck(ctx, streamPending, consumerGroup, msg.ID)
				continue
			}
		}

		// No idle team — request team creation if under max.
		if m.tm.CanCreateTeam() {
			// Guard: only publish team:create once per task.  Use a
			// Redis key as a distributed lock so that repeated pending
			// re-reads don't flood the Python consumer with duplicate
			// events.  The key auto-expires after 5 minutes (safety net).
			dedup := fmt.Sprintf("swarm:team-create-sent:%s", taskID)
			set, err := m.redis.SetNX(ctx, dedup, "1", 5*time.Minute).Result()
			if err != nil {
				slog.Warn("swarm assignLoop: dedup SetNX failed", "task_id", taskID, "error", err)
			}
			if set {
				// First time — publish creation event for Python.
				m.redis.XAdd(ctx, &redis.XAddArgs{
					Stream: streamTeamCreate,
					Values: map[string]interface{}{
						"task_id": taskID.String(),
					},
				})
				slog.Info("swarm assignLoop: requested new team for task", "task_id", taskID)
			} else {
				slog.Debug("swarm assignLoop: team:create already sent for task, waiting", "task_id", taskID)
			}
			// ACK the pending message so we stop re-reading it.  The
			// dedup key + Python consumer handle delivery from here.
			m.redis.XAck(ctx, streamPending, consumerGroup, msg.ID)
			continue
		}

		// All teams busy at max — leave in stream (FIFO).
		slog.Debug("swarm assignLoop: all teams busy, task queued", "task_id", taskID)
	}
}

func (m *TaskManager) assignTask(ctx context.Context, taskID, teamID uuid.UUID) error {
	timeoutAt := time.Now().UTC().Add(TaskTimeout)
	_, err := m.db.Exec(ctx, `
		UPDATE swarm_tasks
		SET status = $1, assigned_team_id = $2, timeout_at = $3
		WHERE id = $4`,
		StatusPlanning, teamID, timeoutAt, taskID,
	)
	if err != nil {
		return err
	}

	m.tm.MarkTeamBusy(ctx, teamID)

	// Publish assignment event for Python.
	m.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: "swarm:events:task:assigned",
		Values: map[string]interface{}{
			"task_id": taskID.String(),
			"team_id": teamID.String(),
		},
	})

	slog.Info("swarm task assigned", "task_id", taskID, "team_id", teamID, "timeout_at", timeoutAt)
	return nil
}

// AssignTaskToTeam is a public wrapper around assignTask, used when an agent
// declares team size for a task that hasn't been formally assigned yet (race
// between the Python consumer creating teams on demand and the Go assign loop).
func (m *TaskManager) AssignTaskToTeam(ctx context.Context, taskID, teamID uuid.UUID) error {
	return m.assignTask(ctx, taskID, teamID)
}

// ── Task Carving ────────────────────────────────────────────────────────────

// TaskComplexity classifies a task description for smart team assignment.
type TaskComplexity string

const (
	ComplexitySmall  TaskComplexity = "small"
	ComplexityMedium TaskComplexity = "medium"
	ComplexityLarge  TaskComplexity = "large"
)

// multiFileKeywords indicate a task likely touches multiple files.
var multiFileKeywords = []string{
	"all", "across", "refactor", "migrate", "system",
	"architecture", "schema change", "entire", "every",
}

// classifyTaskComplexity uses heuristics (word count + keyword presence) to
// classify a task. No LLM call needed.
func classifyTaskComplexity(description string) TaskComplexity {
	words := strings.Fields(description)
	wordCount := len(words)
	lower := strings.ToLower(description)

	for _, kw := range multiFileKeywords {
		if strings.Contains(lower, kw) {
			if wordCount > 150 {
				return ComplexityLarge
			}
			return ComplexityMedium
		}
	}

	if wordCount < 50 {
		return ComplexitySmall
	}
	if wordCount > 150 {
		return ComplexityLarge
	}
	return ComplexityMedium
}

func (m *TaskManager) checkTimeouts(ctx context.Context) {
	now := time.Now().UTC()
	rows, err := m.db.Query(ctx, `
		SELECT id FROM swarm_tasks
		WHERE timeout_at IS NOT NULL AND timeout_at < $1
		  AND status NOT IN ($2, $3, $4, $5)`,
		now, StatusCompleted, StatusCancelled, StatusFailed, StatusTimedOut,
	)
	if err != nil {
		slog.Error("swarm checkTimeouts query failed", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var taskID uuid.UUID
		if err := rows.Scan(&taskID); err != nil {
			continue
		}
		slog.Warn("swarm task timed out", "task_id", taskID)

		// Record failure reason.
		_, _ = m.db.Exec(ctx, `
			UPDATE swarm_tasks SET status = $1, failure_reason = $2, completed_at = $3
			WHERE id = $4`,
			StatusTimedOut, "task exceeded 30 minute timeout", now, taskID,
		)

		SwarmTasksTotal.WithLabelValues(StatusTimedOut).Inc()
		SwarmTasksActive.Dec()

		// Broadcast timeout event.
		if m.wsHub != nil {
			m.wsHub.BroadcastTaskEvent("timed_out", taskID.String(), map[string]interface{}{
				"task_id": taskID.String(),
			})
		}

		// Revoke agent tokens and release the team.
		task, err := m.GetTask(ctx, taskID)
		if err == nil && task.AssignedTeamID != nil {
			m.revokeTeamTokens(ctx, *task.AssignedTeamID)
			m.tm.ReleaseTeam(ctx, *task.AssignedTeamID)
		}
	}
}

// ── Retry ───────────────────────────────────────────────────────────────────

// RetryTask resets a failed or timed-out task for re-execution.
// It increments the retry counter, clears the assignment, and re-publishes
// to the Redis stream. Returns error if the task is not retryable.
func (m *TaskManager) RetryTask(ctx context.Context, taskID uuid.UUID) error {
	task, err := m.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("retry: %w", err)
	}

	// Only failed or timed_out tasks can be retried.
	if task.Status != StatusFailed && task.Status != StatusTimedOut {
		return fmt.Errorf("task %s has status %q — only failed or timed_out tasks can be retried", taskID, task.Status)
	}

	if task.RetryCount >= MaxRetries {
		return fmt.Errorf("task %s has reached max retries (%d)", taskID, MaxRetries)
	}

	newRetry := task.RetryCount + 1
	_, err = m.db.Exec(ctx, `
		UPDATE swarm_tasks
		SET status = $1, retry_count = $2, failure_reason = '',
		    assigned_team_id = NULL, assigned_agents = NULL,
		    timeout_at = NULL, completed_at = NULL,
		    plan_document = NULL
		WHERE id = $3`,
		StatusSubmitted, newRetry, taskID,
	)
	if err != nil {
		return fmt.Errorf("retry update: %w", err)
	}

	// Re-publish to Redis stream.
	if pubErr := m.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: streamPending,
		Values: map[string]interface{}{
			"task_id": taskID.String(),
			"repo_id": task.RepoID,
		},
	}).Err(); pubErr != nil {
		slog.Error("retry: failed to republish task", "task_id", taskID, "error", pubErr)
	}

	SwarmTaskRetriesTotal.Inc()
	SwarmTasksActive.Inc()

	// Broadcast retry event.
	if m.wsHub != nil {
		m.wsHub.BroadcastTaskEvent("retried", taskID.String(), map[string]interface{}{
			"retry_count": newRetry,
		})
	}

	slog.Info("swarm task retried", "task_id", taskID, "retry", newRetry)
	return nil
}

// FailTask marks a task as failed with a reason.
func (m *TaskManager) FailTask(ctx context.Context, taskID uuid.UUID, reason string) error {
	now := time.Now().UTC()
	_, err := m.db.Exec(ctx, `
		UPDATE swarm_tasks SET status = $1, failure_reason = $2, completed_at = $3
		WHERE id = $4`,
		StatusFailed, reason, now, taskID,
	)
	if err != nil {
		return fmt.Errorf("failing task: %w", err)
	}

	SwarmTasksTotal.WithLabelValues(StatusFailed).Inc()
	SwarmTasksActive.Dec()

	// Revoke agent tokens and release the team.
	task, tErr := m.GetTask(ctx, taskID)
	if tErr == nil && task.AssignedTeamID != nil {
		m.revokeTeamTokens(ctx, *task.AssignedTeamID)
		m.tm.ReleaseTeam(ctx, *task.AssignedTeamID)
	}

	if m.wsHub != nil {
		m.wsHub.BroadcastTaskEvent("failed", taskID.String(), map[string]interface{}{
			"reason": reason,
		})
	}

	slog.Warn("swarm task failed", "task_id", taskID, "reason", reason)
	return nil
}

// ── Task History ────────────────────────────────────────────────────────────

// ListTaskHistory returns completed/failed/timed_out tasks with summary stats.
func (m *TaskManager) ListTaskHistory(ctx context.Context, repoID string, limit, offset int) ([]TaskSummary, int, error) {
	if limit <= 0 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}

	// Count total matching rows.
	countQuery := `SELECT COUNT(*) FROM swarm_tasks WHERE status IN ($1, $2, $3, $4)`
	countArgs := []interface{}{StatusCompleted, StatusFailed, StatusTimedOut, StatusCancelled}
	if repoID != "" {
		countQuery += ` AND repo_id = $5`
		countArgs = append(countArgs, repoID)
	}

	var total int
	if err := m.db.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting task history: %w", err)
	}

	// Fetch rows with aggregates.
	query := `
		SELECT t.id, t.repo_id, COALESCE(t.title, ''), t.description, t.status,
		       COALESCE(t.retry_count, 0), COALESCE(t.pr_url, ''), COALESCE(t.pr_number, 0),
		       t.human_rating, t.created_at, t.completed_at,
		       (SELECT COUNT(*) FROM swarm_task_diffs d WHERE d.task_id = t.id) AS diff_count,
		       COALESCE(array_length(t.assigned_agents, 1), 0) AS agent_count,
		       CASE WHEN t.completed_at IS NOT NULL
		            THEN EXTRACT(EPOCH FROM (t.completed_at - t.created_at))
		            ELSE NULL END AS duration_sec
		FROM swarm_tasks t
		WHERE t.status IN ($1, $2, $3, $4)`
	args := []interface{}{StatusCompleted, StatusFailed, StatusTimedOut, StatusCancelled}
	argIdx := 5

	if repoID != "" {
		query += fmt.Sprintf(` AND t.repo_id = $%d`, argIdx)
		args = append(args, repoID)
		argIdx++
	}
	query += fmt.Sprintf(` ORDER BY t.created_at DESC LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := m.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing task history: %w", err)
	}
	defer rows.Close()

	var summaries []TaskSummary
	for rows.Next() {
		var s TaskSummary
		if err := rows.Scan(
			&s.ID, &s.RepoID, &s.Title, &s.Description, &s.Status,
			&s.RetryCount, &s.PRUrl, &s.PRNumber,
			&s.HumanRating, &s.CreatedAt, &s.CompletedAt,
			&s.DiffCount, &s.AgentCount, &s.DurationSec,
		); err != nil {
			return nil, 0, fmt.Errorf("scanning task summary: %w", err)
		}
		summaries = append(summaries, s)
	}
	return summaries, total, nil
}

// ── Agent Task Log ──────────────────────────────────────────────────────────

// RecordAgentContribution logs an agent's contribution to a task.
func (m *TaskManager) RecordAgentContribution(ctx context.Context, taskID, agentID uuid.UUID,
	role, phase, contributionType string, tokensUsed, llmCalls, ragCalls int) error {

	_, err := m.db.Exec(ctx, `
		INSERT INTO agent_task_log
			(id, task_id, agent_id, role, phase, contribution_type,
			 tokens_used, llm_calls, rag_calls, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())`,
		uuid.New(), taskID, agentID, role, phase, contributionType,
		tokensUsed, llmCalls, ragCalls,
	)
	if err != nil {
		return fmt.Errorf("recording agent contribution: %w", err)
	}
	return nil
}

// ── Swarm Overview ──────────────────────────────────────────────────────────

// SwarmOverview holds a snapshot of the swarm's current state.
type SwarmOverview struct {
	ActiveTasks   int             `json:"active_tasks"`
	PendingTasks  int             `json:"pending_tasks"`
	CompletedAll  int             `json:"completed_all_time"`
	FailedAll     int             `json:"failed_all_time"`
	ActiveTeams   int             `json:"active_teams"`
	BusyTeams     int             `json:"busy_teams"`
	OnlineAgents  int             `json:"online_agents"`
	BusyAgents    int             `json:"busy_agents"`
	AvgDurationS  float64         `json:"avg_duration_seconds"`
	TotalRetries  int             `json:"total_retries"`
	LLMPercentage float64         `json:"llm_percentage"`
	Agents        []AgentSnapshot `json:"agents,omitempty"`
}

// AgentSnapshot is a lightweight view of an agent for the overview.
type AgentSnapshot struct {
	ID     uuid.UUID `json:"id"`
	Role   string    `json:"role"`
	Status string    `json:"status"`
	TeamID string    `json:"team_id"`
}

// GetOverview computes the current swarm overview from the database.
func (m *TaskManager) GetOverview(ctx context.Context) (*SwarmOverview, error) {
	o := &SwarmOverview{}

	// Active tasks (non-terminal).
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_tasks
		WHERE status NOT IN ($1, $2, $3, $4)`,
		StatusCompleted, StatusCancelled, StatusFailed, StatusTimedOut,
	).Scan(&o.ActiveTasks)

	// Pending tasks (submitted, not assigned).
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_tasks WHERE status = $1`, StatusSubmitted,
	).Scan(&o.PendingTasks)

	// All-time completed.
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_tasks WHERE status = $1`, StatusCompleted,
	).Scan(&o.CompletedAll)

	// All-time failed + timed out.
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_tasks WHERE status IN ($1, $2)`,
		StatusFailed, StatusTimedOut,
	).Scan(&o.FailedAll)

	// Teams.
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_teams WHERE status != 'offline'`,
	).Scan(&o.ActiveTeams)
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_teams WHERE status = 'busy'`,
	).Scan(&o.BusyTeams)

	// Agents.
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_agents WHERE status != 'offline'`,
	).Scan(&o.OnlineAgents)
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_agents WHERE status = 'busy'`,
	).Scan(&o.BusyAgents)

	// Average duration of completed tasks (last 100).
	_ = m.db.QueryRow(ctx, `
		SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (completed_at - created_at))), 0)
		FROM (SELECT completed_at, created_at FROM swarm_tasks
		      WHERE status = $1 AND completed_at IS NOT NULL
		      ORDER BY completed_at DESC LIMIT 100) sub`,
		StatusCompleted,
	).Scan(&o.AvgDurationS)

	// Total retries.
	_ = m.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(retry_count), 0) FROM swarm_tasks`,
	).Scan(&o.TotalRetries)

	// All non-offline agents with role, status, and team.
	rows, err := m.db.Query(ctx, `
		SELECT id, role, status, COALESCE(team_id::text, '')
		FROM swarm_agents WHERE status != 'offline'
		ORDER BY registered_at DESC`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var a AgentSnapshot
			var tid string
			if err := rows.Scan(&a.ID, &a.Role, &a.Status, &tid); err == nil {
				a.TeamID = tid
				o.Agents = append(o.Agents, a)
			}
		}
	}

	return o, nil
}

// ── Heartbeat Monitor ───────────────────────────────────────────────────────

// heartbeatMonitor periodically checks for agents that have missed heartbeats
// and marks them offline. If a busy agent goes offline, its team's task is
// marked as failed and eligible for retry.
func (m *TaskManager) heartbeatMonitor(ctx context.Context) {
	ticker := time.NewTicker(HeartbeatCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkHeartbeats(ctx)
		}
	}
}

func (m *TaskManager) checkHeartbeats(ctx context.Context) {
	// Find all non-offline agents.
	rows, err := m.db.Query(ctx, `
		SELECT id, team_id, status FROM swarm_agents WHERE status != 'offline'`)
	if err != nil {
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	for rows.Next() {
		var agentID uuid.UUID
		var teamID *uuid.UUID
		var status string
		if err := rows.Scan(&agentID, &teamID, &status); err != nil {
			continue
		}

		// Check Redis for last heartbeat.
		hbKey := fmt.Sprintf("swarm:agent:heartbeat:%s", agentID)
		val, err := m.redis.Get(ctx, hbKey).Int64()
		if err != nil {
			// No heartbeat key → agent never sent one or key expired.
			// Only mark offline if the key is truly missing (not a Redis error).
			if err == redis.Nil {
				m.markAgentOffline(ctx, agentID, teamID, status, now)
			}
			continue
		}

		lastHB := time.Unix(val, 0)
		if now.Sub(lastHB) > HeartbeatTimeout {
			m.markAgentOffline(ctx, agentID, teamID, status, now)
		}
	}
}

func (m *TaskManager) markAgentOffline(ctx context.Context, agentID uuid.UUID, teamID *uuid.UUID, prevStatus string, now time.Time) {
	_, _ = m.db.Exec(ctx, `UPDATE swarm_agents SET status = 'offline' WHERE id = $1`, agentID)
	SwarmAgentHeartbeatMisses.Inc()

	slog.Warn("swarm agent heartbeat missed, marked offline",
		"agent_id", agentID,
		"prev_status", prevStatus,
	)

	if m.wsHub != nil {
		m.wsHub.BroadcastAgentEvent("offline", "", agentID.String(), map[string]interface{}{
			"reason": "heartbeat_timeout",
		})
	}

	// If the agent was busy and part of a team, check if the team should be failed.
	if prevStatus == "busy" && teamID != nil {
		m.checkTeamHealth(ctx, *teamID)
	}
}

// checkTeamHealth verifies whether a team still has enough online agents.
// If the lead agent (orchestrator) is offline, fail the team's current task.
func (m *TaskManager) checkTeamHealth(ctx context.Context, teamID uuid.UUID) {
	// Count online agents in this team.
	var onlineCount int
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_agents
		WHERE team_id = $1 AND status != 'offline'`, teamID,
	).Scan(&onlineCount)

	if onlineCount == 0 {
		// No agents left — fail any active task assigned to this team.
		rows, err := m.db.Query(ctx, `
			SELECT id FROM swarm_tasks
			WHERE assigned_team_id = $1 AND status NOT IN ($2, $3, $4, $5)`,
			teamID, StatusCompleted, StatusCancelled, StatusFailed, StatusTimedOut,
		)
		if err != nil {
			return
		}
		defer rows.Close()

		for rows.Next() {
			var taskID uuid.UUID
			if err := rows.Scan(&taskID); err != nil {
				continue
			}
			_ = m.FailTask(ctx, taskID, "all team agents offline (heartbeat timeout)")
		}

		// Mark team offline.
		m.tm.MarkTeamOffline(ctx, teamID)
	}
}

// ── Metrics Refresh ─────────────────────────────────────────────────────────

// metricsRefreshLoop periodically recomputes gauge metrics from the database.
func (m *TaskManager) metricsRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(MetricsRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refreshMetrics(ctx)
		}
	}
}

func (m *TaskManager) refreshMetrics(ctx context.Context) {
	// Active tasks.
	var active int
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_tasks
		WHERE status NOT IN ($1, $2, $3, $4)`,
		StatusCompleted, StatusCancelled, StatusFailed, StatusTimedOut,
	).Scan(&active)
	SwarmTasksActive.Set(float64(active))

	// Queue depth (submitted but not assigned).
	var pending int
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM swarm_tasks WHERE status = $1`, StatusSubmitted,
	).Scan(&pending)
	SwarmQueueDepth.Set(float64(pending))

	// Teams.
	var activeTeams, busyTeams int
	_ = m.db.QueryRow(ctx, `SELECT COUNT(*) FROM swarm_teams WHERE status != 'offline'`).Scan(&activeTeams)
	_ = m.db.QueryRow(ctx, `SELECT COUNT(*) FROM swarm_teams WHERE status = 'busy'`).Scan(&busyTeams)
	SwarmTeamsActive.Set(float64(activeTeams))
	SwarmTeamsBusy.Set(float64(busyTeams))

	// Agents.
	var onlineAgents, busyAgents int
	_ = m.db.QueryRow(ctx, `SELECT COUNT(*) FROM swarm_agents WHERE status != 'offline'`).Scan(&onlineAgents)
	_ = m.db.QueryRow(ctx, `SELECT COUNT(*) FROM swarm_agents WHERE status = 'busy'`).Scan(&busyAgents)
	SwarmAgentsOnline.Set(float64(onlineAgents))
	if onlineAgents > 0 {
		SwarmAgentUtilisation.Set(float64(busyAgents) / float64(onlineAgents))
	} else {
		SwarmAgentUtilisation.Set(0)
	}
}

// ── Startup Reconciliation & Stale Reaper ───────────────────────────────────

// reconcileOnStartup marks agents without a valid Redis heartbeat as offline.
// This handles the case where the Go server restarted but Python workers
// didn't — their old agent rows sit as "idle" in Postgres with no heartbeat
// key, and GetIdleTeam could hand a dead team to a new task.
func (m *TaskManager) reconcileOnStartup(ctx context.Context) {
	rows, err := m.db.Query(ctx, `
		SELECT id, team_id, status FROM swarm_agents WHERE status != 'offline'`)
	if err != nil {
		slog.Error("swarm reconcile: failed to query agents", "error", err)
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	var reconciled int
	for rows.Next() {
		var agentID uuid.UUID
		var teamID *uuid.UUID
		var status string
		if err := rows.Scan(&agentID, &teamID, &status); err != nil {
			continue
		}

		hbKey := fmt.Sprintf("swarm:agent:heartbeat:%s", agentID)
		_, err := m.redis.Get(ctx, hbKey).Int64()
		if err == redis.Nil {
			m.markAgentOffline(ctx, agentID, teamID, status, now)
			reconciled++
		}
	}

	if reconciled > 0 {
		slog.Info("swarm reconcile: marked stale agents offline on startup", "count", reconciled)
	}
}

// staleReaper periodically deletes agents and teams that have been offline
// longer than StaleOfflineThreshold.  This prevents unbounded row growth
// across server/worker restarts.
func (m *TaskManager) staleReaper(ctx context.Context) {
	ticker := time.NewTicker(StaleReapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reapStaleRows(ctx)
		}
	}
}

func (m *TaskManager) reapStaleRows(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-StaleOfflineThreshold)

	// Revoke tokens for stale agents before deleting them from the DB.
	if m.authSvc != nil {
		rows, err := m.db.Query(ctx,
			`SELECT id FROM swarm_agents WHERE status = 'offline' AND registered_at < $1`, cutoff)
		if err == nil {
			for rows.Next() {
				var agentID uuid.UUID
				if rows.Scan(&agentID) == nil {
					_ = m.authSvc.Revoke(ctx, agentID.String())
					m.redis.Del(ctx, fmt.Sprintf("swarm:agent:heartbeat:%s", agentID))
				}
			}
			rows.Close()
		}
	}

	// Delete offline agents whose last registration is older than the threshold.
	agentResult, err := m.db.Exec(ctx, `
		DELETE FROM swarm_agents
		WHERE status = 'offline' AND registered_at < $1`, cutoff)
	if err != nil {
		slog.Error("swarm reaper: failed to delete stale agents", "error", err)
		return
	}
	agentCount := agentResult.RowsAffected()

	// Delete offline teams that have no remaining agents.
	teamResult, err := m.db.Exec(ctx, `
		DELETE FROM swarm_teams
		WHERE status = 'offline'
		  AND NOT EXISTS (
			SELECT 1 FROM swarm_agents WHERE team_id = swarm_teams.id
		  )`)
	if err != nil {
		slog.Error("swarm reaper: failed to delete stale teams", "error", err)
		return
	}
	teamCount := teamResult.RowsAffected()

	if agentCount > 0 || teamCount > 0 {
		slog.Info("swarm reaper: purged stale rows",
			"agents_deleted", agentCount,
			"teams_deleted", teamCount,
		)
	}
}
