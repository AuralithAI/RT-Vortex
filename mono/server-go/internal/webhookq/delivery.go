// Package webhookq provides reliable webhook delivery with retry support.
//
// When a webhook triggers a review pipeline, the delivery is recorded in the
// webhook_deliveries table. A background worker periodically retries failed
// deliveries with exponential backoff, and moves permanently failed deliveries
// to a dead-letter state after max attempts.
package webhookq

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── Delivery Status ─────────────────────────────────────────────────────────

// DeliveryStatus represents the state of a webhook delivery.
type DeliveryStatus string

const (
	StatusPending    DeliveryStatus = "pending"
	StatusProcessing DeliveryStatus = "processing"
	StatusCompleted  DeliveryStatus = "completed"
	StatusFailed     DeliveryStatus = "failed"
	StatusDead       DeliveryStatus = "dead" // max retries exceeded
)

// ── Delivery Record ─────────────────────────────────────────────────────────

// Delivery represents a single webhook delivery attempt record.
type Delivery struct {
	ID             uuid.UUID      `json:"id" db:"id"`
	WebhookEventID uuid.UUID      `json:"webhook_event_id" db:"webhook_event_id"`
	RepoID         uuid.UUID      `json:"repo_id" db:"repo_id"`
	Platform       string         `json:"platform" db:"platform"`
	PRNumber       int            `json:"pr_number" db:"pr_number"`
	Status         DeliveryStatus `json:"status" db:"status"`
	Attempts       int            `json:"attempts" db:"attempts"`
	MaxAttempts    int            `json:"max_attempts" db:"max_attempts"`
	LastError      string         `json:"last_error,omitempty" db:"last_error"`
	NextRetryAt    *time.Time     `json:"next_retry_at,omitempty" db:"next_retry_at"`
	CreatedAt      time.Time      `json:"created_at" db:"created_at"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty" db:"completed_at"`
}

// DefaultMaxAttempts is the default number of retry attempts.
const DefaultMaxAttempts = 5

// ── Repository ──────────────────────────────────────────────────────────────

// Repository handles persistence for webhook deliveries.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a webhook delivery repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts a new delivery record.
func (r *Repository) Create(ctx context.Context, d *Delivery) error {
	if r.pool == nil {
		return fmt.Errorf("create webhook delivery: database pool is nil")
	}
	d.ID = uuid.New()
	d.Status = StatusPending
	d.Attempts = 0
	d.CreatedAt = time.Now().UTC()
	if d.MaxAttempts == 0 {
		d.MaxAttempts = DefaultMaxAttempts
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO webhook_deliveries
		 (id, webhook_event_id, repo_id, platform, pr_number, status, attempts, max_attempts, last_error, next_retry_at, created_at, completed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		d.ID, d.WebhookEventID, d.RepoID, d.Platform, d.PRNumber,
		d.Status, d.Attempts, d.MaxAttempts, d.LastError, d.NextRetryAt,
		d.CreatedAt, d.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("create webhook delivery: %w", err)
	}
	return nil
}

// MarkProcessing marks a delivery as being processed.
func (r *Repository) MarkProcessing(ctx context.Context, id uuid.UUID) error {
	if r.pool == nil {
		return fmt.Errorf("mark processing: database pool is nil")
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE webhook_deliveries SET status = $2, attempts = attempts + 1 WHERE id = $1`,
		id, StatusProcessing,
	)
	if err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}
	return nil
}

// MarkCompleted marks a delivery as successfully completed.
func (r *Repository) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	if r.pool == nil {
		return fmt.Errorf("mark completed: database pool is nil")
	}
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx,
		`UPDATE webhook_deliveries SET status = $2, completed_at = $3, last_error = '' WHERE id = $1`,
		id, StatusCompleted, now,
	)
	if err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}
	return nil
}

// MarkFailed marks a delivery as failed with retry scheduling.
func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	if r.pool == nil {
		return fmt.Errorf("mark failed: database pool is nil")
	}
	// First get the current state to decide retry vs dead.
	var attempts, maxAttempts int
	err := r.pool.QueryRow(ctx,
		`SELECT attempts, max_attempts FROM webhook_deliveries WHERE id = $1`, id,
	).Scan(&attempts, &maxAttempts)
	if err != nil {
		return fmt.Errorf("get delivery state: %w", err)
	}

	if attempts >= maxAttempts {
		// Move to dead letter queue.
		_, err = r.pool.Exec(ctx,
			`UPDATE webhook_deliveries SET status = $2, last_error = $3 WHERE id = $1`,
			id, StatusDead, errMsg,
		)
	} else {
		// Schedule retry with exponential backoff: 30s, 2m, 8m, 32m, 128m
		backoff := time.Duration(math.Pow(4, float64(attempts))) * 30 * time.Second
		if backoff > 2*time.Hour {
			backoff = 2 * time.Hour
		}
		nextRetry := time.Now().UTC().Add(backoff)
		_, err = r.pool.Exec(ctx,
			`UPDATE webhook_deliveries SET status = $2, last_error = $3, next_retry_at = $4 WHERE id = $1`,
			id, StatusFailed, errMsg, nextRetry,
		)
	}
	if err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return nil
}

// ListRetryable returns deliveries that are due for retry.
func (r *Repository) ListRetryable(ctx context.Context, limit int) ([]*Delivery, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("list retryable: database pool is nil")
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, webhook_event_id, repo_id, platform, pr_number, status,
		        attempts, max_attempts, last_error, next_retry_at, created_at, completed_at
		 FROM webhook_deliveries
		 WHERE status = $1 AND next_retry_at <= NOW()
		 ORDER BY next_retry_at ASC
		 LIMIT $2`, StatusFailed, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list retryable: %w", err)
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

// ListPending returns pending deliveries that haven't been picked up yet.
func (r *Repository) ListPending(ctx context.Context, limit int) ([]*Delivery, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("list pending: database pool is nil")
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, webhook_event_id, repo_id, platform, pr_number, status,
		        attempts, max_attempts, last_error, next_retry_at, created_at, completed_at
		 FROM webhook_deliveries
		 WHERE status = $1
		 ORDER BY created_at ASC
		 LIMIT $2`, StatusPending, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

// ListDeadLetters returns deliveries that have exhausted retries.
func (r *Repository) ListDeadLetters(ctx context.Context, limit int) ([]*Delivery, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("list dead letters: database pool is nil")
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, webhook_event_id, repo_id, platform, pr_number, status,
		        attempts, max_attempts, last_error, next_retry_at, created_at, completed_at
		 FROM webhook_deliveries
		 WHERE status = $1
		 ORDER BY created_at DESC
		 LIMIT $2`, StatusDead, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list dead letters: %w", err)
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

// CountByStatus returns the count of deliveries by status.
func (r *Repository) CountByStatus(ctx context.Context) (map[DeliveryStatus]int, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("count by status: database pool is nil")
	}
	rows, err := r.pool.Query(ctx,
		`SELECT status, COUNT(*) FROM webhook_deliveries GROUP BY status`,
	)
	if err != nil {
		return nil, fmt.Errorf("count by status: %w", err)
	}
	defer rows.Close()

	result := make(map[DeliveryStatus]int)
	for rows.Next() {
		var status DeliveryStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan count: %w", err)
		}
		result[status] = count
	}
	return result, nil
}

// CleanupOld deletes completed/dead deliveries older than the given duration.
func (r *Repository) CleanupOld(ctx context.Context, olderThan time.Duration) (int64, error) {
	if r.pool == nil {
		return 0, fmt.Errorf("cleanup old deliveries: database pool is nil")
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM webhook_deliveries
		 WHERE (status = $1 OR status = $2) AND created_at < $3`,
		StatusCompleted, StatusDead, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup old deliveries: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanDeliveries(rows pgx.Rows) ([]*Delivery, error) {
	var deliveries []*Delivery
	for rows.Next() {
		d := &Delivery{}
		if err := rows.Scan(
			&d.ID, &d.WebhookEventID, &d.RepoID, &d.Platform, &d.PRNumber,
			&d.Status, &d.Attempts, &d.MaxAttempts, &d.LastError,
			&d.NextRetryAt, &d.CreatedAt, &d.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, nil
}

// ── Retry Worker ────────────────────────────────────────────────────────────

// ReviewExecutor is a function that executes a review for a webhook delivery.
type ReviewExecutor func(ctx context.Context, repoID uuid.UUID, prNumber int) error

// RetryWorker processes failed webhook deliveries.
type RetryWorker struct {
	repo     *Repository
	executor ReviewExecutor
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewRetryWorker creates a retry worker.
func NewRetryWorker(ctx context.Context, repo *Repository, executor ReviewExecutor) *RetryWorker {
	workerCtx, cancel := context.WithCancel(ctx)
	return &RetryWorker{
		repo:     repo,
		executor: executor,
		ctx:      workerCtx,
		cancel:   cancel,
	}
}

// Start begins the retry worker loop.
func (w *RetryWorker) Start(interval time.Duration) {
	slog.Info("webhook retry worker starting", "interval", interval)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-w.ctx.Done():
				slog.Info("webhook retry worker stopped")
				return
			case <-ticker.C:
				w.processBatch()
			}
		}
	}()
}

// Stop stops the retry worker.
func (w *RetryWorker) Stop() {
	w.cancel()
}

func (w *RetryWorker) processBatch() {
	// Process pending deliveries first.
	pending, err := w.repo.ListPending(w.ctx, 10)
	if err != nil {
		slog.Error("failed to list pending deliveries", "error", err)
		return
	}
	for _, d := range pending {
		w.processDelivery(d)
	}

	// Then process retryable deliveries.
	retryable, err := w.repo.ListRetryable(w.ctx, 10)
	if err != nil {
		slog.Error("failed to list retryable deliveries", "error", err)
		return
	}
	for _, d := range retryable {
		w.processDelivery(d)
	}
}

func (w *RetryWorker) processDelivery(d *Delivery) {
	slog.Info("processing webhook delivery",
		"delivery_id", d.ID,
		"repo_id", d.RepoID,
		"pr_number", d.PRNumber,
		"attempt", d.Attempts+1,
		"max_attempts", d.MaxAttempts,
	)

	if err := w.repo.MarkProcessing(w.ctx, d.ID); err != nil {
		slog.Error("failed to mark delivery processing", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Minute)
	defer cancel()

	if err := w.executor(ctx, d.RepoID, d.PRNumber); err != nil {
		slog.Error("webhook delivery failed",
			"delivery_id", d.ID, "attempt", d.Attempts+1, "error", err,
		)
		if markErr := w.repo.MarkFailed(w.ctx, d.ID, err.Error()); markErr != nil {
			slog.Error("failed to mark delivery as failed", "error", markErr)
		}
		return
	}

	if err := w.repo.MarkCompleted(w.ctx, d.ID); err != nil {
		slog.Error("failed to mark delivery completed", "error", err)
	}
	slog.Info("webhook delivery completed", "delivery_id", d.ID)
}
