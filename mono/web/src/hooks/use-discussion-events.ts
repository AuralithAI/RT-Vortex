// ─── useDiscussionEvents ─────────────────────────────────────────────────────
// Extracts multi-LLM discussion threads and consensus results from the raw
// SwarmWsEvent stream. Maintains local state so the UI components can render
// discussion panels and consensus cards in real time.
// ─────────────────────────────────────────────────────────────────────────────

import { useMemo } from "react";
import type { SwarmWsEvent } from "@/hooks/use-swarm-events";
import type {
  DiscussionThreadData,
  ConsensusResultData,
  ProviderResponseData,
} from "@/types/swarm";

interface DiscussionState {
  /** Accumulated discussion threads (most recent first). */
  threads: DiscussionThreadData[];
  /** Consensus results (most recent first). */
  consensusResults: ConsensusResultData[];
}

/**
 * Process the raw swarm WS events and extract discussion + consensus data.
 *
 * This is a pure function — no side effects, easy to test. Call it inside
 * a component with the events array from `useSwarmEvents`.
 */
export function useDiscussionEvents(events: SwarmWsEvent[]): DiscussionState {
  return useMemo(() => {
    const threadMap = new Map<string, DiscussionThreadData>();
    const consensusResults: ConsensusResultData[] = [];

    // Process events oldest-first to build up state correctly.
    const ordered = [...events].reverse();

    for (const evt of ordered) {
      if (evt.type !== "swarm_discussion") continue;

      const data = evt.data ?? {};

      switch (evt.event) {
        case "thread_opened": {
          // thread_id may be at the top level or nested inside the thread dict.
          const threadObj = data.thread as Record<string, unknown> | undefined;
          const threadId =
            (data.thread_id as string) ||
            (threadObj?.thread_id as string) ||
            "";
          if (!threadId) break;

          const thread = (data.thread as DiscussionThreadData) ?? {
            thread_id: threadId,
            agent_id: (data.agent_id as string) ?? "",
            agent_role: (data.agent_role as string) ?? "",
            topic: (data.topic as string) ?? "",
            action_type: (data.action_type as string) ?? "",
            responses: [],
            status: "open" as const,
            provider_count: 0,
            success_count: 0,
            created_at: Date.now() / 1000,
          };

          // Ensure the thread object has the thread_id set.
          if (!thread.thread_id) thread.thread_id = threadId;

          threadMap.set(threadId, thread);
          break;
        }

        case "provider_response": {
          const threadId = data.thread_id as string;
          if (!threadId) break;

          // Auto-create thread if it doesn't exist yet (e.g. thread_opened
          // event was missed during reconnect or dropped from replay buffer).
          let existing = threadMap.get(threadId);
          if (!existing) {
            existing = {
              thread_id: threadId,
              agent_id: "",
              agent_role: "",
              topic: "",
              action_type: "",
              responses: [],
              status: "open" as const,
              provider_count: 0,
              success_count: 0,
              created_at: Date.now() / 1000,
            };
            threadMap.set(threadId, existing);
          }

          const resp = data.response as ProviderResponseData | undefined;
          if (resp) {
            // If we were accumulating streaming chunks for this provider,
            // replace the in-progress entry with the final complete response.
            const streamIdx = existing.responses.findIndex(
              (r) =>
                r.provider === resp.provider &&
                r.model === resp.model &&
                (r as ProviderResponseData & { _streaming?: boolean })._streaming,
            );
            if (streamIdx >= 0) {
              existing.responses[streamIdx] = resp;
            } else {
              // Deduplicate: skip if we already have a response from the same
              // provider+model (replay buffer can re-send events the client
              // already received live).
              const isDup = existing.responses.some(
                (r) =>
                  r.provider === resp.provider &&
                  r.model === resp.model &&
                  r.content === resp.content,
              );
              if (!isDup) {
                existing.responses.push(resp);
              }
            }
            existing.provider_count = existing.responses.length;
            existing.success_count = existing.responses.filter(
              (r) => !r.error
            ).length;
          }
          break;
        }

        case "provider_streaming_chunk": {
          // Legacy: server no longer sends this event. Streaming
          // accumulation now happens server-side. Ignore gracefully
          // in case of replay from older history.
          break;
        }

        case "provider_streaming_start": {
          // A provider has started generating. Show a spinner/placeholder
          // in the UI until the full provider_response arrives.
          const threadId = data.thread_id as string;
          if (!threadId) break;

          let existing = threadMap.get(threadId);
          if (!existing) {
            existing = {
              thread_id: threadId,
              agent_id: "",
              agent_role: "",
              topic: "",
              action_type: "",
              responses: [],
              status: "open" as const,
              provider_count: 0,
              success_count: 0,
              created_at: Date.now() / 1000,
            };
            threadMap.set(threadId, existing);
          }

          const provider = data.provider as string;
          const model = data.model as string;
          if (!provider) break;

          // Create an in-progress placeholder if we don't already have
          // a response from this provider.
          const alreadyHas = existing.responses.some(
            (r) => r.provider === provider && r.model === model,
          );
          if (!alreadyHas) {
            existing.responses.push({
              provider,
              model: model ?? provider,
              content: "",
              latency_ms: 0,
              _streaming: true,
            } as ProviderResponseData & { _streaming?: boolean });
            existing.provider_count = existing.responses.length;
          }
          break;
        }

        case "thread_completed": {
          const threadId = data.thread_id as string;
          if (!threadId) break;

          const existing = threadMap.get(threadId);
          if (existing) {
            existing.status = "complete";
            existing.completed_at = Date.now() / 1000;
            // If the event includes the full thread data, update it.
            const fullThread = data.thread as DiscussionThreadData | undefined;
            if (fullThread?.responses) {
              existing.responses = fullThread.responses;
              existing.provider_count = fullThread.provider_count ?? existing.responses.length;
              existing.success_count = fullThread.success_count ?? existing.responses.filter((r) => !r.error).length;
            }
          }
          break;
        }

        case "thread_synthesised": {
          const threadId = data.thread_id as string;
          if (!threadId) break;

          const existing = threadMap.get(threadId);
          if (existing) {
            existing.status = "synthesised";
            existing.synthesis = (data.synthesis as string) ?? "";
            existing.synthesis_provider =
              (data.synthesis_provider as string) ?? "";
          }
          break;
        }

        case "consensus_result": {
          const result: ConsensusResultData = {
            thread_id: (data.thread_id as string) ?? "",
            strategy: (data.strategy as ConsensusResultData["strategy"]) ?? "pick_best",
            provider: (data.provider as string) ?? "",
            model: (data.model as string) ?? "",
            confidence: (data.confidence as number) ?? 0,
            reasoning: (data.reasoning as string) ?? "",
            scores: (data.scores as Record<string, number>) ?? undefined,
            judge_count: (data.judge_count as number) ?? undefined,
            judge_agreement: (data.judge_agreement as number) ?? undefined,
            judge_verdicts: (data.judge_verdicts as ConsensusResultData["judge_verdicts"]) ?? undefined,
          };
          consensusResults.push(result);

          // Also mark the associated discussion thread as synthesised.
          if (result.thread_id) {
            const thread = threadMap.get(result.thread_id);
            if (thread) {
              thread.status = "synthesised";
              thread.synthesis_provider = result.provider;
            }
          }
          break;
        }
      }
    }

    // Return threads most-recent-first.
    const threads = Array.from(threadMap.values()).reverse();

    return {
      threads,
      consensusResults: consensusResults.reverse(),
    };
  }, [events]);
}
