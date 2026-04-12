package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BuildRecord represents a row in the swarm_builds table.
type BuildRecord struct {
	ID          uuid.UUID  `json:"id"`
	TaskID      uuid.UUID  `json:"task_id"`
	RepoID      string     `json:"repo_id"`
	UserID      *uuid.UUID `json:"user_id,omitempty"`
	BuildSystem string     `json:"build_system"`
	Command     string     `json:"command"`
	BaseImage   string     `json:"base_image"`
	Status      string     `json:"status"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	LogSummary  string     `json:"log_summary"`
	SecretNames []string   `json:"secret_names"`
	SandboxMode bool       `json:"sandbox_mode"`
	RetryCount  int        `json:"retry_count"`
	DurationMS  *int       `json:"duration_ms,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// BuildStore persists build records to the swarm_builds table.
type BuildStore struct {
	pool *pgxpool.Pool
}

// NewBuildStore creates a store backed by the given connection pool.
func NewBuildStore(pool *pgxpool.Pool) *BuildStore {
	return &BuildStore{pool: pool}
}

// InsertBuild creates a new build record in "running" status.
func (s *BuildStore) InsertBuild(ctx context.Context, rec *BuildRecord) error {
	if rec.ID == uuid.Nil {
		rec.ID = uuid.New()
	}
	rec.CreatedAt = time.Now().UTC()

	_, err := s.pool.Exec(ctx, `
		INSERT INTO swarm_builds
			(id, task_id, repo_id, user_id, build_system, command, base_image,
			 status, secret_names, sandbox_mode, retry_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		rec.ID, rec.TaskID, rec.RepoID, rec.UserID,
		rec.BuildSystem, rec.Command, rec.BaseImage,
		rec.Status, rec.SecretNames, rec.SandboxMode, rec.RetryCount,
	)
	if err != nil {
		return fmt.Errorf("sandbox store: insert build: %w", err)
	}
	return nil
}

// CompleteBuild updates a build with the final result.
func (s *BuildStore) CompleteBuild(ctx context.Context, id uuid.UUID, status string, exitCode int, logSummary string, durationMS int) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE swarm_builds
		SET status = $2, exit_code = $3, log_summary = $4,
		    duration_ms = $5, completed_at = $6
		WHERE id = $1`,
		id, status, exitCode, logSummary, durationMS, now,
	)
	if err != nil {
		return fmt.Errorf("sandbox store: complete build: %w", err)
	}
	return nil
}

// GetBuild retrieves a single build by ID.
func (s *BuildStore) GetBuild(ctx context.Context, id uuid.UUID) (*BuildRecord, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, task_id, repo_id, user_id, build_system, command, base_image,
		       status, exit_code, log_summary, secret_names, sandbox_mode,
		       retry_count, duration_ms, created_at, completed_at
		FROM swarm_builds WHERE id = $1`, id)

	return scanBuildRecord(row)
}

// GetBuildByTask retrieves the most recent build for a task.
func (s *BuildStore) GetBuildByTask(ctx context.Context, taskID uuid.UUID) (*BuildRecord, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, task_id, repo_id, user_id, build_system, command, base_image,
		       status, exit_code, log_summary, secret_names, sandbox_mode,
		       retry_count, duration_ms, created_at, completed_at
		FROM swarm_builds WHERE task_id = $1
		ORDER BY created_at DESC LIMIT 1`, taskID)

	return scanBuildRecord(row)
}

// ListBuildsByRepo returns recent builds for a repo, newest first.
func (s *BuildStore) ListBuildsByRepo(ctx context.Context, repoID string, limit int) ([]*BuildRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, task_id, repo_id, user_id, build_system, command, base_image,
		       status, exit_code, log_summary, secret_names, sandbox_mode,
		       retry_count, duration_ms, created_at, completed_at
		FROM swarm_builds WHERE repo_id = $1
		ORDER BY created_at DESC LIMIT $2`, repoID, limit)
	if err != nil {
		return nil, fmt.Errorf("sandbox store: list builds: %w", err)
	}
	defer rows.Close()

	var builds []*BuildRecord
	for rows.Next() {
		rec, err := scanBuildRecord(rows)
		if err != nil {
			return nil, err
		}
		builds = append(builds, rec)
	}
	return builds, nil
}

// IncrementRetry bumps the retry_count for a build.
func (s *BuildStore) IncrementRetry(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE swarm_builds SET retry_count = retry_count + 1 WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("sandbox store: increment retry: %w", err)
	}
	return nil
}

// scanBuildRecord scans a single row into a BuildRecord.
func scanBuildRecord(row pgx.Row) (*BuildRecord, error) {
	var rec BuildRecord
	err := row.Scan(
		&rec.ID, &rec.TaskID, &rec.RepoID, &rec.UserID,
		&rec.BuildSystem, &rec.Command, &rec.BaseImage,
		&rec.Status, &rec.ExitCode, &rec.LogSummary, &rec.SecretNames,
		&rec.SandboxMode, &rec.RetryCount, &rec.DurationMS,
		&rec.CreatedAt, &rec.CompletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("sandbox store: scan build: %w", err)
	}
	return &rec, nil
}

// InsertArtifact persists a build artifact to the swarm_build_artifacts table.
func (s *BuildStore) InsertArtifact(ctx context.Context, a *BuildArtifact) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	a.CreatedAt = time.Now().UTC()

	_, err := s.pool.Exec(ctx, `
		INSERT INTO swarm_build_artifacts
			(id, build_id, kind, path, size_bytes, data, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		a.ID, a.BuildID, string(a.Kind), a.Path, a.SizeBytes, a.Data, a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("sandbox store: insert artifact: %w", err)
	}
	return nil
}

// ListArtifacts returns all artifacts for a build, excluding the raw data
// blob (which can be large).  Use GetArtifact to fetch data for a single one.
func (s *BuildStore) ListArtifacts(ctx context.Context, buildID uuid.UUID) ([]*BuildArtifact, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, build_id, kind, path, size_bytes, created_at
		FROM swarm_build_artifacts WHERE build_id = $1
		ORDER BY created_at`, buildID)
	if err != nil {
		return nil, fmt.Errorf("sandbox store: list artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []*BuildArtifact
	for rows.Next() {
		var a BuildArtifact
		if err := rows.Scan(&a.ID, &a.BuildID, &a.Kind, &a.Path, &a.SizeBytes, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("sandbox store: scan artifact: %w", err)
		}
		artifacts = append(artifacts, &a)
	}
	return artifacts, nil
}

// GetArtifact retrieves a single artifact including its data blob.
func (s *BuildStore) GetArtifact(ctx context.Context, id uuid.UUID) (*BuildArtifact, error) {
	var a BuildArtifact
	err := s.pool.QueryRow(ctx, `
		SELECT id, build_id, kind, path, size_bytes, data, created_at
		FROM swarm_build_artifacts WHERE id = $1`, id).
		Scan(&a.ID, &a.BuildID, &a.Kind, &a.Path, &a.SizeBytes, &a.Data, &a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("sandbox store: get artifact: %w", err)
	}
	return &a, nil
}

// UpdateBuildComplexity stores the complexity analysis on the build record.
func (s *BuildStore) UpdateBuildComplexity(ctx context.Context, buildID uuid.UUID, complexity *BuildComplexity) error {
	data, err := json.Marshal(complexity)
	if err != nil {
		return fmt.Errorf("sandbox store: marshal complexity: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE swarm_builds SET complexity = $1 WHERE id = $2`,
		data, buildID,
	)
	if err != nil {
		return fmt.Errorf("sandbox store: update complexity: %w", err)
	}
	return nil
}
