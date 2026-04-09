// ─── CI Signal Badge ─────────────────────────────────────────────────────────
// Displays the CI / PR merge status for a swarm task. Fetches from
// GET /api/v1/swarm/tasks/{id}/ci-signal and shows a compact badge with
// PR merge state, CI check pass/fail counts, and an expandable detail view.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import {
  CheckCircle2,
  XCircle,
  Clock,
  GitMerge,
  GitPullRequest,
  AlertTriangle,
  Activity,
  ChevronDown,
  ChevronUp,
  ExternalLink,
  RefreshCw,
  Loader2,
  Shield,
  Zap,
} from "lucide-react";
import type { CISignalData, CIState, PRState } from "@/types/swarm";

interface CISignalBadgeProps {
  taskId: string;
  /** Compact mode — just shows icons. */
  compact?: boolean;
  /** Auto-refresh interval in ms (0 = no auto-refresh). */
  refreshInterval?: number;
}

// ── State → visual mapping ──────────────────────────────────────────────────

function prStateConfig(state: PRState): {
  label: string;
  color: string;
  bgColor: string;
  icon: React.ReactNode;
} {
  switch (state) {
    case "merged":
      return {
        label: "Merged",
        color: "text-purple-600 dark:text-purple-400",
        bgColor: "bg-purple-100 dark:bg-purple-900/50",
        icon: <GitMerge className="h-3.5 w-3.5" />,
      };
    case "closed":
      return {
        label: "Closed",
        color: "text-red-600 dark:text-red-400",
        bgColor: "bg-red-100 dark:bg-red-900/50",
        icon: <XCircle className="h-3.5 w-3.5" />,
      };
    case "open":
      return {
        label: "Open",
        color: "text-green-600 dark:text-green-400",
        bgColor: "bg-green-100 dark:bg-green-900/50",
        icon: <GitPullRequest className="h-3.5 w-3.5" />,
      };
    default:
      return {
        label: "Unknown",
        color: "text-muted-foreground",
        bgColor: "bg-muted",
        icon: <Clock className="h-3.5 w-3.5" />,
      };
  }
}

function ciStateConfig(state: CIState): {
  label: string;
  color: string;
  bgColor: string;
  icon: React.ReactNode;
} {
  switch (state) {
    case "success":
      return {
        label: "Passing",
        color: "text-green-600 dark:text-green-400",
        bgColor: "bg-green-100 dark:bg-green-900/50",
        icon: <CheckCircle2 className="h-3.5 w-3.5" />,
      };
    case "failure":
      return {
        label: "Failing",
        color: "text-red-600 dark:text-red-400",
        bgColor: "bg-red-100 dark:bg-red-900/50",
        icon: <XCircle className="h-3.5 w-3.5" />,
      };
    case "error":
      return {
        label: "Error",
        color: "text-orange-600 dark:text-orange-400",
        bgColor: "bg-orange-100 dark:bg-orange-900/50",
        icon: <AlertTriangle className="h-3.5 w-3.5" />,
      };
    case "pending":
      return {
        label: "Pending",
        color: "text-yellow-600 dark:text-yellow-400",
        bgColor: "bg-yellow-100 dark:bg-yellow-900/50",
        icon: <Clock className="h-3.5 w-3.5 animate-pulse" />,
      };
    default:
      return {
        label: "Unknown",
        color: "text-muted-foreground",
        bgColor: "bg-muted",
        icon: <Activity className="h-3.5 w-3.5" />,
      };
  }
}

// ── Component ───────────────────────────────────────────────────────────────

export function CISignalBadge({
  taskId,
  compact = false,
  refreshInterval = 30000,
}: CISignalBadgeProps) {
  const [signal, setSignal] = useState<CISignalData | null>(null);
  const [exists, setExists] = useState(true);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const fetchSignal = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true);
      else setRefreshing(true);
      try {
        const res = await fetch(`/api/v1/swarm/tasks/${taskId}/ci-signal`);
        if (res.ok) {
          const data = await res.json();
          if (data.exists === false) {
            setExists(false);
            setSignal(null);
          } else {
            setExists(true);
            setSignal(data);
          }
        }
      } catch {
        // Silently fail
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [taskId]
  );

  useEffect(() => {
    fetchSignal();
  }, [fetchSignal]);

  // Auto-refresh if signal is not finalized.
  useEffect(() => {
    if (!refreshInterval || !signal || signal.finalized) return;
    const timer = setInterval(() => fetchSignal(true), refreshInterval);
    return () => clearInterval(timer);
  }, [refreshInterval, signal, fetchSignal]);

  if (loading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span>Loading CI status…</span>
      </div>
    );
  }

  if (!exists || !signal) {
    return null; // No CI signal for this task (no PR was created).
  }

  const prCfg = prStateConfig(signal.pr_state as PRState);
  const ciCfg = ciStateConfig(signal.ci_state as CIState);

  if (compact) {
    return (
      <div className="flex items-center gap-2">
        <span
          className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${prCfg.bgColor} ${prCfg.color}`}
          title={`PR: ${prCfg.label}`}
        >
          {prCfg.icon}
          PR
        </span>
        <span
          className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${ciCfg.bgColor} ${ciCfg.color}`}
          title={`CI: ${ciCfg.label} (${signal.ci_passed}/${signal.ci_total} passed)`}
        >
          {ciCfg.icon}
          CI
        </span>
      </div>
    );
  }

  return (
    <div className="rounded-lg border bg-card">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-4 py-3">
        <div className="flex items-center gap-2">
          <Zap className="h-4 w-4 text-blue-500" />
          <h4 className="text-sm font-semibold">CI Signal</h4>
          {!signal.finalized && (
            <span className="flex items-center gap-1 text-xs text-muted-foreground">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-yellow-500" />
              Polling
            </span>
          )}
          {signal.elo_ingested && (
            <span className="inline-flex items-center gap-1 rounded-full bg-blue-100 px-2 py-0.5 text-xs font-medium text-blue-600 dark:bg-blue-900/50 dark:text-blue-400">
              <Shield className="h-3 w-3" />
              ELO Updated
            </span>
          )}
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={() => fetchSignal(true)}
            className="rounded p-1 hover:bg-muted"
            disabled={refreshing}
            title="Refresh"
          >
            <RefreshCw
              className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`}
            />
          </button>
          <button
            onClick={() => setExpanded(!expanded)}
            className="rounded p-1 hover:bg-muted"
          >
            {expanded ? (
              <ChevronUp className="h-3.5 w-3.5" />
            ) : (
              <ChevronDown className="h-3.5 w-3.5" />
            )}
          </button>
        </div>
      </div>

      {/* Status badges */}
      <div className="flex items-center gap-3 px-4 py-3">
        {/* PR state */}
        <div
          className={`inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium ${prCfg.bgColor} ${prCfg.color}`}
        >
          {prCfg.icon}
          PR: {prCfg.label}
        </div>

        {/* CI state */}
        <div
          className={`inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium ${ciCfg.bgColor} ${ciCfg.color}`}
        >
          {ciCfg.icon}
          CI: {ciCfg.label}
        </div>

        {/* Check counts */}
        {signal.ci_total > 0 && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span className="text-green-600 dark:text-green-400">
              ✓ {signal.ci_passed}
            </span>
            {signal.ci_failed > 0 && (
              <span className="text-red-600 dark:text-red-400">
                ✗ {signal.ci_failed}
              </span>
            )}
            {signal.ci_pending > 0 && (
              <span className="text-yellow-600 dark:text-yellow-400">
                ◷ {signal.ci_pending}
              </span>
            )}
            <span>/ {signal.ci_total}</span>
          </div>
        )}
      </div>

      {/* Expanded details */}
      {expanded && (
        <div className="border-t px-4 py-3">
          <div className="space-y-3">
            {/* Metadata */}
            <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
              <dt className="text-muted-foreground">Polls</dt>
              <dd>{signal.poll_count}</dd>
              <dt className="text-muted-foreground">Last Polled</dt>
              <dd>
                {signal.last_polled_at
                  ? new Date(signal.last_polled_at).toLocaleString()
                  : "—"}
              </dd>
              <dt className="text-muted-foreground">Finalized</dt>
              <dd>
                {signal.finalized
                  ? signal.finalized_at
                    ? new Date(signal.finalized_at).toLocaleString()
                    : "Yes"
                  : "No (still polling)"}
              </dd>
              <dt className="text-muted-foreground">ELO Ingested</dt>
              <dd>
                {signal.elo_ingested
                  ? signal.elo_ingested_at
                    ? new Date(signal.elo_ingested_at).toLocaleString()
                    : "Yes"
                  : "Not yet"}
              </dd>
            </dl>

            {/* Individual checks */}
            {signal.ci_details && signal.ci_details.length > 0 && (
              <div className="space-y-1">
                <h5 className="text-xs font-medium text-muted-foreground">
                  CI Checks ({signal.ci_details.length})
                </h5>
                <div className="max-h-48 space-y-1 overflow-y-auto">
                  {signal.ci_details.map((check, i) => {
                    const checkCfg = ciStateConfig(check.state as CIState);
                    return (
                      <div
                        key={`${check.context}-${i}`}
                        className="flex items-center justify-between rounded-md border px-2 py-1.5 text-xs"
                      >
                        <div className="flex items-center gap-1.5">
                          {checkCfg.icon}
                          <span className="font-medium">{check.context}</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <span className={checkCfg.color}>
                            {checkCfg.label}
                          </span>
                          {check.target_url && (
                            <a
                              href={check.target_url}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="text-blue-500 hover:underline"
                            >
                              <ExternalLink className="h-3 w-3" />
                            </a>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
