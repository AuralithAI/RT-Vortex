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

// ── OrgRepository ───────────────────────────────────────────────────────────

// OrgRepository handles organization and membership persistence.
type OrgRepository struct {
	pool *pgxpool.Pool
}

// NewOrgRepository creates an organization repository.
func NewOrgRepository(pool *pgxpool.Pool) *OrgRepository {
	return &OrgRepository{pool: pool}
}

// Create inserts a new organization.
func (r *OrgRepository) Create(ctx context.Context, org *model.Organization) error {
	org.ID = uuid.New()
	now := time.Now().UTC()
	org.CreatedAt = now
	org.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO organizations (id, name, slug, plan, settings, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		org.ID, org.Name, org.Slug, org.Plan, org.Settings, org.CreatedAt, org.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create organization: %w", err)
	}
	return nil
}

// GetByID returns an organization by ID.
func (r *OrgRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Organization, error) {
	org := &model.Organization{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, slug, plan, settings, created_at, updated_at
		 FROM organizations WHERE id = $1`, id,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.Plan, &org.Settings, &org.CreatedAt, &org.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}
	return org, nil
}

// GetBySlug returns an organization by slug.
func (r *OrgRepository) GetBySlug(ctx context.Context, slug string) (*model.Organization, error) {
	org := &model.Organization{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, slug, plan, settings, created_at, updated_at
		 FROM organizations WHERE slug = $1`, slug,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.Plan, &org.Settings, &org.CreatedAt, &org.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get organization by slug: %w", err)
	}
	return org, nil
}

// Update updates an organization.
func (r *OrgRepository) Update(ctx context.Context, org *model.Organization) error {
	org.UpdatedAt = time.Now().UTC()
	tag, err := r.pool.Exec(ctx,
		`UPDATE organizations SET name=$2, slug=$3, plan=$4, settings=$5, updated_at=$6
		 WHERE id=$1`,
		org.ID, org.Name, org.Slug, org.Plan, org.Settings, org.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update organization: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByUser returns organizations a user belongs to with pagination.
func (r *OrgRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*model.Organization, int, error) {
	// Get total count first.
	var total int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM organizations o
		 JOIN org_members om ON o.id = om.org_id
		 WHERE om.user_id = $1`, userID,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count orgs by user: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT o.id, o.name, o.slug, o.plan, o.settings, o.created_at, o.updated_at
		 FROM organizations o
		 JOIN org_members om ON o.id = om.org_id
		 WHERE om.user_id = $1
		 ORDER BY o.name
		 LIMIT $2 OFFSET $3`, userID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list orgs by user: %w", err)
	}
	defer rows.Close()

	var orgs []*model.Organization
	for rows.Next() {
		org := &model.Organization{}
		if err := rows.Scan(&org.ID, &org.Name, &org.Slug, &org.Plan, &org.Settings, &org.CreatedAt, &org.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan organization: %w", err)
		}
		orgs = append(orgs, org)
	}
	return orgs, total, nil
}

// ── Membership ──────────────────────────────────────────────────────────────

// AddMember adds a user to an organization.
func (r *OrgRepository) AddMember(ctx context.Context, orgID, userID uuid.UUID, role string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role, joined_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (org_id, user_id) DO UPDATE SET role=$3`,
		orgID, userID, role, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("add org member: %w", err)
	}
	return nil
}

// RemoveMember removes a user from an organization.
func (r *OrgRepository) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`,
		orgID, userID,
	)
	if err != nil {
		return fmt.Errorf("remove org member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListMembers returns members of an organization with pagination.
func (r *OrgRepository) ListMembers(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*MemberInfo, int, error) {
	// Get total count first.
	var total int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM org_members WHERE org_id = $1`, orgID,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count org members: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT u.id, u.email, u.display_name, u.avatar_url, om.role, om.joined_at
		 FROM users u
		 JOIN org_members om ON u.id = om.user_id
		 WHERE om.org_id = $1
		 ORDER BY om.joined_at
		 LIMIT $2 OFFSET $3`, orgID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list org members: %w", err)
	}
	defer rows.Close()

	var members []*MemberInfo
	for rows.Next() {
		m := &MemberInfo{}
		if err := rows.Scan(&m.UserID, &m.Email, &m.DisplayName, &m.AvatarURL, &m.Role, &m.JoinedAt); err != nil {
			return nil, 0, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, m)
	}
	return members, total, nil
}

// GetMember returns a user's membership in an organization.
func (r *OrgRepository) GetMember(ctx context.Context, orgID, userID uuid.UUID) (*model.OrgMember, error) {
	m := &model.OrgMember{}
	err := r.pool.QueryRow(ctx,
		`SELECT org_id, user_id, role, joined_at
		 FROM org_members WHERE org_id = $1 AND user_id = $2`,
		orgID, userID,
	).Scan(&m.OrgID, &m.UserID, &m.Role, &m.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get org member: %w", err)
	}
	return m, nil
}

// MemberInfo is a joined view of user + membership data.
type MemberInfo struct {
	UserID      uuid.UUID `json:"user_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
}
