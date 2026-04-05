// ─── Probe Tuning Card ───────────────────────────────────────────────────────
// Displays adaptive probe tuning configuration, provider statistics, and
// recent probe history for a given role.  Fetches from:
//   GET /api/v1/swarm/probe-configs/{role}   — config
//   GET /api/v1/swarm/probe-stats/{role}     — provider stats
//   GET /api/v1/swarm/probe-history          — recent outcomes
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Activity,
  BarChart3,
  ChevronDown,
  ChevronUp,
  Clock,
  DollarSign,
  Flame,
  Loader2,
  RefreshCw,
  Settings2,
  Shield,
  Sparkles,
  Target,
  Thermometer,
  Timer,
  Trophy,
  TrendingUp,
  Zap,
  XCircle,
  CheckCircle2,
} from "lucide-react";
import type {
  ProbeConfigData,
  ProbeHistoryData,
  ProbeStrategy,
  ProviderStatsData,
} from "@/types/swarm";

// ── Props ───────────────────────────────────────────────────────────────────

interface ProbeTuningCardProps {
  /** Role to display probe tuning for (e.g. "senior_dev"). */
  role: string;
  /** Optional repo_id filter. */
  repoId?: string;
  /** Compact mode — just strategy badge and model count. */
  compact?: boolean;
  /** Auto-refresh interval in ms (0 = no auto-refresh). */
  refreshInterval?: number;
}

// ── Strategy → visual mapping ───────────────────────────────────────────────

function strategyConfig(strategy: ProbeStrategy): {
  label: string;
  color: string;
  bgColor: string;
  borderColor: string;
  icon: React.ReactNode;
} {
  switch (strategy) {
    case "adaptive":
      return {
        label: "Adaptive",
        color: "text-violet-600 dark:text-violet-400",
        bgColor: "bg-violet-100 dark:bg-violet-900/40",
        borderColor: "border-violet-300 dark:border-violet-700",
        icon: <Sparkles className="h-3.5 w-3.5" />,
      };
    case "static":
      return {
        label: "Static",
        color: "text-slate-600 dark:text-slate-400",
        bgColor: "bg-slate-100 dark:bg-slate-800/50",
        borderColor: "border-slate-300 dark:border-slate-600",
        icon: <Settings2 className="h-3.5 w-3.5" />,
      };
    case "aggressive":
      return {
        label: "Aggressive",
        color: "text-orange-600 dark:text-orange-400",
        bgColor: "bg-orange-100 dark:bg-orange-900/40",
        borderColor: "border-orange-300 dark:border-orange-700",
        icon: <Flame className="h-3.5 w-3.5" />,
      };
    default:
      return {
        label: strategy,
        color: "text-gray-600 dark:text-gray-400",
        bgColor: "bg-gray-100 dark:bg-gray-800/50",
        borderColor: "border-gray-300 dark:border-gray-600",
        icon: <Settings2 className="h-3.5 w-3.5" />,
      };
  }
}

// ── Provider reliability → color ────────────────────────────────────────────

function reliabilityColor(score: number): string {
  if (score >= 0.8) return "text-emerald-600 dark:text-emerald-400";
  if (score >= 0.6) return "text-amber-600 dark:text-amber-400";
  return "text-red-600 dark:text-red-400";
}

function reliabilityBg(score: number): string {
  if (score >= 0.8) return "bg-emerald-500";
  if (score >= 0.6) return "bg-amber-500";
  return "bg-red-500";
}

// ── Role label helper ───────────────────────────────────────────────────────

function roleLabel(role: string): string {
  const map: Record<string, string> = {
    architect: "Architect",
    senior_dev: "Senior Dev",
    junior_dev: "Junior Dev",
    qa: "QA",
    security: "Security",
    docs: "Docs",
    ui_ux: "UI/UX",
    ops: "DevOps",
    orchestrator: "Orchestrator",
  };
  return map[role] ?? role;
}

// ── Component ───────────────────────────────────────────────────────────────

export function ProbeTuningCard({
  role,
  repoId,
  compact = false,
  refreshInterval = 0,
}: ProbeTuningCardProps) {
  const [config, setConfig] = useState<ProbeConfigData | null>(null);
  const [stats, setStats] = useState<ProviderStatsData[]>([]);
  const [history, setHistory] = useState<ProbeHistoryData[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [showHistory, setShowHistory] = useState(false);

  const fetchData = useCallback(async () => {
    try {
      const repoQ = repoId ? `?repo_id=${encodeURIComponent(repoId)}` : "";

      const [cfgRes, statsRes, histRes] = await Promise.all([
        fetch(`/api/v1/swarm/probe-configs/${encodeURIComponent(role)}${repoQ}`),
        fetch(`/api/v1/swarm/probe-stats/${encodeURIComponent(role)}${repoQ}`),
        fetch(`/api/v1/swarm/probe-history?role=${encodeURIComponent(role)}&limit=20`),
      ]);

      if (cfgRes.ok) {
        const cfgData = await cfgRes.json();
        setConfig(cfgData.probe_config ?? null);
      } else if (cfgRes.status !== 404) {
        throw new Error(`Config: HTTP ${cfgRes.status}`);
      }

      if (statsRes.ok) {
        const statsData = await statsRes.json();
        setStats(statsData.stats ?? []);
      }

      if (histRes.ok) {
        const histData = await histRes.json();
        setHistory(histData.history ?? []);
      }

      setError(null);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  }, [role, repoId]);

  useEffect(() => {
    fetchData();
    if (refreshInterval > 0) {
      const iv = setInterval(fetchData, refreshInterval);
      return () => clearInterval(iv);
    }
  }, [fetchData, refreshInterval]);

  // ── Loading / Empty states ────────────────────────────────────────────

  if (loading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground py-2">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span>Loading probe tuning…</span>
      </div>
    );
  }

  if (!config && stats.length === 0 && history.length === 0) {
    return null; // No data — nothing to show.
  }

  const sc = config ? strategyConfig(config.strategy as ProbeStrategy) : strategyConfig("adaptive");

  // ── Compact mode ──────────────────────────────────────────────────────

  if (compact) {
    return (
      <div className="flex items-center gap-2">
        <span
          className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${sc.bgColor} ${sc.color}`}
        >
          {sc.icon}
          {sc.label}
        </span>
        {config && (
          <span className="text-xs text-muted-foreground">
            {config.num_models} models · {config.temperature.toFixed(1)}°
          </span>
        )}
      </div>
    );
  }

  // ── Aggregate stats for header ────────────────────────────────────────

  const successCount = history.filter((h) => h.success).length;
  const successRate = history.length > 0 ? (successCount / history.length) * 100 : 0;
  const avgLatency =
    history.length > 0
      ? history.reduce((s, h) => s + h.total_ms, 0) / history.length
      : 0;
  const totalCost = history.reduce((s, h) => s + (h.estimated_cost_usd || 0), 0);

  // ── Full mode ─────────────────────────────────────────────────────────

  return (
    <div className={`rounded-lg border ${sc.borderColor} ${sc.bgColor}/30 p-4`}>
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className={`flex items-center gap-1.5 ${sc.color}`}>
            {sc.icon}
            <span className="font-semibold text-sm">
              Probe Tuning — {roleLabel(role)}
            </span>
          </div>
          <span
            className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${sc.bgColor} ${sc.color}`}
          >
            {sc.label}
          </span>
          {config && (
            <span className="text-xs text-muted-foreground">
              {config.num_models} models
            </span>
          )}
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={() => {
              setLoading(true);
              fetchData();
            }}
            className="p-1 hover:bg-white/50 dark:hover:bg-white/10 rounded"
            title="Refresh"
          >
            <RefreshCw className="h-3.5 w-3.5 text-muted-foreground" />
          </button>
          <button
            onClick={() => setExpanded(!expanded)}
            className="p-1 hover:bg-white/50 dark:hover:bg-white/10 rounded"
          >
            {expanded ? (
              <ChevronUp className="h-4 w-4 text-muted-foreground" />
            ) : (
              <ChevronDown className="h-4 w-4 text-muted-foreground" />
            )}
          </button>
        </div>
      </div>

      {/* Quick Stats Row */}
      <div className="mt-3 grid grid-cols-2 sm:grid-cols-4 gap-2">
        <StatPill
          icon={<Target className="h-3.5 w-3.5" />}
          label="Success"
          value={`${successRate.toFixed(0)}%`}
          color={successRate >= 80 ? "text-emerald-600 dark:text-emerald-400" : successRate >= 50 ? "text-amber-600 dark:text-amber-400" : "text-red-600 dark:text-red-400"}
        />
        <StatPill
          icon={<Timer className="h-3.5 w-3.5" />}
          label="Avg Latency"
          value={`${avgLatency.toFixed(0)}ms`}
        />
        <StatPill
          icon={<DollarSign className="h-3.5 w-3.5" />}
          label="Total Cost"
          value={`$${totalCost.toFixed(4)}`}
        />
        <StatPill
          icon={<Activity className="h-3.5 w-3.5" />}
          label="Probes"
          value={String(history.length)}
        />
      </div>

      {/* Config Summary */}
      {config && (
        <div className="mt-3 flex flex-wrap gap-2">
          <ConfigBadge
            icon={<Zap className="h-3 w-3" />}
            label="Models"
            value={String(config.num_models)}
          />
          <ConfigBadge
            icon={<Thermometer className="h-3 w-3" />}
            label="Temp"
            value={config.temperature.toFixed(2)}
          />
          <ConfigBadge
            icon={<Clock className="h-3 w-3" />}
            label="Timeout"
            value={`${(config.timeout_ms / 1000).toFixed(0)}s`}
          />
          <ConfigBadge
            icon={<Shield className="h-3 w-3" />}
            label="Confidence"
            value={config.confidence_threshold.toFixed(2)}
          />
          {config.budget_cap_usd > 0 && (
            <ConfigBadge
              icon={<DollarSign className="h-3 w-3" />}
              label="Budget"
              value={`$${config.budget_cap_usd.toFixed(2)}`}
            />
          )}
        </div>
      )}

      {/* Reasoning */}
      {config?.reasoning && (
        <p className="mt-2 text-xs text-muted-foreground leading-relaxed">
          {config.reasoning}
        </p>
      )}

      {/* Expanded: Provider Stats + History */}
      {expanded && (
        <div className="mt-4 space-y-4 border-t pt-3 border-gray-200 dark:border-gray-700">
          {/* Provider Stats */}
          {stats.length > 0 && (
            <div>
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-2">
                Provider Reliability
              </h4>
              <div className="space-y-2">
                {stats
                  .sort((a, b) => b.reliability_score - a.reliability_score)
                  .map((s) => (
                    <ProviderStatRow key={s.provider} stat={s} />
                  ))}
              </div>
            </div>
          )}

          {/* Preferred / Excluded Providers */}
          {config && (config.preferred_providers.length > 0 || config.excluded_providers.length > 0) && (
            <div>
              <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-2">
                Provider Preferences
              </h4>
              <div className="flex flex-wrap gap-1.5">
                {config.preferred_providers.map((p) => (
                  <span
                    key={`pref-${p}`}
                    className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium bg-emerald-100 dark:bg-emerald-900/40 text-emerald-600 dark:text-emerald-400"
                  >
                    <TrendingUp className="h-2.5 w-2.5" />
                    {p}
                  </span>
                ))}
                {config.excluded_providers.map((p) => (
                  <span
                    key={`excl-${p}`}
                    className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium bg-red-100 dark:bg-red-900/40 text-red-600 dark:text-red-400"
                  >
                    <XCircle className="h-2.5 w-2.5" />
                    {p}
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Recent History Toggle */}
          <div>
            <button
              onClick={() => setShowHistory(!showHistory)}
              className="flex items-center gap-1.5 text-xs font-semibold text-muted-foreground uppercase tracking-wide hover:text-foreground transition-colors"
            >
              <BarChart3 className="h-3.5 w-3.5" />
              Recent Probe History ({history.length})
              {showHistory ? (
                <ChevronUp className="h-3 w-3" />
              ) : (
                <ChevronDown className="h-3 w-3" />
              )}
            </button>

            {showHistory && history.length > 0 && (
              <div className="mt-2 space-y-1.5 max-h-64 overflow-y-auto">
                {history.map((h) => (
                  <HistoryRow key={h.id} entry={h} />
                ))}
              </div>
            )}
          </div>

          {/* Metadata */}
          {config && (
            <div className="flex items-center gap-3 text-[10px] text-muted-foreground pt-1">
              <span>
                Last tuned:{" "}
                {config.last_tuned_at
                  ? new Date(config.last_tuned_at).toLocaleString()
                  : "never"}
              </span>
              <span>
                Tokens spent: {config.tokens_spent.toLocaleString()}
              </span>
              <span>
                Retries: {config.max_retries}
              </span>
            </div>
          )}
        </div>
      )}

      {/* Error */}
      {error && <p className="mt-2 text-xs text-red-500">Error: {error}</p>}
    </div>
  );
}

// ── Sub-components ──────────────────────────────────────────────────────────

function StatPill({
  icon,
  label,
  value,
  color,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  color?: string;
}) {
  return (
    <div className="flex items-center gap-1.5 rounded-md bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 px-2 py-1 text-xs">
      <span className="text-muted-foreground">{icon}</span>
      <span className="text-muted-foreground">{label}:</span>
      <span className={`font-semibold ${color ?? ""}`}>{value}</span>
    </div>
  );
}

function ConfigBadge({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
}) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 px-2 py-0.5 text-[11px]">
      <span className="text-muted-foreground">{icon}</span>
      <span className="text-muted-foreground">{label}:</span>
      <span className="font-semibold">{value}</span>
    </span>
  );
}

function ProviderStatRow({ stat }: { stat: ProviderStatsData }) {
  const relColor = reliabilityColor(stat.reliability_score);
  const barWidth = Math.max(5, stat.reliability_score * 100);

  return (
    <div className="flex items-center gap-3 text-xs">
      <span className="w-20 font-medium truncate" title={stat.provider}>
        {stat.provider}
      </span>
      <div className="flex-1 h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${reliabilityBg(stat.reliability_score)}`}
          style={{ width: `${barWidth}%` }}
        />
      </div>
      <span className={`w-12 text-right font-semibold ${relColor}`}>
        {(stat.reliability_score * 100).toFixed(0)}%
      </span>
      <div className="flex items-center gap-2 text-muted-foreground">
        <span className="flex items-center gap-0.5" title="Win rate">
          <Trophy className="h-3 w-3" />
          {(stat.win_rate * 100).toFixed(0)}%
        </span>
        <span className="flex items-center gap-0.5" title="Avg latency">
          <Timer className="h-3 w-3" />
          {stat.avg_latency_ms.toFixed(0)}ms
        </span>
        <span title={`${stat.successes}/${stat.total_probes} probes`}>
          {stat.successes}/{stat.total_probes}
        </span>
      </div>
    </div>
  );
}

function HistoryRow({ entry }: { entry: ProbeHistoryData }) {
  return (
    <div
      className={`flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs ${
        entry.success
          ? "border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20"
          : "border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20"
      }`}
    >
      {entry.success ? (
        <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500 flex-shrink-0" />
      ) : (
        <XCircle className="h-3.5 w-3.5 text-red-500 flex-shrink-0" />
      )}
      <span className="font-medium truncate w-16" title={entry.provider_winner}>
        {entry.provider_winner || "—"}
      </span>
      <span className="text-muted-foreground">
        {entry.num_models_used}m
      </span>
      <span className="text-muted-foreground">
        {entry.total_ms}ms
      </span>
      {entry.consensus_confidence > 0 && (
        <span className="text-muted-foreground">
          conf: {(entry.consensus_confidence * 100).toFixed(0)}%
        </span>
      )}
      {entry.complexity_label && (
        <span className="rounded-full bg-gray-100 dark:bg-gray-800 px-1.5 py-0 text-[10px]">
          {entry.complexity_label}
        </span>
      )}
      <span className="ml-auto text-muted-foreground text-[10px]">
        {new Date(entry.created_at).toLocaleTimeString()}
      </span>
    </div>
  );
}
