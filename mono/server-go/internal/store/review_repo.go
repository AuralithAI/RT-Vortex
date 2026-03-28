package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AuralithAI/rtvortex-server/internal/model"
)

// ── ReviewRepository ────────────────────────────────────────────────────────

// ReviewRepository handles review persistence.
type ReviewRepository struct {
	pool *pgxpool.Pool
}

// NewReviewRepository creates a review repository.
func NewReviewRepository(pool *pgxpool.Pool) *ReviewRepository {
	return &ReviewRepository{pool: pool}
}

// Create inserts a new review.
func (r *ReviewRepository) Create(ctx context.Context, rev *model.Review) error {
	rev.ID = uuid.New()
	rev.CreatedAt = time.Now().UTC()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO reviews (id, repo_id, triggered_by, platform, pr_number, pr_title, pr_author, base_branch, head_branch, status, files_changed, additions, deletions, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		rev.ID, rev.RepoID, rev.TriggeredBy, rev.Platform, rev.PRNumber, rev.PRTitle, rev.PRAuthor,
		rev.BaseBranch, rev.HeadBranch, rev.Status, rev.FilesChanged, rev.Additions, rev.Deletions,
		rev.Metadata, rev.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create review: %w", err)
	}
	return nil
}

// GetByID returns a review by ID.
func (r *ReviewRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Review, error) {
	rev := &model.Review{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, repo_id, triggered_by, platform, pr_number, pr_title, pr_author, base_branch, head_branch, status, files_changed, additions, deletions, metadata, created_at, completed_at
		 FROM reviews WHERE id = $1`, id,
	).Scan(&rev.ID, &rev.RepoID, &rev.TriggeredBy, &rev.Platform, &rev.PRNumber, &rev.PRTitle, &rev.PRAuthor,
		&rev.BaseBranch, &rev.HeadBranch, &rev.Status, &rev.FilesChanged, &rev.Additions, &rev.Deletions,
		&rev.Metadata, &rev.CreatedAt, &rev.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get review: %w", err)
	}
	return rev, nil
}

// UpdateStatus updates the review status and optionally marks it completed.
func (r *ReviewRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status model.ReviewStatus, metadata map[string]interface{}) error {
	now := time.Now().UTC()
	var completedAt *time.Time
	if status == model.ReviewStatusCompleted || status == model.ReviewStatusFailed {
		completedAt = &now
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE reviews SET status=$2, metadata=COALESCE($3, metadata), completed_at=COALESCE($4, completed_at)
		 WHERE id=$1`,
		id, status, metadata, completedAt,
	)
	if err != nil {
		return fmt.Errorf("update review status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByRepo returns reviews for a repository with pagination.
func (r *ReviewRepository) ListByRepo(ctx context.Context, repoID uuid.UUID, limit, offset int) ([]*model.Review, int, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM reviews WHERE repo_id = $1`, repoID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count reviews: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, repo_id, triggered_by, platform, pr_number, pr_title, pr_author, base_branch, head_branch, status, files_changed, additions, deletions, metadata, created_at, completed_at
		 FROM reviews WHERE repo_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, repoID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list reviews: %w", err)
	}
	defer rows.Close()

	var reviews []*model.Review
	for rows.Next() {
		rev := &model.Review{}
		if err := rows.Scan(&rev.ID, &rev.RepoID, &rev.TriggeredBy, &rev.Platform, &rev.PRNumber, &rev.PRTitle, &rev.PRAuthor,
			&rev.BaseBranch, &rev.HeadBranch, &rev.Status, &rev.FilesChanged, &rev.Additions, &rev.Deletions,
			&rev.Metadata, &rev.CreatedAt, &rev.CompletedAt); err != nil {
			return nil, 0, fmt.Errorf("scan review: %w", err)
		}
		reviews = append(reviews, rev)
	}
	return reviews, total, nil
}

// ListByUser returns all reviews accessible by a user via org memberships.
func (r *ReviewRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*model.Review, int, error) {
	var total int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM reviews rv
		 JOIN repositories rep ON rep.id = rv.repo_id
		 JOIN org_members om ON om.org_id = rep.org_id
		 WHERE om.user_id = $1`, userID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count user reviews: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT rv.id, rv.repo_id, rv.triggered_by, rv.platform, rv.pr_number, rv.pr_title, rv.pr_author,
		        rv.base_branch, rv.head_branch, rv.status, rv.files_changed, rv.additions, rv.deletions,
		        rv.metadata, rv.created_at, rv.completed_at
		 FROM reviews rv
		 JOIN repositories rep ON rep.id = rv.repo_id
		 JOIN org_members om ON om.org_id = rep.org_id
		 WHERE om.user_id = $1
		 ORDER BY rv.created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list user reviews: %w", err)
	}
	defer rows.Close()

	var reviews []*model.Review
	for rows.Next() {
		rev := &model.Review{}
		if err := rows.Scan(&rev.ID, &rev.RepoID, &rev.TriggeredBy, &rev.Platform, &rev.PRNumber, &rev.PRTitle, &rev.PRAuthor,
			&rev.BaseBranch, &rev.HeadBranch, &rev.Status, &rev.FilesChanged, &rev.Additions, &rev.Deletions,
			&rev.Metadata, &rev.CreatedAt, &rev.CompletedAt); err != nil {
			return nil, 0, fmt.Errorf("scan user review: %w", err)
		}
		reviews = append(reviews, rev)
	}
	return reviews, total, nil
}

// ── Review Comments ─────────────────────────────────────────────────────────

// CreateComment inserts a review comment.
func (r *ReviewRepository) CreateComment(ctx context.Context, c *model.ReviewComment) error {
	c.ID = uuid.New()
	c.CreatedAt = time.Now().UTC()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO review_comments (id, review_id, file_path, line_number, end_line, severity, category, title, body, suggestion, posted, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		c.ID, c.ReviewID, c.FilePath, c.LineNumber, c.EndLine, c.Severity, c.Category, c.Title, c.Body, c.Suggestion, c.Posted, c.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create review comment: %w", err)
	}
	return nil
}

// ListCommentsByReview returns all comments for a review.
func (r *ReviewRepository) ListCommentsByReview(ctx context.Context, reviewID uuid.UUID) ([]*model.ReviewComment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, review_id, file_path, line_number, end_line, severity, category, title, body, suggestion, posted, created_at
		 FROM review_comments WHERE review_id = $1 ORDER BY file_path, line_number`, reviewID,
	)
	if err != nil {
		return nil, fmt.Errorf("list review comments: %w", err)
	}
	defer rows.Close()

	var comments []*model.ReviewComment
	for rows.Next() {
		c := &model.ReviewComment{}
		if err := rows.Scan(&c.ID, &c.ReviewID, &c.FilePath, &c.LineNumber, &c.EndLine, &c.Severity, &c.Category, &c.Title, &c.Body, &c.Suggestion, &c.Posted, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan review comment: %w", err)
		}
		comments = append(comments, c)
	}
	return comments, nil
}
