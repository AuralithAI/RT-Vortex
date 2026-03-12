package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
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

// Redis stream name for pending tasks.
const streamPending = "swarm:tasks:pending"

// Redis consumer group.
const consumerGroup = "swarm-controller"

// ── Task ────────────────────────────────────────────────────────────────────

// Task represents a swarm task from the database.
type Task struct {
	ID             uuid.UUID       `json:"id"`
	RepoID         string          `json:"repo_id"`
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
	CreatedAt      time.Time       `json:"created_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	TimeoutAt      *time.Time      `json:"timeout_at,omitempty"`
}

// ── TaskManager ─────────────────────────────────────────────────────────────

// TaskManager handles task lifecycle: creation, assignment, state transitions,
// and the assignLoop that matches pending tasks to idle teams.
type TaskManager struct {
	db    *pgxpool.Pool
	redis *redis.Client
	tm    *TeamManager

	mu     sync.Mutex
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

// Start launches the assignLoop goroutine.
func (m *TaskManager) Start(ctx context.Context) {
	ctx, m.cancel = context.WithCancel(ctx)

	// Ensure consumer group exists.
	_ = m.redis.XGroupCreateMkStream(ctx, streamPending, consumerGroup, "0").Err()

	go m.assignLoop(ctx)
	slog.Info("swarm task_manager started", "interval", AssignLoopInterval)
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
func (m *TaskManager) CreateTask(ctx context.Context, repoID, description string, submittedBy uuid.UUID) (*Task, error) {
	task := &Task{
		ID:          uuid.New(),
		RepoID:      repoID,
		Description: description,
		Status:      StatusSubmitted,
		SubmittedBy: &submittedBy,
		CreatedAt:   time.Now().UTC(),
	}

	_, err := m.db.Exec(ctx, `
		INSERT INTO swarm_tasks (id, repo_id, description, status, submitted_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		task.ID, task.RepoID, task.Description, task.Status, task.SubmittedBy, task.CreatedAt,
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

	slog.Info("swarm task created", "task_id", task.ID, "repo_id", repoID)
	return task, nil
}

// GetTask returns a single task by ID.
func (m *TaskManager) GetTask(ctx context.Context, taskID uuid.UUID) (*Task, error) {
	row := m.db.QueryRow(ctx, `
		SELECT id, repo_id, description, status, plan_document,
		       assigned_team_id, assigned_agents, pr_url, pr_number,
		       human_rating, human_comment, submitted_by,
		       created_at, completed_at, timeout_at
		FROM swarm_tasks WHERE id = $1`, taskID)

	var t Task
	err := row.Scan(
		&t.ID, &t.RepoID, &t.Description, &t.Status, &t.PlanDocument,
		&t.AssignedTeamID, &t.AssignedAgents, &t.PRUrl, &t.PRNumber,
		&t.HumanRating, &t.HumanComment, &t.SubmittedBy,
		&t.CreatedAt, &t.CompletedAt, &t.TimeoutAt,
	)
	if err != nil {
		return nil, fmt.Errorf("querying task %s: %w", taskID, err)
	}
	return &t, nil
}

// ListTasks returns tasks filtered by optional repo_id and status.
func (m *TaskManager) ListTasks(ctx context.Context, repoID, status string, limit int) ([]Task, error) {
	query := `SELECT id, repo_id, description, status, plan_document,
	                 assigned_team_id, assigned_agents, pr_url, pr_number,
	                 human_rating, human_comment, submitted_by,
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
			&t.ID, &t.RepoID, &t.Description, &t.Status, &t.PlanDocument,
			&t.AssignedTeamID, &t.AssignedAgents, &t.PRUrl, &t.PRNumber,
			&t.HumanRating, &t.HumanComment, &t.SubmittedBy,
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

	// Release the team.
	task, err := m.GetTask(ctx, taskID)
	if err == nil && task.AssignedTeamID != nil {
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

func (m *TaskManager) processAssignments(ctx context.Context) {
	// Read pending messages from the stream.
	result, err := m.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    consumerGroup,
		Consumer: "go-controller",
		Streams:  []string{streamPending, ">"},
		Count:    10,
		Block:    0,
	}).Result()
	if err != nil {
		if err != redis.Nil && ctx.Err() == nil {
			slog.Error("swarm assignLoop: XReadGroup failed", "error", err)
		}
		return
	}

	for _, stream := range result {
		for _, msg := range stream.Messages {
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

			// No idle team — request team creation if under max.
			if m.tm.CanCreateTeam() {
				// Publish creation event — Python will pick this up.
				m.redis.XAdd(ctx, &redis.XAddArgs{
					Stream: "swarm:events:team:create",
					Values: map[string]interface{}{
						"task_id": taskID.String(),
					},
				})
				slog.Info("swarm assignLoop: requested new team for task", "task_id", taskID)
				// Don't ACK — we'll assign when the team is ready.
				continue
			}

			// All teams busy at max — leave in stream (FIFO).
			slog.Debug("swarm assignLoop: all teams busy, task queued", "task_id", taskID)
		}
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
		_ = m.UpdateStatus(ctx, taskID, StatusTimedOut)

		// Release the team.
		task, err := m.GetTask(ctx, taskID)
		if err == nil && task.AssignedTeamID != nil {
			m.tm.ReleaseTeam(ctx, *task.AssignedTeamID)
		}
	}
}
