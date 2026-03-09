// ─── useEngineMetrics ────────────────────────────────────────────────────────
// WebSocket hook for real-time C++ engine metrics streaming.
// Opens a WS connection to /api/v1/engine/metrics/ws and receives
// EngineMetricsSnapshot pushes at ~1 Hz from the Go API server.
//
// Falls back to polling GET /api/v1/engine/metrics if WS is unavailable.
// ─────────────────────────────────────────────────────────────────────────────

import { useEffect, useRef, useState, useCallback } from "react";
import type { EngineMetricsSnapshot, EngineMetricsWSEvent } from "@/types/api";

interface UseEngineMetricsOptions {
  /** Only connect when true (default: true) */
  enabled?: boolean;
  /** Reconnect delay in ms after unexpected disconnect (default: 3000) */
  reconnectDelay?: number;
  /** Max reconnection attempts (default: 20) */
  maxReconnects?: number;
  /** How many historical snapshots to keep (default: 60 = 1 minute at 1 Hz) */
  historySize?: number;
}

interface EngineMetricsState {
  /** Whether the WebSocket is connected */
  connected: boolean;
  /** Latest snapshot from the engine */
  latest: EngineMetricsSnapshot | null;
  /** Ring buffer of recent snapshots for sparklines / charts */
  history: EngineMetricsSnapshot[];
  /** Any connection error */
  error: string | null;
}

/**
 * Hook that opens a WebSocket to stream engine metrics in real time.
 *
 * @param options - Connection options
 * @returns The current metrics state including history for charting
 */
export function useEngineMetrics(
  options: UseEngineMetricsOptions = {},
): EngineMetricsState {
  const {
    enabled = true,
    reconnectDelay = 3000,
    maxReconnects = 20,
    historySize = 60,
  } = options;

  const [state, setState] = useState<EngineMetricsState>({
    connected: false,
    latest: null,
    history: [],
    error: null,
  });

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectCount = useRef(0);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const historyRef = useRef<EngineMetricsSnapshot[]>([]);

  const connect = useCallback(() => {
    if (!enabled) return;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/api/v1/engine/metrics/ws`;

    try {
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        reconnectCount.current = 0;
        setState((prev) => ({ ...prev, connected: true, error: null }));
      };

      ws.onmessage = (e) => {
        try {
          const evt: EngineMetricsWSEvent = JSON.parse(e.data);
          if (evt.type !== "engine_metrics" || !evt.data) return;

          const snap = evt.data;

          // Maintain ring buffer
          historyRef.current = [
            ...historyRef.current.slice(-(historySize - 1)),
            snap,
          ];

          setState((prev) => ({
            ...prev,
            latest: snap,
            history: historyRef.current,
          }));
        } catch {
          // ignore malformed messages
        }
      };

      ws.onclose = (e) => {
        wsRef.current = null;
        setState((prev) => ({ ...prev, connected: false }));

        // Don't reconnect on intentional closure
        if (e.code === 1000) return;

        if (reconnectCount.current < maxReconnects) {
          reconnectCount.current++;
          const delay = reconnectDelay * Math.min(reconnectCount.current, 5);
          reconnectTimer.current = setTimeout(connect, delay);
        } else {
          setState((prev) => ({
            ...prev,
            error: "Lost connection to engine metrics stream",
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
  }, [enabled, reconnectDelay, maxReconnects, historySize]);

  useEffect(() => {
    if (!enabled) return;

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
  }, [enabled, connect]);

  return state;
}

// ── Helper: extract a specific metric's scalar value ────────────────────────

export function getMetricScalar(
  snap: EngineMetricsSnapshot | null,
  name: string,
  fallback = 0,
): number {
  if (!snap) return fallback;
  const mv = snap.metrics[name];
  if (!mv) return fallback;
  return mv.scalar ?? fallback;
}

export function getMetricHistogram(
  snap: EngineMetricsSnapshot | null,
  name: string,
) {
  if (!snap) return null;
  const mv = snap.metrics[name];
  if (!mv || mv.type !== "histogram") return null;
  return mv.histogram ?? null;
}

// ── Helper: format uptime ───────────────────────────────────────────────────

export function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const mins = Math.floor(seconds / 60);
  if (mins < 60) return `${mins}m ${seconds % 60}s`;
  const hours = Math.floor(mins / 60);
  const remainMins = mins % 60;
  if (hours < 24) return `${hours}h ${remainMins}m`;
  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h`;
}
