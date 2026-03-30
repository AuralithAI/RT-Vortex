package ws

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

// SwarmHandler is an HTTP handler that upgrades to WebSocket for swarm events.
type SwarmHandler struct {
	hub *Hub
}

// NewSwarmHandler creates a WebSocket handler wired to the given Hub.
func NewSwarmHandler(hub *Hub) *SwarmHandler {
	return &SwarmHandler{hub: hub}
}

// ServeHTTP upgrades the connection to a WebSocket and streams swarm events.
//
// If {id} is present in the URL, only events for that task are streamed.
// Otherwise all swarm events are streamed (global subscription).
//
// Route: GET /api/v1/swarm/tasks/{id}/ws  (per-task)
// Route: GET /api/v1/swarm/ws             (global)
func (sh *SwarmHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id") // may be empty for global

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("ws swarm: accept failed", "error", err, "task_id", taskID)
		return
	}

	slog.Info("ws swarm: client connected", "task_id", taskID, "remote", r.RemoteAddr)

	client := sh.hub.SubscribeSwarm(conn, taskID)

	wsCtx := context.WithoutCancel(r.Context())

	// WritePump blocks until the client disconnects.
	sh.hub.WritePump(wsCtx, client)

	slog.Info("ws swarm: client disconnected", "task_id", taskID, "remote", r.RemoteAddr)
}
