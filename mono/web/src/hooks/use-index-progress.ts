// ─── useIndexProgress ────────────────────────────────────────────────────────
// WebSocket hook for real-time indexing progress. Opens a WS connection to
// /api/v1/repos/{repoID}/index/ws and emits IndexProgressEvent updates.
// Falls back to polling via useIndexStatus if WS is unavailable.
// ─────────────────────────────────────────────────────────────────────────────

import { useEffect, useRef, useState, useCallback } from "react";
import type { IndexProgressEvent } from "@/types/api";

interface UseIndexProgressOptions {
  /** Only connect when true (default: true) */
  enabled?: boolean;
  /** Reconnect delay in ms after unexpected disconnect (default: 3000) */
  reconnectDelay?: number;
  /** Max reconnection attempts (default: 10) */
  maxReconnects?: number;
}

interface IndexProgressState {
  /** Whether the WebSocket is connected */
  connected: boolean;
  /** Latest progress event from the engine */
  event: IndexProgressEvent | null;
  /** Any connection error */
  error: string | null;
}

/**
 * Hook that opens a WebSocket to stream indexing progress for a repository.
 *
 * @param repoId - The repository ID to watch
 * @param options - Connection options
 * @returns The current progress state
 */
export function useIndexProgress(
  repoId: string | undefined,
  options: UseIndexProgressOptions = {},
): IndexProgressState {
  const { enabled = true, reconnectDelay = 3000, maxReconnects = 10 } = options;

  const [state, setState] = useState<IndexProgressState>({
    connected: false,
    event: null,
    error: null,
  });

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectCount = useRef(0);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const connect = useCallback(() => {
    if (!repoId || !enabled) return;

    // Build WebSocket URL from the current origin
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/api/v1/repos/${repoId}/index/ws`;

    try {
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        reconnectCount.current = 0;
        setState((prev) => ({ ...prev, connected: true, error: null }));
      };

      ws.onmessage = (e) => {
        try {
          const evt: IndexProgressEvent = JSON.parse(e.data);
          setState((prev) => ({ ...prev, event: evt }));

          // Auto-close when done
          if (evt.state === "completed" || evt.state === "failed") {
            // Keep the connection open briefly so UI can show final state
            setTimeout(() => {
              ws.close(1000, "indexing finished");
            }, 2000);
          }
        } catch {
          // ignore malformed messages
        }
      };

      ws.onclose = (e) => {
        wsRef.current = null;
        setState((prev) => ({ ...prev, connected: false }));

        // Don't reconnect on normal closure or if indexing finished
        if (e.code === 1000) return;

        // Attempt reconnection with backoff
        if (reconnectCount.current < maxReconnects) {
          reconnectCount.current++;
          const delay = reconnectDelay * Math.min(reconnectCount.current, 5);
          reconnectTimer.current = setTimeout(connect, delay);
        } else {
          setState((prev) => ({
            ...prev,
            error: "Lost connection to indexing progress stream",
          }));
        }
      };

      ws.onerror = () => {
        setState((prev) => ({
          ...prev,
          error: "WebSocket connection error",
        }));
      };
    } catch {
      setState((prev) => ({
        ...prev,
        error: "Failed to create WebSocket connection",
      }));
    }
  }, [repoId, enabled, reconnectDelay, maxReconnects]);

  useEffect(() => {
    if (!repoId || !enabled) return;

    connect();

    return () => {
      if (reconnectTimer.current) {
        clearTimeout(reconnectTimer.current);
      }
      if (wsRef.current) {
        wsRef.current.close(1000, "component unmounted");
        wsRef.current = null;
      }
    };
  }, [repoId, enabled, connect]);

  return state;
}

/**
 * Format ETA seconds into a human-readable string.
 */
export function formatETA(seconds: number): string {
  if (seconds < 0) return "Calculating...";
  if (seconds === 0) return "Almost done";
  if (seconds < 60) return `~${Math.ceil(seconds)}s remaining`;
  const mins = Math.floor(seconds / 60);
  const secs = Math.ceil(seconds % 60);
  if (mins < 60) return `~${mins}m ${secs}s remaining`;
  const hours = Math.floor(mins / 60);
  const remainMins = mins % 60;
  return `~${hours}h ${remainMins}m remaining`;
}
