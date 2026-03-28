package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AuralithAI/rtvortex-server/internal/model"
)

// ── WebhookRepository ───────────────────────────────────────────────────────

// WebhookRepository handles webhook event persistence for auditing.
type WebhookRepository struct {
	pool *pgxpool.Pool
}

// NewWebhookRepository creates a webhook repository.
func NewWebhookRepository(pool *pgxpool.Pool) *WebhookRepository {
	return &WebhookRepository{pool: pool}
}

// RecordEvent inserts a webhook event for audit trail.
func (r *WebhookRepository) RecordEvent(ctx context.Context, evt *model.WebhookEvent) error {
	evt.ID = uuid.New()
	evt.CreatedAt = time.Now().UTC()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO webhook_events (id, platform, event_type, repo_id, payload, processed, error, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		evt.ID, evt.Platform, evt.EventType, evt.RepoID, evt.Payload, evt.Processed, evt.Error, evt.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record webhook event: %w", err)
	}
	return nil
}

// MarkProcessed marks a webhook event as processed.
func (r *WebhookRepository) MarkProcessed(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE webhook_events SET processed=true, error=$2 WHERE id=$1`,
		id, errMsg,
	)
	if err != nil {
		return fmt.Errorf("mark webhook processed: %w", err)
	}
	return nil
}
