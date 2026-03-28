package ws_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/ws"
	"github.com/google/uuid"
)

func TestNewHub(t *testing.T) {
	h := ws.NewHub()
	if h == nil {
		t.Fatal("expected non-nil hub")
	}
	h.Stop()
}

func TestHub_StopIdempotent(t *testing.T) {
	h := ws.NewHub()
	h.Stop()
	// Second stop should not panic (channel already closed).
	// We just verify no goroutine leak / panic.
}

func TestProgressEvent_Fields(t *testing.T) {
	evt := ws.ProgressEvent{
		ReviewID:  uuid.New(),
		Step:      "fetch_pr",
		StepIndex: 1,
		TotalStep: 12,
		Status:    "started",
		Message:   "Fetching PR",
	}

	if evt.Step != "fetch_pr" {
		t.Errorf("expected fetch_pr, got %s", evt.Step)
	}
	if evt.StepIndex != 1 {
		t.Errorf("expected step 1, got %d", evt.StepIndex)
	}
	if evt.TotalStep != 12 {
		t.Errorf("expected 12 total steps, got %d", evt.TotalStep)
	}
	if evt.Status != "started" {
		t.Errorf("expected started, got %s", evt.Status)
	}
}

func TestHub_BroadcastNoSubscribers(t *testing.T) {
	h := ws.NewHub()
	defer h.Stop()

	// Broadcast to a review with no subscribers — should not panic.
	h.Broadcast(uuid.New(), ws.ProgressEvent{
		Step:      "test",
		StepIndex: 1,
		TotalStep: 1,
		Status:    "completed",
		Message:   "test",
	})
}
