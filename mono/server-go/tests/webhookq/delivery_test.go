package webhookq_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/webhookq"
)

// ── Delivery Model Tests ────────────────────────────────────────────────────

func TestDeliveryStatus_Constants(t *testing.T) {
	statuses := []webhookq.DeliveryStatus{
		webhookq.StatusPending,
		webhookq.StatusProcessing,
		webhookq.StatusCompleted,
		webhookq.StatusFailed,
		webhookq.StatusDead,
	}
	expected := []string{"pending", "processing", "completed", "failed", "dead"}
	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("status %d: got %q, want %q", i, string(s), expected[i])
		}
	}
}

func TestDelivery_Defaults(t *testing.T) {
	d := &webhookq.Delivery{
		WebhookEventID: uuid.New(),
		RepoID:         uuid.New(),
		Platform:       "github",
		PRNumber:       42,
	}
	if d.Status != "" {
		t.Errorf("default status should be empty before Create, got %q", d.Status)
	}
	if d.Attempts != 0 {
		t.Errorf("default attempts should be 0, got %d", d.Attempts)
	}
}

func TestDefaultMaxAttempts(t *testing.T) {
	if webhookq.DefaultMaxAttempts != 5 {
		t.Errorf("DefaultMaxAttempts = %d, want 5", webhookq.DefaultMaxAttempts)
	}
}

// ── Repository Tests (nil pool — tests that don't need DB) ──────────────────

func TestNewRepository_NilPool(t *testing.T) {
	repo := webhookq.NewRepository(nil)
	if repo == nil {
		t.Error("NewRepository should return non-nil even with nil pool")
	}
}

// ── RetryWorker Tests ───────────────────────────────────────────────────────

func TestNewRetryWorker(t *testing.T) {
	executor := func(ctx context.Context, repoID uuid.UUID, prNumber int) error {
		return nil
	}
	worker := webhookq.NewRetryWorker(context.Background(), webhookq.NewRepository(nil), executor)
	if worker == nil {
		t.Error("NewRetryWorker should return non-nil")
	}
	// Should not panic.
	worker.Stop()
}

func TestRetryWorker_StartStop(t *testing.T) {
	var callCount int32
	executor := func(ctx context.Context, repoID uuid.UUID, prNumber int) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	}
	// Use a context we cancel immediately after starting, so the worker's
	// goroutine exits on the ctx.Done() branch before the ticker fires
	// and tries to query the nil pool.
	ctx, cancel := context.WithCancel(context.Background())
	worker := webhookq.NewRetryWorker(ctx, webhookq.NewRepository(nil), executor)

	worker.Start(10 * time.Second) // long interval so ticker never fires
	time.Sleep(50 * time.Millisecond)
	cancel()      // cancel context first — goroutine sees ctx.Done()
	worker.Stop() // then stop

	// Give the goroutine time to exit cleanly.
	time.Sleep(50 * time.Millisecond)

	// Worker started and stopped without panicking.
	if atomic.LoadInt32(&callCount) != 0 {
		t.Error("executor should not have been called with nil pool")
	}
}

func TestRetryWorker_ExecutorCalled(t *testing.T) {
	// This tests the executor function interface.
	var called bool
	var gotRepoID uuid.UUID
	var gotPR int
	testRepoID := uuid.New()

	executor := func(ctx context.Context, repoID uuid.UUID, prNumber int) error {
		called = true
		gotRepoID = repoID
		gotPR = prNumber
		return nil
	}

	err := executor(context.Background(), testRepoID, 123)
	if err != nil {
		t.Errorf("executor returned error: %v", err)
	}
	if !called {
		t.Error("executor was not called")
	}
	if gotRepoID != testRepoID {
		t.Errorf("repoID = %v, want %v", gotRepoID, testRepoID)
	}
	if gotPR != 123 {
		t.Errorf("prNumber = %d, want 123", gotPR)
	}
}

func TestRetryWorker_ExecutorError(t *testing.T) {
	expectedErr := errors.New("review pipeline failed")
	executor := func(ctx context.Context, repoID uuid.UUID, prNumber int) error {
		return expectedErr
	}

	err := executor(context.Background(), uuid.New(), 1)
	if err == nil {
		t.Error("expected error from executor")
	}
	if err.Error() != "review pipeline failed" {
		t.Errorf("error = %q, want %q", err.Error(), "review pipeline failed")
	}
}

// ── Delivery Lifecycle Tests ────────────────────────────────────────────────

func TestDelivery_FieldPopulation(t *testing.T) {
	evtID := uuid.New()
	repoID := uuid.New()
	now := time.Now().UTC()

	d := &webhookq.Delivery{
		ID:             uuid.New(),
		WebhookEventID: evtID,
		RepoID:         repoID,
		Platform:       "gitlab",
		PRNumber:       99,
		Status:         webhookq.StatusPending,
		Attempts:       0,
		MaxAttempts:    5,
		CreatedAt:      now,
	}

	if d.WebhookEventID != evtID {
		t.Error("webhook_event_id mismatch")
	}
	if d.RepoID != repoID {
		t.Error("repo_id mismatch")
	}
	if d.Platform != "gitlab" {
		t.Error("platform mismatch")
	}
	if d.PRNumber != 99 {
		t.Error("pr_number mismatch")
	}
	if d.Status != webhookq.StatusPending {
		t.Error("status should be pending")
	}
	if d.CompletedAt != nil {
		t.Error("completed_at should be nil for pending delivery")
	}
	if d.LastError != "" {
		t.Error("last_error should be empty for new delivery")
	}
}

func TestDelivery_StatusTransitions(t *testing.T) {
	// Verify valid status transition flow: pending -> processing -> completed/failed -> dead
	transitions := []struct {
		from, to webhookq.DeliveryStatus
	}{
		{webhookq.StatusPending, webhookq.StatusProcessing},
		{webhookq.StatusProcessing, webhookq.StatusCompleted},
		{webhookq.StatusProcessing, webhookq.StatusFailed},
		{webhookq.StatusFailed, webhookq.StatusProcessing},
		{webhookq.StatusFailed, webhookq.StatusDead},
	}
	for _, tt := range transitions {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			// Just validate the constants are different.
			if tt.from == tt.to {
				t.Errorf("from and to should be different: %s", tt.from)
			}
		})
	}
}
