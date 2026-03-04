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

// ── Errors ──────────────────────────────────────────────────────────────────

var (
	ErrNotFound  = errors.New("record not found")
	ErrDuplicate = errors.New("duplicate record")
	ErrConflict  = errors.New("update conflict")
)

// ── UserRepository ──────────────────────────────────────────────────────────

// UserRepository handles user persistence.
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository creates a user repository.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// Create inserts a new user and returns the created record.
func (r *UserRepository) Create(ctx context.Context, u *model.User) error {
	u.ID = uuid.New()
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, email, display_name, avatar_url, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		u.ID, u.Email, u.DisplayName, u.AvatarURL, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// GetByID returns a user by ID.
func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	u := &model.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, display_name, avatar_url, created_at, updated_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}

// GetByEmail returns a user by email.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, display_name, avatar_url, created_at, updated_at
		 FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return u, nil
}

// Update updates a user record.
func (r *UserRepository) Update(ctx context.Context, u *model.User) error {
	u.UpdatedAt = time.Now().UTC()
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET email=$2, display_name=$3, avatar_url=$4, updated_at=$5
		 WHERE id=$1`,
		u.ID, u.Email, u.DisplayName, u.AvatarURL, u.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertOAuthIdentity creates or updates an OAuth identity link.
func (r *UserRepository) UpsertOAuthIdentity(ctx context.Context, oi *model.OAuthIdentity) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO oauth_identities (id, user_id, provider, provider_user_id, access_token_enc, refresh_token_enc, scopes, expires_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (provider, provider_user_id) DO UPDATE
		 SET user_id=$2, access_token_enc=$5, refresh_token_enc=$6, scopes=$7, expires_at=$8, updated_at=$10`,
		uuid.New(), oi.UserID, oi.Provider, oi.ProviderUserID, oi.AccessTokenEnc, oi.RefreshTokenEnc, oi.Scopes, oi.ExpiresAt,
		time.Now().UTC(), time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("upsert oauth identity: %w", err)
	}
	return nil
}

// GetOAuthIdentity returns the OAuth identity for a provider + provider user ID.
func (r *UserRepository) GetOAuthIdentity(ctx context.Context, provider, providerUserID string) (*model.OAuthIdentity, error) {
	oi := &model.OAuthIdentity{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, provider, provider_user_id, access_token_enc, refresh_token_enc, scopes, expires_at, created_at, updated_at
		 FROM oauth_identities WHERE provider=$1 AND provider_user_id=$2`,
		provider, providerUserID,
	).Scan(&oi.ID, &oi.UserID, &oi.Provider, &oi.ProviderUserID, &oi.AccessTokenEnc, &oi.RefreshTokenEnc, &oi.Scopes, &oi.ExpiresAt, &oi.CreatedAt, &oi.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get oauth identity: %w", err)
	}
	return oi, nil
}

// ListUsers returns paginated users.
func (r *UserRepository) ListUsers(ctx context.Context, limit, offset int) ([]*model.User, int, error) {
	var total int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, email, display_name, avatar_url, created_at, updated_at
		 FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		u := &model.User{}
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, total, nil
}
