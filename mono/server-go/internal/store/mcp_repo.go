package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MCPConnection struct {
	ID              uuid.UUID  `json:"id"`
	UserID          uuid.UUID  `json:"user_id"`
	OrgID           *uuid.UUID `json:"org_id,omitempty"`
	IsOrgLevel      bool       `json:"is_org_level"`
	Provider        string     `json:"provider"`
	Status          string     `json:"status"`
	VaultKey        string     `json:"vault_key"`
	RefreshVaultKey string     `json:"-"`
	Scopes          []string   `json:"scopes"`
	Metadata        string     `json:"metadata,omitempty"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	ConnectedAt     time.Time  `json:"connected_at"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type MCPCallLogEntry struct {
	ID           uuid.UUID `json:"id"`
	ConnectionID uuid.UUID `json:"connection_id"`
	AgentID      string    `json:"agent_id,omitempty"`
	TaskID       string    `json:"task_id,omitempty"`
	Action       string    `json:"action"`
	InputHash    string    `json:"input_hash"`
	OutputHash   string    `json:"output_hash"`
	LatencyMs    int       `json:"latency_ms"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type MCPRepository struct {
	pool *pgxpool.Pool
}

func NewMCPRepository(pool *pgxpool.Pool) *MCPRepository {
	return &MCPRepository{pool: pool}
}

func (r *MCPRepository) CreateConnection(ctx context.Context, conn *MCPConnection) error {
	if conn.ID == uuid.Nil {
		conn.ID = uuid.New()
	}
	now := time.Now().UTC()
	conn.CreatedAt = now
	conn.UpdatedAt = now
	if conn.ConnectedAt.IsZero() {
		conn.ConnectedAt = now
	}
	// Ensure metadata is valid JSON for the jsonb column.
	if conn.Metadata == "" {
		conn.Metadata = "{}"
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO mcp_connections
			(id, user_id, org_id, is_org_level, provider, status, vault_key, refresh_vault_key, scopes, metadata, connected_at, expires_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14)`,
		conn.ID, conn.UserID, conn.OrgID, conn.IsOrgLevel, conn.Provider, conn.Status,
		conn.VaultKey, conn.RefreshVaultKey, conn.Scopes, conn.Metadata,
		conn.ConnectedAt, conn.ExpiresAt, conn.CreatedAt, conn.UpdatedAt,
	)
	return err
}

func (r *MCPRepository) GetConnection(ctx context.Context, id uuid.UUID) (*MCPConnection, error) {
	var c MCPConnection
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, org_id, is_org_level, provider, status, vault_key, refresh_vault_key,
		        scopes, metadata, last_used_at, connected_at, expires_at, created_at, updated_at
		 FROM mcp_connections WHERE id = $1`, id,
	).Scan(
		&c.ID, &c.UserID, &c.OrgID, &c.IsOrgLevel, &c.Provider, &c.Status,
		&c.VaultKey, &c.RefreshVaultKey, &c.Scopes, &c.Metadata,
		&c.LastUsedAt, &c.ConnectedAt, &c.ExpiresAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *MCPRepository) FindActiveConnection(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, provider string) (*MCPConnection, error) {
	q := `SELECT id, user_id, org_id, is_org_level, provider, status, vault_key, refresh_vault_key,
	             scopes, metadata, last_used_at, connected_at, expires_at, created_at, updated_at
	      FROM mcp_connections
	      WHERE provider = $1 AND status = 'active'
	        AND (user_id = $2 OR (is_org_level = true AND org_id = $3))
	      ORDER BY is_org_level ASC
	      LIMIT 1`

	var c MCPConnection
	err := r.pool.QueryRow(ctx, q, provider, userID, orgID).Scan(
		&c.ID, &c.UserID, &c.OrgID, &c.IsOrgLevel, &c.Provider, &c.Status,
		&c.VaultKey, &c.RefreshVaultKey, &c.Scopes, &c.Metadata,
		&c.LastUsedAt, &c.ConnectedAt, &c.ExpiresAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *MCPRepository) ListByUser(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID) ([]MCPConnection, error) {
	q := `SELECT id, user_id, org_id, is_org_level, provider, status, vault_key, refresh_vault_key,
	             scopes, metadata, last_used_at, connected_at, expires_at, created_at, updated_at
	      FROM mcp_connections
	      WHERE user_id = $1 OR (is_org_level = true AND org_id = $2)
	      ORDER BY provider, is_org_level ASC`

	rows, err := r.pool.Query(ctx, q, userID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MCPConnection, error) {
		var c MCPConnection
		err := row.Scan(
			&c.ID, &c.UserID, &c.OrgID, &c.IsOrgLevel, &c.Provider, &c.Status,
			&c.VaultKey, &c.RefreshVaultKey, &c.Scopes, &c.Metadata,
			&c.LastUsedAt, &c.ConnectedAt, &c.ExpiresAt, &c.CreatedAt, &c.UpdatedAt,
		)
		return c, err
	})
}

func (r *MCPRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE mcp_connections SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (r *MCPRepository) UpdateTokenRefs(ctx context.Context, id uuid.UUID, vaultKey, refreshVaultKey string, expiresAt *time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE mcp_connections SET vault_key = $1, refresh_vault_key = $2, expires_at = $3, status = 'active' WHERE id = $4`,
		vaultKey, refreshVaultKey, expiresAt, id)
	return err
}

func (r *MCPRepository) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE mcp_connections SET last_used_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *MCPRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM mcp_connections WHERE id = $1`, id)
	return err
}

func (r *MCPRepository) ListExpiring(ctx context.Context, threshold time.Duration) ([]MCPConnection, error) {
	q := `SELECT id, user_id, org_id, is_org_level, provider, status, vault_key, refresh_vault_key,
	             scopes, metadata, last_used_at, connected_at, expires_at, created_at, updated_at
	      FROM mcp_connections
	      WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at < (NOW() + $1::interval)`

	rows, err := r.pool.Query(ctx, q, fmt.Sprintf("%d seconds", int(threshold.Seconds())))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MCPConnection, error) {
		var c MCPConnection
		err := row.Scan(
			&c.ID, &c.UserID, &c.OrgID, &c.IsOrgLevel, &c.Provider, &c.Status,
			&c.VaultKey, &c.RefreshVaultKey, &c.Scopes, &c.Metadata,
			&c.LastUsedAt, &c.ConnectedAt, &c.ExpiresAt, &c.CreatedAt, &c.UpdatedAt,
		)
		return c, err
	})
}

func (r *MCPRepository) InsertCallLog(ctx context.Context, entry *MCPCallLogEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	entry.CreatedAt = time.Now().UTC()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO mcp_call_log (id, connection_id, agent_id, task_id, action, input_hash, output_hash, latency_ms, status, error_message, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		entry.ID, entry.ConnectionID, entry.AgentID, entry.TaskID,
		entry.Action, entry.InputHash, entry.OutputHash,
		entry.LatencyMs, entry.Status, entry.ErrorMessage, entry.CreatedAt,
	)
	return err
}

func (r *MCPRepository) ListCallLog(ctx context.Context, connectionID uuid.UUID, limit int) ([]MCPCallLogEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, connection_id, agent_id, task_id, action, input_hash, output_hash, latency_ms, status, error_message, created_at
	      FROM mcp_call_log WHERE connection_id = $1 ORDER BY created_at DESC LIMIT $2`

	rows, err := r.pool.Query(ctx, q, connectionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MCPCallLogEntry, error) {
		var e MCPCallLogEntry
		err := row.Scan(&e.ID, &e.ConnectionID, &e.AgentID, &e.TaskID, &e.Action,
			&e.InputHash, &e.OutputHash, &e.LatencyMs, &e.Status, &e.ErrorMessage, &e.CreatedAt)
		return e, err
	})
}

func (r *MCPRepository) CountCallsForTask(ctx context.Context, taskID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM mcp_call_log WHERE task_id = $1`, taskID).Scan(&count)
	return count, err
}

// ── Custom MCP Templates ────────────────────────────────────────────────────

type MCPCustomTemplate struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Label       string     `json:"label"`
	Category    string     `json:"category"`
	Description string     `json:"description"`
	BaseURL     string     `json:"base_url"`
	AuthType    string     `json:"auth_type"`
	AuthHeader  string     `json:"auth_header"`
	ActionsJSON string     `json:"-"`
	CreatedBy   uuid.UUID  `json:"created_by"`
	OrgID       *uuid.UUID `json:"org_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (r *MCPRepository) CreateCustomTemplate(ctx context.Context, t *MCPCustomTemplate) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO mcp_custom_templates
			(id, name, label, category, description, base_url, auth_type, auth_header, actions, created_by, org_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13)`,
		t.ID, t.Name, t.Label, t.Category, t.Description, t.BaseURL,
		t.AuthType, t.AuthHeader, t.ActionsJSON, t.CreatedBy, t.OrgID,
		t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (r *MCPRepository) GetCustomTemplate(ctx context.Context, id uuid.UUID) (*MCPCustomTemplate, error) {
	var t MCPCustomTemplate
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, label, category, description, base_url, auth_type, auth_header,
		        actions::text, created_by, org_id, created_at, updated_at
		 FROM mcp_custom_templates WHERE id = $1`, id,
	).Scan(
		&t.ID, &t.Name, &t.Label, &t.Category, &t.Description, &t.BaseURL,
		&t.AuthType, &t.AuthHeader, &t.ActionsJSON, &t.CreatedBy, &t.OrgID,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *MCPRepository) GetCustomTemplateByName(ctx context.Context, name string) (*MCPCustomTemplate, error) {
	var t MCPCustomTemplate
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, label, category, description, base_url, auth_type, auth_header,
		        actions::text, created_by, org_id, created_at, updated_at
		 FROM mcp_custom_templates WHERE name = $1`, name,
	).Scan(
		&t.ID, &t.Name, &t.Label, &t.Category, &t.Description, &t.BaseURL,
		&t.AuthType, &t.AuthHeader, &t.ActionsJSON, &t.CreatedBy, &t.OrgID,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *MCPRepository) ListCustomTemplates(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID) ([]MCPCustomTemplate, error) {
	q := `SELECT id, name, label, category, description, base_url, auth_type, auth_header,
	             actions::text, created_by, org_id, created_at, updated_at
	      FROM mcp_custom_templates
	      WHERE created_by = $1 OR org_id = $2
	      ORDER BY name`

	rows, err := r.pool.Query(ctx, q, userID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MCPCustomTemplate, error) {
		var t MCPCustomTemplate
		err := row.Scan(
			&t.ID, &t.Name, &t.Label, &t.Category, &t.Description, &t.BaseURL,
			&t.AuthType, &t.AuthHeader, &t.ActionsJSON, &t.CreatedBy, &t.OrgID,
			&t.CreatedAt, &t.UpdatedAt,
		)
		return t, err
	})
}

func (r *MCPRepository) UpdateCustomTemplate(ctx context.Context, t *MCPCustomTemplate) error {
	t.UpdatedAt = time.Now().UTC()
	_, err := r.pool.Exec(ctx,
		`UPDATE mcp_custom_templates
		 SET label = $1, category = $2, description = $3, base_url = $4,
		     auth_type = $5, auth_header = $6, actions = $7::jsonb, updated_at = $8
		 WHERE id = $9`,
		t.Label, t.Category, t.Description, t.BaseURL,
		t.AuthType, t.AuthHeader, t.ActionsJSON, t.UpdatedAt, t.ID,
	)
	return err
}

func (r *MCPRepository) DeleteCustomTemplate(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM mcp_custom_templates WHERE id = $1`, id)
	return err
}

// ListAllCustomTemplates returns every template (used on startup to register providers).
func (r *MCPRepository) ListAllCustomTemplates(ctx context.Context) ([]MCPCustomTemplate, error) {
	q := `SELECT id, name, label, category, description, base_url, auth_type, auth_header,
	             actions::text, created_by, org_id, created_at, updated_at
	      FROM mcp_custom_templates
	      ORDER BY name`

	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MCPCustomTemplate, error) {
		var t MCPCustomTemplate
		err := row.Scan(
			&t.ID, &t.Name, &t.Label, &t.Category, &t.Description, &t.BaseURL,
			&t.AuthType, &t.AuthHeader, &t.ActionsJSON, &t.CreatedBy, &t.OrgID,
			&t.CreatedAt, &t.UpdatedAt,
		)
		return t, err
	})
}
