// ─── WebSocket Hook — Real-time Review Progress ─────────────────────────────
// Connects to /reviews/{id}/ws to receive ProgressEvent messages
// while a review is in progress.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { getWsBaseUrl } from "@/lib/utils";
import type { ReviewProgressEvent } from "@/types/api";

interface UseReviewStreamOptions {
  /** Review ID to stream progress for. */
  reviewId: string;
  /** Only connect when true (e.g. review status === "in_progress"). */
  enabled?: boolean;
  /** Called for each progress event. */
  onEvent?: (event: ReviewProgressEvent) => void;
  /** Called when the review completes (status === "completed" or "failed"). */
  onComplete?: () => void;
  /** Called on connection error. */
  onError?: (err: Event) => void;
}

interface StreamState {
  connected: boolean;
  events: ReviewProgressEvent[];
  error: string | null;
}

export function useReviewStream({
  reviewId,
  enabled = true,
  onEvent,
  onComplete,
  onError,
}: UseReviewStreamOptions): StreamState {
  const [state, setState] = useState<StreamState>({
    connected: false,
    events: [],
    error: null,
  });

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const retriesRef = useRef(0);
  const MAX_RETRIES = 5;

  const connect = useCallback(() => {
    if (!reviewId || !enabled) return;

    const base = getWsBaseUrl();
    const url = `${base}/api/v1/reviews/${reviewId}/ws`;

    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => {
      retriesRef.current = 0;
      setState((prev) => ({ ...prev, connected: true, error: null }));
    };

    ws.onmessage = (msg) => {
      try {
        const event: ReviewProgressEvent = JSON.parse(msg.data);
        setState((prev) => ({
          ...prev,
          events: [...prev.events, event],
        }));
        onEvent?.(event);

        if (event.status === "completed" || event.status === "failed") {
          onComplete?.();
          ws.close(1000, "Review completed");
        }
      } catch {
        // Ignore non-JSON messages (heartbeat pings, etc.)
      }
    };

    ws.onerror = (err) => {
      setState((prev) => ({
        ...prev,
        error: "WebSocket connection error",
      }));
      onError?.(err);
    };

    ws.onclose = (ev) => {
      setState((prev) => ({ ...prev, connected: false }));

      // Don't reconnect on intentional close or after complete
      if (ev.code === 1000 || !enabled) return;

      // Exponential backoff reconnect
      if (retriesRef.current < MAX_RETRIES) {
        const delay = Math.min(1000 * 2 ** retriesRef.current, 30_000);
        retriesRef.current += 1;
        reconnectTimer.current = setTimeout(connect, delay);
      } else {
        setState((prev) => ({
          ...prev,
          error: "Max reconnection attempts reached",
        }));
      }
    };
  }, [reviewId, enabled, onEvent, onComplete, onError]);

  useEffect(() => {
    connect();
    return () => {
      clearTimeout(reconnectTimer.current);
      wsRef.current?.close(1000, "Cleanup");
      wsRef.current = null;
    };
  }, [connect]);

  return state;
}
