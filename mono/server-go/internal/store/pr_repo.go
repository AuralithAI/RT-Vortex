package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AuralithAI/rtvortex-server/internal/model"
)

// ── PullRequestRepo ─────────────────────────────────────────────────────────

// PullRequestRepo handles tracked pull request persistence.
type PullRequestRepo struct {
	pool *pgxpool.Pool
}

// NewPullRequestRepo creates a pull request repo.
func NewPullRequestRepo(pool *pgxpool.Pool) *PullRequestRepo {
	return &PullRequestRepo{pool: pool}
}

// scanColumns is the canonical column list for SELECT queries.
const prSelectColumns = `id, repo_id, platform, pr_number, external_id, title, description, author,
	source_branch, target_branch, head_sha, base_sha, pr_url,
	sync_status, review_status, last_review_id,
	files_changed, additions, deletions,
	embedded_at, embed_error,
	synced_at, created_at, updated_at`

// scanPR scans a row into a TrackedPullRequest.
func scanPR(row pgx.Row) (*model.TrackedPullRequest, error) {
	pr := &model.TrackedPullRequest{}
	err := row.Scan(
		&pr.ID, &pr.RepoID, &pr.Platform, &pr.PRNumber, &pr.ExternalID,
		&pr.Title, &pr.Description, &pr.Author,
		&pr.SourceBranch, &pr.TargetBranch, &pr.HeadSHA, &pr.BaseSHA, &pr.PRURL,
		&pr.SyncStatus, &pr.ReviewStatus, &pr.LastReviewID,
		&pr.FilesChanged, &pr.Additions, &pr.Deletions,
		&pr.EmbeddedAt, &pr.EmbedError,
		&pr.SyncedAt, &pr.CreatedAt, &pr.UpdatedAt,
	)
	return pr, err
}

// scanPRRows scans multiple rows into a slice of TrackedPullRequest.
func scanPRRows(rows pgx.Rows) ([]*model.TrackedPullRequest, error) {
	var prs []*model.TrackedPullRequest
	for rows.Next() {
		pr := &model.TrackedPullRequest{}
		if err := rows.Scan(
			&pr.ID, &pr.RepoID, &pr.Platform, &pr.PRNumber, &pr.ExternalID,
			&pr.Title, &pr.Description, &pr.Author,
			&pr.SourceBranch, &pr.TargetBranch, &pr.HeadSHA, &pr.BaseSHA, &pr.PRURL,
			&pr.SyncStatus, &pr.ReviewStatus, &pr.LastReviewID,
			&pr.FilesChanged, &pr.Additions, &pr.Deletions,
			&pr.EmbeddedAt, &pr.EmbedError,
			&pr.SyncedAt, &pr.CreatedAt, &pr.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan tracked PR: %w", err)
		}
		prs = append(prs, pr)
	}
	return prs, nil
}

// Upsert inserts a new tracked PR or updates it if one already exists for the
// same (repo_id, platform, pr_number). On conflict the mutable fields are updated.
func (r *PullRequestRepo) Upsert(ctx context.Context, pr *model.TrackedPullRequest) error {
	if pr.ID == uuid.Nil {
		pr.ID = uuid.New()
	}
	now := time.Now().UTC()
	pr.SyncedAt = now
	if pr.CreatedAt.IsZero() {
		pr.CreatedAt = now
	}
	pr.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO tracked_pull_requests (
			id, repo_id, platform, pr_number, external_id, title, description, author,
			source_branch, target_branch, head_sha, base_sha, pr_url,
			sync_status, review_status, last_review_id,
			files_changed, additions, deletions,
			embedded_at, embed_error,
			synced_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
		ON CONFLICT (repo_id, platform, pr_number) DO UPDATE SET
			external_id    = EXCLUDED.external_id,
			title          = EXCLUDED.title,
			description    = EXCLUDED.description,
			author         = EXCLUDED.author,
			source_branch  = EXCLUDED.source_branch,
			target_branch  = EXCLUDED.target_branch,
			head_sha       = EXCLUDED.head_sha,
			base_sha       = EXCLUDED.base_sha,
			pr_url         = EXCLUDED.pr_url,
			-- Preserve embed-related statuses unless the head SHA changed
			-- (new commits invalidate the existing embedding).
			sync_status    = CASE
				WHEN tracked_pull_requests.head_sha != EXCLUDED.head_sha
					THEN EXCLUDED.sync_status
				WHEN tracked_pull_requests.sync_status IN ('embedded', 'embedding', 'embed_error')
					THEN tracked_pull_requests.sync_status
				ELSE EXCLUDED.sync_status
			END,
			-- Clear embedded_at when head SHA changes so the PR gets re-embedded.
			embedded_at    = CASE
				WHEN tracked_pull_requests.head_sha != EXCLUDED.head_sha
					THEN NULL
				ELSE tracked_pull_requests.embedded_at
			END,
			files_changed  = EXCLUDED.files_changed,
			additions      = EXCLUDED.additions,
			deletions      = EXCLUDED.deletions,
			synced_at      = EXCLUDED.synced_at,
			-- Only bump updated_at when meaningful data changed (head SHA, title,
			-- description, branches) so that embedded PRs don't appear stale.
			updated_at     = CASE
				WHEN tracked_pull_requests.head_sha != EXCLUDED.head_sha
				  OR tracked_pull_requests.title != EXCLUDED.title
				  OR tracked_pull_requests.source_branch != EXCLUDED.source_branch
				  OR tracked_pull_requests.target_branch != EXCLUDED.target_branch
					THEN EXCLUDED.updated_at
				ELSE tracked_pull_requests.updated_at
			END`,
		pr.ID, pr.RepoID, pr.Platform, pr.PRNumber, pr.ExternalID,
		pr.Title, pr.Description, pr.Author,
		pr.SourceBranch, pr.TargetBranch, pr.HeadSHA, pr.BaseSHA, pr.PRURL,
		pr.SyncStatus, pr.ReviewStatus, pr.LastReviewID,
		pr.FilesChanged, pr.Additions, pr.Deletions,
		pr.EmbeddedAt, pr.EmbedError,
		pr.SyncedAt, pr.CreatedAt, pr.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert tracked PR: %w", err)
	}
	return nil
}

// GetByID returns a tracked PR by its primary key.
func (r *PullRequestRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.TrackedPullRequest, error) {
	pr, err := scanPR(r.pool.QueryRow(ctx,
		`SELECT `+prSelectColumns+` FROM tracked_pull_requests WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tracked PR: %w", err)
	}
	return pr, nil
}

// GetByRepoAndNumber returns a tracked PR by repository ID + PR number.
func (r *PullRequestRepo) GetByRepoAndNumber(ctx context.Context, repoID uuid.UUID, prNumber int) (*model.TrackedPullRequest, error) {
	pr, err := scanPR(r.pool.QueryRow(ctx,
		`SELECT `+prSelectColumns+` FROM tracked_pull_requests WHERE repo_id = $1 AND pr_number = $2`,
		repoID, prNumber))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tracked PR by number: %w", err)
	}
	return pr, nil
}

// ListByRepo returns tracked PRs for a repository with pagination and optional filters.
func (r *PullRequestRepo) ListByRepo(ctx context.Context, repoID uuid.UUID, filter model.PRListFilter, limit, offset int) ([]*model.TrackedPullRequest, int, error) {
	where, args := r.buildFilterClause(repoID, filter)

	// Count
	var total int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tracked_pull_requests `+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count tracked PRs: %w", err)
	}

	// Fetch
	argIdx := len(args)
	query := fmt.Sprintf(
		`SELECT `+prSelectColumns+` FROM tracked_pull_requests %s ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`,
		where, argIdx+1, argIdx+2,
	)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list tracked PRs: %w", err)
	}
	defer rows.Close()

	prs, err := scanPRRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return prs, total, nil
}

// buildFilterClause builds a WHERE clause from the filter parameters.
func (r *PullRequestRepo) buildFilterClause(repoID uuid.UUID, f model.PRListFilter) (string, []interface{}) {
	conditions := []string{"repo_id = $1"}
	args := []interface{}{repoID}
	idx := 2

	if f.SyncStatus != nil {
		conditions = append(conditions, fmt.Sprintf("sync_status = $%d", idx))
		args = append(args, string(*f.SyncStatus))
		idx++
	}
	if f.ReviewStatus != nil {
		conditions = append(conditions, fmt.Sprintf("review_status = $%d", idx))
		args = append(args, string(*f.ReviewStatus))
		idx++
	}
	if f.Author != "" {
		conditions = append(conditions, fmt.Sprintf("author = $%d", idx))
		args = append(args, f.Author)
		idx++
	}
	if f.TargetBranch != "" {
		conditions = append(conditions, fmt.Sprintf("target_branch = $%d", idx))
		args = append(args, f.TargetBranch)
	}

	return "WHERE " + strings.Join(conditions, " AND "), args
}

// UpdateSyncStatus sets the sync status for a tracked PR.
func (r *PullRequestRepo) UpdateSyncStatus(ctx context.Context, id uuid.UUID, status model.PRSyncStatus) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tracked_pull_requests SET sync_status = $2, updated_at = NOW() WHERE id = $1`,
		id, status,
	)
	if err != nil {
		return fmt.Errorf("update PR sync status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateReviewStatus sets the review status and optionally the last review ID.
func (r *PullRequestRepo) UpdateReviewStatus(ctx context.Context, id uuid.UUID, status model.PRReviewStatus, reviewID *uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tracked_pull_requests SET review_status = $2, last_review_id = $3, updated_at = NOW() WHERE id = $1`,
		id, status, reviewID,
	)
	if err != nil {
		return fmt.Errorf("update PR review status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkEmbedded marks a PR's diff as successfully embedded by the engine.
func (r *PullRequestRepo) MarkEmbedded(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx,
		`UPDATE tracked_pull_requests SET sync_status = $2, embedded_at = $3, embed_error = NULL, updated_at = $3 WHERE id = $1`,
		id, model.PRSyncStatusEmbedded, now,
	)
	if err != nil {
		return fmt.Errorf("mark PR embedded: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkEmbedError records an embedding failure for a tracked PR.
func (r *PullRequestRepo) MarkEmbedError(ctx context.Context, id uuid.UUID, embedErr string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tracked_pull_requests SET sync_status = $2, embed_error = $3, updated_at = NOW() WHERE id = $1`,
		id, model.PRSyncStatusEmbedError, embedErr,
	)
	if err != nil {
		return fmt.Errorf("mark PR embed error: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkStaleBefore marks any open PRs that were not synced after the given cutoff
// time as stale. Returns the number of rows affected.
func (r *PullRequestRepo) MarkStaleBefore(ctx context.Context, repoID uuid.UUID, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tracked_pull_requests SET sync_status = $3, updated_at = NOW()
		 WHERE repo_id = $1 AND sync_status = 'open' AND synced_at < $2`,
		repoID, cutoff, model.PRSyncStatusStale,
	)
	if err != nil {
		return 0, fmt.Errorf("mark stale PRs: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ListNeedingEmbedding returns open PRs whose head SHA changed or haven't been
// embedded yet, ordered by most recently updated first.
func (r *PullRequestRepo) ListNeedingEmbedding(ctx context.Context, limit int) ([]*model.TrackedPullRequest, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+prSelectColumns+` FROM tracked_pull_requests
		 WHERE sync_status = 'open'
		   AND (embedded_at IS NULL OR embedded_at < updated_at)
		 ORDER BY updated_at DESC
		 LIMIT $1`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list PRs needing embedding: %w", err)
	}
	defer rows.Close()
	return scanPRRows(rows)
}

// CountByRepo returns counts of tracked PRs grouped by sync status.
func (r *PullRequestRepo) CountByRepo(ctx context.Context, repoID uuid.UUID) (map[model.PRSyncStatus]int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT sync_status, COUNT(*) FROM tracked_pull_requests WHERE repo_id = $1 GROUP BY sync_status`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("count PRs by repo: %w", err)
	}
	defer rows.Close()

	counts := make(map[model.PRSyncStatus]int)
	for rows.Next() {
		var status model.PRSyncStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan PR count: %w", err)
		}
		counts[status] = count
	}
	return counts, nil
}

// CountEmbedQueue returns how many PRs in a repo still need embedding
// (open + not yet embedded or head changed since last embed).
func (r *PullRequestRepo) CountEmbedQueue(ctx context.Context, repoID uuid.UUID) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM tracked_pull_requests
		 WHERE repo_id = $1
		   AND sync_status = 'open'
		   AND (embedded_at IS NULL OR embedded_at < updated_at)`,
		repoID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count embed queue: %w", err)
	}
	return count, nil
}

// DeleteByRepo removes all tracked PRs for a repository.
func (r *PullRequestRepo) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM tracked_pull_requests WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("delete tracked PRs: %w", err)
	}
	return nil
}
