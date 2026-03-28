// Package ws provides real-time WebSocket streaming.
//
// metrics_handler.go serves the /engine/metrics/ws endpoint.

package ws

import (
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
)

// MetricsHandler upgrades an HTTP connection to a WebSocket for engine metrics streaming.
type MetricsHandler struct {
	hub *Hub
}

// NewMetricsHandler creates a new MetricsHandler.
func NewMetricsHandler(hub *Hub) *MetricsHandler {
	return &MetricsHandler{hub: hub}
}

// ServeHTTP implements http.Handler.
func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // CORS handled by middleware
	})
	if err != nil {
		slog.Error("ws: failed to accept engine metrics connection", "error", err)
		return
	}

	client := h.hub.SubscribeMetrics(conn)
	slog.Info("ws: engine metrics subscriber connected")

	// WritePump blocks until the client disconnects.
	h.hub.WritePump(r.Context(), client)
}
