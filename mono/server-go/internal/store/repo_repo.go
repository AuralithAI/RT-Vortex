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

// ── RepositoryRepo ──────────────────────────────────────────────────────────

// RepositoryRepo handles repository persistence.
type RepositoryRepo struct {
	pool *pgxpool.Pool
}

// NewRepositoryRepo creates a repository repo.
func NewRepositoryRepo(pool *pgxpool.Pool) *RepositoryRepo {
	return &RepositoryRepo{pool: pool}
}

// Create inserts a new repository.
func (r *RepositoryRepo) Create(ctx context.Context, repo *model.Repository) error {
	repo.ID = uuid.New()
	now := time.Now().UTC()
	repo.CreatedAt = now
	repo.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO repositories (id, org_id, platform, external_id, owner, name, default_branch, clone_url, webhook_secret, config, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		repo.ID, repo.OrgID, repo.Platform, repo.ExternalID, repo.Owner, repo.Name,
		repo.DefaultBranch, repo.CloneURL, repo.WebhookSecret, repo.Config, repo.CreatedAt, repo.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create repository: %w", err)
	}
	return nil
}

// GetByID returns a repository by ID.
func (r *RepositoryRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.Repository, error) {
	repo := &model.Repository{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, org_id, platform, external_id, owner, name, default_branch, clone_url, webhook_secret, config, indexed_at, created_at, updated_at
		 FROM repositories WHERE id = $1`, id,
	).Scan(&repo.ID, &repo.OrgID, &repo.Platform, &repo.ExternalID, &repo.Owner, &repo.Name,
		&repo.DefaultBranch, &repo.CloneURL, &repo.WebhookSecret, &repo.Config, &repo.IndexedAt, &repo.CreatedAt, &repo.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}
	return repo, nil
}

// GetByPlatformAndExternalID looks up a repo by its VCS platform identity.
func (r *RepositoryRepo) GetByPlatformAndExternalID(ctx context.Context, platform, externalID string) (*model.Repository, error) {
	repo := &model.Repository{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, org_id, platform, external_id, owner, name, default_branch, clone_url, webhook_secret, config, indexed_at, created_at, updated_at
		 FROM repositories WHERE platform = $1 AND external_id = $2`, platform, externalID,
	).Scan(&repo.ID, &repo.OrgID, &repo.Platform, &repo.ExternalID, &repo.Owner, &repo.Name,
		&repo.DefaultBranch, &repo.CloneURL, &repo.WebhookSecret, &repo.Config, &repo.IndexedAt, &repo.CreatedAt, &repo.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get repository by platform: %w", err)
	}
	return repo, nil
}

// ListByOrg returns all repositories for an organization.
func (r *RepositoryRepo) ListByOrg(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*model.Repository, int, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM repositories WHERE org_id = $1`, orgID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count repositories: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, platform, external_id, owner, name, default_branch, clone_url, config, indexed_at, created_at, updated_at
		 FROM repositories WHERE org_id = $1 ORDER BY name LIMIT $2 OFFSET $3`, orgID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list repositories: %w", err)
	}
	defer rows.Close()

	var repos []*model.Repository
	for rows.Next() {
		repo := &model.Repository{}
		if err := rows.Scan(&repo.ID, &repo.OrgID, &repo.Platform, &repo.ExternalID, &repo.Owner, &repo.Name,
			&repo.DefaultBranch, &repo.CloneURL, &repo.Config, &repo.IndexedAt, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan repository: %w", err)
		}
		repos = append(repos, repo)
	}
	return repos, total, nil
}

// Update updates a repository.
func (r *RepositoryRepo) Update(ctx context.Context, repo *model.Repository) error {
	repo.UpdatedAt = time.Now().UTC()
	tag, err := r.pool.Exec(ctx,
		`UPDATE repositories SET platform=$2, owner=$3, name=$4, default_branch=$5, clone_url=$6, config=$7, updated_at=$8
		 WHERE id=$1`,
		repo.ID, repo.Platform, repo.Owner, repo.Name, repo.DefaultBranch, repo.CloneURL, repo.Config, repo.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update repository: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a repository by ID.
func (r *RepositoryRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM repositories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete repository: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
