// ─── Agent Benchmark Dashboard ───────────────────────────────────────────────
// /dashboard/benchmarks
// Shows benchmark results, ELO ratings, A/B comparison, and quality metrics.
// Fetches from /api/v1/benchmark/* endpoints.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Activity,
  BarChart3,
  Bot,
  CheckCircle2,
  Clock,
  Download,
  Flame,
  Loader2,
  Play,
  RefreshCw,
  Swords,
  Target,
  Timer,
  Trophy,
  XCircle,
  Zap,
} from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  BarChart,
  Bar,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  RadarChart,
  PolarGrid,
  PolarAngleAxis,
  PolarRadiusAxis,
  Radar,
  Legend,
} from "recharts";
import { getApiBaseUrl } from "@/lib/utils";

// ── Types ───────────────────────────────────────────────────────────────────

interface QualityScore {
  correctness: number;
  precision: number;
  recall: number;
  speed: number;
  efficiency: number;
  human_quality: number;
  composite: number;
}

interface RunResult {
  task_id: string;
  task_name: string;
  mode: string;
  status: string;
  latency_ms: number;
  llm_calls: number;
  tokens_used: number;
  score?: QualityScore;
  error?: string;
  trace?: string[];
}

interface ModeStats {
  mode: string;
  total_tasks: number;
  passed: number;
  failed: number;
  avg_latency_ms: number;
  avg_llm_calls: number;
  avg_score: number;
}

interface RunSummary {
  total_tasks: number;
  passed: number;
  failed: number;
  avg_latency_ms: number;
  avg_llm_calls: number;
  avg_tokens_used: number;
  avg_score: number;
  p50_latency_ms: number;
  p90_latency_ms: number;
  p99_latency_ms: number;
  by_mode?: Record<string, ModeStats>;
}

interface BenchmarkRun {
  id: string;
  name: string;
  mode: string;
  status: string;
  started_at: string;
  ended_at?: string;
  results: RunResult[];
  summary?: RunSummary;
}

interface ELORating {
  mode: string;
  rating: number;
  wins: number;
  losses: number;
  draws: number;
  last_update: string;
}

interface BenchmarkSummary {
  latest_run?: BenchmarkRun;
  ratings: Record<string, ELORating>;
  total_runs: number;
  total_tasks: number;
}

// ── API helpers ─────────────────────────────────────────────────────────────

const API_BASE = getApiBaseUrl();

async function fetchBenchmarkSummary(): Promise<BenchmarkSummary> {
  const res = await fetch(`${API_BASE}/api/v1/benchmark/summary`, {
    credentials: "include",
  });
  if (!res.ok) throw new Error("Failed to fetch benchmark summary");
  return res.json();
}

async function fetchBenchmarkRuns(): Promise<BenchmarkRun[]> {
  const res = await fetch(`${API_BASE}/api/v1/benchmark/runs`, {
    credentials: "include",
  });
  if (!res.ok) throw new Error("Failed to fetch benchmark runs");
  return res.json();
}

async function startBenchmarkRun(
  name: string,
  mode: string
): Promise<BenchmarkRun> {
  const res = await fetch(`${API_BASE}/api/v1/benchmark/runs`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, mode }),
  });
  if (!res.ok) throw new Error("Failed to start benchmark run");
  return res.json();
}

async function downloadReport(runId: string): Promise<void> {
  const res = await fetch(
    `${API_BASE}/api/v1/benchmark/runs/${runId}/report`,
    { credentials: "include" }
  );
  if (!res.ok) throw new Error("Failed to download report");
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `benchmark-report-${runId.slice(0, 8)}.json`;
  a.click();
  URL.revokeObjectURL(url);
}

// ── Components ──────────────────────────────────────────────────────────────

function StatsCard({
  title,
  value,
  subtitle,
  icon: Icon,
  trend,
}: {
  title: string;
  value: string;
  subtitle?: string;
  icon: React.ElementType;
  trend?: "up" | "down" | "neutral";
}) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {title}
        </CardTitle>
        <Icon className="h-4 w-4 text-muted-foreground" />
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold">{value}</div>
        {subtitle && (
          <p
            className={`text-xs mt-1 ${
              trend === "up"
                ? "text-green-500"
                : trend === "down"
                  ? "text-red-500"
                  : "text-muted-foreground"
            }`}
          >
            {subtitle}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function ELOComparisonCard({
  ratings,
}: {
  ratings: Record<string, ELORating>;
}) {
  const swarm = ratings["swarm"];
  const single = ratings["single_agent"];

  if (!swarm && !single) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Swords className="h-5 w-5" />
            ELO Ratings
          </CardTitle>
          <CardDescription>Run an A/B benchmark to see ratings</CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">No data yet</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Swords className="h-5 w-5" />
          ELO Ratings — Swarm vs Single Agent
        </CardTitle>
        <CardDescription>
          Head-to-head comparison from A/B benchmark runs
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-6">
          {/* Swarm */}
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <Bot className="h-5 w-5 text-blue-500" />
              <span className="font-semibold">Swarm</span>
            </div>
            <div className="text-4xl font-bold text-blue-500">
              {swarm?.rating?.toFixed(0) ?? "—"}
            </div>
            <div className="flex gap-3 text-sm text-muted-foreground">
              <span className="text-green-500">W {swarm?.wins ?? 0}</span>
              <span className="text-red-500">L {swarm?.losses ?? 0}</span>
              <span>D {swarm?.draws ?? 0}</span>
            </div>
          </div>

          {/* Single Agent */}
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <Zap className="h-5 w-5 text-amber-500" />
              <span className="font-semibold">Single Agent</span>
            </div>
            <div className="text-4xl font-bold text-amber-500">
              {single?.rating?.toFixed(0) ?? "—"}
            </div>
            <div className="flex gap-3 text-sm text-muted-foreground">
              <span className="text-green-500">W {single?.wins ?? 0}</span>
              <span className="text-red-500">L {single?.losses ?? 0}</span>
              <span>D {single?.draws ?? 0}</span>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function QualityRadarChart({ results }: { results: RunResult[] }) {
  // Aggregate scores by mode
  const modeScores: Record<string, QualityScore> = {};
  const modeCounts: Record<string, number> = {};

  for (const r of results) {
    if (!r.score) continue;
    if (!modeScores[r.mode]) {
      modeScores[r.mode] = {
        correctness: 0,
        precision: 0,
        recall: 0,
        speed: 0,
        efficiency: 0,
        human_quality: 0,
        composite: 0,
      };
      modeCounts[r.mode] = 0;
    }
    modeCounts[r.mode]++;
    for (const key of Object.keys(r.score) as (keyof QualityScore)[]) {
      modeScores[r.mode][key] += r.score[key];
    }
  }

  // Average
  const radarData = [
    "correctness",
    "precision",
    "recall",
    "speed",
    "efficiency",
  ].map((dim) => {
    const entry: Record<string, unknown> = {
      dimension: dim.charAt(0).toUpperCase() + dim.slice(1),
    };
    for (const mode of Object.keys(modeScores)) {
      const count = modeCounts[mode] || 1;
      entry[mode] =
        parseFloat(
          (modeScores[mode][dim as keyof QualityScore] / count).toFixed(3)
        ) * 100;
    }
    return entry;
  });

  const colors: Record<string, string> = {
    swarm: "#3b82f6",
    single_agent: "#f59e0b",
    both: "#8b5cf6",
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Target className="h-5 w-5" />
          Quality Dimensions
        </CardTitle>
        <CardDescription>
          Radar chart of quality metrics across modes
        </CardDescription>
      </CardHeader>
      <CardContent>
        {radarData.length > 0 ? (
          <ResponsiveContainer width="100%" height={300}>
            <RadarChart data={radarData}>
              <PolarGrid />
              <PolarAngleAxis dataKey="dimension" />
              <PolarRadiusAxis angle={30} domain={[0, 100]} />
              {Object.keys(modeScores).map((mode) => (
                <Radar
                  key={mode}
                  name={mode === "swarm" ? "Swarm" : "Single Agent"}
                  dataKey={mode}
                  stroke={colors[mode] || "#666"}
                  fill={colors[mode] || "#666"}
                  fillOpacity={0.15}
                />
              ))}
              <Legend />
            </RadarChart>
          </ResponsiveContainer>
        ) : (
          <p className="text-sm text-muted-foreground">
            No scored results yet
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function LatencyChart({ results }: { results: RunResult[] }) {
  // Group by task, show swarm vs single-agent latency
  const taskMap = new Map<
    string,
    { name: string; swarm?: number; single_agent?: number }
  >();
  for (const r of results) {
    if (!taskMap.has(r.task_id)) {
      taskMap.set(r.task_id, { name: r.task_name?.slice(0, 20) || r.task_id });
    }
    const entry = taskMap.get(r.task_id)!;
    if (r.mode === "swarm") entry.swarm = r.latency_ms;
    if (r.mode === "single_agent") entry.single_agent = r.latency_ms;
  }

  const chartData = Array.from(taskMap.values());

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Timer className="h-5 w-5" />
          Latency Comparison
        </CardTitle>
        <CardDescription>Task execution time by mode (ms)</CardDescription>
      </CardHeader>
      <CardContent>
        {chartData.length > 0 ? (
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={chartData}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="name" fontSize={11} />
              <YAxis />
              <Tooltip />
              <Legend />
              <Bar
                dataKey="swarm"
                name="Swarm"
                fill="#3b82f6"
                radius={[4, 4, 0, 0]}
              />
              <Bar
                dataKey="single_agent"
                name="Single Agent"
                fill="#f59e0b"
                radius={[4, 4, 0, 0]}
              />
            </BarChart>
          </ResponsiveContainer>
        ) : (
          <p className="text-sm text-muted-foreground">No results yet</p>
        )}
      </CardContent>
    </Card>
  );
}

function ResultsTable({ results }: { results: RunResult[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <BarChart3 className="h-5 w-5" />
          Task Results
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b">
                <th className="text-left py-2 px-3 font-medium">Task</th>
                <th className="text-left py-2 px-3 font-medium">Mode</th>
                <th className="text-left py-2 px-3 font-medium">Status</th>
                <th className="text-right py-2 px-3 font-medium">
                  Latency (ms)
                </th>
                <th className="text-right py-2 px-3 font-medium">LLM Calls</th>
                <th className="text-right py-2 px-3 font-medium">Score</th>
              </tr>
            </thead>
            <tbody>
              {results.map((r, i) => (
                <tr
                  key={`${r.task_id}-${r.mode}-${i}`}
                  className="border-b last:border-0 hover:bg-muted/50"
                >
                  <td className="py-2 px-3 font-mono text-xs">
                    {r.task_name || r.task_id}
                  </td>
                  <td className="py-2 px-3">
                    <Badge
                      variant={
                        r.mode === "swarm" ? "default" : "secondary"
                      }
                      className="text-xs"
                    >
                      {r.mode === "swarm" ? "Swarm" : "Single"}
                    </Badge>
                  </td>
                  <td className="py-2 px-3">
                    {r.status === "passed" ? (
                      <CheckCircle2 className="h-4 w-4 text-green-500" />
                    ) : (
                      <XCircle className="h-4 w-4 text-red-500" />
                    )}
                  </td>
                  <td className="py-2 px-3 text-right font-mono">
                    {r.latency_ms.toLocaleString()}
                  </td>
                  <td className="py-2 px-3 text-right font-mono">
                    {r.llm_calls}
                  </td>
                  <td className="py-2 px-3 text-right font-mono">
                    {r.score
                      ? (r.score.composite * 100).toFixed(1) + "%"
                      : "—"}
                  </td>
                </tr>
              ))}
              {results.length === 0 && (
                <tr>
                  <td
                    colSpan={6}
                    className="py-8 text-center text-muted-foreground"
                  >
                    No results yet. Run a benchmark to see data.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
}

// ── Main Page ───────────────────────────────────────────────────────────────

export default function BenchmarkDashboardPage() {
  const [summary, setSummary] = useState<BenchmarkSummary | null>(null);
  const [runs, setRuns] = useState<BenchmarkRun[]>([]);
  const [selectedRun, setSelectedRun] = useState<BenchmarkRun | null>(null);
  const [loading, setLoading] = useState(true);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      setLoading(true);
      const [s, r] = await Promise.all([
        fetchBenchmarkSummary(),
        fetchBenchmarkRuns(),
      ]);
      setSummary(s);
      setRuns(r);
      if (r.length > 0 && !selectedRun) {
        setSelectedRun(r[0]);
      }
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load");
    } finally {
      setLoading(false);
    }
  }, [selectedRun]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const handleStartRun = async (mode: string) => {
    try {
      setRunning(true);
      const run = await startBenchmarkRun(
        `bench-${new Date().toISOString().slice(0, 19)}`,
        mode
      );
      setSelectedRun(run);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to start run");
    } finally {
      setRunning(false);
    }
  };

  const latestRun = selectedRun || summary?.latest_run;
  const results = latestRun?.results ?? [];
  const runSummary = latestRun?.summary;

  return (
    <>
      <PageHeader
        title="Agent Benchmark"
        description="Compare swarm vs single-agent performance across benchmark tasks"
      />

      {/* Actions Bar */}
      <div className="flex items-center gap-3 mb-6">
        <Button
          onClick={() => handleStartRun("both")}
          disabled={running}
          className="gap-2"
        >
          {running ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Play className="h-4 w-4" />
          )}
          Run A/B Benchmark
        </Button>
        <Button
          variant="outline"
          onClick={() => handleStartRun("swarm")}
          disabled={running}
          className="gap-2"
        >
          <Bot className="h-4 w-4" />
          Swarm Only
        </Button>
        <Button
          variant="outline"
          onClick={() => handleStartRun("single_agent")}
          disabled={running}
          className="gap-2"
        >
          <Zap className="h-4 w-4" />
          Single Agent Only
        </Button>
        <div className="flex-1" />
        <Button variant="ghost" onClick={refresh} className="gap-2">
          <RefreshCw className="h-4 w-4" />
          Refresh
        </Button>
        {latestRun && (
          <Button
            variant="ghost"
            onClick={() => downloadReport(latestRun.id)}
            className="gap-2"
          >
            <Download className="h-4 w-4" />
            JSON Report
          </Button>
        )}
      </div>

      {error && (
        <div className="mb-6 p-4 bg-red-500/10 border border-red-500/20 rounded-lg text-red-500 text-sm">
          {error}
        </div>
      )}

      {/* Stats Grid */}
      {loading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-28" />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
          <StatsCard
            title="Total Runs"
            value={summary?.total_runs?.toString() ?? "0"}
            subtitle={`${summary?.total_tasks ?? 0} tasks loaded`}
            icon={Activity}
          />
          <StatsCard
            title="Avg Score"
            value={
              runSummary
                ? (runSummary.avg_score * 100).toFixed(1) + "%"
                : "—"
            }
            subtitle={
              runSummary
                ? `${runSummary.passed}/${runSummary.total_tasks} passed`
                : undefined
            }
            icon={Target}
            trend={
              runSummary
                ? runSummary.avg_score >= 0.7
                  ? "up"
                  : "down"
                : undefined
            }
          />
          <StatsCard
            title="P90 Latency"
            value={
              runSummary
                ? runSummary.p90_latency_ms.toFixed(0) + "ms"
                : "—"
            }
            subtitle={
              runSummary
                ? `P50: ${runSummary.p50_latency_ms.toFixed(0)}ms`
                : undefined
            }
            icon={Timer}
          />
          <StatsCard
            title="Avg LLM Calls"
            value={
              runSummary ? runSummary.avg_llm_calls.toFixed(1) : "—"
            }
            subtitle={
              runSummary
                ? `${runSummary.avg_tokens_used.toFixed(0)} avg tokens`
                : undefined
            }
            icon={Flame}
          />
        </div>
      )}

      {/* Run Selector + ELO */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-6">
        <ELOComparisonCard ratings={summary?.ratings ?? {}} />

        {/* Run History */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Clock className="h-5 w-5" />
              Run History
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2 max-h-64 overflow-y-auto">
              {runs.map((run) => (
                <button
                  key={run.id}
                  onClick={() => setSelectedRun(run)}
                  className={`w-full text-left p-3 rounded-lg border transition-colors ${
                    selectedRun?.id === run.id
                      ? "border-primary bg-primary/5"
                      : "border-border hover:bg-muted/50"
                  }`}
                >
                  <div className="flex items-center justify-between">
                    <span className="font-medium text-sm">{run.name}</span>
                    <Badge
                      variant={
                        run.status === "completed" ? "default" : "secondary"
                      }
                      className="text-xs"
                    >
                      {run.status}
                    </Badge>
                  </div>
                  <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                    <span>{run.mode}</span>
                    <span>
                      {new Date(run.started_at).toLocaleDateString()}{" "}
                      {new Date(run.started_at).toLocaleTimeString()}
                    </span>
                    {run.summary && (
                      <span>
                        {run.summary.passed}/{run.summary.total_tasks} passed
                      </span>
                    )}
                  </div>
                </button>
              ))}
              {runs.length === 0 && (
                <p className="text-sm text-muted-foreground text-center py-4">
                  No benchmark runs yet
                </p>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Charts */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-6">
        <QualityRadarChart results={results} />
        <LatencyChart results={results} />
      </div>

      {/* Results Table */}
      <ResultsTable results={results} />
    </>
  );
}
