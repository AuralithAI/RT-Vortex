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
import { sanitizeLLMContent } from "@/lib/sanitize-llm-content";
import { LLMMarkdown } from "@/components/ui/llm-markdown";
import {
  OpenAIIcon,
  AnthropicIcon,
  GeminiIcon,
  GrokIcon,
  OllamaIcon,
} from "@/components/icons/brand-icons";
import type {
  DiscussionThreadData,
  ProviderResponseData,
} from "@/types/swarm";

// ── Provider icon lookup ────────────────────────────────────────────────────

const providerIconMap: Record<string, React.ComponentType<{ size?: number; className?: string }>> = {
  openai: OpenAIIcon,
  anthropic: AnthropicIcon,
  gemini: GeminiIcon,
  google: GeminiIcon,
  grok: GrokIcon,
  ollama: OllamaIcon,
};

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

  return (
    <div
      className={`rounded-lg border p-4 transition-all ${meta.borderColor} ${meta.bgColor} ${
        isWinner ? "ring-2 ring-offset-1 ring-green-500/50 dark:ring-green-400/40" : ""
      }`}
    >
      {/* Header */}
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-2">
          {(() => {
            const key = response.provider.toLowerCase().replace(/[^a-z]/g, "");
            const Icon = providerIconMap[key];
            return Icon ? <Icon size={18} className="shrink-0" /> : null;
          })()}
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
        (() => {
          const sanitized = sanitizeLLMContent(response.content, response.provider);
          // If sanitization removed everything, show a subtle note instead of blank.
          if (!sanitized) {
            return (
              <p className="text-xs italic text-muted-foreground">
                (tool-call only — no text content)
              </p>
            );
          }
          return (
            <div className="relative">
              <div className={!expanded && sanitized.length > 300 ? "max-h-[200px] overflow-hidden" : ""}>
                <LLMMarkdown
                  content={expanded || sanitized.length <= 300 ? sanitized : sanitized.slice(0, 300) + "…"}
                  variant="light"
                  className="text-xs"
                />
              </div>
              {sanitized.length > 300 && (
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
          );
        })()
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

// ── Synthesis Footer (with expand/collapse) ─────────────────────────────────

const SYNTHESIS_PREVIEW_LEN = 500;

function DiscussionSynthesisFooter({
  synthesis,
  synthesisProvider,
}: {
  synthesis: string;
  synthesisProvider?: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const sanitized = sanitizeLLMContent(synthesis, synthesisProvider);
  const needsTruncation = sanitized.length > SYNTHESIS_PREVIEW_LEN;
  const displayContent =
    needsTruncation && !expanded
      ? sanitized.slice(0, SYNTHESIS_PREVIEW_LEN) + "…"
      : sanitized;

  return (
    <div className="rounded-lg border border-green-200 bg-green-50 p-3 dark:border-green-800 dark:bg-green-950/30">
      <div className="mb-1 flex items-center gap-1.5 text-xs font-medium text-green-700 dark:text-green-300">
        <Sparkles className="h-3 w-3" />
        Selected Answer
        {synthesisProvider && (
          <span className="ml-1 rounded bg-green-200/60 px-1 py-0.5 text-[10px] dark:bg-green-800/40">
            from {getProviderMeta(synthesisProvider).displayName}
          </span>
        )}
      </div>
      <div className="max-h-[600px] overflow-y-auto">
        <LLMMarkdown
          content={displayContent}
          variant="light"
          className="text-xs"
        />
      </div>
      {needsTruncation && (
        <button
          onClick={() => setExpanded(!expanded)}
          className="mt-1.5 flex items-center gap-1 text-[11px] font-medium text-green-600 hover:text-green-700 dark:text-green-400 dark:hover:text-green-300 transition-colors"
        >
          {expanded ? (
            <>
              <ChevronUp className="h-3 w-3" /> Collapse
            </>
          ) : (
            <>
              <ChevronDown className="h-3 w-3" /> Show full answer
            </>
          )}
        </button>
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
            <DiscussionSynthesisFooter
              synthesis={thread.synthesis}
              synthesisProvider={thread.synthesis_provider}
            />
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
