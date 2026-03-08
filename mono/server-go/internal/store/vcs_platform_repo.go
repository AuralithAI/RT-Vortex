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

// ── VCSPlatformRepo ─────────────────────────────────────────────────────────

// VCSPlatformRepo handles persistence of per-user VCS platform configuration
// (non-secret fields like URLs, usernames, org names). Secrets are stored in
// the file vault — this repo only handles PostgreSQL-backed metadata.
type VCSPlatformRepo struct {
	pool *pgxpool.Pool
}

// NewVCSPlatformRepo creates a new VCS platform repository.
func NewVCSPlatformRepo(pool *pgxpool.Pool) *VCSPlatformRepo {
	return &VCSPlatformRepo{pool: pool}
}

// Upsert creates or updates a user's VCS platform configuration.
func (r *VCSPlatformRepo) Upsert(ctx context.Context, p *model.UserVCSPlatform) error {
	now := time.Now().UTC()
	p.UpdatedAt = now

	if p.ID == uuid.Nil {
		p.ID = uuid.New()
		p.CreatedAt = now
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_vcs_platforms (id, user_id, platform, base_url, api_url, organization, username, tenant_id, client_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (user_id, platform) DO UPDATE SET
		   base_url     = EXCLUDED.base_url,
		   api_url      = EXCLUDED.api_url,
		   organization = EXCLUDED.organization,
		   username     = EXCLUDED.username,
		   tenant_id    = EXCLUDED.tenant_id,
		   client_id    = EXCLUDED.client_id,
		   updated_at   = EXCLUDED.updated_at`,
		p.ID, p.UserID, p.Platform, p.BaseURL, p.APIURL, p.Organization, p.Username, p.TenantID, p.ClientID, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert vcs platform: %w", err)
	}
	return nil
}

// GetByUserAndPlatform returns a user's config for a specific platform.
func (r *VCSPlatformRepo) GetByUserAndPlatform(ctx context.Context, userID uuid.UUID, platform string) (*model.UserVCSPlatform, error) {
	p := &model.UserVCSPlatform{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, platform, base_url, api_url, organization, username, tenant_id, client_id, created_at, updated_at
		 FROM user_vcs_platforms WHERE user_id = $1 AND platform = $2`, userID, platform,
	).Scan(&p.ID, &p.UserID, &p.Platform, &p.BaseURL, &p.APIURL, &p.Organization, &p.Username, &p.TenantID, &p.ClientID, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get vcs platform: %w", err)
	}
	return p, nil
}

// ListByUser returns all VCS platform configs for a user.
func (r *VCSPlatformRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.UserVCSPlatform, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, platform, base_url, api_url, organization, username, tenant_id, client_id, created_at, updated_at
		 FROM user_vcs_platforms WHERE user_id = $1 ORDER BY platform`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list vcs platforms: %w", err)
	}
	defer rows.Close()

	var platforms []*model.UserVCSPlatform
	for rows.Next() {
		p := &model.UserVCSPlatform{}
		if err := rows.Scan(&p.ID, &p.UserID, &p.Platform, &p.BaseURL, &p.APIURL, &p.Organization, &p.Username, &p.TenantID, &p.ClientID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan vcs platform: %w", err)
		}
		platforms = append(platforms, p)
	}
	return platforms, nil
}

// DeleteByUserAndPlatform removes a user's VCS platform configuration.
func (r *VCSPlatformRepo) DeleteByUserAndPlatform(ctx context.Context, userID uuid.UUID, platform string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM user_vcs_platforms WHERE user_id = $1 AND platform = $2`, userID, platform,
	)
	if err != nil {
		return fmt.Errorf("delete vcs platform: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
