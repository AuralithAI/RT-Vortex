// ─── useChatStream — SSE Streaming Hook for Chat ─────────────────────────────
// Handles streaming SSE events from the chat endpoint, accumulating deltas,
// citations, thinking phases, and done signals. Designed for real-time display.
// ─────────────────────────────────────────────────────────────────────────────

import { useCallback, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { queryKeys } from "@/lib/api/queries";
import api from "@/lib/api/client";
import type { ChatAttachment, ChatCitation, ChatStreamEvent } from "@/types/api";

export interface ChatStreamState {
  /** Whether we're currently streaming. */
  isStreaming: boolean;
  /** Accumulated response text so far. */
  streamedContent: string;
  /** Collected citations from the engine search. */
  citations: ChatCitation[];
  /** Current thinking phase ("searching", "retrieving", "synthesizing"). */
  thinkingPhase: string | null;
  /** Current thinking status message. */
  thinkingMessage: string | null;
  /** Final message ID after done. */
  messageId: string | null;
  /** Token usage from the completed response. */
  usage: { prompt: number; completion: number } | null;
  /** Engine search time in ms. */
  searchTimeMs: number | null;
  /** Number of code chunks retrieved. */
  chunksRetrieved: number | null;
  /** Error message if something went wrong. */
  error: string | null;
}

const INITIAL_STATE: ChatStreamState = {
  isStreaming: false,
  streamedContent: "",
  citations: [],
  thinkingPhase: null,
  thinkingMessage: null,
  messageId: null,
  usage: null,
  searchTimeMs: null,
  chunksRetrieved: null,
  error: null,
};

export function useChatStream(repoId: string, sessionId: string) {
  const [state, setState] = useState<ChatStreamState>(INITIAL_STATE);
  const abortRef = useRef<(() => void) | null>(null);
  const qc = useQueryClient();

  const sendMessage = useCallback(
    async (content: string, attachments?: ChatAttachment[]) => {
      // Reset state for new message.
      setState({
        ...INITIAL_STATE,
        isStreaming: true,
      });

      const { read, abort } = api.chat.sendMessageStream(
        repoId,
        sessionId,
        content,
        attachments,
      );
      abortRef.current = abort;

      try {
        const stream = await read();
        const reader = stream.getReader();
        let buffer = "";

        // eslint-disable-next-line no-constant-condition
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;

          buffer += value;

          // SSE format: "data: {...}\n\n"
          const lines = buffer.split("\n\n");
          // Keep last partial chunk in buffer.
          buffer = lines.pop() ?? "";

          for (const block of lines) {
            for (const line of block.split("\n")) {
              if (!line.startsWith("data: ")) continue;
              const payload = line.slice(6).trim();

              // End of stream signal.
              if (payload === "[DONE]") {
                setState((prev) => ({ ...prev, isStreaming: false }));
                // Invalidate messages cache to refetch with persisted data.
                qc.invalidateQueries({ queryKey: queryKeys.chatMessages(repoId, sessionId) });
                qc.invalidateQueries({ queryKey: queryKeys.chatSessions(repoId) });
                return;
              }

              try {
                const event: ChatStreamEvent = JSON.parse(payload);
                setState((prev) => applyEvent(prev, event));
              } catch {
                // Skip malformed JSON.
              }
            }
          }
        }

        // Stream ended without [DONE] — finalize.
        setState((prev) => ({ ...prev, isStreaming: false }));
        qc.invalidateQueries({ queryKey: queryKeys.chatMessages(repoId, sessionId) });
        qc.invalidateQueries({ queryKey: queryKeys.chatSessions(repoId) });
      } catch (err) {
        if ((err as Error).name === "AbortError") {
          setState((prev) => ({ ...prev, isStreaming: false }));
          return;
        }
        setState((prev) => ({
          ...prev,
          isStreaming: false,
          error: err instanceof Error ? err.message : "Unknown error",
        }));
      }
    },
    [repoId, sessionId, qc],
  );

  const cancelStream = useCallback(() => {
    abortRef.current?.();
    setState((prev) => ({ ...prev, isStreaming: false }));
  }, []);

  const reset = useCallback(() => {
    setState(INITIAL_STATE);
  }, []);

  return {
    ...state,
    sendMessage,
    cancelStream,
    reset,
  };
}

// ── Event reducer ───────────────────────────────────────────────────────────

function applyEvent(prev: ChatStreamState, event: ChatStreamEvent): ChatStreamState {
  switch (event.type) {
    case "delta":
      return {
        ...prev,
        streamedContent: prev.streamedContent + (event.content ?? ""),
        thinkingPhase: null,
        thinkingMessage: null,
      };

    case "citation":
      return {
        ...prev,
        citations: event.citation
          ? [...prev.citations, event.citation]
          : prev.citations,
      };

    case "thinking":
      return {
        ...prev,
        thinkingPhase: event.phase ?? prev.thinkingPhase,
        thinkingMessage: event.message ?? prev.thinkingMessage,
      };

    case "done":
      return {
        ...prev,
        isStreaming: false,
        messageId: event.message_id ?? prev.messageId,
        usage:
          event.prompt_tokens != null
            ? { prompt: event.prompt_tokens, completion: event.completion_tokens ?? 0 }
            : prev.usage,
        searchTimeMs: event.search_time_ms ?? prev.searchTimeMs,
        chunksRetrieved: event.chunks_retrieved ?? prev.chunksRetrieved,
      };

    case "error":
      return {
        ...prev,
        isStreaming: false,
        error: event.error ?? "Unknown error",
      };

    default:
      return prev;
  }
}
