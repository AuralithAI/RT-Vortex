// ─── Consensus Decision Panel ────────────────────────────────────────────────
// A visually rich, animated panel showing how the consensus engine decided on
// the final answer. Features: animated confidence ring, strategy badge,
// provider comparison bars, expandable judge verdicts, and reasoning display.
// Replaces the basic ConsensusResultCard with a more transparent, engaging UI.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import {
  Trophy,
  Shield,
  ChevronDown,
  ChevronUp,
  Sparkles,
  Scale,
  Users,
  Brain,
  Gavel,
  BarChart3,
  ArrowRight,
  Info,
  CheckCircle2,
  Zap,
  Target,
} from "lucide-react";
import { getProviderMeta } from "@/lib/llm-providers";
import { JudgeVerdictPanel } from "@/components/swarm/judge-verdict-panel";
import type { ConsensusResultData } from "@/types/swarm";

// ── Strategy display config ─────────────────────────────────────────────────

const strategyMeta: Record<
  string,
  {
    label: string;
    icon: typeof Trophy;
    description: string;
    color: string;
    bgColor: string;
    borderColor: string;
  }
> = {
  pick_best: {
    label: "Pick Best",
    icon: Trophy,
    description: "Selected the highest-priority provider with a valid response",
    color: "text-blue-600 dark:text-blue-400",
    bgColor: "bg-blue-50 dark:bg-blue-950/30",
    borderColor: "border-blue-200 dark:border-blue-800",
  },
  majority_vote: {
    label: "Majority Vote",
    icon: Users,
    description: "Token-overlap voting across multiple providers to find agreement",
    color: "text-amber-600 dark:text-amber-400",
    bgColor: "bg-amber-50 dark:bg-amber-950/30",
    borderColor: "border-amber-200 dark:border-amber-800",
  },
  gpt_as_judge: {
    label: "LLM as Judge",
    icon: Scale,
    description: "An independent LLM evaluated all responses and picked the best",
    color: "text-emerald-600 dark:text-emerald-400",
    bgColor: "bg-emerald-50 dark:bg-emerald-950/30",
    borderColor: "border-emerald-200 dark:border-emerald-800",
  },
  multi_judge_panel: {
    label: "Multi-Judge Panel",
    icon: Shield,
    description: "3+ independent LLM judges evaluated all responses and voted",
    color: "text-violet-600 dark:text-violet-400",
    bgColor: "bg-violet-50 dark:bg-violet-950/30",
    borderColor: "border-violet-200 dark:border-violet-800",
  },
};

// ── Animated Confidence Ring ────────────────────────────────────────────────

function ConfidenceRing({ value, size = 80 }: { value: number; size?: number }) {
  const pct = Math.round(value * 100);
  const radius = (size - 10) / 2;
  const circumference = 2 * Math.PI * radius;
  const offset = circumference - (pct / 100) * circumference;

  const color =
    pct >= 80 ? "#22c55e" : pct >= 60 ? "#eab308" : pct >= 40 ? "#f97316" : "#ef4444";
  const label =
    pct >= 90
      ? "Excellent"
      : pct >= 75
        ? "High"
        : pct >= 60
          ? "Good"
          : pct >= 40
            ? "Fair"
            : "Low";

  return (
    <div className="flex flex-col items-center">
      <svg width={size} height={size} className="rotate-[-90deg]">
        {/* Background ring */}
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke="currentColor"
          strokeWidth="5"
          className="text-muted/20"
        />
        {/* Progress ring */}
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke={color}
          strokeWidth="5"
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          className="transition-all duration-1000 ease-out"
        />
      </svg>
      <div
        className="absolute flex flex-col items-center justify-center"
        style={{ width: size, height: size }}
      >
        <span className="text-xl font-bold tabular-nums" style={{ color }}>
          {pct}%
        </span>
      </div>
      <span className="mt-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
        {label} Confidence
      </span>
    </div>
  );
}

// ── Score Comparison ────────────────────────────────────────────────────────

function ScoreComparison({
  scores,
  winnerProvider,
}: {
  scores: Record<string, number>;
  winnerProvider: string;
}) {
  const entries = Object.entries(scores).sort(([, a], [, b]) => b - a);
  if (!entries.length) return null;

  const maxScore = Math.max(...entries.map(([, s]) => s), 0.01);

  return (
    <div className="space-y-2">
      {entries.map(([provider, score], i) => {
        const meta = getProviderMeta(provider);
        const pct = Math.round((score / maxScore) * 100);
        const isWinner = provider === winnerProvider;

        return (
          <div key={provider} className="flex items-center gap-3">
            {/* Rank */}
            <span className={`flex h-5 w-5 items-center justify-center rounded text-[10px] font-bold ${
              i === 0
                ? "bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300"
                : "bg-muted text-muted-foreground"
            }`}>
              {i + 1}
            </span>

            {/* Provider name */}
            <span className={`w-16 truncate text-xs font-semibold ${meta.color}`}>
              {meta.displayName}
            </span>

            {/* Bar */}
            <div className="h-3 flex-1 overflow-hidden rounded-full bg-muted/40">
              <div
                className={`h-full rounded-full transition-all duration-700 ease-out ${
                  isWinner
                    ? "bg-gradient-to-r from-green-400 to-emerald-500"
                    : i === 0
                      ? "bg-gradient-to-r from-violet-400 to-violet-600"
                      : "bg-gradient-to-r from-gray-300 to-gray-400 dark:from-gray-600 dark:to-gray-500"
                }`}
                style={{ width: `${pct}%` }}
              />
            </div>

            {/* Score */}
            <span className={`w-12 text-right text-xs tabular-nums ${
              isWinner ? "font-bold text-green-600 dark:text-green-400" : "text-muted-foreground"
            }`}>
              {(score * 100).toFixed(0)}%
            </span>

            {isWinner && (
              <Trophy className="h-3.5 w-3.5 text-amber-500 flex-shrink-0" />
            )}
          </div>
        );
      })}
    </div>
  );
}

// ── Main Component ──────────────────────────────────────────────────────────

interface ConsensusDecisionPanelProps {
  results: ConsensusResultData[];
}

export function ConsensusDecisionPanel({ results }: ConsensusDecisionPanelProps) {
  if (results.length === 0) return null;

  return (
    <div className="space-y-4">
      {/* Section header */}
      <div className="flex items-center gap-3">
        <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-green-100 to-emerald-100 dark:from-green-900/40 dark:to-emerald-900/40">
          <Gavel className="h-5 w-5 text-green-600 dark:text-green-400" />
        </div>
        <div>
          <h3 className="text-lg font-semibold">Consensus Decisions</h3>
          <p className="text-xs text-muted-foreground">
            How the engine chose the final answer from multiple AI models
          </p>
        </div>
        <span className="ml-auto rounded-full bg-green-100 px-2.5 py-1 text-[11px] font-medium text-green-700 dark:bg-green-900/40 dark:text-green-300">
          {results.length} decision{results.length !== 1 ? "s" : ""}
        </span>
      </div>

      {/* Decision cards */}
      <div className="space-y-3">
        {results.map((result, i) => (
          <ConsensusCard key={`consensus-${i}`} result={result} index={i} />
        ))}
      </div>
    </div>
  );
}

function ConsensusCard({
  result,
  index,
}: {
  result: ConsensusResultData;
  index: number;
}) {
  const [showDetails, setShowDetails] = useState(false);
  const strat = strategyMeta[result.strategy] ?? strategyMeta.pick_best;
  const StratIcon = strat.icon;
  const winnerMeta = getProviderMeta(result.provider);
  const isMultiJudge = result.strategy === "multi_judge_panel";
  const isConsensus = result.provider === "consensus";

  return (
    <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
      {/* Top banner — strategy */}
      <div className={`flex items-center gap-2 px-5 py-2.5 ${strat.bgColor} border-b ${strat.borderColor}`}>
        <StratIcon className={`h-4 w-4 ${strat.color}`} />
        <span className={`text-xs font-semibold ${strat.color}`}>
          {strat.label}
        </span>
        <span className="text-[10px] text-muted-foreground">—</span>
        <span className="text-[11px] text-muted-foreground">{strat.description}</span>
      </div>

      {/* Main content */}
      <div className="p-5">
        <div className="flex items-start gap-6">
          {/* Confidence ring */}
          <div className="relative flex-shrink-0">
            <ConfidenceRing value={result.confidence} />
          </div>

          {/* Details */}
          <div className="flex-1 space-y-3">
            {/* Winner row */}
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-muted-foreground">Winner:</span>
              <span
                className={`inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-semibold ${winnerMeta.bgColor} ${winnerMeta.color} border ${winnerMeta.borderColor}`}
              >
                {isConsensus ? (
                  <>
                    <Sparkles className="h-3 w-3" />
                    Synthesised
                  </>
                ) : (
                  <>
                    <Target className="h-3 w-3" />
                    {winnerMeta.displayName}
                  </>
                )}
              </span>
              {result.model && (
                <span className="rounded-md bg-black/5 px-2 py-0.5 text-[10px] font-mono text-muted-foreground dark:bg-white/5">
                  {result.model}
                </span>
              )}

              {/* Multi-judge agreement badge */}
              {isMultiJudge && result.judge_count && result.judge_count > 0 && (
                <span className="inline-flex items-center gap-1 rounded-full bg-violet-50 px-2.5 py-1 text-[10px] font-medium text-violet-700 dark:bg-violet-950/30 dark:text-violet-300">
                  <Scale className="h-3 w-3" />
                  {result.judge_count} judges • {Math.round((result.judge_agreement ?? 0) * 100)}% agreement
                </span>
              )}
            </div>

            {/* Reasoning */}
            <div className="rounded-lg bg-muted/40 p-3">
              <div className="mb-1 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                <Brain className="h-3 w-3" />
                Reasoning
              </div>
              <p className="text-[13px] leading-relaxed text-foreground/80">
                {result.reasoning}
              </p>
            </div>
          </div>
        </div>

        {/* Expand toggle */}
        {((result.scores && Object.keys(result.scores).length > 0) ||
          (isMultiJudge && result.judge_verdicts && result.judge_verdicts.length > 0)) && (
          <button
            onClick={() => setShowDetails(!showDetails)}
            className="mt-3 flex w-full items-center justify-center gap-1.5 rounded-lg border border-dashed py-2 text-xs font-medium text-muted-foreground hover:border-violet-300 hover:text-violet-600 transition-colors"
          >
            <Info className="h-3.5 w-3.5" />
            {showDetails ? "Hide" : "Show"} detailed scores & judge verdicts
            {showDetails ? (
              <ChevronUp className="h-3.5 w-3.5" />
            ) : (
              <ChevronDown className="h-3.5 w-3.5" />
            )}
          </button>
        )}
      </div>

      {/* Expanded details */}
      {showDetails && (
        <div className="space-y-5 border-t px-5 pb-5 pt-4">
          {/* Score comparison */}
          {result.scores && Object.keys(result.scores).length > 0 && (
            <div>
              <div className="mb-3 flex items-center gap-2">
                <BarChart3 className="h-4 w-4 text-muted-foreground" />
                <h4 className="text-sm font-semibold">
                  {isMultiJudge
                    ? "Composite Scores (averaged across judges)"
                    : "Provider Scores"}
                </h4>
              </div>
              <ScoreComparison
                scores={result.scores}
                winnerProvider={result.provider}
              />
            </div>
          )}

          {/* Judge verdicts */}
          {isMultiJudge &&
            result.judge_verdicts &&
            result.judge_verdicts.length > 0 && (
              <JudgeVerdictPanel
                verdicts={result.judge_verdicts}
                winnerProvider={result.provider}
              />
            )}
        </div>
      )}
    </div>
  );
}
