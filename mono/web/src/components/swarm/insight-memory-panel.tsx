// ─── Insight Memory Panel ─────────────────────────────────────────────────────
// Displays cross-task consensus insights grouped by category, plus aggregated
// provider reliability stats. Shows how the system learns from its own
// multi-LLM decisions over time.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Brain,
  TrendingUp,
  Shield,
  Zap,
  GitBranch,
  Users,
  BarChart3,
  ChevronDown,
  ChevronUp,
  RefreshCw,
} from "lucide-react";
import { getProviderMeta } from "@/lib/llm-providers";
import type {
  ConsensusInsightData,
  ProviderReliabilityStatsData,
  InsightCategory,
} from "@/types/swarm";

// ── Category display config ─────────────────────────────────────────────────

const categoryDisplay: Record<
  InsightCategory,
  { label: string; icon: typeof Brain; color: string; description: string }
> = {
  provider_reliability: {
    label: "Provider Reliability",
    icon: Shield,
    color: "text-blue-600 dark:text-blue-400",
    description: "Which LLM providers consistently win consensus decisions",
  },
  strategy_effectiveness: {
    label: "Strategy Effectiveness",
    icon: Zap,
    color: "text-amber-600 dark:text-amber-400",
    description: "How well different consensus strategies perform",
  },
  code_pattern: {
    label: "Code Patterns",
    icon: GitBranch,
    color: "text-emerald-600 dark:text-emerald-400",
    description: "Coding patterns and conventions discovered across tasks",
  },
  provider_agreement: {
    label: "Provider Agreement",
    icon: Users,
    color: "text-violet-600 dark:text-violet-400",
    description: "How often providers agree in multi-judge evaluations",
  },
  quality_signal: {
    label: "Quality Signals",
    icon: BarChart3,
    color: "text-rose-600 dark:text-rose-400",
    description: "Score spreads and quality patterns across providers",
  },
};

// ── Confidence badge ────────────────────────────────────────────────────────

function ConfidenceBadge({ value }: { value: number }) {
  const pct = Math.round(value * 100);
  const color =
    pct >= 75
      ? "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400"
      : pct >= 50
        ? "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400"
        : "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400";
  return (
    <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${color}`}>
      {pct}%
    </span>
  );
}

// ── Provider Stats Bar ──────────────────────────────────────────────────────

function ProviderStatsBar({
  stats,
}: {
  stats: ProviderReliabilityStatsData[];
}) {
  if (!stats.length) return null;

  const maxWins = Math.max(...stats.map((s) => s.win_count), 1);

  return (
    <div className="space-y-2">
      {stats.map((stat) => {
        const meta = getProviderMeta(stat.provider);
        const widthPct = Math.round((stat.win_count / maxWins) * 100);
        return (
          <div key={stat.provider} className="space-y-1">
            <div className="flex items-center justify-between text-xs">
              <span className="font-medium">{meta?.displayName ?? stat.provider}</span>
              <span className="text-muted-foreground">
                {stat.win_count} win{stat.win_count !== 1 ? "s" : ""} / {stat.total_decisions} decisions
                {stat.avg_confidence > 0 && (
                  <> • avg {Math.round(stat.avg_confidence * 100)}%</>
                )}
              </span>
            </div>
            <div className="h-2 overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full transition-all"
                style={{
                  width: `${widthPct}%`,
                  backgroundColor: meta?.color ?? "#6b7280",
                }}
              />
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ── Insight Category Group ──────────────────────────────────────────────────

function InsightCategoryGroup({
  category,
  insights,
}: {
  category: InsightCategory;
  insights: ConsensusInsightData[];
}) {
  const [expanded, setExpanded] = useState(true);
  const config = categoryDisplay[category];
  const Icon = config.icon;

  return (
    <div className="rounded-lg border bg-card">
      <button
        className="flex w-full items-center justify-between p-4 text-left"
        onClick={() => setExpanded(!expanded)}
      >
        <div className="flex items-center gap-2">
          <Icon className={`h-4 w-4 ${config.color}`} />
          <span className="font-medium">{config.label}</span>
          <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
            {insights.length}
          </span>
        </div>
        {expanded ? (
          <ChevronUp className="h-4 w-4 text-muted-foreground" />
        ) : (
          <ChevronDown className="h-4 w-4 text-muted-foreground" />
        )}
      </button>

      {expanded && (
        <div className="border-t px-4 pb-4">
          <p className="mb-3 mt-2 text-xs text-muted-foreground">
            {config.description}
          </p>
          <div className="space-y-2">
            {insights.map((insight) => {
              const meta = insight.provider
                ? getProviderMeta(insight.provider)
                : null;
              return (
                <div
                  key={insight.id}
                  className="flex items-start gap-3 rounded-md bg-muted/50 p-3 text-sm"
                >
                  <div className="min-w-0 flex-1">
                    <p className="text-foreground">{insight.insight}</p>
                    <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                      <ConfidenceBadge value={insight.confidence} />
                      {meta && (
                        <span
                          className="rounded px-1.5 py-0.5"
                          style={{
                            color: meta.color,
                            backgroundColor: meta.bgColor,
                          }}
                        >
                          {meta.displayName}
                        </span>
                      )}
                      {insight.strategy && (
                        <span>via {insight.strategy.replace(/_/g, " ")}</span>
                      )}
                      <span>
                        {new Date(insight.updated_at).toLocaleDateString()}
                      </span>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

// ── Main Panel ──────────────────────────────────────────────────────────────

export function InsightMemoryPanel({ taskId }: { taskId: string }) {
  const [insights, setInsights] = useState<ConsensusInsightData[]>([]);
  const [stats, setStats] = useState<ProviderReliabilityStatsData[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [insightsRes, statsRes] = await Promise.all([
        fetch(`/api/v1/swarm/insights?task_id=${taskId}`),
        // provider-stats uses the public insights endpoint with category filter
        fetch(`/api/v1/swarm/insights?task_id=${taskId}&category=provider_reliability`),
      ]);

      if (insightsRes.ok) {
        const data = await insightsRes.json();
        setInsights(data.insights ?? []);
      }

      // Try to extract stats from reliability insights
      if (statsRes.ok) {
        const data = await statsRes.json();
        const reliabilityInsights: ConsensusInsightData[] =
          data.insights ?? [];
        // Aggregate from reliability insights into stats-like structure
        const provMap = new Map<string, { wins: number; total: number; confSum: number }>();
        for (const ins of reliabilityInsights) {
          const prov = ins.provider;
          if (!prov) continue;
          const existing = provMap.get(prov) ?? { wins: 0, total: 0, confSum: 0 };
          existing.wins += 1;
          existing.total += 1;
          existing.confSum += ins.confidence;
          provMap.set(prov, existing);
        }
        const aggStats: ProviderReliabilityStatsData[] = [];
        for (const [prov, s] of provMap) {
          aggStats.push({
            provider: prov,
            win_count: s.wins,
            total_decisions: s.total,
            avg_confidence: s.total > 0 ? s.confSum / s.total : 0,
          });
        }
        aggStats.sort((a, b) => b.win_count - a.win_count);
        setStats(aggStats);
      }
    } catch (err) {
      setError("Failed to load insights");
      console.error("Insight fetch error:", err);
    } finally {
      setLoading(false);
    }
  }, [taskId]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // Group insights by category
  const grouped = insights.reduce(
    (acc, ins) => {
      const cat = ins.category as InsightCategory;
      if (!acc[cat]) acc[cat] = [];
      acc[cat].push(ins);
      return acc;
    },
    {} as Record<InsightCategory, ConsensusInsightData[]>,
  );

  const categoryOrder: InsightCategory[] = [
    "provider_reliability",
    "strategy_effectiveness",
    "provider_agreement",
    "quality_signal",
    "code_pattern",
  ];

  if (loading) {
    return (
      <div className="rounded-lg border bg-card p-6">
        <div className="flex items-center gap-2 text-muted-foreground">
          <Brain className="h-5 w-5 animate-pulse" />
          <span className="text-sm">Loading cross-task insights…</span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-lg border bg-card p-6">
        <p className="text-sm text-destructive">{error}</p>
      </div>
    );
  }

  if (!insights.length && !stats.length) {
    return null; // Nothing to show yet — insights will appear after consensus decisions
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Brain className="h-5 w-5 text-purple-600 dark:text-purple-400" />
          <h3 className="text-lg font-semibold">Cross-Task Insights</h3>
          <span className="rounded-full bg-purple-100 px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/30 dark:text-purple-400">
            {insights.length} insight{insights.length !== 1 ? "s" : ""}
          </span>
        </div>
        <button
          onClick={fetchData}
          className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted"
        >
          <RefreshCw className="h-3 w-3" />
          Refresh
        </button>
      </div>

      {/* Provider Reliability Stats */}
      {stats.length > 0 && (
        <div className="rounded-lg border bg-card p-4">
          <div className="mb-3 flex items-center gap-2">
            <TrendingUp className="h-4 w-4 text-blue-600 dark:text-blue-400" />
            <span className="text-sm font-medium">
              Provider Win Rates (This Repository)
            </span>
          </div>
          <ProviderStatsBar stats={stats} />
        </div>
      )}

      {/* Insight Groups */}
      {categoryOrder.map((cat) =>
        grouped[cat]?.length ? (
          <InsightCategoryGroup
            key={cat}
            category={cat}
            insights={grouped[cat]}
          />
        ) : null,
      )}
    </div>
  );
}
