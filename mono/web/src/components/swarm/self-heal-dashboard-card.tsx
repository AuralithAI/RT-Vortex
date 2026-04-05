// ─── Self-Healing Pipeline Dashboard Card ────────────────────────────────────
// Displays circuit-breaker status for LLM providers and a scrollable log of
// self-heal events (stuck-task recovery, circuit transitions, failovers).
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import {
  ShieldCheck,
  ShieldAlert,
  ShieldOff,
  Activity,
  RefreshCw,
  CheckCircle2,
  AlertTriangle,
  XCircle,
  RotateCcw,
  Loader2,
} from "lucide-react";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type {
  SelfHealSummaryData,
  ProviderCircuitData,
  SelfHealEventData,
  CircuitState,
  SelfHealSeverity,
} from "@/types/swarm";

// ── Helpers ──────────────────────────────────────────────────────────────────

function circuitIcon(state: CircuitState) {
  switch (state) {
    case "closed":
      return <ShieldCheck className="h-4 w-4 text-green-500" />;
    case "half_open":
      return <ShieldAlert className="h-4 w-4 text-yellow-500" />;
    case "open":
      return <ShieldOff className="h-4 w-4 text-red-500" />;
  }
}

function circuitBadge(state: CircuitState) {
  const v: Record<CircuitState, "default" | "secondary" | "destructive"> = {
    closed: "default",
    half_open: "secondary",
    open: "destructive",
  };
  const labels: Record<CircuitState, string> = {
    closed: "Healthy",
    half_open: "Recovering",
    open: "Open",
  };
  return <Badge variant={v[state]}>{labels[state]}</Badge>;
}

function severityIcon(sev: SelfHealSeverity) {
  switch (sev) {
    case "info":
      return <CheckCircle2 className="h-3.5 w-3.5 text-blue-500" />;
    case "warn":
      return <AlertTriangle className="h-3.5 w-3.5 text-yellow-500" />;
    case "critical":
      return <XCircle className="h-3.5 w-3.5 text-red-500" />;
  }
}

function eventLabel(type: string): string {
  const map: Record<string, string> = {
    circuit_opened: "Circuit Opened",
    circuit_closed: "Circuit Closed",
    circuit_half_open: "Circuit Half-Open",
    task_retry: "Task Retry",
    task_timeout_recovery: "Timeout Recovery",
    provider_failover: "Provider Failover",
    agent_restarted: "Agent Restarted",
    stuck_task_detected: "Stuck Task",
  };
  return map[type] ?? type;
}

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

// ── Component ────────────────────────────────────────────────────────────────

export function SelfHealDashboardCard() {
  const [data, setData] = useState<SelfHealSummaryData | null>(null);
  const [loading, setLoading] = useState(false);
  const [resettingProvider, setResettingProvider] = useState<string | null>(null);

  const fetchSummary = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetch("/api/v1/swarm/self-heal/summary?limit=15");
      if (res.ok) {
        setData(await res.json());
      }
    } catch {
      /* swallow */
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchSummary();
    const iv = setInterval(fetchSummary, 30_000);
    return () => clearInterval(iv);
  }, [fetchSummary]);

  const resetCircuit = async (provider: string) => {
    setResettingProvider(provider);
    try {
      await fetch(`/api/v1/swarm/self-heal/circuits/${encodeURIComponent(provider)}/reset`, {
        method: "POST",
      });
      await fetchSummary();
    } catch {
      /* swallow */
    } finally {
      setResettingProvider(null);
    }
  };

  const resolveEvent = async (eventId: string) => {
    try {
      await fetch(`/api/v1/swarm/self-heal/events/${eventId}/resolve`, {
        method: "POST",
      });
      await fetchSummary();
    } catch {
      /* swallow */
    }
  };

  // ── Loading skeleton ──────────────────────────────────────────────────

  if (!data) {
    return (
      <Card>
        <CardHeader className="pb-3">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-4 w-64 mt-1" />
        </CardHeader>
        <CardContent className="space-y-3">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
        </CardContent>
      </Card>
    );
  }

  const { providers, recent_events, open_circuits, half_open_circuits, unresolved_events } = data;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Activity className="h-5 w-5 text-muted-foreground" />
            <CardTitle className="text-base">Self-Healing Pipeline</CardTitle>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={fetchSummary}
            disabled={loading}
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>
        <CardDescription>
          Circuit breakers &amp; automatic recovery ·{" "}
          <span className={open_circuits > 0 ? "text-red-500 font-medium" : ""}>
            {open_circuits} open
          </span>{" "}
          · {half_open_circuits} recovering · {unresolved_events} unresolved
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-4">
        {/* ── Provider Circuit Breakers ──────────────────────────────── */}
        {providers.length > 0 && (
          <div className="space-y-2">
            <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
              Provider Health
            </p>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
              {providers.map((p: ProviderCircuitData) => (
                <div
                  key={p.provider}
                  className="flex items-center justify-between rounded-lg border p-2.5 text-sm"
                >
                  <div className="flex items-center gap-2">
                    {circuitIcon(p.state)}
                    <span className="font-medium capitalize">{p.provider}</span>
                    {circuitBadge(p.state)}
                  </div>
                  <div className="flex items-center gap-2">
                    <TooltipProvider>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="text-xs text-muted-foreground tabular-nums">
                            {p.total_successes}✓ {p.total_failures}✗
                          </span>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>Successes: {p.total_successes}</p>
                          <p>Failures: {p.total_failures}</p>
                          <p>Consecutive fails: {p.consecutive_failures}</p>
                          {p.open_until && (
                            <p>Open until: {new Date(p.open_until).toLocaleTimeString()}</p>
                          )}
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                    {p.state !== "closed" && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-6 w-6"
                        onClick={() => resetCircuit(p.provider)}
                        disabled={resettingProvider === p.provider}
                      >
                        {resettingProvider === p.provider ? (
                          <Loader2 className="h-3 w-3 animate-spin" />
                        ) : (
                          <RotateCcw className="h-3 w-3" />
                        )}
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* ── Recent Events ─────────────────────────────────────────── */}
        {recent_events && recent_events.length > 0 && (
          <div className="space-y-2">
            <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
              Recent Events
            </p>
            <div className="max-h-52 overflow-y-auto space-y-1.5 pr-1">
              {recent_events.map((ev: SelfHealEventData) => (
                <div
                  key={ev.id}
                  className="flex items-start gap-2 rounded border p-2 text-xs"
                >
                  {severityIcon(ev.severity)}
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5">
                      <span className="font-medium">{eventLabel(ev.event_type)}</span>
                      {ev.provider && (
                        <Badge variant="outline" className="text-[10px] px-1 py-0">
                          {ev.provider}
                        </Badge>
                      )}
                      <span className="text-muted-foreground ml-auto flex-shrink-0">
                        {relativeTime(ev.created_at)}
                      </span>
                    </div>
                    {ev.task_id && (
                      <span className="text-muted-foreground truncate block">
                        Task: {ev.task_id.slice(0, 8)}…
                      </span>
                    )}
                  </div>
                  {!ev.resolved && (
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-5 w-5 flex-shrink-0"
                      onClick={() => resolveEvent(ev.id)}
                      title="Mark resolved"
                    >
                      <CheckCircle2 className="h-3 w-3 text-green-500" />
                    </Button>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* ── Empty State ───────────────────────────────────────────── */}
        {providers.length === 0 && (!recent_events || recent_events.length === 0) && (
          <div className="text-center py-6 text-sm text-muted-foreground">
            <ShieldCheck className="h-8 w-8 mx-auto mb-2 text-green-500" />
            <p>All systems healthy — no self-heal events recorded yet.</p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
