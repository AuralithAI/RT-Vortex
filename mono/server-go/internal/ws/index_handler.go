package ws

import (
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

// IndexHandler is an HTTP handler that upgrades to WebSocket for indexing progress.
type IndexHandler struct {
	hub *Hub
}

// NewIndexHandler creates a WebSocket handler for indexing progress.
func NewIndexHandler(hub *Hub) *IndexHandler {
	return &IndexHandler{hub: hub}
}

// ServeHTTP upgrades the connection to a WebSocket and streams indexing progress
// for the repository identified by the {repoID} URL parameter.
//
// Route: GET /api/v1/repos/{repoID}/index/ws
func (ih *IndexHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "repoID")
	if repoID == "" {
		http.Error(w, `{"error":"missing repo ID"}`, http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // restrict in production
	})
	if err != nil {
		slog.Error("ws: index accept failed", "error", err, "repo_id", repoID)
		return
	}

	slog.Info("ws: index client connected", "repo_id", repoID, "remote", r.RemoteAddr)

	client := ih.hub.SubscribeIndex(conn, repoID)

	// WritePump blocks until the client disconnects.
	ih.hub.WritePump(r.Context(), client)

	slog.Info("ws: index client disconnected", "repo_id", repoID, "remote", r.RemoteAddr)
}
