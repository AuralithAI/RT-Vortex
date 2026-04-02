package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AuralithAI/rtvortex-server/internal/model"
)

// ── RepoLinkRepo ────────────────────────────────────────────────────────────

// RepoLinkRepo handles cross-repo link persistence.
type RepoLinkRepo struct {
	pool *pgxpool.Pool
}

// NewRepoLinkRepo creates a new RepoLinkRepo.
func NewRepoLinkRepo(pool *pgxpool.Pool) *RepoLinkRepo {
	return &RepoLinkRepo{pool: pool}
}

// ── CRUD ────────────────────────────────────────────────────────────────────

// Create inserts a new repo link. Returns ErrConflict if the link already exists.
func (r *RepoLinkRepo) Create(ctx context.Context, link *model.RepoLink) error {
	link.ID = uuid.New()
	now := time.Now().UTC()
	link.CreatedAt = now
	link.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO repo_links (id, org_id, source_repo_id, target_repo_id, share_profile, label, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		link.ID, link.OrgID, link.SourceRepoID, link.TargetRepoID,
		link.ShareProfile, link.Label, link.CreatedBy, link.CreatedAt, link.UpdatedAt,
	)
	if err != nil {
		// Detect unique violation (org_id, source_repo_id, target_repo_id).
		if isDuplicateKeyError(err) {
			return ErrConflict
		}
		return fmt.Errorf("create repo link: %w", err)
	}
	return nil
}

// GetByID returns a repo link by its ID.
func (r *RepoLinkRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.RepoLink, error) {
	link := &model.RepoLink{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, org_id, source_repo_id, target_repo_id, share_profile, label, created_by, created_at, updated_at
		 FROM repo_links WHERE id = $1`, id,
	).Scan(&link.ID, &link.OrgID, &link.SourceRepoID, &link.TargetRepoID,
		&link.ShareProfile, &link.Label, &link.CreatedBy, &link.CreatedAt, &link.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get repo link: %w", err)
	}
	return link, nil
}

// GetByRepos returns a link between two specific repos (directed: source → target).
func (r *RepoLinkRepo) GetByRepos(ctx context.Context, orgID, sourceRepoID, targetRepoID uuid.UUID) (*model.RepoLink, error) {
	link := &model.RepoLink{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, org_id, source_repo_id, target_repo_id, share_profile, label, created_by, created_at, updated_at
		 FROM repo_links WHERE org_id = $1 AND source_repo_id = $2 AND target_repo_id = $3`,
		orgID, sourceRepoID, targetRepoID,
	).Scan(&link.ID, &link.OrgID, &link.SourceRepoID, &link.TargetRepoID,
		&link.ShareProfile, &link.Label, &link.CreatedBy, &link.CreatedAt, &link.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get repo link by repos: %w", err)
	}
	return link, nil
}

// UpdateShareProfile updates a link's share profile and label.
func (r *RepoLinkRepo) UpdateShareProfile(ctx context.Context, id uuid.UUID, shareProfile, label string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE repo_links SET share_profile = $2, label = $3, updated_at = $4
		 WHERE id = $1`,
		id, shareProfile, label, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("update repo link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a repo link by ID.
func (r *RepoLinkRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM repo_links WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("delete repo link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ── Queries ─────────────────────────────────────────────────────────────────

// ListByOrg returns all links within an organization, with repo names.
func (r *RepoLinkRepo) ListByOrg(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*model.RepoLinkWithNames, int, error) {
	var total int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM repo_links WHERE org_id = $1`, orgID,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count repo links: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT rl.id, rl.org_id, rl.source_repo_id, rl.target_repo_id,
		        rl.share_profile, rl.label, rl.created_by, rl.created_at, rl.updated_at,
		        COALESCE(sr.owner || '/' || sr.name, '') AS source_name,
		        COALESCE(tr.owner || '/' || tr.name, '') AS target_name
		 FROM repo_links rl
		 LEFT JOIN repositories sr ON sr.id = rl.source_repo_id
		 LEFT JOIN repositories tr ON tr.id = rl.target_repo_id
		 WHERE rl.org_id = $1
		 ORDER BY rl.created_at DESC
		 LIMIT $2 OFFSET $3`, orgID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list repo links: %w", err)
	}
	defer rows.Close()

	var links []*model.RepoLinkWithNames
	for rows.Next() {
		lw := &model.RepoLinkWithNames{}
		if err := rows.Scan(
			&lw.ID, &lw.OrgID, &lw.SourceRepoID, &lw.TargetRepoID,
			&lw.ShareProfile, &lw.Label, &lw.CreatedBy, &lw.CreatedAt, &lw.UpdatedAt,
			&lw.SourceRepoName, &lw.TargetRepoName,
		); err != nil {
			return nil, 0, fmt.Errorf("scan repo link: %w", err)
		}
		links = append(links, lw)
	}
	return links, total, nil
}

// ListByRepo returns all links where the given repo is either source or target.
func (r *RepoLinkRepo) ListByRepo(ctx context.Context, repoID uuid.UUID) ([]*model.RepoLinkWithNames, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT rl.id, rl.org_id, rl.source_repo_id, rl.target_repo_id,
		        rl.share_profile, rl.label, rl.created_by, rl.created_at, rl.updated_at,
		        COALESCE(sr.owner || '/' || sr.name, '') AS source_name,
		        COALESCE(tr.owner || '/' || tr.name, '') AS target_name
		 FROM repo_links rl
		 LEFT JOIN repositories sr ON sr.id = rl.source_repo_id
		 LEFT JOIN repositories tr ON tr.id = rl.target_repo_id
		 WHERE rl.source_repo_id = $1 OR rl.target_repo_id = $1
		 ORDER BY rl.created_at DESC`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("list links by repo: %w", err)
	}
	defer rows.Close()

	var links []*model.RepoLinkWithNames
	for rows.Next() {
		lw := &model.RepoLinkWithNames{}
		if err := rows.Scan(
			&lw.ID, &lw.OrgID, &lw.SourceRepoID, &lw.TargetRepoID,
			&lw.ShareProfile, &lw.Label, &lw.CreatedBy, &lw.CreatedAt, &lw.UpdatedAt,
			&lw.SourceRepoName, &lw.TargetRepoName,
		); err != nil {
			return nil, fmt.Errorf("scan repo link: %w", err)
		}
		links = append(links, lw)
	}
	return links, nil
}

// ListLinkedRepoIDs returns the IDs of all repos linked FROM the given source repo
// that have an active share profile (not "none").
func (r *RepoLinkRepo) ListLinkedRepoIDs(ctx context.Context, sourceRepoID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT target_repo_id FROM repo_links
		 WHERE source_repo_id = $1 AND share_profile != 'none'`, sourceRepoID,
	)
	if err != nil {
		return nil, fmt.Errorf("list linked repo IDs: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan linked repo ID: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// CountByOrg returns the number of active links (share_profile != 'none') in an org.
func (r *RepoLinkRepo) CountByOrg(ctx context.Context, orgID uuid.UUID) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM repo_links WHERE org_id = $1 AND share_profile != 'none'`, orgID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count repo links by org: %w", err)
	}
	return count, nil
}

// ── Audit Events ────────────────────────────────────────────────────────────

// LogEvent writes a cross-repo link event to the repo_link_events table.
func (r *RepoLinkRepo) LogEvent(ctx context.Context, evt *model.RepoLinkEvent) error {
	meta, err := json.Marshal(evt.Metadata)
	if err != nil {
		meta = []byte("{}")
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO repo_link_events (link_id, org_id, source_repo_id, target_repo_id, action, actor_id, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
		evt.LinkID, evt.OrgID, evt.SourceRepoID, evt.TargetRepoID,
		evt.Action, evt.ActorID, meta,
	)
	if err != nil {
		return fmt.Errorf("log repo link event: %w", err)
	}
	return nil
}

// ListEvents returns recent events for a link or org.
func (r *RepoLinkRepo) ListEvents(ctx context.Context, orgID uuid.UUID, linkID *uuid.UUID, limit int) ([]*model.RepoLinkEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	var rows pgx.Rows
	var err error
	if linkID != nil {
		rows, err = r.pool.Query(ctx,
			`SELECT id, link_id, org_id, source_repo_id, target_repo_id, action, actor_id, metadata, created_at
			 FROM repo_link_events
			 WHERE org_id = $1 AND link_id = $2
			 ORDER BY created_at DESC LIMIT $3`, orgID, *linkID, limit,
		)
	} else {
		rows, err = r.pool.Query(ctx,
			`SELECT id, link_id, org_id, source_repo_id, target_repo_id, action, actor_id, metadata, created_at
			 FROM repo_link_events
			 WHERE org_id = $1
			 ORDER BY created_at DESC LIMIT $2`, orgID, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("list repo link events: %w", err)
	}
	defer rows.Close()

	var events []*model.RepoLinkEvent
	for rows.Next() {
		e := &model.RepoLinkEvent{}
		var meta []byte
		if err := rows.Scan(&e.ID, &e.LinkID, &e.OrgID, &e.SourceRepoID, &e.TargetRepoID,
			&e.Action, &e.ActorID, &meta, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan repo link event: %w", err)
		}
		_ = json.Unmarshal(meta, &e.Metadata)
		events = append(events, e)
	}
	return events, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// isDuplicateKeyError checks if a pgx error is a unique constraint violation.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	// Walk the error chain looking for a PgError with code 23505 (unique_violation).
	return containsCode(err, "23505")
}

func containsCode(err error, code string) bool {
	type pgErr interface{ SQLState() string }
	var pge pgErr
	if errors.As(err, &pge) {
		return pge.SQLState() == code
	}
	return false
}
