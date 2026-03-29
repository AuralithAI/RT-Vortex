package rtvortex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SwarmClient provides methods for the /api/v1/swarm endpoints.
type SwarmClient struct {
	c *Client
}

// CreateTask creates a new swarm review task.
func (s *SwarmClient) CreateTask(ctx context.Context, req CreateTaskRequest) (*SwarmTask, error) {
	var task SwarmTask
	err := s.c.do(ctx, http.MethodPost, "/api/v1/swarm/tasks", req, &task)
	return &task, err
}

// ListTasks returns the user's tasks with optional status filter.
func (s *SwarmClient) ListTasks(ctx context.Context, status string, limit, offset int) ([]SwarmTask, error) {
	path := fmt.Sprintf("/api/v1/swarm/tasks?limit=%d&offset=%d", limit, offset)
	if status != "" {
		path += "&status=" + status
	}
	var resp struct {
		Tasks []SwarmTask `json:"tasks"`
		Total int         `json:"total"`
	}
	err := s.c.do(ctx, http.MethodGet, path, nil, &resp)
	return resp.Tasks, err
}

// GetTask retrieves a single task by ID.
func (s *SwarmClient) GetTask(ctx context.Context, id uuid.UUID) (*SwarmTask, error) {
	var task SwarmTask
	err := s.c.do(ctx, http.MethodGet, "/api/v1/swarm/tasks/"+id.String(), nil, &task)
	return &task, err
}

// DeleteTask deletes a task.
func (s *SwarmClient) DeleteTask(ctx context.Context, id uuid.UUID) error {
	return s.c.do(ctx, http.MethodDelete, "/api/v1/swarm/tasks/"+id.String(), nil, nil)
}

// CancelTask cancels a running task.
func (s *SwarmClient) CancelTask(ctx context.Context, id uuid.UUID) error {
	return s.c.do(ctx, http.MethodPost, "/api/v1/swarm/tasks/"+id.String()+"/cancel", nil, nil)
}

// RetryTask re-queues a failed task.
func (s *SwarmClient) RetryTask(ctx context.Context, id uuid.UUID) error {
	return s.c.do(ctx, http.MethodPost, "/api/v1/swarm/tasks/"+id.String()+"/retry", nil, nil)
}

// RateTask rates a completed task (1-5) which feeds into ELO auto-tier.
func (s *SwarmClient) RateTask(ctx context.Context, id uuid.UUID, rating int, feedback string) error {
	body := map[string]interface{}{"rating": rating}
	if feedback != "" {
		body["feedback"] = feedback
	}
	return s.c.do(ctx, http.MethodPost, "/api/v1/swarm/tasks/"+id.String()+"/rate", body, nil)
}

// PlanAction approves, rejects, or modifies the swarm's proposed plan.
func (s *SwarmClient) PlanAction(ctx context.Context, id uuid.UUID, action, modifications string) error {
	body := map[string]interface{}{"action": action}
	if modifications != "" {
		body["modifications"] = modifications
	}
	return s.c.do(ctx, http.MethodPost, "/api/v1/swarm/tasks/"+id.String()+"/plan-action", body, nil)
}

// DiffAction approves or rejects the produced diffs.
func (s *SwarmClient) DiffAction(ctx context.Context, id uuid.UUID, action, comment string) error {
	body := map[string]interface{}{"action": action}
	if comment != "" {
		body["comment"] = comment
	}
	return s.c.do(ctx, http.MethodPost, "/api/v1/swarm/tasks/"+id.String()+"/diff-action", body, nil)
}

// ListDiffs returns diffs for a task.
func (s *SwarmClient) ListDiffs(ctx context.Context, taskID uuid.UUID) ([]SwarmDiff, error) {
	var diffs []SwarmDiff
	err := s.c.do(ctx, http.MethodGet, "/api/v1/swarm/tasks/"+taskID.String()+"/diffs", nil, &diffs)
	return diffs, err
}

// ListAgents returns all registered agents.
func (s *SwarmClient) ListAgents(ctx context.Context) ([]SwarmAgent, error) {
	var agents []SwarmAgent
	err := s.c.do(ctx, http.MethodGet, "/api/v1/swarm/agents", nil, &agents)
	return agents, err
}

// Overview returns the swarm dashboard stats.
func (s *SwarmClient) Overview(ctx context.Context) (*SwarmOverview, error) {
	var ov SwarmOverview
	err := s.c.do(ctx, http.MethodGet, "/api/v1/swarm/overview", nil, &ov)
	return &ov, err
}

// HITLRespond submits a human-in-the-loop response.
func (s *SwarmClient) HITLRespond(ctx context.Context, taskID uuid.UUID, answer string, approved bool) error {
	body := map[string]interface{}{
		"task_id":  taskID,
		"answer":   answer,
		"approved": approved,
	}
	return s.c.do(ctx, http.MethodPost, "/api/v1/swarm/hitl/respond", body, nil)
}

// StreamEvents opens a WebSocket connection and returns a channel of events.
// The channel is closed when the connection ends or the context is cancelled.
func (s *SwarmClient) StreamEvents(ctx context.Context, taskID uuid.UUID) (<-chan SwarmWsEvent, error) {
	// Build the WebSocket URL.
	wsURL := strings.Replace(s.c.BaseURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += "/api/v1/swarm/tasks/" + taskID.String() + "/ws"

	header := http.Header{}
	if s.c.token != "" {
		header.Set("Authorization", "Bearer "+s.c.token)
	}

	// For the SDK stub we provide the URL and channel type.
	// Full WebSocket implementation requires gorilla/websocket or nhooyr.io/websocket
	// which will be added as a dependency when the SDK is finalized.
	ch := make(chan SwarmWsEvent)

	go func() {
		defer close(ch)
		// Polling fallback for the stub — production will use real WebSocket.
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var task SwarmTask
				if err := s.c.do(ctx, http.MethodGet, "/api/v1/swarm/tasks/"+taskID.String(), nil, &task); err != nil {
					return
				}
				ch <- SwarmWsEvent{
					Type:      "swarm_task",
					TaskID:    taskID.String(),
					Event:     "status_poll",
					Data:      map[string]interface{}{"status": task.Status},
					Timestamp: time.Now(),
				}
				if task.Status == "completed" || task.Status == "failed" || task.Status == "cancelled" {
					return
				}
			}
		}
	}()

	return ch, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// UnmarshalEvent parses raw JSON bytes into a SwarmWsEvent.
func UnmarshalEvent(data []byte) (*SwarmWsEvent, error) {
	var evt SwarmWsEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return nil, err
	}
	return &evt, nil
}
