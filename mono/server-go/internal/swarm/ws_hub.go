package swarm

import (
	"log/slog"
	"sync"
)

// WSHub broadcasts swarm events to connected WebSocket clients.
// This is a thin wrapper — in Phase 0 we just log events. Full WebSocket
// integration will be wired in Phase 1 via the existing ws.Hub.
type WSHub struct {
	mu          sync.RWMutex
	subscribers map[string]chan []byte
}

// NewWSHub creates a new swarm WebSocket hub.
func NewWSHub() *WSHub {
	return &WSHub{
		subscribers: make(map[string]chan []byte),
	}
}

// BroadcastTaskEvent sends a task-related event to all subscribers.
func (h *WSHub) BroadcastTaskEvent(eventType string, payload interface{}) {
	slog.Debug("swarm ws event", "type", eventType, "payload", payload)
	// Phase 0: log only. Phase 1 wires into ws.Hub for real-time React updates.
}

// BroadcastAgentEvent sends an agent-related event.
func (h *WSHub) BroadcastAgentEvent(eventType string, agentID string, payload interface{}) {
	slog.Debug("swarm ws agent event", "type", eventType, "agent_id", agentID, "payload", payload)
}
