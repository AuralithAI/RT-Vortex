// ─── Observability Dashboard ─────────────────────────────────────────────────
// Real-time system health, metrics time-series, provider performance,
// cost tracking, and budget management for the Agent Swarm.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Activity,
  Heart,
  DollarSign,
  Cpu,
  Zap,
  TrendingUp,
  BarChart3,
  RefreshCw,
  AlertTriangle,
  CheckCircle2,
  Clock,
  Loader2,
  Server,
  Shield,
} from "lucide-react";
import {
  LineChart,
  Line,
  BarChart,
  Bar,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
  Cell,
} from "recharts";
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
import type {
  ObservabilityDashboardData,
  MetricsSnapshotData,
  ProviderPerfData,
  HealthBreakdownData,
  CostSummaryData,
  CostBudgetData,
} from "@/types/swarm";

// ── Constants ────────────────────────────────────────────────────────────────

const REFRESH_INTERVAL = 30_000; // 30s auto-refresh
const CHART_COLORS = [
  "hsl(var(--chart-1))",
  "hsl(var(--chart-2))",
  "hsl(var(--chart-3))",
  "hsl(var(--chart-4))",
  "hsl(var(--chart-5))",
];

const PROVIDER_COLORS: Record<string, string> = {
  openai: "#10b981",
  anthropic: "#f59e0b",
  google: "#3b82f6",
  mistral: "#8b5cf6",
  deepseek: "#ef4444",
  cohere: "#06b6d4",
};

// ── Helpers ──────────────────────────────────────────────────────────────────

function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return `${h}h ${m}m`;
}

function healthColor(score: number): string {
  if (score >= 80) return "text-green-500";
  if (score >= 60) return "text-yellow-500";
  if (score >= 40) return "text-orange-500";
  return "text-red-500";
}

function healthBg(score: number): string {
  if (score >= 80) return "bg-green-100 dark:bg-green-900/30";
  if (score >= 60) return "bg-yellow-100 dark:bg-yellow-900/30";
  if (score >= 40) return "bg-orange-100 dark:bg-orange-900/30";
  return "bg-red-100 dark:bg-red-900/30";
}

function healthLabel(score: number): string {
  if (score >= 80) return "Healthy";
  if (score >= 60) return "Degraded";
  if (score >= 40) return "Warning";
  return "Critical";
}

// ── Main Component ───────────────────────────────────────────────────────────

export function ObservabilityDashboard() {
  const [dashboard, setDashboard] =
    useState<ObservabilityDashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [hours, setHours] = useState(24);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);

  const fetchDashboard = useCallback(async () => {
    try {
      const res = await fetch(
        `/api/v1/swarm/observability/dashboard?hours=${hours}`
      );
      if (res.ok) {
        const data: ObservabilityDashboardData = await res.json();
        setDashboard(data);
        setLastRefresh(new Date());
      }
    } catch {
      /* retry on next interval */
    } finally {
      setLoading(false);
    }
  }, [hours]);

  useEffect(() => {
    setLoading(true);
    fetchDashboard();
    const iv = setInterval(fetchDashboard, REFRESH_INTERVAL);
    return () => clearInterval(iv);
  }, [fetchDashboard]);

  if (loading && !dashboard) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-full" />
        <div className="grid gap-4 md:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-32" />
          ))}
        </div>
        <Skeleton className="h-[300px]" />
      </div>
    );
  }

  if (!dashboard) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center py-12 text-muted-foreground">
          <Activity className="mb-3 h-10 w-10 opacity-50" />
          <p>No observability data available yet.</p>
          <p className="text-sm">
            The system collects snapshots every 60 seconds.
          </p>
        </CardContent>
      </Card>
    );
  }

  const { current, time_series, provider_perf, health_breakdown, cost_summary, uptime_seconds } =
    dashboard;

  return (
    <div className="space-y-6">
      {/* Controls */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="text-sm text-muted-foreground">Time Range:</span>
          {[1, 6, 24, 72].map((h) => (
            <Button
              key={h}
              size="sm"
              variant={hours === h ? "default" : "outline"}
              onClick={() => setHours(h)}
            >
              {h}h
            </Button>
          ))}
        </div>
        <div className="flex items-center gap-3">
          {lastRefresh && (
            <span className="text-xs text-muted-foreground">
              Updated {lastRefresh.toLocaleTimeString()}
            </span>
          )}
          <Button size="sm" variant="outline" onClick={fetchDashboard}>
            <RefreshCw className="mr-1 h-3.5 w-3.5" />
            Refresh
          </Button>
        </div>
      </div>

      {/* Health Score + Top Metrics */}
      <div className="grid gap-4 md:grid-cols-4">
        <HealthScoreCard
          breakdown={health_breakdown}
          uptime={uptime_seconds}
        />
        <MetricCard
          icon={<Cpu className="h-5 w-5 text-blue-500" />}
          label="Active Tasks"
          value={current?.active_tasks ?? 0}
          subLabel="Pending"
          subValue={current?.pending_tasks ?? 0}
        />
        <MetricCard
          icon={<Server className="h-5 w-5 text-purple-500" />}
          label="Online Agents"
          value={current?.online_agents ?? 0}
          subLabel="Busy"
          subValue={current?.busy_agents ?? 0}
        />
        <MetricCard
          icon={<Zap className="h-5 w-5 text-amber-500" />}
          label="LLM Calls"
          value={current?.llm_calls ?? 0}
          subLabel="Avg Latency"
          subValue={`${(current?.llm_avg_latency_ms ?? 0).toFixed(0)}ms`}
        />
      </div>

      {/* Secondary stats row */}
      <div className="grid gap-4 md:grid-cols-5">
        <MiniStat label="Completed" value={current?.completed_tasks ?? 0} color="text-green-500" />
        <MiniStat label="Failed" value={current?.failed_tasks ?? 0} color="text-red-500" />
        <MiniStat label="LLM Tokens" value={current?.llm_tokens ?? 0} color="text-blue-500" />
        <MiniStat label="Consensus Runs" value={current?.consensus_runs ?? 0} color="text-purple-500" />
        <MiniStat
          label="Heal Events"
          value={current?.heal_events ?? 0}
          color="text-amber-500"
        />
      </div>

      {/* Time-Series Charts */}
      <div className="grid gap-6 lg:grid-cols-2">
        <TaskTimeSeries data={time_series} />
        <LLMTimeSeries data={time_series} />
      </div>

      {/* Provider Performance */}
      <ProviderPerformanceCard providers={provider_perf} />

      {/* Cost Tracking */}
      <CostTrackingCard costSummary={cost_summary} />

      {/* Health Breakdown Detail */}
      {health_breakdown && (
        <HealthBreakdownCard breakdown={health_breakdown} />
      )}
    </div>
  );
}

// ── Sub Components ───────────────────────────────────────────────────────────

function HealthScoreCard({
  breakdown,
  uptime,
}: {
  breakdown: HealthBreakdownData | null;
  uptime: number;
}) {
  const score = breakdown?.score ?? 0;

  return (
    <Card className={healthBg(score)}>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          <Heart className={`h-5 w-5 ${healthColor(score)}`} />
          System Health
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex items-baseline gap-2">
          <span className={`text-4xl font-bold ${healthColor(score)}`}>
            {score}
          </span>
          <span className="text-lg text-muted-foreground">/ 100</span>
        </div>
        <Badge
          variant={score >= 80 ? "default" : score >= 60 ? "secondary" : "destructive"}
          className="mt-2"
        >
          {healthLabel(score)}
        </Badge>
        <p className="mt-2 text-xs text-muted-foreground">
          Uptime: {formatDuration(uptime)}
        </p>
      </CardContent>
    </Card>
  );
}

function MetricCard({
  icon,
  label,
  value,
  subLabel,
  subValue,
}: {
  icon: React.ReactNode;
  label: string;
  value: number | string;
  subLabel: string;
  subValue: number | string;
}) {
  return (
    <Card>
      <CardContent className="pt-6">
        <div className="flex items-center gap-3">
          {icon}
          <div>
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className="text-2xl font-bold">{value}</p>
          </div>
        </div>
        <div className="mt-3 flex items-center gap-1 text-xs text-muted-foreground">
          <span>{subLabel}:</span>
          <span className="font-medium text-foreground">{subValue}</span>
        </div>
      </CardContent>
    </Card>
  );
}

function MiniStat({
  label,
  value,
  color,
}: {
  label: string;
  value: number | string;
  color: string;
}) {
  return (
    <div className="flex items-center gap-2 rounded-lg border bg-card px-4 py-3">
      <div className={`h-2 w-2 rounded-full ${color.replace("text-", "bg-")}`} />
      <div>
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className="text-lg font-semibold">{value}</p>
      </div>
    </div>
  );
}

function TaskTimeSeries({ data }: { data: MetricsSnapshotData[] }) {
  const chartData = data.map((d) => ({
    time: formatTime(d.created_at),
    active: d.active_tasks,
    pending: d.pending_tasks,
    completed: d.completed_tasks,
    failed: d.failed_tasks,
  }));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          <BarChart3 className="mr-2 inline h-4 w-4" />
          Task Activity
        </CardTitle>
        <CardDescription>Active, pending, completed, and failed tasks over time</CardDescription>
      </CardHeader>
      <CardContent>
        {chartData.length > 0 ? (
          <ResponsiveContainer width="100%" height={250}>
            <AreaChart data={chartData}>
              <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
              <XAxis dataKey="time" fontSize={11} />
              <YAxis width={35} fontSize={11} />
              <Tooltip
                contentStyle={{
                  backgroundColor: "hsl(var(--popover))",
                  border: "1px solid hsl(var(--border))",
                  borderRadius: "var(--radius)",
                  fontSize: 12,
                }}
              />
              <Legend wrapperStyle={{ fontSize: 11 }} />
              <Area
                type="monotone"
                dataKey="active"
                stroke={CHART_COLORS[0]}
                fill={CHART_COLORS[0]}
                fillOpacity={0.15}
                strokeWidth={2}
              />
              <Area
                type="monotone"
                dataKey="pending"
                stroke={CHART_COLORS[1]}
                fill={CHART_COLORS[1]}
                fillOpacity={0.1}
                strokeWidth={2}
              />
              <Area
                type="monotone"
                dataKey="completed"
                stroke={CHART_COLORS[2]}
                fill={CHART_COLORS[2]}
                fillOpacity={0.1}
                strokeWidth={1}
              />
              <Area
                type="monotone"
                dataKey="failed"
                stroke={CHART_COLORS[4]}
                fill={CHART_COLORS[4]}
                fillOpacity={0.1}
                strokeWidth={1}
              />
            </AreaChart>
          </ResponsiveContainer>
        ) : (
          <div className="flex h-[250px] items-center justify-center text-muted-foreground">
            Waiting for time-series data…
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function LLMTimeSeries({ data }: { data: MetricsSnapshotData[] }) {
  const chartData = data.map((d) => ({
    time: formatTime(d.created_at),
    calls: d.llm_calls,
    tokens: Math.round(d.llm_tokens / 1000), // k tokens
    latency: Math.round(d.llm_avg_latency_ms),
    errorRate: +(d.llm_error_rate * 100).toFixed(1),
  }));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          <Zap className="mr-2 inline h-4 w-4" />
          LLM Performance
        </CardTitle>
        <CardDescription>Calls, latency, and error rate over time</CardDescription>
      </CardHeader>
      <CardContent>
        {chartData.length > 0 ? (
          <ResponsiveContainer width="100%" height={250}>
            <LineChart data={chartData}>
              <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
              <XAxis dataKey="time" fontSize={11} />
              <YAxis yAxisId="left" width={35} fontSize={11} />
              <YAxis
                yAxisId="right"
                orientation="right"
                width={40}
                fontSize={11}
                tickFormatter={(v: number) => `${v}ms`}
              />
              <Tooltip
                contentStyle={{
                  backgroundColor: "hsl(var(--popover))",
                  border: "1px solid hsl(var(--border))",
                  borderRadius: "var(--radius)",
                  fontSize: 12,
                }}
              />
              <Legend wrapperStyle={{ fontSize: 11 }} />
              <Line
                yAxisId="left"
                type="monotone"
                dataKey="calls"
                stroke={CHART_COLORS[0]}
                strokeWidth={2}
                dot={false}
                name="LLM Calls"
              />
              <Line
                yAxisId="left"
                type="monotone"
                dataKey="tokens"
                stroke={CHART_COLORS[2]}
                strokeWidth={2}
                dot={false}
                name="Tokens (K)"
              />
              <Line
                yAxisId="right"
                type="monotone"
                dataKey="latency"
                stroke={CHART_COLORS[3]}
                strokeWidth={2}
                dot={false}
                name="Latency (ms)"
              />
            </LineChart>
          </ResponsiveContainer>
        ) : (
          <div className="flex h-[250px] items-center justify-center text-muted-foreground">
            Waiting for LLM data…
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function ProviderPerformanceCard({
  providers,
}: {
  providers: ProviderPerfData[];
}) {
  if (providers.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            <Server className="mr-2 inline h-4 w-4" />
            Provider Performance
          </CardTitle>
        </CardHeader>
        <CardContent className="flex h-20 items-center justify-center text-muted-foreground">
          No provider performance data yet.
        </CardContent>
      </Card>
    );
  }

  // Bar chart data
  const barData = providers.map((p) => ({
    provider: p.provider,
    calls: p.calls,
    errorRate: +(p.error_rate * 100).toFixed(1),
    avgLatency: Math.round(p.avg_latency_ms),
    p95Latency: Math.round(p.p95_latency_ms),
    cost: +p.estimated_cost_usd.toFixed(4),
    winRate:
      p.consensus_total > 0
        ? +((p.consensus_wins / p.consensus_total) * 100).toFixed(1)
        : 0,
  }));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          <Server className="mr-2 inline h-4 w-4" />
          Provider Performance
        </CardTitle>
        <CardDescription>
          Comparison of LLM providers by calls, latency, and cost
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-6 lg:grid-cols-2">
          {/* Calls bar chart */}
          <div>
            <h4 className="mb-2 text-xs font-medium text-muted-foreground">
              Calls &amp; Latency
            </h4>
            <ResponsiveContainer width="100%" height={200}>
              <BarChart data={barData}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                <XAxis dataKey="provider" fontSize={11} />
                <YAxis width={40} fontSize={11} />
                <Tooltip
                  contentStyle={{
                    backgroundColor: "hsl(var(--popover))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: "var(--radius)",
                    fontSize: 12,
                  }}
                />
                <Bar dataKey="calls" name="Calls" radius={[4, 4, 0, 0]}>
                  {barData.map((entry, i) => (
                    <Cell
                      key={entry.provider}
                      fill={
                        PROVIDER_COLORS[entry.provider] ||
                        CHART_COLORS[i % CHART_COLORS.length]
                      }
                    />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>

          {/* Provider detail table */}
          <div className="overflow-x-auto">
            <h4 className="mb-2 text-xs font-medium text-muted-foreground">
              Detail
            </h4>
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b text-left text-muted-foreground">
                  <th className="px-2 py-1.5 font-medium">Provider</th>
                  <th className="px-2 py-1.5 font-medium text-right">Calls</th>
                  <th className="px-2 py-1.5 font-medium text-right">
                    Err %
                  </th>
                  <th className="px-2 py-1.5 font-medium text-right">
                    Avg ms
                  </th>
                  <th className="px-2 py-1.5 font-medium text-right">
                    P95 ms
                  </th>
                  <th className="px-2 py-1.5 font-medium text-right">
                    Win %
                  </th>
                  <th className="px-2 py-1.5 font-medium text-right">
                    Cost
                  </th>
                </tr>
              </thead>
              <tbody>
                {barData.map((p) => (
                  <tr
                    key={p.provider}
                    className="border-b transition-colors hover:bg-muted/50"
                  >
                    <td className="px-2 py-1.5 font-medium capitalize">
                      <span
                        className="mr-1.5 inline-block h-2 w-2 rounded-full"
                        style={{
                          backgroundColor:
                            PROVIDER_COLORS[p.provider] || "#6b7280",
                        }}
                      />
                      {p.provider}
                    </td>
                    <td className="px-2 py-1.5 text-right">{p.calls}</td>
                    <td className="px-2 py-1.5 text-right">
                      <span
                        className={
                          p.errorRate > 10
                            ? "text-red-500"
                            : p.errorRate > 5
                              ? "text-yellow-500"
                              : ""
                        }
                      >
                        {p.errorRate}%
                      </span>
                    </td>
                    <td className="px-2 py-1.5 text-right">{p.avgLatency}</td>
                    <td className="px-2 py-1.5 text-right">{p.p95Latency}</td>
                    <td className="px-2 py-1.5 text-right">{p.winRate}%</td>
                    <td className="px-2 py-1.5 text-right">
                      ${p.cost.toFixed(4)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function CostTrackingCard({
  costSummary,
}: {
  costSummary: CostSummaryData | null;
}) {
  if (!costSummary) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            <DollarSign className="mr-2 inline h-4 w-4" />
            Cost Tracking
          </CardTitle>
        </CardHeader>
        <CardContent className="flex h-20 items-center justify-center text-muted-foreground">
          No cost data available yet.
        </CardContent>
      </Card>
    );
  }

  const byProvider = Object.entries(costSummary.by_provider || {}).map(
    ([name, cost]) => ({
      provider: name,
      cost: +cost.toFixed(4),
    })
  );

  const budget = costSummary.budget;
  const budgetUsedPct = budget
    ? Math.min(100, (budget.spent_usd / budget.budget_usd) * 100)
    : null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          <DollarSign className="mr-2 inline h-4 w-4" />
          Cost Tracking
        </CardTitle>
        <CardDescription>
          Estimated costs from LLM API token usage
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-6 lg:grid-cols-3">
          {/* Cost summary */}
          <div className="space-y-3">
            <CostRow label="Today" value={costSummary.today_usd} />
            <CostRow label="This Week" value={costSummary.this_week_usd} />
            <CostRow label="This Month" value={costSummary.this_month_usd} />
            {budget && budgetUsedPct !== null && (
              <div className="mt-4 space-y-1">
                <div className="flex items-center justify-between text-xs">
                  <span className="text-muted-foreground">
                    Monthly Budget
                  </span>
                  <span className="font-medium">
                    ${budget.spent_usd.toFixed(2)} / ${budget.budget_usd.toFixed(2)}
                  </span>
                </div>
                <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                  <div
                    className={`h-full rounded-full transition-all ${
                      budgetUsedPct >= 90
                        ? "bg-red-500"
                        : budgetUsedPct >= budget.alert_threshold * 100
                          ? "bg-yellow-500"
                          : "bg-green-500"
                    }`}
                    style={{ width: `${budgetUsedPct}%` }}
                  />
                </div>
              </div>
            )}
          </div>

          {/* By provider bar chart */}
          <div className="lg:col-span-2">
            {byProvider.length > 0 ? (
              <ResponsiveContainer width="100%" height={180}>
                <BarChart data={byProvider} layout="vertical">
                  <CartesianGrid
                    strokeDasharray="3 3"
                    className="stroke-muted"
                  />
                  <XAxis
                    type="number"
                    fontSize={11}
                    tickFormatter={(v: number) => `$${v}`}
                  />
                  <YAxis
                    dataKey="provider"
                    type="category"
                    width={80}
                    fontSize={11}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "hsl(var(--popover))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: "var(--radius)",
                      fontSize: 12,
                    }}
                    formatter={(value: number) => [`$${value.toFixed(4)}`, "Cost"]}
                  />
                  <Bar
                    dataKey="cost"
                    name="Cost (USD)"
                    radius={[0, 4, 4, 0]}
                  >
                    {byProvider.map((entry, i) => (
                      <Cell
                        key={entry.provider}
                        fill={
                          PROVIDER_COLORS[entry.provider] ||
                          CHART_COLORS[i % CHART_COLORS.length]
                        }
                      />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <div className="flex h-[180px] items-center justify-center text-muted-foreground">
                No per-provider cost data yet.
              </div>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function CostRow({ label, value }: { label: string; value: number }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="font-mono text-sm font-semibold">
        ${value.toFixed(4)}
      </span>
    </div>
  );
}

function HealthBreakdownCard({
  breakdown,
}: {
  breakdown: HealthBreakdownData;
}) {
  const dimensions = [
    {
      label: "Task Success Rate",
      value: breakdown.task_health_pct,
      max: 20,
      icon: <CheckCircle2 className="h-4 w-4" />,
    },
    {
      label: "Agent Availability",
      value: breakdown.agent_health_pct,
      max: 20,
      icon: <Cpu className="h-4 w-4" />,
    },
    {
      label: "Provider Circuits",
      value: breakdown.provider_health_pct,
      max: 20,
      icon: <Shield className="h-4 w-4" />,
    },
    {
      label: "Queue Depth",
      value: breakdown.queue_health_pct,
      max: 20,
      icon: <TrendingUp className="h-4 w-4" />,
    },
    {
      label: "LLM Error Rate",
      value: breakdown.error_rate_pct,
      max: 20,
      icon: <AlertTriangle className="h-4 w-4" />,
    },
  ];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          <Heart className="mr-2 inline h-4 w-4" />
          Health Breakdown
        </CardTitle>
        <CardDescription>
          Five dimensions × 20 points each = composite health score
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {dimensions.map((dim) => {
            const pct = (dim.value / dim.max) * 100;
            return (
              <div key={dim.label} className="space-y-1">
                <div className="flex items-center justify-between text-xs">
                  <span className="flex items-center gap-1.5 text-muted-foreground">
                    {dim.icon}
                    {dim.label}
                  </span>
                  <span className="font-mono font-medium">
                    {dim.value.toFixed(1)} / {dim.max}
                  </span>
                </div>
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
                  <div
                    className={`h-full rounded-full transition-all ${
                      pct >= 75
                        ? "bg-green-500"
                        : pct >= 50
                          ? "bg-yellow-500"
                          : "bg-red-500"
                    }`}
                    style={{ width: `${pct}%` }}
                  />
                </div>
              </div>
            );
          })}
        </div>
        {breakdown.details && (
          <p className="mt-4 text-xs text-muted-foreground">
            {breakdown.details}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
