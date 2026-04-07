// ─── useSwarmEvents ──────────────────────────────────────────────────────────
// WebSocket hook for real-time swarm events (task status changes, agent
// activity, diff submissions, plan updates). Connects to the swarm WS
// endpoint and dispatches typed events.
// ─────────────────────────────────────────────────────────────────────────────

import { useEffect, useRef, useState, useCallback } from "react";

// ── Event types ─────────────────────────────────────────────────────────────

export interface SwarmWsEvent {
  type: "swarm_task" | "swarm_agent" | "swarm_diff" | "swarm_plan" | "swarm_discussion";
  task_id?: string;
  agent_id?: string;
  event: string;
  data?: Record<string, unknown>;
  timestamp: string;
}

interface UseSwarmEventsOptions {
  /** Only connect when true (default: true) */
  enabled?: boolean;
  /** Reconnect delay in ms (default: 3000) */
  reconnectDelay?: number;
  /** Max reconnection attempts (default: 15) */
  maxReconnects?: number;
}

interface SwarmEventsState {
  /** Whether the WebSocket is connected */
  connected: boolean;
  /** Latest event received */
  lastEvent: SwarmWsEvent | null;
  /** All events received (most recent first, capped at 100) */
  events: SwarmWsEvent[];
  /** Connection error */
  error: string | null;
}

const MAX_EVENTS = 100;

/**
 * Hook that opens a WebSocket to stream swarm events for a task.
 *
 * @param taskId - Task ID to watch (empty/undefined for global events)
 * @param options - Connection options
 * @returns The current events state
 */
export function useSwarmEvents(
  taskId?: string,
  options: UseSwarmEventsOptions = {},
): SwarmEventsState {
  const { enabled = true, reconnectDelay = 3000, maxReconnects = 15 } = options;

  const [state, setState] = useState<SwarmEventsState>({
    connected: false,
    lastEvent: null,
    events: [],
    error: null,
  });

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectCount = useRef(0);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const connect = useCallback(() => {
    if (!enabled) return;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const path = taskId
      ? `/api/v1/swarm/tasks/${taskId}/ws`
      : `/api/v1/swarm/ws`;
    const url = `${protocol}//${window.location.host}${path}`;

    try {
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        reconnectCount.current = 0;
        // Clear existing events on (re)connect — the server replays
        // buffered history so we'll get everything again without duplicates.
        setState((prev) => ({ ...prev, connected: true, error: null, events: [], lastEvent: null }));
      };

      ws.onmessage = (e) => {
        try {
          const evt: SwarmWsEvent = JSON.parse(e.data);
          setState((prev) => ({
            ...prev,
            lastEvent: evt,
            events: [evt, ...prev.events].slice(0, MAX_EVENTS),
          }));
        } catch {
          // ignore malformed messages
        }
      };

      ws.onerror = () => {
        setState((prev) => ({
          ...prev,
          error: "WebSocket connection error",
        }));
      };

      ws.onclose = (e) => {
        setState((prev) => ({ ...prev, connected: false }));
        wsRef.current = null;

        // Reconnect unless intentionally closed.
        if (e.code !== 1000 && reconnectCount.current < maxReconnects) {
          reconnectCount.current += 1;
          reconnectTimer.current = setTimeout(connect, reconnectDelay);
        }
      };
    } catch {
      setState((prev) => ({
        ...prev,
        error: "Failed to create WebSocket",
      }));
    }
  }, [taskId, enabled, reconnectDelay, maxReconnects]);

  useEffect(() => {
    connect();

    return () => {
      if (reconnectTimer.current) clearTimeout(reconnectTimer.current);
      if (wsRef.current) {
        wsRef.current.close(1000, "component unmounted");
        wsRef.current = null;
      }
    };
  }, [connect]);

  return state;
}
