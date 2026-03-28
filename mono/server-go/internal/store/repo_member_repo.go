package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── RepoMemberRepo ──────────────────────────────────────────────────────────

// RepoMemberInfo represents a member's access to a specific repository.
type RepoMemberInfo struct {
	RepoID  uuid.UUID `json:"repo_id"`
	UserID  uuid.UUID `json:"user_id"`
	Role    string    `json:"role"`
	AddedAt time.Time `json:"added_at"`
	// Joined user info.
	UserName  string `json:"user_name,omitempty"`
	UserEmail string `json:"user_email,omitempty"`
}

// RepoMemberRepo handles repo-level member access persistence.
type RepoMemberRepo struct {
	pool *pgxpool.Pool
}

// NewRepoMemberRepo creates a new repo member repo.
func NewRepoMemberRepo(pool *pgxpool.Pool) *RepoMemberRepo {
	return &RepoMemberRepo{pool: pool}
}

// AddMember grants a user access to a repository with the given role.
func (r *RepoMemberRepo) AddMember(ctx context.Context, repoID, userID uuid.UUID, role string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO repo_members (repo_id, user_id, role, added_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (repo_id, user_id)
		 DO UPDATE SET role = EXCLUDED.role`,
		repoID, userID, role,
	)
	if err != nil {
		return fmt.Errorf("add repo member: %w", err)
	}
	return nil
}

// RemoveMember revokes a user's access to a repository.
func (r *RepoMemberRepo) RemoveMember(ctx context.Context, repoID, userID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM repo_members WHERE repo_id = $1 AND user_id = $2`,
		repoID, userID,
	)
	if err != nil {
		return fmt.Errorf("remove repo member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListMembers returns all members with access to a repository.
func (r *RepoMemberRepo) ListMembers(ctx context.Context, repoID uuid.UUID) ([]*RepoMemberInfo, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rm.repo_id, rm.user_id, rm.role, rm.added_at,
		        COALESCE(u.name, ''), COALESCE(u.email, '')
		 FROM repo_members rm
		 JOIN users u ON u.id = rm.user_id
		 WHERE rm.repo_id = $1
		 ORDER BY rm.added_at`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("list repo members: %w", err)
	}
	defer rows.Close()

	var members []*RepoMemberInfo
	for rows.Next() {
		m := &RepoMemberInfo{}
		if err := rows.Scan(&m.RepoID, &m.UserID, &m.Role, &m.AddedAt, &m.UserName, &m.UserEmail); err != nil {
			return nil, fmt.Errorf("scan repo member: %w", err)
		}
		members = append(members, m)
	}
	return members, nil
}

// ListReposByUser returns repo IDs that a user has been explicitly granted access to.
func (r *RepoMemberRepo) ListReposByUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT repo_id FROM repo_members WHERE user_id = $1`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list repos by user: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan repo id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// HasAccess checks if a user has been explicitly granted access to a repo.
// Returns false if no repo_members rows exist for this repo (meaning it's open to all org members).
func (r *RepoMemberRepo) HasAccess(ctx context.Context, repoID, userID uuid.UUID) (bool, error) {
	// First check if any repo_members rows exist for this repo.
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM repo_members WHERE repo_id = $1`, repoID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count repo members: %w", err)
	}
	// If no explicit members are set, repo is open to all org members.
	if count == 0 {
		return true, nil
	}
	// Otherwise, check if this specific user is in the list.
	err = r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM repo_members WHERE repo_id = $1 AND user_id = $2`,
		repoID, userID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check repo access: %w", err)
	}
	return count > 0, nil
}
