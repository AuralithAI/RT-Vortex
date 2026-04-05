// ─── Multi-LLM Discussion Panel ──────────────────────────────────────────────
// Renders a multi-model discussion thread: each provider's response shown
// side-by-side with latency badges, model names, and syntax-highlighted
// content. Streams in real-time via WebSocket events.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import {
  Brain,
  ChevronDown,
  ChevronUp,
  Clock,
  CheckCircle,
  XCircle,
  Sparkles,
  MessageSquare,
} from "lucide-react";
import { getProviderMeta } from "@/lib/llm-providers";
import type {
  DiscussionThreadData,
  ProviderResponseData,
} from "@/types/swarm";

// ── Props ───────────────────────────────────────────────────────────────────

interface MultiLLMDiscussionProps {
  /** Discussion threads from WebSocket events. */
  threads: DiscussionThreadData[];
  /** Max threads to display (default: 5). */
  maxThreads?: number;
}

// ── Provider Response Card ──────────────────────────────────────────────────

function ProviderResponseCard({
  response,
  isWinner,
}: {
  response: ProviderResponseData;
  isWinner: boolean;
}) {
  const [expanded, setExpanded] = useState(false);
  const meta = getProviderMeta(response.provider);
  const succeeded = !response.error;
  const contentPreview = response.content.length > 300 && !expanded
    ? response.content.slice(0, 300) + "…"
    : response.content;

  return (
    <div
      className={`rounded-lg border p-4 transition-all ${meta.borderColor} ${meta.bgColor} ${
        isWinner ? "ring-2 ring-offset-1 ring-green-500/50 dark:ring-green-400/40" : ""
      }`}
    >
      {/* Header */}
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className={`text-sm font-semibold ${meta.color}`}>
            {meta.displayName}
          </span>
          {response.model && (
            <span className="rounded bg-black/5 px-1.5 py-0.5 text-[10px] text-muted-foreground dark:bg-white/5">
              {response.model}
            </span>
          )}
          {isWinner && (
            <span className="flex items-center gap-0.5 rounded-full bg-green-100 px-2 py-0.5 text-[10px] font-medium text-green-700 dark:bg-green-900/40 dark:text-green-300">
              <Sparkles className="h-3 w-3" /> Winner
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          {succeeded ? (
            <>
              <Clock className="h-3 w-3" />
              <span>{response.latency_ms}ms</span>
              <CheckCircle className="h-3 w-3 text-green-500" />
            </>
          ) : (
            <>
              <XCircle className="h-3 w-3 text-red-500" />
              <span className="text-red-500">Failed</span>
            </>
          )}
        </div>
      </div>

      {/* Content */}
      {succeeded ? (
        <div className="relative">
          <pre className="whitespace-pre-wrap text-xs leading-relaxed text-foreground/90">
            {contentPreview}
          </pre>
          {response.content.length > 300 && (
            <button
              onClick={() => setExpanded(!expanded)}
              className="mt-1 flex items-center gap-1 text-xs text-blue-600 hover:underline dark:text-blue-400"
            >
              {expanded ? (
                <>
                  <ChevronUp className="h-3 w-3" /> Show less
                </>
              ) : (
                <>
                  <ChevronDown className="h-3 w-3" /> Show full response
                </>
              )}
            </button>
          )}
        </div>
      ) : (
        <p className="text-xs text-red-600 dark:text-red-400">
          {response.error}
        </p>
      )}

      {/* Token usage footer */}
      {response.token_usage && response.token_usage.total_tokens > 0 && (
        <div className="mt-2 flex gap-3 border-t border-dashed pt-2 text-[10px] text-muted-foreground">
          <span>Prompt: {response.token_usage.prompt_tokens}</span>
          <span>Completion: {response.token_usage.completion_tokens}</span>
          <span>Total: {response.token_usage.total_tokens}</span>
        </div>
      )}
    </div>
  );
}

// ── Discussion Thread ───────────────────────────────────────────────────────

function DiscussionThreadCard({
  thread,
}: {
  thread: DiscussionThreadData;
}) {
  const [collapsed, setCollapsed] = useState(false);
  const statusColor =
    thread.status === "synthesised"
      ? "text-green-600 dark:text-green-400"
      : thread.status === "complete"
        ? "text-blue-600 dark:text-blue-400"
        : "text-yellow-600 dark:text-yellow-400";

  return (
    <div className="rounded-xl border bg-card shadow-sm">
      {/* Thread Header */}
      <button
        onClick={() => setCollapsed(!collapsed)}
        className="flex w-full items-center justify-between p-4 text-left"
      >
        <div className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-violet-100 dark:bg-violet-900/40">
            <Brain className="h-4 w-4 text-violet-600 dark:text-violet-400" />
          </div>
          <div>
            <h4 className="text-sm font-semibold">
              Multi-LLM Discussion
            </h4>
            <p className="max-w-md truncate text-xs text-muted-foreground">
              {thread.topic || "Probe query"}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1 text-xs">
            <span className={statusColor}>
              {thread.status === "synthesised"
                ? "✓ Resolved"
                : thread.status === "complete"
                  ? "Awaiting consensus"
                  : "In progress"}
            </span>
          </div>
          <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
            {thread.success_count}/{thread.provider_count} providers
          </span>
          {collapsed ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronUp className="h-4 w-4 text-muted-foreground" />
          )}
        </div>
      </button>

      {/* Provider Responses */}
      {!collapsed && (
        <div className="space-y-3 border-t px-4 pb-4 pt-3">
          <div className="grid gap-3 md:grid-cols-2">
            {thread.responses.map((resp, i) => (
              <ProviderResponseCard
                key={`${resp.provider}-${i}`}
                response={resp}
                isWinner={
                  thread.synthesis_provider === resp.provider
                }
              />
            ))}
          </div>

          {/* Synthesis footer */}
          {thread.synthesis && (
            <div className="rounded-lg border border-green-200 bg-green-50 p-3 dark:border-green-800 dark:bg-green-950/30">
              <div className="mb-1 flex items-center gap-1.5 text-xs font-medium text-green-700 dark:text-green-300">
                <Sparkles className="h-3 w-3" />
                Selected Answer
                {thread.synthesis_provider && (
                  <span className="ml-1 rounded bg-green-200/60 px-1 py-0.5 text-[10px] dark:bg-green-800/40">
                    from {getProviderMeta(thread.synthesis_provider).displayName}
                  </span>
                )}
              </div>
              <pre className="whitespace-pre-wrap text-xs leading-relaxed text-foreground/90">
                {thread.synthesis.length > 500
                  ? thread.synthesis.slice(0, 500) + "…"
                  : thread.synthesis}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ── Main Component ──────────────────────────────────────────────────────────

export function MultiLLMDiscussion({
  threads,
  maxThreads = 5,
}: MultiLLMDiscussionProps) {
  const displayThreads = threads.slice(0, maxThreads);

  if (displayThreads.length === 0) {
    return null;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <MessageSquare className="h-5 w-5 text-violet-600 dark:text-violet-400" />
        <h3 className="text-lg font-semibold">
          Multi-LLM Discussions
        </h3>
        <span className="rounded-full bg-violet-100 px-2 py-0.5 text-xs font-medium text-violet-700 dark:bg-violet-900/40 dark:text-violet-300">
          {threads.length}
        </span>
      </div>
      <div className="space-y-3">
        {displayThreads.map((thread) => (
          <DiscussionThreadCard key={thread.thread_id} thread={thread} />
        ))}
      </div>
      {threads.length > maxThreads && (
        <p className="text-center text-xs text-muted-foreground">
          +{threads.length - maxThreads} more discussion
          {threads.length - maxThreads !== 1 ? "s" : ""}
        </p>
      )}
    </div>
  );
}
