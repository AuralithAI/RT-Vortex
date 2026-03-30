package ws

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Handler is an HTTP handler that upgrades to WebSocket for review progress.
type Handler struct {
	hub *Hub
}

// NewHandler creates a WebSocket handler wired to the given Hub.
func NewHandler(hub *Hub) *Handler {
	return &Handler{hub: hub}
}

// ServeHTTP upgrades the connection to a WebSocket and streams progress events
// for the review identified by the {reviewID} URL parameter.
//
// Route: GET /api/v1/reviews/{reviewID}/ws
func (wh *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reviewIDStr := chi.URLParam(r, "reviewID")
	reviewID, err := uuid.Parse(reviewIDStr)
	if err != nil {
		http.Error(w, `{"error":"invalid review ID"}`, http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Allow all origins for development; in production this should be restricted.
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("ws: accept failed", "error", err, "review_id", reviewID)
		return
	}

	slog.Info("ws: client connected", "review_id", reviewID, "remote", r.RemoteAddr)

	client := wh.hub.Subscribe(conn, reviewID)

	wsCtx := context.WithoutCancel(r.Context())

	// WritePump blocks until the client disconnects.
	wh.hub.WritePump(wsCtx, client)

	slog.Info("ws: client disconnected", "review_id", reviewID, "remote", r.RemoteAddr)
}
