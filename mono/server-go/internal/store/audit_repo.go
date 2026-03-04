package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditEntry represents a row in the audit_log table.
type AuditEntry struct {
	ID           uuid.UUID              `json:"id"`
	UserID       *uuid.UUID             `json:"user_id,omitempty"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type,omitempty"`
	ResourceID   string                 `json:"resource_id,omitempty"`
	IPAddress    string                 `json:"ip_address,omitempty"`
	UserAgent    string                 `json:"user_agent,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// AuditRepository provides persistence for audit log entries.
type AuditRepository struct {
	pool *pgxpool.Pool
}

// NewAuditRepository creates an AuditRepository.
func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{pool: pool}
}

// Create inserts a new audit log entry.
func (r *AuditRepository) Create(ctx context.Context, entry *AuditEntry) error {
	meta, err := json.Marshal(entry.Metadata)
	if err != nil {
		meta = []byte("{}")
	}

	const q = `
		INSERT INTO audit_log (user_id, action, resource_type, resource_id, ip_address, user_agent, metadata)
		VALUES ($1, $2, $3, $4, $5::inet, $6, $7::jsonb)
	`

	// Allow nil IP to avoid INET parse errors on empty string.
	var ipArg interface{}
	if entry.IPAddress != "" {
		ipArg = entry.IPAddress
	}

	_, err = r.pool.Exec(ctx, q,
		entry.UserID,
		entry.Action,
		entry.ResourceType,
		entry.ResourceID,
		ipArg,
		entry.UserAgent,
		meta,
	)
	if err != nil {
		return fmt.Errorf("audit_log insert: %w", err)
	}
	return nil
}

// ListByUser returns recent audit entries for a specific user (most recent first).
func (r *AuditRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
		SELECT id, user_id, action, resource_type, resource_id,
		       host(ip_address)::text, user_agent, metadata
		FROM audit_log
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := r.pool.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("audit_log query: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var meta []byte
		var ip *string
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.ResourceType, &e.ResourceID, &ip, &e.UserAgent, &meta); err != nil {
			slog.Error("audit_log scan", "error", err)
			continue
		}
		if ip != nil {
			e.IPAddress = *ip
		}
		_ = json.Unmarshal(meta, &e.Metadata)
		entries = append(entries, e)
	}
	return entries, nil
}
