// Package ws provides real-time WebSocket streaming of review progress events.
//
// Architecture:
//
//	Hub (singleton)
//	 ├── per-review channel map: reviewID → set of *Client
//	 ├── Broadcast(reviewID, event) → fans out to all subscribed clients
//	 └── manages client lifecycle (register / unregister)
//
//	Client (one per WebSocket connection)
//	 ├── reads from the send channel → writes to the WebSocket
//	 └── keeps alive with periodic pings
//
// Usage in the pipeline:
//
//	hub.Broadcast(reviewID, ProgressEvent{Step: "fetch_diff", ...})
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/metrics"
)

// ── Event types ─────────────────────────────────────────────────────────────

// ProgressEvent is the payload sent to WebSocket clients when a review
// pipeline step starts or completes.
type ProgressEvent struct {
	ReviewID  uuid.UUID              `json:"review_id"`
	Step      string                 `json:"step"`               // e.g. "fetch_pr", "analyze_files"
	StepIndex int                    `json:"step_index"`         // 1-based step number
	TotalStep int                    `json:"total_steps"`        // total pipeline steps (12)
	Status    string                 `json:"status"`             // "started", "completed", "failed"
	Message   string                 `json:"message,omitempty"`  // human-readable status
	Metadata  map[string]interface{} `json:"metadata,omitempty"` // extra data (file count, etc.)
	Timestamp time.Time              `json:"timestamp"`
}

// IndexProgressEvent is the payload sent to WebSocket clients during indexing.
type IndexProgressEvent struct {
	Type           string    `json:"type"` // always "index_progress"
	RepoID         string    `json:"repo_id"`
	JobID          string    `json:"job_id"`
	State          string    `json:"state"`    // "pending", "queued", "running", "completed", "failed"
	Progress       int       `json:"progress"` // 0-100
	Phase          string    `json:"phase"`    // "cloning", "scanning", "chunking", etc.
	Message        string    `json:"message,omitempty"`
	FilesProcessed uint64    `json:"files_processed"`
	FilesTotal     uint64    `json:"files_total"`
	CurrentFile    string    `json:"current_file,omitempty"`
	ETASeconds     int64     `json:"eta_seconds"` // -1 = unknown
	Error          string    `json:"error,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

// PREmbedProgressEvent is the payload sent to WebSocket clients during PR diff embedding.
type PREmbedProgressEvent struct {
	Type           string    `json:"type"` // always "pr_embed_progress"
	RepoID         string    `json:"repo_id"`
	PRNumber       int       `json:"pr_number"`
	PRID           string    `json:"pr_id"`    // tracked PR UUID
	State          string    `json:"state"`    // "pending", "embedding", "completed", "failed"
	Progress       int       `json:"progress"` // 0-100
	Phase          string    `json:"phase"`    // "parsing_diff", "resolving_symbols", "building_graph", "embedding_chunks", "finalizing"
	Message        string    `json:"message,omitempty"`
	FilesProcessed uint32    `json:"files_processed"`
	FilesTotal     uint32    `json:"files_total"`
	CurrentFile    string    `json:"current_file,omitempty"`
	ETASeconds     int64     `json:"eta_seconds"` // -1 = unknown
	Error          string    `json:"error,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

// SwarmEvent is the payload sent to WebSocket clients for swarm activity.
type SwarmEvent struct {
	Type      string                 `json:"type"` // "swarm_task", "swarm_agent", "swarm_diff", "swarm_plan"
	TaskID    string                 `json:"task_id,omitempty"`
	AgentID   string                 `json:"agent_id,omitempty"`
	Event     string                 `json:"event"` // "created", "status_changed", "plan_submitted", "diff_submitted", etc.
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// ── Client ──────────────────────────────────────────────────────────────────

// Client represents a single WebSocket subscriber.
type Client struct {
	conn        *websocket.Conn
	reviewID    uuid.UUID // set for review subscriptions
	indexRepoID string    // set for indexing subscriptions
	embedRepoID string    // set for PR embed progress subscriptions
	swarmTaskID string    // set for swarm task subscriptions
	metrics     bool      // set for engine metrics subscriptions
	send        chan []byte
}

const (
	sendBufSize = 64 // per-client outbound buffer; safe now that streaming
	// chunks are accumulated server-side (2 WS msgs per
	// provider instead of hundreds).
	writeWait = 10 * time.Second
)

// ── Hub ─────────────────────────────────────────────────────────────────────

// Hub manages WebSocket clients subscribed to review progress.
type Hub struct {
	mu       sync.RWMutex
	reviews  map[uuid.UUID]map[*Client]struct{} // reviewID → set of clients
	indexes  map[string]map[*Client]struct{}    // repoID → set of clients (indexing)
	prEmbeds map[string]map[*Client]struct{}    // repoID → set of clients (PR embedding)
	swarm    map[string]map[*Client]struct{}    // taskID → set of clients (swarm)
	metrics  map[*Client]struct{}               // engine metrics subscribers

	// swarmHistory stores recent swarm events per task so that newly-connecting
	// WebSocket clients receive the full discussion history
	swarmHistory map[string][]swarmHistoryEntry

	register   chan *Client
	unregister chan *Client
	done       chan struct{}
}

// swarmHistoryEntry holds a pre-serialized swarm event for replay.
type swarmHistoryEntry struct {
	data []byte
}

// Maximum number of swarm events kept per task for replay.
// With server-side chunk accumulation, only structural events are stored
// (thread_opened, provider_response, consensus_result, etc.).
const swarmHistoryMax = 200

// NewHub creates and starts a new Hub.
func NewHub() *Hub {
	h := &Hub{
		reviews:      make(map[uuid.UUID]map[*Client]struct{}),
		indexes:      make(map[string]map[*Client]struct{}),
		prEmbeds:     make(map[string]map[*Client]struct{}),
		swarm:        make(map[string]map[*Client]struct{}),
		metrics:      make(map[*Client]struct{}),
		swarmHistory: make(map[string][]swarmHistoryEntry),
		register:     make(chan *Client, 32),
		unregister:   make(chan *Client, 32),
		done:         make(chan struct{}),
	}
	go h.run()
	return h
}

// Stop shuts down the hub.
func (h *Hub) Stop() {
	close(h.done)
}

func (h *Hub) run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			if c.metrics {
				// Engine metrics subscription
				h.metrics[c] = struct{}{}
				slog.Debug("ws metrics client registered")
			} else if c.swarmTaskID != "" {
				// Swarm task subscription
				clients, ok := h.swarm[c.swarmTaskID]
				if !ok {
					clients = make(map[*Client]struct{})
					h.swarm[c.swarmTaskID] = clients
				}
				clients[c] = struct{}{}
				slog.Debug("ws swarm client registered", "task_id", c.swarmTaskID)
			} else if c.embedRepoID != "" {
				// PR embed subscription
				clients, ok := h.prEmbeds[c.embedRepoID]
				if !ok {
					clients = make(map[*Client]struct{})
					h.prEmbeds[c.embedRepoID] = clients
				}
				clients[c] = struct{}{}
				slog.Debug("ws pr-embed client registered", "repo_id", c.embedRepoID)
			} else if c.indexRepoID != "" {
				// Indexing subscription
				clients, ok := h.indexes[c.indexRepoID]
				if !ok {
					clients = make(map[*Client]struct{})
					h.indexes[c.indexRepoID] = clients
				}
				clients[c] = struct{}{}
				slog.Debug("ws index client registered", "repo_id", c.indexRepoID)
			} else {
				// Review subscription
				clients, ok := h.reviews[c.reviewID]
				if !ok {
					clients = make(map[*Client]struct{})
					h.reviews[c.reviewID] = clients
				}
				clients[c] = struct{}{}
				slog.Debug("ws client registered", "review_id", c.reviewID)
			}
			h.mu.Unlock()

		case c := <-h.unregister:
			h.mu.Lock()
			if c.metrics {
				delete(h.metrics, c)
				slog.Debug("ws metrics client unregistered")
			} else if c.swarmTaskID != "" {
				if clients, ok := h.swarm[c.swarmTaskID]; ok {
					delete(clients, c)
					if len(clients) == 0 {
						delete(h.swarm, c.swarmTaskID)
					}
				}
				slog.Debug("ws swarm client unregistered", "task_id", c.swarmTaskID)
			} else if c.embedRepoID != "" {
				if clients, ok := h.prEmbeds[c.embedRepoID]; ok {
					delete(clients, c)
					if len(clients) == 0 {
						delete(h.prEmbeds, c.embedRepoID)
					}
				}
				slog.Debug("ws pr-embed client unregistered", "repo_id", c.embedRepoID)
			} else if c.indexRepoID != "" {
				if clients, ok := h.indexes[c.indexRepoID]; ok {
					delete(clients, c)
					if len(clients) == 0 {
						delete(h.indexes, c.indexRepoID)
					}
				}
				slog.Debug("ws index client unregistered", "repo_id", c.indexRepoID)
			} else {
				if clients, ok := h.reviews[c.reviewID]; ok {
					delete(clients, c)
					if len(clients) == 0 {
						delete(h.reviews, c.reviewID)
					}
				}
				slog.Debug("ws client unregistered", "review_id", c.reviewID)
			}
			close(c.send)
			h.mu.Unlock()

		case <-h.done:
			h.mu.Lock()
			for _, clients := range h.reviews {
				for c := range clients {
					close(c.send)
				}
			}
			for _, clients := range h.indexes {
				for c := range clients {
					close(c.send)
				}
			}
			for _, clients := range h.prEmbeds {
				for c := range clients {
					close(c.send)
				}
			}
			for _, clients := range h.swarm {
				for c := range clients {
					close(c.send)
				}
			}
			for c := range h.metrics {
				close(c.send)
			}
			h.reviews = make(map[uuid.UUID]map[*Client]struct{})
			h.indexes = make(map[string]map[*Client]struct{})
			h.prEmbeds = make(map[string]map[*Client]struct{})
			h.swarm = make(map[string]map[*Client]struct{})
			h.metrics = make(map[*Client]struct{})
			h.mu.Unlock()
			return
		}
	}
}

// Subscribe adds a client for the given review.
func (h *Hub) Subscribe(conn *websocket.Conn, reviewID uuid.UUID) *Client {
	c := &Client{
		conn:     conn,
		reviewID: reviewID,
		send:     make(chan []byte, sendBufSize),
	}
	h.register <- c
	metrics.WSConnectionsActive.Inc()
	return c
}

// Unsubscribe removes a client.
func (h *Hub) Unsubscribe(c *Client) {
	h.unregister <- c
	metrics.WSConnectionsActive.Dec()
}

// Broadcast sends a ProgressEvent to all clients watching the given review.
func (h *Hub) Broadcast(reviewID uuid.UUID, evt ProgressEvent) {
	evt.ReviewID = reviewID
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	data, err := json.Marshal(evt)
	if err != nil {
		slog.Error("ws: failed to marshal event", "error", err)
		return
	}

	h.mu.RLock()
	clients, ok := h.reviews[reviewID]
	h.mu.RUnlock()
	if !ok || len(clients) == 0 {
		return // no subscribers — skip
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range clients {
		select {
		case c.send <- data:
			metrics.WSMessagesTotal.Inc()
		default:
			// Client too slow — drop
			slog.Debug("ws: dropping message, client buffer full", "review_id", reviewID)
		}
	}
}

// HasSubscribers returns true if at least one client is watching the review.
func (h *Hub) HasSubscribers(reviewID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients, ok := h.reviews[reviewID]
	return ok && len(clients) > 0
}

// ── Indexing subscriptions ──────────────────────────────────────────────────

// SubscribeIndex registers a client for indexing progress on a repo.
func (h *Hub) SubscribeIndex(conn *websocket.Conn, repoID string) *Client {
	c := &Client{
		conn:        conn,
		indexRepoID: repoID,
		send:        make(chan []byte, sendBufSize),
	}
	h.register <- c
	metrics.WSConnectionsActive.Inc()
	return c
}

// BroadcastIndex sends an IndexProgressEvent to all clients watching a repo.
func (h *Hub) BroadcastIndex(repoID string, evt IndexProgressEvent) {
	evt.RepoID = repoID
	evt.Type = "index_progress"
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	data, err := json.Marshal(evt)
	if err != nil {
		slog.Error("ws: failed to marshal index event", "error", err)
		return
	}

	h.mu.RLock()
	clients, ok := h.indexes[repoID]
	h.mu.RUnlock()
	if !ok || len(clients) == 0 {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range clients {
		select {
		case c.send <- data:
			metrics.WSMessagesTotal.Inc()
		default:
			slog.Debug("ws: dropping index message, client buffer full", "repo_id", repoID)
		}
	}
}

// HasIndexSubscribers returns true if at least one client is watching indexing for a repo.
func (h *Hub) HasIndexSubscribers(repoID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients, ok := h.indexes[repoID]
	return ok && len(clients) > 0
}

// ── PR embed subscriptions ─────────────────────────────────────────────────

// SubscribePREmbed registers a client for PR embedding progress on a repo.
func (h *Hub) SubscribePREmbed(conn *websocket.Conn, repoID string) *Client {
	c := &Client{
		conn:        conn,
		embedRepoID: repoID,
		send:        make(chan []byte, sendBufSize),
	}
	h.register <- c
	metrics.WSConnectionsActive.Inc()
	return c
}

// BroadcastPREmbed sends a PREmbedProgressEvent to all clients watching PR embedding for a repo.
func (h *Hub) BroadcastPREmbed(repoID string, evt PREmbedProgressEvent) {
	evt.RepoID = repoID
	evt.Type = "pr_embed_progress"
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	data, err := json.Marshal(evt)
	if err != nil {
		slog.Error("ws: failed to marshal PR embed event", "error", err)
		return
	}

	h.mu.RLock()
	clients, ok := h.prEmbeds[repoID]
	h.mu.RUnlock()
	if !ok || len(clients) == 0 {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range clients {
		select {
		case c.send <- data:
			metrics.WSMessagesTotal.Inc()
		default:
			slog.Debug("ws: dropping PR embed message, client buffer full", "repo_id", repoID)
		}
	}
}

// HasPREmbedSubscribers returns true if at least one client is watching PR embedding for a repo.
func (h *Hub) HasPREmbedSubscribers(repoID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients, ok := h.prEmbeds[repoID]
	return ok && len(clients) > 0
}

// ── Swarm subscriptions ────────────────────────────────────────────────────

// SubscribeSwarm registers a client for swarm events on a task.
// If taskID is empty, the client receives ALL swarm events (global subscriber).
// On connect, any buffered history for the task is replayed so the client
// catches up on discussion threads that started before it joined.
func (h *Hub) SubscribeSwarm(conn *websocket.Conn, taskID string) *Client {
	if taskID == "" {
		taskID = "*" // sentinel for global subscribers
	}
	c := &Client{
		conn:        conn,
		swarmTaskID: taskID,
		send:        make(chan []byte, sendBufSize),
	}
	h.register <- c
	metrics.WSConnectionsActive.Inc()

	// Replay buffered history for this task so the UI is never empty.
	if taskID != "*" {
		h.mu.RLock()
		hist := h.swarmHistory[taskID]
		h.mu.RUnlock()
		if len(hist) > 0 {
			slog.Info("ws swarm: replaying history", "task_id", taskID, "events", len(hist))
			for _, entry := range hist {
				select {
				case c.send <- entry.data:
					metrics.WSMessagesTotal.Inc()
				default:
					slog.Debug("ws: dropping replay message, client buffer full", "task_id", taskID)
				}
			}
		}
	}

	return c
}

// BroadcastSwarm sends a SwarmEvent to all clients watching a specific task
// and to all global ("*") subscribers.
func (h *Hub) BroadcastSwarm(evt SwarmEvent) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	data, err := json.Marshal(evt)
	if err != nil {
		slog.Error("ws: failed to marshal swarm event", "error", err)
		return
	}

	h.mu.Lock()

	// Buffer the event for late-joining clients so they get the full
	// discussion state on reconnect. All events we broadcast now are
	// structural (no more per-token streaming chunks).
	if evt.TaskID != "" {
		hist := h.swarmHistory[evt.TaskID]
		if len(hist) >= swarmHistoryMax {
			hist = hist[1:] // drop oldest
		}
		h.swarmHistory[evt.TaskID] = append(hist, swarmHistoryEntry{data: data})
	}

	// Send to task-specific subscribers.
	if evt.TaskID != "" {
		if clients, ok := h.swarm[evt.TaskID]; ok {
			for c := range clients {
				select {
				case c.send <- data:
					metrics.WSMessagesTotal.Inc()
				default:
					slog.Debug("ws: dropping swarm message, client buffer full", "task_id", evt.TaskID)
				}
			}
		}
	}

	// Send to global subscribers.
	if clients, ok := h.swarm["*"]; ok {
		for c := range clients {
			select {
			case c.send <- data:
				metrics.WSMessagesTotal.Inc()
			default:
				slog.Debug("ws: dropping swarm message, global client buffer full")
			}
		}
	}

	h.mu.Unlock()
}

// HasSwarmSubscribers returns true if at least one client is watching swarm events
// for the given task (or globally).
func (h *Hub) HasSwarmSubscribers(taskID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if clients, ok := h.swarm[taskID]; ok && len(clients) > 0 {
		return true
	}
	clients, ok := h.swarm["*"]
	return ok && len(clients) > 0
}

// ClearSwarmHistory removes the buffered event history for a task.
// Call this when a task reaches a terminal state (completed, failed, cancelled)
// to prevent unbounded memory growth.
func (h *Hub) ClearSwarmHistory(taskID string) {
	h.mu.Lock()
	delete(h.swarmHistory, taskID)
	h.mu.Unlock()
}

// ── Engine metrics subscriptions ───────────────────────────────────────────

// SubscribeMetrics registers a client for engine metrics broadcasts.
func (h *Hub) SubscribeMetrics(conn *websocket.Conn) *Client {
	c := &Client{
		conn:    conn,
		metrics: true,
		send:    make(chan []byte, sendBufSize),
	}
	h.register <- c
	metrics.WSConnectionsActive.Inc()
	return c
}

// BroadcastMetrics sends a raw JSON payload to all metrics subscribers.
func (h *Hub) BroadcastMetrics(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.metrics {
		select {
		case c.send <- data:
			metrics.WSMessagesTotal.Inc()
		default:
			slog.Debug("ws: dropping metrics message, client buffer full")
		}
	}
}

// HasMetricsSubscribers returns true if at least one client is watching engine metrics.
func (h *Hub) HasMetricsSubscribers() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.metrics) > 0
}

// WritePump writes queued messages to the WebSocket until the client disconnects.
// Callers must pass a context created with context.WithoutCancel.
//
// After writing one message, the pump drains any additional queued messages
// without blocking. This reduces write-syscall overhead when multiple events
// arrive in a burst (e.g. streaming chunks from several LLM providers).
func (h *Hub) WritePump(ctx context.Context, c *Client) {
	defer func() {
		h.Unsubscribe(c)
		_ = c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	closeCtx := c.conn.CloseRead(ctx)

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeWait)
			err := c.conn.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				slog.Debug("ws: write failed", "error", err, "review_id", c.reviewID)
				return
			}

			// Drain any already-queued messages to reduce per-message
			// write overhead. This is the same pattern gorilla/websocket
			// examples use and is critical under streaming load.
			n := len(c.send)
			for i := 0; i < n; i++ {
				extra, ok := <-c.send
				if !ok {
					return
				}
				writeCtx2, cancel2 := context.WithTimeout(ctx, writeWait)
				err = c.conn.Write(writeCtx2, websocket.MessageText, extra)
				cancel2()
				if err != nil {
					slog.Debug("ws: write failed (drain)", "error", err)
					return
				}
			}

		case <-closeCtx.Done():
			return

		case <-ctx.Done():
			return
		}
	}
}
