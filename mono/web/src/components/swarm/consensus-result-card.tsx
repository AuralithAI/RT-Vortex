// ─── Consensus Result Card ───────────────────────────────────────────────────
// Renders the consensus engine's final decision: which strategy was used,
// which provider won, the confidence level, composite scores across all
// providers, and (for multi-judge panel) the individual judge verdicts.
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
  BarChart3,
  Info,
} from "lucide-react";
import { getProviderMeta } from "@/lib/llm-providers";
import { JudgeVerdictPanel } from "@/components/swarm/judge-verdict-panel";
import type { ConsensusResultData } from "@/types/swarm";

// ── Strategy display config ─────────────────────────────────────────────────

const strategyDisplay: Record<
  string,
  { label: string; icon: typeof Trophy; description: string; color: string }
> = {
  pick_best: {
    label: "Pick Best",
    icon: Trophy,
    description: "Selected the highest-priority provider",
    color: "text-blue-600 dark:text-blue-400",
  },
  majority_vote: {
    label: "Majority Vote",
    icon: Users,
    description: "Selected by token-overlap voting across providers",
    color: "text-amber-600 dark:text-amber-400",
  },
  gpt_as_judge: {
    label: "GPT as Judge",
    icon: Scale,
    description: "GPT independently evaluated all responses",
    color: "text-emerald-600 dark:text-emerald-400",
  },
  multi_judge_panel: {
    label: "Multi-Judge Panel",
    icon: Shield,
    description: "3+ independent LLM judges evaluated all responses",
    color: "text-violet-600 dark:text-violet-400",
  },
};

// ── Confidence Meter ────────────────────────────────────────────────────────

function ConfidenceMeter({ value }: { value: number }) {
  const pct = Math.round(value * 100);
  const label =
    pct >= 90
      ? "Very High"
      : pct >= 75
        ? "High"
        : pct >= 60
          ? "Moderate"
          : pct >= 40
            ? "Low"
            : "Very Low";
  const bgColor =
    pct >= 75
      ? "bg-green-500"
      : pct >= 50
        ? "bg-yellow-500"
        : "bg-red-500";
  const textColor =
    pct >= 75
      ? "text-green-600 dark:text-green-400"
      : pct >= 50
        ? "text-yellow-600 dark:text-yellow-400"
        : "text-red-600 dark:text-red-400";

  return (
    <div className="flex items-center gap-3">
      <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
        <div
          className={`h-full rounded-full transition-all duration-500 ${bgColor}`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className={`whitespace-nowrap text-xs font-medium tabular-nums ${textColor}`}>
        {pct}% — {label}
      </span>
    </div>
  );
}

// ── Score Bar Chart ─────────────────────────────────────────────────────────

function ScoreBarChart({ scores }: { scores: Record<string, number> }) {
  const entries = Object.entries(scores).sort(([, a], [, b]) => b - a);
  if (!entries.length) return null;

  const maxScore = Math.max(...entries.map(([, s]) => s), 0.01);

  return (
    <div className="space-y-1.5">
      {entries.map(([provider, score]) => {
        const meta = getProviderMeta(provider);
        const pct = Math.round((score / maxScore) * 100);
        return (
          <div key={provider} className="flex items-center gap-2">
            <span className={`w-16 truncate text-xs font-medium ${meta.color}`}>
              {meta.displayName}
            </span>
            <div className="h-2.5 flex-1 overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-gradient-to-r from-violet-400 to-violet-600 transition-all duration-500 dark:from-violet-500 dark:to-violet-700"
                style={{ width: `${pct}%` }}
              />
            </div>
            <span className="w-10 text-right text-[10px] tabular-nums text-muted-foreground">
              {(score * 100).toFixed(0)}%
            </span>
          </div>
        );
      })}
    </div>
  );
}

// ── Main Component ──────────────────────────────────────────────────────────

interface ConsensusResultCardProps {
  result: ConsensusResultData;
}

export function ConsensusResultCard({ result }: ConsensusResultCardProps) {
  const [showDetails, setShowDetails] = useState(false);
  const strat = strategyDisplay[result.strategy] ?? strategyDisplay.pick_best;
  const StratIcon = strat.icon;
  const winnerMeta = getProviderMeta(result.provider);
  const isMultiJudge = result.strategy === "multi_judge_panel";

  return (
    <div className="rounded-xl border bg-card shadow-sm">
      {/* Header */}
      <div className="flex items-center justify-between border-b p-4">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-full bg-violet-100 dark:bg-violet-900/40">
            <StratIcon className={`h-5 w-5 ${strat.color}`} />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h4 className="text-sm font-semibold">Consensus Decision</h4>
              <span
                className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${strat.color}`}
              >
                {strat.label}
              </span>
            </div>
            <p className="text-xs text-muted-foreground">{strat.description}</p>
          </div>
        </div>

        <button
          onClick={() => setShowDetails(!showDetails)}
          className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted"
        >
          <Info className="h-3 w-3" />
          Details
          {showDetails ? (
            <ChevronUp className="h-3 w-3" />
          ) : (
            <ChevronDown className="h-3 w-3" />
          )}
        </button>
      </div>

      {/* Winner + Confidence */}
      <div className="space-y-4 p-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Trophy className="h-4 w-4 text-amber-500" />
            <span className="text-xs text-muted-foreground">Winner:</span>
            <span
              className={`rounded-full px-2.5 py-1 text-xs font-semibold ${winnerMeta.bgColor} ${winnerMeta.color}`}
            >
              {result.provider === "consensus" ? (
                <>
                  <Sparkles className="mr-1 inline h-3 w-3" />
                  Synthesised
                </>
              ) : (
                winnerMeta.displayName
              )}
            </span>
            {result.model && (
              <span className="rounded bg-black/5 px-1.5 py-0.5 text-[10px] text-muted-foreground dark:bg-white/5">
                {result.model}
              </span>
            )}
          </div>

          {/* Judge agreement badge (multi-judge only) */}
          {isMultiJudge && result.judge_count && result.judge_count > 0 && (
            <div className="flex items-center gap-1.5 rounded-full bg-violet-50 px-2.5 py-1 text-[10px] dark:bg-violet-950/30">
              <Scale className="h-3 w-3 text-violet-500" />
              <span className="font-medium text-violet-700 dark:text-violet-300">
                {result.judge_count} judges •{" "}
                {Math.round((result.judge_agreement ?? 0) * 100)}% agreement
              </span>
            </div>
          )}
        </div>

        {/* Confidence bar */}
        <div>
          <p className="mb-1 text-[10px] uppercase tracking-wider text-muted-foreground">
            Confidence
          </p>
          <ConfidenceMeter value={result.confidence} />
        </div>

        {/* Reasoning */}
        <div className="rounded-lg bg-muted/50 p-3">
          <p className="text-xs leading-relaxed text-muted-foreground">
            {result.reasoning}
          </p>
        </div>
      </div>

      {/* Expandable Details */}
      {showDetails && (
        <div className="space-y-4 border-t px-4 pb-4 pt-3">
          {/* Composite Scores */}
          {result.scores && Object.keys(result.scores).length > 0 && (
            <div>
              <div className="mb-2 flex items-center gap-2">
                <BarChart3 className="h-4 w-4 text-muted-foreground" />
                <h4 className="text-sm font-semibold">
                  {isMultiJudge
                    ? "Composite Scores (averaged across judges)"
                    : "Provider Scores"}
                </h4>
              </div>
              <ScoreBarChart scores={result.scores} />
            </div>
          )}

          {/* Judge Verdicts (multi-judge panel only) */}
          {isMultiJudge && result.judge_verdicts && result.judge_verdicts.length > 0 && (
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
