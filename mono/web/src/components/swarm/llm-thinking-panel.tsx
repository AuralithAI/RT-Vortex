// ─── LLM Thinking Panel ──────────────────────────────────────────────────────
// A real-time, visually rich panel that shows what each LLM provider is
// responding, how they are "thinking", and the discussion flow between models.
// Inspired by modern multi-model comparison UIs — transparent AI reasoning
// shown live as it happens.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useRef } from "react";
import {
  Brain,
  Sparkles,
  Zap,
  Clock,
  CheckCircle,
  XCircle,
  ChevronDown,
  ChevronUp,
  Eye,
  Layers,
  ArrowRight,
  Activity,
  MessageCircle,
  Cpu,
  BarChart3,
  Wand2,
} from "lucide-react";
import { getProviderMeta } from "@/lib/llm-providers";
import { sanitizeLLMContent } from "@/lib/sanitize-llm-content";
import { LLMMarkdown } from "@/components/ui/llm-markdown";
import type {
  DiscussionThreadData,
  ProviderResponseData,
  ConsensusResultData,
} from "@/types/swarm";

// ── Animated Typing Indicator ───────────────────────────────────────────────

function TypingIndicator({ providerColor }: { providerColor: string }) {
  return (
    <div className="flex items-center gap-1 py-2">
      <span
        className={`inline-block h-2 w-2 animate-bounce rounded-full ${providerColor}`}
        style={{ animationDelay: "0ms" }}
      />
      <span
        className={`inline-block h-2 w-2 animate-bounce rounded-full ${providerColor}`}
        style={{ animationDelay: "150ms" }}
      />
      <span
        className={`inline-block h-2 w-2 animate-bounce rounded-full ${providerColor}`}
        style={{ animationDelay: "300ms" }}
      />
    </div>
  );
}

// ── Animated Confidence Ring ────────────────────────────────────────────────

function ConfidenceRing({ value, size = 64 }: { value: number; size?: number }) {
  const pct = Math.round(value * 100);
  const radius = (size - 8) / 2;
  const circumference = 2 * Math.PI * radius;
  const offset = circumference - (pct / 100) * circumference;

  const color =
    pct >= 80 ? "#22c55e" : pct >= 60 ? "#eab308" : pct >= 40 ? "#f97316" : "#ef4444";
  const label =
    pct >= 90 ? "Excellent" : pct >= 75 ? "High" : pct >= 60 ? "Good" : pct >= 40 ? "Fair" : "Low";

  return (
    <div className="relative flex flex-col items-center">
      <svg width={size} height={size} className="rotate-[-90deg]">
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke="currentColor"
          strokeWidth="4"
          className="text-muted/30"
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke={color}
          strokeWidth="4"
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          className="transition-all duration-1000 ease-out"
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className="text-lg font-bold tabular-nums" style={{ color }}>
          {pct}%
        </span>
      </div>
      <span className="mt-1 text-[10px] font-medium text-muted-foreground">{label}</span>
    </div>
  );
}

// ── Provider Response Tile ──────────────────────────────────────────────────
// Individual card for each provider's response — shown in a grid

function ProviderTile({
  response,
  isWinner,
  isStreaming,
  threadStatus,
}: {
  response: ProviderResponseData;
  isWinner: boolean;
  isStreaming: boolean;
  threadStatus: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const meta = getProviderMeta(response.provider);
  const succeeded = !response.error;
  const sanitized = sanitizeLLMContent(response.content);
  const contentPreview =
    sanitized.length > 400 && !expanded
      ? sanitized.slice(0, 400) + "…"
      : sanitized;

  return (
    <div
      className={`group relative overflow-hidden rounded-xl border transition-all duration-300 hover:shadow-md ${
        isWinner
          ? "ring-2 ring-green-500/40 shadow-green-500/10 shadow-lg dark:ring-green-400/30"
          : ""
      } ${meta.borderColor} ${meta.bgColor}`}
    >
      {/* Winner glow effect */}
      {isWinner && (
        <div className="absolute inset-0 bg-gradient-to-br from-green-500/5 to-transparent pointer-events-none" />
      )}

      {/* Header bar */}
      <div className="flex items-center justify-between border-b border-inherit px-4 py-2.5">
        <div className="flex items-center gap-2.5">
          {/* Provider pill */}
          <div className={`flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-semibold ${meta.color} ${meta.bgColor} border ${meta.borderColor}`}>
            <Cpu className="h-3 w-3" />
            {meta.displayName}
          </div>

          {/* Model badge */}
          {response.model && (
            <span className="rounded-md bg-black/5 px-2 py-0.5 text-[10px] font-mono text-muted-foreground dark:bg-white/5">
              {response.model}
            </span>
          )}

          {/* Winner badge */}
          {isWinner && (
            <span className="flex items-center gap-1 rounded-full bg-green-100 px-2.5 py-0.5 text-[10px] font-semibold text-green-700 dark:bg-green-900/40 dark:text-green-300 animate-in fade-in slide-in-from-left-1 duration-300">
              <Sparkles className="h-3 w-3" />
              Selected
            </span>
          )}
        </div>

        {/* Status indicators */}
        <div className="flex items-center gap-2">
          {succeeded ? (
            <>
              <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
                <Clock className="h-3 w-3" />
                {response.latency_ms}ms
              </span>
              <CheckCircle className="h-3.5 w-3.5 text-green-500" />
            </>
          ) : (
            <>
              <XCircle className="h-3.5 w-3.5 text-red-500" />
              <span className="text-[11px] text-red-500">Failed</span>
            </>
          )}
        </div>
      </div>

      {/* Content area */}
      <div className="px-4 py-3">
        {isStreaming && !response.content ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Activity className="h-3.5 w-3.5 animate-pulse" />
            <span>Thinking…</span>
            <TypingIndicator providerColor={meta.color.includes("orange") ? "bg-orange-400" : meta.color.includes("amber") ? "bg-amber-400" : meta.color.includes("emerald") ? "bg-emerald-400" : meta.color.includes("blue") ? "bg-blue-400" : "bg-gray-400"} />
          </div>
        ) : succeeded ? (
          <div className="relative">
            {sanitized ? (
              <>
                <div className="max-h-[500px] overflow-y-auto">
                  <LLMMarkdown
                    content={contentPreview}
                    variant="light"
                    className="text-[13px] leading-relaxed"
                  />
                </div>
                {sanitized.length > 400 && (
                  <button
                    onClick={() => setExpanded(!expanded)}
                    className="mt-2 flex items-center gap-1 text-xs font-medium text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300 transition-colors"
                  >
                    {expanded ? (
                      <>
                        <ChevronUp className="h-3 w-3" /> Collapse
                      </>
                    ) : (
                      <>
                        <ChevronDown className="h-3 w-3" /> Show full response ({sanitized.length} chars)
                      </>
                    )}
                  </button>
                )}
              </>
            ) : (
              <p className="text-xs italic text-muted-foreground">
                (tool-call only — no text content)
              </p>
            )}
          </div>
        ) : (
          <div className="flex items-center gap-2 text-xs text-red-600 dark:text-red-400">
            <XCircle className="h-3.5 w-3.5 flex-shrink-0" />
            <span>{response.error}</span>
          </div>
        )}
      </div>

      {/* Token usage footer */}
      {response.token_usage && response.token_usage.total_tokens > 0 && (
        <div className="flex items-center gap-4 border-t border-inherit px-4 py-2 text-[10px] text-muted-foreground">
          <span className="flex items-center gap-1">
            <ArrowRight className="h-2.5 w-2.5" />
            In: {response.token_usage.prompt_tokens.toLocaleString()}
          </span>
          <span className="flex items-center gap-1">
            <Wand2 className="h-2.5 w-2.5" />
            Out: {response.token_usage.completion_tokens.toLocaleString()}
          </span>
          <span className="font-medium">
            Σ {response.token_usage.total_tokens.toLocaleString()} tokens
          </span>
        </div>
      )}
    </div>
  );
}

// ── Latency Comparison Bar ──────────────────────────────────────────────────

function LatencyComparisonBar({ responses }: { responses: ProviderResponseData[] }) {
  const successful = responses.filter((r) => !r.error && r.latency_ms > 0);
  if (successful.length < 2) return null;

  const maxLatency = Math.max(...successful.map((r) => r.latency_ms));
  const minLatency = Math.min(...successful.map((r) => r.latency_ms));

  return (
    <div className="rounded-lg border bg-card/50 p-3">
      <div className="mb-2 flex items-center gap-2 text-[11px] font-medium text-muted-foreground">
        <BarChart3 className="h-3.5 w-3.5" />
        Response Latency Comparison
      </div>
      <div className="space-y-1.5">
        {successful
          .sort((a, b) => a.latency_ms - b.latency_ms)
          .map((resp, i) => {
            const meta = getProviderMeta(resp.provider);
            const pct = maxLatency > 0 ? (resp.latency_ms / maxLatency) * 100 : 0;
            const isFastest = resp.latency_ms === minLatency;

            return (
              <div key={`${resp.provider}-${i}`} className="flex items-center gap-2">
                <span className={`w-14 truncate text-[11px] font-medium ${meta.color}`}>
                  {meta.displayName}
                </span>
                <div className="h-2.5 flex-1 overflow-hidden rounded-full bg-muted/50">
                  <div
                    className={`h-full rounded-full transition-all duration-700 ${
                      isFastest
                        ? "bg-gradient-to-r from-green-400 to-green-500"
                        : "bg-gradient-to-r from-violet-300 to-violet-500 dark:from-violet-400 dark:to-violet-600"
                    }`}
                    style={{ width: `${pct}%` }}
                  />
                </div>
                <span className={`w-14 text-right text-[10px] tabular-nums ${isFastest ? "font-semibold text-green-600 dark:text-green-400" : "text-muted-foreground"}`}>
                  {resp.latency_ms}ms
                </span>
              </div>
            );
          })}
      </div>
    </div>
  );
}

// ── Thread Timeline Step ────────────────────────────────────────────────────

function TimelineStep({
  icon,
  label,
  sublabel,
  isActive,
  isComplete,
  color,
}: {
  icon: React.ReactNode;
  label: string;
  sublabel?: string;
  isActive: boolean;
  isComplete: boolean;
  color: string;
}) {
  return (
    <div className="flex items-center gap-2">
      <div
        className={`flex h-7 w-7 items-center justify-center rounded-full border-2 transition-all ${
          isComplete
            ? `${color} border-current bg-current/10`
            : isActive
              ? `${color} border-current animate-pulse`
              : "border-muted text-muted-foreground"
        }`}
      >
        {icon}
      </div>
      <div>
        <p className={`text-[11px] font-medium ${isComplete || isActive ? color : "text-muted-foreground"}`}>
          {label}
        </p>
        {sublabel && (
          <p className="text-[10px] text-muted-foreground">{sublabel}</p>
        )}
      </div>
    </div>
  );
}

// ── Discussion Flow Timeline ────────────────────────────────────────────────

function DiscussionTimeline({ thread }: { thread: DiscussionThreadData }) {
  const isOpen = thread.status === "open";
  const isComplete = thread.status === "complete" || thread.status === "synthesised";
  const isSynthesised = thread.status === "synthesised";
  const responsesIn = thread.responses.length;

  return (
    <div className="flex items-center gap-1 overflow-x-auto pb-1">
      <TimelineStep
        icon={<Zap className="h-3 w-3" />}
        label="Probe Sent"
        isActive={false}
        isComplete={true}
        color="text-blue-600 dark:text-blue-400"
      />
      <div className={`h-0.5 w-6 rounded ${responsesIn > 0 ? "bg-blue-400" : "bg-muted"}`} />
      <TimelineStep
        icon={<MessageCircle className="h-3 w-3" />}
        label={`${thread.success_count}/${thread.provider_count} Responded`}
        sublabel={isOpen ? "Waiting…" : undefined}
        isActive={isOpen && responsesIn > 0}
        isComplete={isComplete}
        color="text-violet-600 dark:text-violet-400"
      />
      <div className={`h-0.5 w-6 rounded ${isComplete ? "bg-violet-400" : "bg-muted"}`} />
      <TimelineStep
        icon={<Brain className="h-3 w-3" />}
        label="Consensus"
        sublabel={isSynthesised ? "Resolved" : isComplete ? "Running…" : undefined}
        isActive={isComplete && !isSynthesised}
        isComplete={isSynthesised}
        color="text-green-600 dark:text-green-400"
      />
      {isSynthesised && (
        <>
          <div className="h-0.5 w-6 rounded bg-green-400" />
          <TimelineStep
            icon={<Sparkles className="h-3 w-3" />}
            label="Answer Selected"
            isActive={false}
            isComplete={true}
            color="text-green-600 dark:text-green-400"
          />
        </>
      )}
    </div>
  );
}

// ── Synthesis Display ───────────────────────────────────────────────────────

function SynthesisBlock({
  synthesis,
  synthesisProvider,
}: {
  synthesis: string;
  synthesisProvider?: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const sanitized = sanitizeLLMContent(synthesis);
  const displayContent =
    sanitized.length > 600 && !expanded
      ? sanitized.slice(0, 600) + "…"
      : sanitized;
  const providerMeta = synthesisProvider
    ? getProviderMeta(synthesisProvider)
    : null;

  if (!sanitized) {
    return null;
  }

  return (
    <div className="relative overflow-hidden rounded-xl border-2 border-green-200 bg-gradient-to-br from-green-50 to-emerald-50/50 p-4 dark:border-green-800/60 dark:from-green-950/30 dark:to-emerald-950/20">
      {/* Glow effect */}
      <div className="absolute -right-8 -top-8 h-24 w-24 rounded-full bg-green-400/10 blur-2xl" />

      <div className="relative">
        <div className="mb-2.5 flex items-center gap-2">
          <div className="flex h-6 w-6 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/40">
            <Sparkles className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
          </div>
          <span className="text-sm font-semibold text-green-700 dark:text-green-300">
            Selected Answer
          </span>
          {providerMeta && (
            <span
              className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${providerMeta.bgColor} ${providerMeta.color} border ${providerMeta.borderColor}`}
            >
              via {providerMeta.displayName}
            </span>
          )}
        </div>

        <div className="max-h-[600px] overflow-y-auto">
          <LLMMarkdown
            content={displayContent}
            variant="light"
            className="text-[13px] leading-relaxed"
          />
        </div>

        {sanitized.length > 600 && (
          <button
            onClick={() => setExpanded(!expanded)}
            className="mt-2 flex items-center gap-1 text-xs font-medium text-green-600 hover:text-green-700 dark:text-green-400 dark:hover:text-green-300"
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
    </div>
  );
}

// ── Single Thread Panel ─────────────────────────────────────────────────────

function ThreadPanel({ thread }: { thread: DiscussionThreadData }) {
  const [collapsed, setCollapsed] = useState(false);
  const isStreaming = thread.status === "open";
  const isSynthesised = thread.status === "synthesised";

  const statusConfig = {
    open: {
      label: "Models responding…",
      dotColor: "bg-yellow-500",
      textColor: "text-yellow-600 dark:text-yellow-400",
      animate: true,
    },
    complete: {
      label: "Awaiting consensus",
      dotColor: "bg-blue-500",
      textColor: "text-blue-600 dark:text-blue-400",
      animate: true,
    },
    synthesised: {
      label: "Resolved",
      dotColor: "bg-green-500",
      textColor: "text-green-600 dark:text-green-400",
      animate: false,
    },
  };

  const status = statusConfig[thread.status] ?? statusConfig.open;

  return (
    <div className="overflow-hidden rounded-xl border bg-card shadow-sm transition-all hover:shadow-md">
      {/* Header */}
      <button
        onClick={() => setCollapsed(!collapsed)}
        className="flex w-full items-center justify-between px-5 py-4 text-left hover:bg-muted/30 transition-colors"
      >
        <div className="flex items-center gap-3 min-w-0">
          {/* Animated icon */}
          <div className={`flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl ${
            isSynthesised
              ? "bg-green-100 dark:bg-green-900/40"
              : isStreaming
                ? "bg-yellow-100 dark:bg-yellow-900/40"
                : "bg-violet-100 dark:bg-violet-900/40"
          }`}>
            {isStreaming ? (
              <Activity className="h-5 w-5 text-yellow-600 dark:text-yellow-400 animate-pulse" />
            ) : isSynthesised ? (
              <Sparkles className="h-5 w-5 text-green-600 dark:text-green-400" />
            ) : (
              <Brain className="h-5 w-5 text-violet-600 dark:text-violet-400" />
            )}
          </div>

          <div className="min-w-0">
            <h4 className="text-sm font-semibold truncate">
              {thread.action_type
                ? thread.action_type.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase())
                : "Multi-Model Analysis"}
            </h4>
            <p className="max-w-lg truncate text-xs text-muted-foreground">
              {thread.topic || "Probe query"}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-3 flex-shrink-0 ml-3">
          {/* Live status */}
          <div className="flex items-center gap-1.5">
            <span className={`h-2 w-2 rounded-full ${status.dotColor} ${status.animate ? "animate-pulse" : ""}`} />
            <span className={`text-xs font-medium ${status.textColor}`}>
              {status.label}
            </span>
          </div>

          {/* Provider count pill */}
          <span className="flex items-center gap-1 rounded-full bg-muted px-2.5 py-1 text-[11px] font-medium text-muted-foreground">
            <Layers className="h-3 w-3" />
            {thread.success_count}/{thread.provider_count}
          </span>

          {collapsed ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronUp className="h-4 w-4 text-muted-foreground" />
          )}
        </div>
      </button>

      {/* Expanded Content */}
      {!collapsed && (
        <div className="space-y-4 border-t px-5 pb-5 pt-4">
          {/* Timeline */}
          <DiscussionTimeline thread={thread} />

          {/* Provider response grid */}
          <div className="grid gap-3 lg:grid-cols-2">
            {thread.responses.map((resp, i) => (
              <ProviderTile
                key={`${resp.provider}-${i}`}
                response={resp}
                isWinner={thread.synthesis_provider === resp.provider}
                isStreaming={isStreaming}
                threadStatus={thread.status}
              />
            ))}
          </div>

          {/* Streaming placeholder for providers that haven't responded yet */}
          {isStreaming && thread.responses.length < thread.provider_count && (
            <div className="grid gap-3 lg:grid-cols-2">
              {Array.from({ length: thread.provider_count - thread.responses.length }).map((_, i) => (
                <div
                  key={`placeholder-${i}`}
                  className="flex items-center gap-3 rounded-xl border border-dashed p-4 animate-pulse"
                >
                  <div className="h-4 w-4 rounded-full bg-muted" />
                  <div className="space-y-1.5 flex-1">
                    <div className="h-3 w-24 rounded bg-muted" />
                    <div className="h-2 w-48 rounded bg-muted/60" />
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Latency comparison */}
          <LatencyComparisonBar responses={thread.responses} />

          {/* Synthesis */}
          {thread.synthesis && (
            <SynthesisBlock
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

interface LLMThinkingPanelProps {
  threads: DiscussionThreadData[];
  consensusResults: ConsensusResultData[];
  maxThreads?: number;
}

export function LLMThinkingPanel({
  threads,
  consensusResults,
  maxThreads = 10,
}: LLMThinkingPanelProps) {
  const [showAll, setShowAll] = useState(false);
  const displayThreads = showAll ? threads : threads.slice(0, maxThreads);
  const hasActiveThreads = threads.some((t) => t.status === "open");

  if (threads.length === 0 && consensusResults.length === 0) {
    return null;
  }

  return (
    <div className="space-y-4">
      {/* Section header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-violet-100 to-purple-100 dark:from-violet-900/40 dark:to-purple-900/40">
            <Eye className="h-5 w-5 text-violet-600 dark:text-violet-400" />
          </div>
          <div>
            <h3 className="text-lg font-semibold flex items-center gap-2">
              AI Reasoning
              {hasActiveThreads && (
                <span className="flex items-center gap-1.5 rounded-full bg-yellow-100 px-2.5 py-0.5 text-[11px] font-medium text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300 animate-pulse">
                  <Activity className="h-3 w-3" />
                  Live
                </span>
              )}
            </h3>
            <p className="text-xs text-muted-foreground">
              See how multiple AI models analyze and reason about this task
            </p>
          </div>
        </div>

        {/* Stats pills */}
        <div className="flex items-center gap-2">
          <span className="rounded-full bg-violet-100 px-2.5 py-1 text-[11px] font-medium text-violet-700 dark:bg-violet-900/40 dark:text-violet-300">
            {threads.length} discussion{threads.length !== 1 ? "s" : ""}
          </span>
          {consensusResults.length > 0 && (
            <span className="rounded-full bg-green-100 px-2.5 py-1 text-[11px] font-medium text-green-700 dark:bg-green-900/40 dark:text-green-300">
              {consensusResults.length} consensus
            </span>
          )}
        </div>
      </div>

      {/* Thread list */}
      <div className="space-y-3">
        {displayThreads.map((thread) => (
          <ThreadPanel key={thread.thread_id} thread={thread} />
        ))}
      </div>

      {/* Show more */}
      {!showAll && threads.length > maxThreads && (
        <button
          onClick={() => setShowAll(true)}
          className="flex w-full items-center justify-center gap-1.5 rounded-lg border border-dashed py-2.5 text-xs font-medium text-muted-foreground hover:border-violet-300 hover:text-violet-600 transition-colors"
        >
          <ChevronDown className="h-3.5 w-3.5" />
          Show {threads.length - maxThreads} more discussions
        </button>
      )}
    </div>
  );
}
