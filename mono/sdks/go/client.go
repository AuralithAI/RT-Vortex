// Package rtvortex provides a Go client for the RTVortex API.
//
// Usage:
//
//	client := rtvortex.NewClient("https://api.rtvortex.example.com",
//	    rtvortex.WithToken("your-jwt-token"),
//	)
//
//	// Create a swarm task
//	task, err := client.Swarm.CreateTask(ctx, rtvortex.CreateTaskRequest{
//	    RepoID: repoID,
//	    Title:  "Review PR #42",
//	})
//
//	// Stream real-time events
//	events, err := client.Swarm.StreamEvents(ctx, task.ID)
//	for evt := range events {
//	    fmt.Printf("[%s] %s: %s\n", evt.Type, evt.Event, evt.Data)
//	}
package rtvortex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// ── Client ──────────────────────────────────────────────────────────────────

// Client is the top-level RTVortex API client.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	token      string

	// Sub-clients for each API area.
	Swarm  *SwarmClient
	Repos  *RepoClient
	Reviews *ReviewClient
}

// Option configures the Client.
type Option func(*Client)

// WithToken sets the JWT bearer token.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.HTTPClient = hc }
}

// NewClient creates a new RTVortex API client.
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, o := range opts {
		o(c)
	}
	c.Swarm = &SwarmClient{c: c}
	c.Repos = &RepoClient{c: c}
	c.Reviews = &ReviewClient{c: c}
	return c
}

// do executes an HTTP request with auth headers and JSON body.
func (c *Client) do(ctx context.Context, method, path string, body, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// ── Error ───────────────────────────────────────────────────────────────────

// APIError represents an HTTP error from the RTVortex API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("rtvortex: HTTP %d: %s", e.StatusCode, e.Body)
}

// ── Shared types ────────────────────────────────────────────────────────────

// SwarmTask represents a swarm review task.
type SwarmTask struct {
	ID          uuid.UUID  `json:"id"`
	RepoID      uuid.UUID  `json:"repo_id"`
	PRNumber    int        `json:"pr_number,omitempty"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"`
	Priority    int        `json:"priority"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// SwarmAgent represents a registered swarm agent.
type SwarmAgent struct {
	ID            uuid.UUID  `json:"id"`
	Role          string     `json:"role"`
	Status        string     `json:"status"`
	ELORating     float64    `json:"elo_rating"`
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty"`
}

// SwarmDiff represents a diff produced by the swarm.
type SwarmDiff struct {
	ID        uuid.UUID `json:"id"`
	TaskID    uuid.UUID `json:"task_id"`
	FilePath  string    `json:"file_path"`
	DiffType  string    `json:"diff_type"`
	Content   string    `json:"content"`
	AgentRole string    `json:"agent_role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// SwarmWsEvent is a real-time event from the swarm WebSocket.
type SwarmWsEvent struct {
	Type      string                 `json:"type"`
	TaskID    string                 `json:"task_id,omitempty"`
	AgentID   string                 `json:"agent_id,omitempty"`
	Event     string                 `json:"event"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// SwarmOverview holds dashboard summary stats.
type SwarmOverview struct {
	ActiveTasks          int     `json:"active_tasks"`
	QueuedTasks          int     `json:"queued_tasks"`
	ActiveAgents         int     `json:"active_agents"`
	ActiveTeams          int     `json:"active_teams"`
	TasksCompleted24h    int     `json:"tasks_completed_24h"`
	AvgCompletionSeconds float64 `json:"avg_completion_seconds"`
}

// CreateTaskRequest is the payload for creating a new swarm task.
type CreateTaskRequest struct {
	RepoID      uuid.UUID `json:"repo_id"`
	PRNumber    int       `json:"pr_number,omitempty"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	Priority    int       `json:"priority,omitempty"`
}
