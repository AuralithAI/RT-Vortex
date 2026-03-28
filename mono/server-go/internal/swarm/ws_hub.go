package swarm

import (
	"log/slog"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/ws"
)

// WSHub broadcasts swarm events to connected WebSocket clients.
// It delegates to the main ws.Hub which manages all WebSocket connections.
type WSHub struct {
	hub *ws.Hub
}

// NewWSHub creates a new swarm WebSocket hub.
// If hub is nil, events are logged but not broadcast (test/standalone mode).
func NewWSHub(hub *ws.Hub) *WSHub {
	return &WSHub{hub: hub}
}

// BroadcastTaskEvent sends a task-related event to all subscribers.
func (h *WSHub) BroadcastTaskEvent(eventType string, taskID string, payload map[string]interface{}) {
	slog.Debug("swarm ws event", "type", eventType, "task_id", taskID, "payload", payload)
	if h.hub == nil {
		return
	}
	h.hub.BroadcastSwarm(ws.SwarmEvent{
		Type:      "swarm_task",
		TaskID:    taskID,
		Event:     eventType,
		Data:      payload,
		Timestamp: time.Now(),
	})
}

// BroadcastAgentEvent sends an agent-related event.
func (h *WSHub) BroadcastAgentEvent(eventType string, taskID string, agentID string, payload map[string]interface{}) {
	slog.Debug("swarm ws agent event", "type", eventType, "task_id", taskID, "agent_id", agentID, "payload", payload)
	if h.hub == nil {
		return
	}
	h.hub.BroadcastSwarm(ws.SwarmEvent{
		Type:      "swarm_agent",
		TaskID:    taskID,
		AgentID:   agentID,
		Event:     eventType,
		Data:      payload,
		Timestamp: time.Now(),
	})
}

// BroadcastDiffEvent sends a diff-related event.
func (h *WSHub) BroadcastDiffEvent(taskID string, diffID string, payload map[string]interface{}) {
	slog.Debug("swarm ws diff event", "task_id", taskID, "diff_id", diffID, "payload", payload)
	if h.hub == nil {
		return
	}
	data := payload
	if data == nil {
		data = make(map[string]interface{})
	}
	data["diff_id"] = diffID
	h.hub.BroadcastSwarm(ws.SwarmEvent{
		Type:      "swarm_diff",
		TaskID:    taskID,
		Event:     "diff_submitted",
		Data:      data,
		Timestamp: time.Now(),
	})
}

// BroadcastPlanEvent sends a plan-related event.
func (h *WSHub) BroadcastPlanEvent(taskID string, eventType string, payload map[string]interface{}) {
	slog.Debug("swarm ws plan event", "type", eventType, "task_id", taskID, "payload", payload)
	if h.hub == nil {
		return
	}
	h.hub.BroadcastSwarm(ws.SwarmEvent{
		Type:      "swarm_plan",
		TaskID:    taskID,
		Event:     eventType,
		Data:      payload,
		Timestamp: time.Now(),
	})
}
