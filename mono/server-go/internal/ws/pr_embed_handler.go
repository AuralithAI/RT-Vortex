package ws

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

// PREmbedHandler is an HTTP handler that upgrades to WebSocket for PR embedding progress.
type PREmbedHandler struct {
	hub *Hub
}

// NewPREmbedHandler creates a WebSocket handler for PR embedding progress.
func NewPREmbedHandler(hub *Hub) *PREmbedHandler {
	return &PREmbedHandler{hub: hub}
}

// ServeHTTP upgrades the connection to a WebSocket and streams PR embedding
// progress for the repository identified by the {repoID} URL parameter.
//
// Route: GET /api/v1/repos/{repoID}/pull-requests/embed/ws
func (ph *PREmbedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "repoID")
	if repoID == "" {
		http.Error(w, `{"error":"missing repo ID"}`, http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // restrict in production
	})
	if err != nil {
		slog.Error("ws: pr-embed accept failed", "error", err, "repo_id", repoID)
		return
	}

	slog.Info("ws: pr-embed client connected", "repo_id", repoID, "remote", r.RemoteAddr)

	client := ph.hub.SubscribePREmbed(conn, repoID)

	wsCtx := context.WithoutCancel(r.Context())

	// WritePump blocks until the client disconnects.
	ph.hub.WritePump(wsCtx, client)

	slog.Info("ws: pr-embed client disconnected", "repo_id", repoID, "remote", r.RemoteAddr)
}
