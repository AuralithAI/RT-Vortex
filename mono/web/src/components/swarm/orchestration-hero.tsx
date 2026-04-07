// ─── Orchestration Hero ──────────────────────────────────────────────────────
// Stunning, real-time multi-LLM orchestration UI inspired by modern parallel
// task execution interfaces. Shows agent header with LIVE badge, connected
// model avatars with status dots, agent instruction bubble, broadcast stats
// bar, and a grid of per-model response cards with streaming indicators.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useMemo, useState } from "react";
import {
  Activity,
  Brain,
  CheckCircle,
  ChevronDown,
  ChevronUp,
  Clock,
  Cpu,
  Layers,
  MessageCircle,
  Radio,
  Sparkles,
  XCircle,
  Zap,
} from "lucide-react";
import { getProviderMeta } from "@/lib/llm-providers";
import type {
  DiscussionThreadData,
  ProviderResponseData,
  ConsensusResultData,
} from "@/types/swarm";

// ─── Provider Icon (coloured circle with initial) ───────────────────────────

function ProviderIcon({
  provider,
  size = 32,
}: {
  provider: string;
  size?: number;
}) {
  const meta = getProviderMeta(provider);
  const accent = meta.accentHex ?? "#6b7280";
  const initials = meta.displayName.slice(0, 2);

  return (
    <div
      className="flex items-center justify-center rounded-full font-bold text-white select-none"
      style={{
        width: size,
        height: size,
        fontSize: size * 0.38,
        background: `linear-gradient(135deg, ${accent}, ${accent}cc)`,
      }}
      title={meta.displayName}
    >
      {initials}
    </div>
  );
}

// ─── Typing dots ────────────────────────────────────────────────────────────

function TypingDots({ className = "" }: { className?: string }) {
  return (
    <span className={`inline-flex items-center gap-0.5 ${className}`}>
      <span className="inline-block h-1.5 w-1.5 animate-bounce rounded-full bg-current" style={{ animationDelay: "0ms" }} />
      <span className="inline-block h-1.5 w-1.5 animate-bounce rounded-full bg-current" style={{ animationDelay: "150ms" }} />
      <span className="inline-block h-1.5 w-1.5 animate-bounce rounded-full bg-current" style={{ animationDelay: "300ms" }} />
    </span>
  );
}

// ─── Model Response Card ────────────────────────────────────────────────────
// Each provider gets one of these — shows status, content preview, timing.

type CardStatus = "thinking" | "streaming" | "complete" | "failed";

function ModelResponseCard({
  response,
  status,
  isWinner,
  timeAgo,
}: {
  response: ProviderResponseData;
  status: CardStatus;
  isWinner: boolean;
  timeAgo: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const meta = getProviderMeta(response.provider);
  const succeeded = !response.error;
  const preview =
    response.content.length > 220 && !expanded
      ? response.content.slice(0, 220) + "…"
      : response.content;

  const statusBadge: Record<CardStatus, { label: string; cls: string }> = {
    thinking: {
      label: "Thinking…",
      cls: "bg-yellow-500/20 text-yellow-300 border border-yellow-500/30",
    },
    streaming: {
      label: "Streaming…",
      cls: "bg-blue-500/20 text-blue-300 border border-blue-500/30",
    },
    complete: {
      label: "Complete",
      cls: "bg-emerald-500/20 text-emerald-300 border border-emerald-500/30",
    },
    failed: {
      label: "Failed",
      cls: "bg-red-500/20 text-red-300 border border-red-500/30",
    },
  };

  const badge = statusBadge[status];

  return (
    <div
      className={`group relative flex flex-col overflow-hidden rounded-2xl border transition-all duration-300 hover:shadow-lg ${
        isWinner
          ? "ring-2 ring-emerald-500/40 shadow-emerald-500/10 shadow-lg"
          : ""
      } border-white/[0.06] bg-[#1a1b2e]/80 backdrop-blur-sm`}
    >
      {/* ── Card header ──────────────────────────────────────────── */}
      <div className="flex items-center justify-between px-4 py-3">
        <div className="flex items-center gap-2.5">
          <ProviderIcon provider={response.provider} size={28} />
          <span className="text-sm font-semibold text-white/90">
            {meta.displayName}
          </span>
          {/* Animated dots for thinking/streaming */}
          {(status === "thinking" || status === "streaming") && (
            <TypingDots className="text-white/40" />
          )}
        </div>
        <div className="flex items-center gap-2">
          <span className={`rounded-full px-2.5 py-0.5 text-[10px] font-semibold ${badge.cls}`}>
            {badge.label}
          </span>
          {(status === "streaming" || status === "thinking") && (
            <span className="relative flex h-2.5 w-2.5">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-blue-400 opacity-75" />
              <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-blue-500" />
            </span>
          )}
        </div>
      </div>

      {/* ── Card body ────────────────────────────────────────────── */}
      <div className="flex-1 px-4 pb-3">
        {status === "thinking" && !response.content ? (
          <div className="flex items-center gap-2 py-3 text-sm text-white/40">
            <Activity className="h-4 w-4 animate-pulse" />
            <span>Analysing the problem…</span>
          </div>
        ) : succeeded ? (
          <div className="relative">
            <p className="text-[13px] leading-relaxed text-white/70">
              {preview}
            </p>
            {response.content.length > 220 && (
              <button
                onClick={() => setExpanded(!expanded)}
                className="mt-1.5 flex items-center gap-1 text-[11px] font-medium text-blue-400 hover:text-blue-300 transition-colors"
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
          <div className="flex items-center gap-2 py-1 text-sm text-red-400">
            <XCircle className="h-4 w-4 flex-shrink-0" />
            <span className="truncate">{response.error || "Request failed"}</span>
          </div>
        )}
      </div>

      {/* ── Card footer ──────────────────────────────────────────── */}
      <div className="flex items-center justify-between border-t border-white/[0.06] px-4 py-2 text-[11px] text-white/30">
        <span>{timeAgo}</span>
        <div className="flex items-center gap-3">
          {succeeded && response.latency_ms > 0 && (
            <span className="flex items-center gap-1">
              <Clock className="h-3 w-3" />
              {response.latency_ms}ms
            </span>
          )}
          {response.token_usage && response.token_usage.total_tokens > 0 && (
            <span>{response.token_usage.total_tokens.toLocaleString()} tokens</span>
          )}
          {isWinner && (
            <span className="flex items-center gap-1 text-emerald-400 font-medium">
              <Sparkles className="h-3 w-3" /> Winner
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Placeholder card (provider still thinking, no response yet) ────────────

function PlaceholderCard({ providerName }: { providerName?: string }) {
  return (
    <div className="flex flex-col overflow-hidden rounded-2xl border border-white/[0.06] bg-[#1a1b2e]/50 backdrop-blur-sm animate-pulse">
      <div className="flex items-center gap-2.5 px-4 py-3">
        <div className="h-7 w-7 rounded-full bg-white/10" />
        <div className="h-4 w-20 rounded bg-white/10" />
        <TypingDots className="text-white/20" />
      </div>
      <div className="space-y-2 px-4 pb-4">
        <div className="h-3 w-full rounded bg-white/5" />
        <div className="h-3 w-3/4 rounded bg-white/5" />
        <div className="h-3 w-1/2 rounded bg-white/5" />
      </div>
    </div>
  );
}

// ─── Agent instruction bubble ───────────────────────────────────────────────

function AgentInstructionBubble({
  agentRole,
  topic,
}: {
  agentRole: string;
  topic: string;
}) {
  const label = agentRole
    ? agentRole.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase())
    : "Agent";

  return (
    <div className="flex items-start gap-3 py-2">
      <div className="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-xl bg-violet-600/20 border border-violet-500/20">
        <Brain className="h-4.5 w-4.5 text-violet-400" />
      </div>
      <div className="rounded-2xl rounded-tl-sm bg-violet-600/20 border border-violet-500/20 px-4 py-3 max-w-2xl">
        <p className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-violet-400">
          {label}
        </p>
        <p className="text-sm leading-relaxed text-white/80">
          {topic}
        </p>
      </div>
    </div>
  );
}

// ─── Broadcast Stats Bar ────────────────────────────────────────────────────

function BroadcastStatsBar({
  totalProviders,
  responding,
  complete,
  failed,
}: {
  totalProviders: number;
  responding: number;
  complete: number;
  failed: number;
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-xl border border-white/[0.06] bg-white/[0.03] px-4 py-2.5 text-xs text-white/50">
      <span className="flex items-center gap-1.5">
        <Radio className="h-3.5 w-3.5 text-violet-400" />
        <span className="text-white/70 font-medium">
          Broadcast to {totalProviders} LLMs
        </span>
      </span>
      <span className="text-white/20">•</span>
      <span className="flex items-center gap-1.5">
        <Layers className="h-3.5 w-3.5" />
        Running in parallel
      </span>
      {responding > 0 && (
        <>
          <span className="text-white/20">•</span>
          <span className="flex items-center gap-1.5">
            <Activity className="h-3.5 w-3.5 text-blue-400 animate-pulse" />
            {responding} responding
          </span>
        </>
      )}
      {complete > 0 && (
        <>
          <span className="text-white/20">•</span>
          <span className="flex items-center gap-1.5">
            <CheckCircle className="h-3.5 w-3.5 text-emerald-400" />
            {complete} complete
          </span>
        </>
      )}
      {failed > 0 && (
        <>
          <span className="text-white/20">•</span>
          <span className="flex items-center gap-1.5">
            <XCircle className="h-3.5 w-3.5 text-red-400" />
            {failed} failed
          </span>
        </>
      )}
    </div>
  );
}

// ─── Single Discussion Thread (hero layout) ─────────────────────────────────

function ThreadHero({
  thread,
  consensusResult,
}: {
  thread: DiscussionThreadData;
  consensusResult?: ConsensusResultData;
}) {
  const uniqueProviders = useMemo(() => {
    const seen = new Set<string>();
    for (const r of thread.responses) {
      seen.add(r.provider);
    }
    return Array.from(seen);
  }, [thread.responses]);

  const responding = thread.status === "open"
    ? thread.provider_count - thread.responses.length
    : 0;
  const complete = thread.responses.filter((r) => !r.error).length;
  const failed = thread.responses.filter((r) => !!r.error).length;

  // Determine card status for each response
  const cardStatus = (resp: ProviderResponseData): CardStatus => {
    if (resp.error) return "failed";
    if (thread.status === "open" && !resp.content) return "thinking";
    if (thread.status === "open") return "streaming";
    return "complete";
  };

  const timeAgoStr = (resp: ProviderResponseData) => {
    if (!resp.timestamp) return "";
    const sec = Math.round(Date.now() / 1000 - resp.timestamp);
    if (sec < 5) return "just now";
    if (sec < 60) return `${sec}s ago`;
    if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
    return `${Math.floor(sec / 3600)}h ago`;
  };

  return (
    <div className="space-y-4">
      {/* Agent instruction */}
      {thread.topic && (
        <AgentInstructionBubble
          agentRole={thread.agent_role}
          topic={thread.topic}
        />
      )}

      {/* Stats bar */}
      <BroadcastStatsBar
        totalProviders={thread.provider_count}
        responding={responding}
        complete={complete}
        failed={failed}
      />

      {/* Response card grid */}
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {thread.responses.map((resp, i) => (
          <ModelResponseCard
            key={`${resp.provider}-${i}`}
            response={resp}
            status={cardStatus(resp)}
            isWinner={
              (consensusResult?.provider === resp.provider) ||
              (thread.synthesis_provider === resp.provider)
            }
            timeAgo={timeAgoStr(resp)}
          />
        ))}
        {/* Placeholder cards for providers still thinking */}
        {thread.status === "open" &&
          Array.from({ length: Math.max(0, thread.provider_count - thread.responses.length) }).map(
            (_, i) => <PlaceholderCard key={`ph-${i}`} />,
          )}
      </div>

      {/* Synthesis / selected answer */}
      {thread.synthesis && (
        <div className="relative overflow-hidden rounded-2xl border border-emerald-500/20 bg-emerald-500/[0.08] p-5">
          <div className="absolute -right-10 -top-10 h-28 w-28 rounded-full bg-emerald-500/5 blur-3xl" />
          <div className="relative flex items-start gap-3">
            <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-xl bg-emerald-500/20">
              <Sparkles className="h-4 w-4 text-emerald-400" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="mb-2 flex items-center gap-2">
                <span className="text-sm font-semibold text-emerald-300">
                  Selected Answer
                </span>
                {thread.synthesis_provider && (
                  <span className="rounded-full bg-emerald-500/20 px-2 py-0.5 text-[10px] font-medium text-emerald-300 border border-emerald-500/20">
                    via {getProviderMeta(thread.synthesis_provider).displayName}
                  </span>
                )}
              </div>
              <p className="text-[13px] leading-relaxed text-white/70">
                {thread.synthesis.length > 600
                  ? thread.synthesis.slice(0, 600) + "…"
                  : thread.synthesis}
              </p>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Main Orchestration Hero ────────────────────────────────────────────────

interface OrchestrationHeroProps {
  /** Agent display name (e.g. "Senior Dev Agent") */
  agentLabel: string;
  /** Whether the WS is connected */
  connected: boolean;
  /** All discussion threads */
  threads: DiscussionThreadData[];
  /** Consensus results */
  consensusResults: ConsensusResultData[];
  /** Names of configured/active providers */
  activeProviders: string[];
}

export function OrchestrationHero({
  agentLabel,
  connected,
  threads,
  consensusResults,
  activeProviders,
}: OrchestrationHeroProps) {
  const [expandedThreadIdx, setExpandedThreadIdx] = useState<number | null>(null);

  // Derive unique providers across all threads
  const allRespondingProviders = useMemo(() => {
    const seen = new Set<string>();
    for (const t of threads) {
      for (const r of t.responses) {
        seen.add(r.provider);
      }
    }
    return Array.from(seen);
  }, [threads]);

  // Use active providers from config, or fall back to what we've seen in threads
  const displayProviders =
    activeProviders.length > 0 ? activeProviders : allRespondingProviders;

  const hasActiveThreads = threads.some((t) => t.status === "open");
  const totalModels = displayProviders.length;

  // Build consensus lookup
  const consensusByThread = useMemo(() => {
    const map = new Map<string, ConsensusResultData>();
    for (const c of consensusResults) {
      if (c.thread_id) map.set(c.thread_id, c);
    }
    return map;
  }, [consensusResults]);

  return (
    <div className="relative overflow-hidden rounded-3xl border border-white/[0.06] bg-gradient-to-br from-[#0f1029] via-[#131432] to-[#0d0e24] p-6 shadow-2xl">
      {/* Subtle background glow */}
      <div className="pointer-events-none absolute -left-20 -top-20 h-60 w-60 rounded-full bg-violet-600/10 blur-3xl" />
      <div className="pointer-events-none absolute -bottom-20 -right-20 h-60 w-60 rounded-full bg-blue-600/10 blur-3xl" />

      <div className="relative space-y-5">
        {/* ── Hero Header ──────────────────────────────────────────── */}
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-2">
            {/* Title row */}
            <div className="flex items-center gap-3">
              <h2 className="text-2xl font-bold text-white tracking-tight">
                {agentLabel}
              </h2>
              <span className="text-2xl">🧠</span>
            </div>

            {/* Subtitle */}
            <p className="text-sm text-white/40">
              Orchestrating real-time conversations with multiple LLMs
            </p>

            {/* Provider avatar row */}
            {displayProviders.length > 0 && (
              <div className="flex items-center gap-3 pt-1">
                {displayProviders.map((p) => {
                  const meta = getProviderMeta(p);
                  const isResponding = allRespondingProviders.includes(p);
                  return (
                    <div key={p} className="flex items-center gap-1.5">
                      <div className="relative">
                        <ProviderIcon provider={p} size={24} />
                        {/* Status dot */}
                        <span
                          className={`absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full border-2 border-[#131432] ${
                            isResponding ? (meta.dotColor ?? "bg-green-500") : "bg-gray-600"
                          }`}
                        />
                      </div>
                      <span className="text-xs font-medium text-white/50">
                        {meta.displayName}
                      </span>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* LIVE badge + model count */}
          <div className="flex items-center gap-3">
            {connected && (
              <div className="flex items-center gap-2 rounded-full bg-emerald-500/20 px-3.5 py-1.5 border border-emerald-500/30">
                <span className="relative flex h-2.5 w-2.5">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                  <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-emerald-500" />
                </span>
                <span className="text-xs font-bold text-emerald-300 uppercase tracking-wider">
                  Live
                </span>
              </div>
            )}
            {totalModels > 0 && (
              <div className="flex items-center gap-1.5 rounded-full bg-white/[0.05] px-3 py-1.5 border border-white/[0.08] text-xs text-white/50">
                <Cpu className="h-3.5 w-3.5" />
                Connected to{" "}
                <span className="font-bold text-white/80">{totalModels} models</span>
              </div>
            )}
          </div>
        </div>

        {/* ── Thread list ──────────────────────────────────────────── */}
        {threads.length > 0 ? (
          <div className="space-y-6">
            {threads.map((thread, idx) => {
              const isExpanded = expandedThreadIdx === null || expandedThreadIdx === idx;
              const consensus = consensusByThread.get(thread.thread_id);

              return (
                <div key={thread.thread_id}>
                  {/* Thread toggle header (if multiple) */}
                  {threads.length > 1 && (
                    <button
                      onClick={() =>
                        setExpandedThreadIdx(expandedThreadIdx === idx ? null : idx)
                      }
                      className="mb-3 flex w-full items-center gap-2 text-left"
                    >
                      <div className={`flex h-7 w-7 items-center justify-center rounded-lg ${
                        thread.status === "open"
                          ? "bg-yellow-500/20"
                          : thread.status === "synthesised"
                            ? "bg-emerald-500/20"
                            : "bg-violet-500/20"
                      }`}>
                        {thread.status === "open" ? (
                          <Activity className="h-3.5 w-3.5 text-yellow-400 animate-pulse" />
                        ) : thread.status === "synthesised" ? (
                          <Sparkles className="h-3.5 w-3.5 text-emerald-400" />
                        ) : (
                          <Brain className="h-3.5 w-3.5 text-violet-400" />
                        )}
                      </div>
                      <span className="text-sm font-semibold text-white/80 truncate flex-1">
                        {thread.action_type
                          ? thread.action_type.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase())
                          : `Discussion #${idx + 1}`}
                      </span>
                      <span className="rounded-full bg-white/[0.05] px-2 py-0.5 text-[10px] font-medium text-white/40">
                        {thread.success_count}/{thread.provider_count} providers
                      </span>
                      {expandedThreadIdx === idx ? (
                        <ChevronUp className="h-4 w-4 text-white/30" />
                      ) : (
                        <ChevronDown className="h-4 w-4 text-white/30" />
                      )}
                    </button>
                  )}

                  {isExpanded && (
                    <ThreadHero thread={thread} consensusResult={consensus} />
                  )}
                </div>
              );
            })}
          </div>
        ) : (
          /* ── Empty state ──────────────────────────────────────────── */
          <div className="flex flex-col items-center justify-center py-16">
            <div className="mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-violet-500/10 border border-violet-500/20">
              <Brain className="h-8 w-8 text-violet-400/60" />
            </div>
            <p className="text-sm font-semibold text-white/50">
              AI reasoning will appear here
            </p>
            <p className="mt-1.5 max-w-md text-center text-xs text-white/30">
              When multi-LLM routing is enabled, you&apos;ll see each model&apos;s
              response side-by-side with live streaming, discussion, and
              consensus — all in real time
            </p>
            {hasActiveThreads && (
              <div className="mt-4 flex items-center gap-2 text-xs text-yellow-400/60">
                <Activity className="h-4 w-4 animate-pulse" />
                Models are responding…
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
