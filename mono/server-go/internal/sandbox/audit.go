package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditAction string

const (
	AuditSecretAccess     AuditAction = "secret_access"
	AuditSecretDenied     AuditAction = "secret_denied"
	AuditContainerCreated AuditAction = "container_created"
	AuditContainerDestroy AuditAction = "container_destroyed"
	AuditLogRedacted      AuditAction = "log_redacted"
	AuditWorkspaceScrub   AuditAction = "workspace_scrubbed"
	AuditAccessDenied     AuditAction = "access_denied"
	AuditDataExport       AuditAction = "data_export"
	AuditConfigChange     AuditAction = "config_change"
	AuditOwnershipCheck   AuditAction = "ownership_check"
)

type AuditEvent struct {
	ID        uuid.UUID      `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Action    AuditAction    `json:"action"`
	UserID    string         `json:"user_id,omitempty"`
	RepoID    string         `json:"repo_id,omitempty"`
	BuildID   string         `json:"build_id,omitempty"`
	Detail    map[string]any `json:"detail,omitempty"`
}

type AuditLogger struct {
	logger *slog.Logger
	pool   *pgxpool.Pool
}

func NewAuditLogger(logger *slog.Logger, pool *pgxpool.Pool) *AuditLogger {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuditLogger{logger: logger, pool: pool}
}

func (a *AuditLogger) Log(ctx context.Context, event AuditEvent) {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	a.logger.Info("sandbox: audit",
		"audit_id", event.ID,
		"action", event.Action,
		"user_id", event.UserID,
		"repo_id", event.RepoID,
		"build_id", event.BuildID,
		"detail", event.Detail,
	)

	AuditEventsTotal.WithLabelValues(string(event.Action)).Inc()

	if a.pool != nil {
		detailJSON, _ := json.Marshal(event.Detail)
		_, err := a.pool.Exec(ctx, `
			INSERT INTO swarm_audit_events
				(id, action, user_id, repo_id, build_id, detail, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			event.ID, string(event.Action), event.UserID, event.RepoID,
			event.BuildID, detailJSON, event.Timestamp,
		)
		if err != nil {
			a.logger.Warn("sandbox: failed to persist audit event",
				"audit_id", event.ID, "error", err)
		}
	}
}

func (a *AuditLogger) LogSecretAccess(ctx context.Context, userID, repoID, buildID, secretName string) {
	a.Log(ctx, AuditEvent{
		Action:  AuditSecretAccess,
		UserID:  userID,
		RepoID:  repoID,
		BuildID: buildID,
		Detail:  map[string]any{"secret_name": secretName},
	})
}

func (a *AuditLogger) LogSecretDenied(ctx context.Context, userID, repoID, secretName, reason string) {
	a.Log(ctx, AuditEvent{
		Action: AuditSecretDenied,
		UserID: userID,
		RepoID: repoID,
		Detail: map[string]any{"secret_name": secretName, "reason": reason},
	})
}

func (a *AuditLogger) LogContainerCreated(ctx context.Context, buildID, image string, sandboxMode bool) {
	a.Log(ctx, AuditEvent{
		Action:  AuditContainerCreated,
		BuildID: buildID,
		Detail:  map[string]any{"image": image, "sandbox_mode": sandboxMode},
	})
}

func (a *AuditLogger) LogContainerDestroyed(ctx context.Context, buildID string) {
	a.Log(ctx, AuditEvent{
		Action:  AuditContainerDestroy,
		BuildID: buildID,
	})
}

func (a *AuditLogger) LogRedaction(ctx context.Context, buildID string, patternsMatched int) {
	a.Log(ctx, AuditEvent{
		Action:  AuditLogRedacted,
		BuildID: buildID,
		Detail:  map[string]any{"patterns_matched": patternsMatched},
	})
}

func (a *AuditLogger) LogWorkspaceScrub(ctx context.Context, buildID string, filesRemoved int) {
	a.Log(ctx, AuditEvent{
		Action:  AuditWorkspaceScrub,
		BuildID: buildID,
		Detail:  map[string]any{"files_removed": filesRemoved},
	})
}

func (a *AuditLogger) LogAccessDenied(ctx context.Context, userID, resource, reason string) {
	a.Log(ctx, AuditEvent{
		Action: AuditAccessDenied,
		UserID: userID,
		Detail: map[string]any{"resource": resource, "reason": reason},
	})
}

// ValidateBuildOwnership confirms that the requesting user has access to the
// build record.
func ValidateBuildOwnership(ctx context.Context, store *BuildStore, buildID uuid.UUID, userID uuid.UUID) (*BuildRecord, error) {
	rec, err := store.GetBuild(ctx, buildID)
	if err != nil {
		return nil, fmt.Errorf("build not found: %w", err)
	}
	if rec.UserID != nil && *rec.UserID != userID {
		return nil, fmt.Errorf("access denied: user %s does not own build %s", userID, buildID)
	}
	return rec, nil
}

// SecureCleanupWorkspace removes the workspace directory, zeroes in-memory
// file contents, and returns the number of files removed.
func SecureCleanupWorkspace(plan *BuildPlan) int {
	if plan.WorkspaceDir == "" {
		return 0
	}
	count := countDirFiles(plan.WorkspaceDir)
	for k := range plan.WorkspaceFS {
		plan.WorkspaceFS[k] = ""
	}
	CleanupWorkspace(plan)
	return count
}

func countDirFiles(dir string) int {
	count := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() {
			count += countDirFiles(filepath.Join(dir, e.Name()))
		} else {
			count++
		}
	}
	return count
}

// AuditQuery holds optional filter parameters for querying audit events.
type AuditQuery struct {
	BuildID string
	UserID  string
	Action  string
	Limit   int
}

// Query retrieves persisted audit events matching the given filters.
func (a *AuditLogger) Query(ctx context.Context, q AuditQuery) ([]AuditEvent, error) {
	if a.pool == nil {
		return nil, fmt.Errorf("no database configured for audit")
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 50
	}

	query := `SELECT id, action, user_id, repo_id, build_id, detail, created_at
		FROM swarm_audit_events WHERE 1=1`
	args := []any{}
	idx := 1

	if q.BuildID != "" {
		query += fmt.Sprintf(" AND build_id = $%d", idx)
		args = append(args, q.BuildID)
		idx++
	}
	if q.UserID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", idx)
		args = append(args, q.UserID)
		idx++
	}
	if q.Action != "" {
		query += fmt.Sprintf(" AND action = $%d", idx)
		args = append(args, q.Action)
		idx++
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", idx)
	args = append(args, q.Limit)

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit query: %w", err)
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var detailJSON []byte
		if err := rows.Scan(&e.ID, &e.Action, &e.UserID, &e.RepoID, &e.BuildID, &detailJSON, &e.Timestamp); err != nil {
			return nil, fmt.Errorf("audit scan: %w", err)
		}
		if len(detailJSON) > 0 {
			json.Unmarshal(detailJSON, &e.Detail)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
