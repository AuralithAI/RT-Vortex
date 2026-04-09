// ─── Judge Verdict Panel ─────────────────────────────────────────────────────
// Displays individual judge verdicts from the Multi-Judge Panel consensus
// strategy. Each judge card shows which LLM served as judge, who it picked
// as winner, its confidence level, and its reasoning.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { Scale, ThumbsUp, AlertTriangle, Info } from "lucide-react";
import { getProviderMeta } from "@/lib/llm-providers";
import type { JudgeVerdictData } from "@/types/swarm";

interface JudgeVerdictPanelProps {
  verdicts: JudgeVerdictData[];
  winnerProvider: string;
}

function ConfidenceBar({ value }: { value: number }) {
  const pct = Math.round(value * 100);
  const color =
    pct >= 80
      ? "bg-green-500"
      : pct >= 60
        ? "bg-yellow-500"
        : "bg-red-500";

  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-16 overflow-hidden rounded-full bg-muted">
        <div
          className={`h-full rounded-full transition-all ${color}`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="text-[10px] tabular-nums text-muted-foreground">
        {pct}%
      </span>
    </div>
  );
}

function JudgeCard({
  verdict,
  agreedWithMajority,
}: {
  verdict: JudgeVerdictData;
  agreedWithMajority: boolean;
}) {
  const judgeMeta = getProviderMeta(verdict.judge_provider);
  const winnerMeta = verdict.winner ? getProviderMeta(verdict.winner) : null;
  const hasFailed = !!verdict.error;

  return (
    <div
      className={`rounded-lg border p-3 transition-all ${judgeMeta.borderColor} ${judgeMeta.bgColor} ${
        hasFailed ? "opacity-60" : ""
      }`}
    >
      {/* Judge header */}
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Scale className={`h-4 w-4 ${judgeMeta.color}`} />
          <span className={`text-xs font-semibold ${judgeMeta.color}`}>
            {judgeMeta.displayName}
          </span>
          {verdict.judge_model && (
            <span className="rounded bg-black/5 px-1 py-0.5 text-[10px] text-muted-foreground dark:bg-white/5">
              {verdict.judge_model}
            </span>
          )}
        </div>
        {!hasFailed && (
          <div className="flex items-center gap-1">
            {agreedWithMajority ? (
              <ThumbsUp className="h-3 w-3 text-green-500" />
            ) : (
              <AlertTriangle className="h-3 w-3 text-yellow-500" />
            )}
          </div>
        )}
      </div>

      {hasFailed ? (
        <p className="text-xs text-red-600 dark:text-red-400">
          ⚠ {verdict.error}
        </p>
      ) : (
        <>
          {/* Verdict */}
          <div className="mb-2 flex items-center gap-2">
            <span className="text-[10px] uppercase tracking-wider text-muted-foreground">
              Picked:
            </span>
            {winnerMeta && (
              <span
                className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${winnerMeta.bgColor} ${winnerMeta.color}`}
              >
                {verdict.winner === "synthesis"
                  ? "🔀 Synthesis"
                  : winnerMeta.displayName}
              </span>
            )}
          </div>

          {/* Confidence */}
          <div className="mb-2">
            <ConfidenceBar value={verdict.confidence} />
          </div>

          {/* Reasoning */}
          {verdict.reasoning && (
            <p className="text-[11px] leading-relaxed text-muted-foreground">
              {verdict.reasoning}
            </p>
          )}

          {/* Score breakdown */}
          {verdict.scores && Object.keys(verdict.scores).length > 0 && (
            <div className="mt-2 flex flex-wrap gap-2 border-t border-dashed pt-2">
              {Object.entries(verdict.scores)
                .sort(([, a], [, b]) => b - a)
                .map(([provider, score]) => {
                  const pm = getProviderMeta(provider);
                  return (
                    <span
                      key={provider}
                      className="flex items-center gap-1 text-[10px] text-muted-foreground"
                    >
                      <span className={pm.color}>{pm.displayName}</span>
                      <span className="tabular-nums">{(score * 100).toFixed(0)}%</span>
                    </span>
                  );
                })}
            </div>
          )}
        </>
      )}
    </div>
  );
}

export function JudgeVerdictPanel({
  verdicts,
  winnerProvider,
}: JudgeVerdictPanelProps) {
  if (!verdicts.length) return null;

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Scale className="h-4 w-4 text-muted-foreground" />
        <h4 className="text-sm font-semibold">Judge Verdicts</h4>
        <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] text-muted-foreground">
          {verdicts.filter((v) => !v.error).length} of {verdicts.length} judges
        </span>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {verdicts.map((verdict, i) => (
          <JudgeCard
            key={`${verdict.judge_provider}-${i}`}
            verdict={verdict}
            agreedWithMajority={
              !verdict.error && verdict.winner === winnerProvider
            }
          />
        ))}
      </div>
    </div>
  );
}
