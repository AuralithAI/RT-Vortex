// ─── usePREmbedProgress ──────────────────────────────────────────────────────
// WebSocket hook for real-time PR embedding progress. Opens a WS connection to
// /api/v1/repos/{repoID}/pull-requests/embed/ws and emits PREmbedProgressEvent
// updates per PR.
// ─────────────────────────────────────────────────────────────────────────────

import { useEffect, useRef, useState, useCallback } from "react";
import type { PREmbedProgressEvent } from "@/types/api";

interface UsePREmbedProgressOptions {
  /** Only connect when true (default: true) */
  enabled?: boolean;
  /** Reconnect delay in ms after unexpected disconnect (default: 3000) */
  reconnectDelay?: number;
  /** Max reconnection attempts (default: 10) */
  maxReconnects?: number;
}

interface PREmbedProgressState {
  /** Whether the WebSocket is connected */
  connected: boolean;
  /** Map of PR number → latest progress event */
  events: Map<number, PREmbedProgressEvent>;
  /** Any connection error */
  error: string | null;
}

/**
 * Hook that opens a WebSocket to stream PR embedding progress for a repository.
 * Unlike index progress (single job), this tracks multiple PRs simultaneously.
 *
 * @param repoId - The repository ID to watch
 * @param options - Connection options
 * @returns The current progress state keyed by PR number
 */
export function usePREmbedProgress(
  repoId: string | undefined,
  options: UsePREmbedProgressOptions = {},
): PREmbedProgressState {
  const { enabled = true, reconnectDelay = 3000, maxReconnects = 10 } = options;

  const [state, setState] = useState<PREmbedProgressState>({
    connected: false,
    events: new Map(),
    error: null,
  });

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectCount = useRef(0);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const connect = useCallback(() => {
    if (!repoId || !enabled) return;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/api/v1/repos/${repoId}/pull-requests/embed/ws`;

    try {
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        reconnectCount.current = 0;
        setState((prev) => ({ ...prev, connected: true, error: null }));
      };

      ws.onmessage = (e) => {
        try {
          const evt: PREmbedProgressEvent = JSON.parse(e.data);
          setState((prev) => {
            const next = new Map(prev.events);
            next.set(evt.pr_number, evt);

            // Remove completed/failed entries after a delay so UI can show final state
            if (evt.state === "completed" || evt.state === "failed") {
              setTimeout(() => {
                setState((p) => {
                  const cleaned = new Map(p.events);
                  const current = cleaned.get(evt.pr_number);
                  // Only remove if it's still the same terminal event
                  if (current && (current.state === "completed" || current.state === "failed")) {
                    cleaned.delete(evt.pr_number);
                  }
                  return { ...p, events: cleaned };
                });
              }, 5000);
            }

            return { ...prev, events: next };
          });
        } catch {
          // ignore malformed messages
        }
      };

      ws.onclose = (e) => {
        wsRef.current = null;
        setState((prev) => ({ ...prev, connected: false }));

        if (e.code === 1000) return;

        if (reconnectCount.current < maxReconnects) {
          reconnectCount.current++;
          const delay = reconnectDelay * Math.min(reconnectCount.current, 5);
          reconnectTimer.current = setTimeout(connect, delay);
        } else {
          setState((prev) => ({
            ...prev,
            error: "Lost connection to PR embedding progress stream",
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
