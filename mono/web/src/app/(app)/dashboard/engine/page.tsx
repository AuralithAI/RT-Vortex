// ─── Engine Performance Dashboard ────────────────────────────────────────────
// Observability: real-time metrics from the C++ engine streamed via
// WebSocket.  Shows embedding latency, search performance, FAISS stats,
// component health, and TMS memory system indicators.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import {
  Activity,
  Brain,
  Database,
  Gauge,
  Search,
  Sparkles,
  Timer,
  Wifi,
  WifiOff,
  Zap,
} from "lucide-react";
import {
  useEngineMetrics,
  getMetricScalar,
  getMetricHistogram,
  formatUptime,
} from "@/hooks/use-engine-metrics";
import { PageHeader } from "@/components/layout/page-header";
import { StatsCard } from "@/components/dashboard/stats-card";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
} from "recharts";
import { MemoryAccountsChart } from "@/components/dashboard/engine-metrics/memory-accounts-chart";

// ── Well-known metric names (match C++ metrics.h constants) ─────────────────

const EMBED_LATENCY_S = "embed_latency_s";
const SEARCH_LATENCY_S = "search_latency_s";
const CHUNKS_INGESTED = "chunks_ingested";
const INDEX_SIZE_VECTORS = "index_size_vectors";
const EMBED_CACHE_HIT_RATE = "embed_cache_hit_rate";
const EMBED_TOTAL_CALLS = "embed_total_calls";
const TMS_FORWARD_LATENCY_S = "tms_forward_latency_s";
const CMA_SCORE = "cma_score";
const MINILM_READY = "minilm_ready";
const FAISS_LOADED = "faiss_loaded";
const LLM_AVOIDED_RATE = "llm_avoided_rate";
const KG_NODES_TOTAL = "aipr_kg_nodes_total";
const KG_EDGES_TOTAL = "aipr_kg_edges_total";

export default function EnginePerformancePage() {
  const { connected, latest, history, error } = useEngineMetrics({
    historySize: 60,
  });

  // ── Derived values ────────────────────────────────────────────────────
  const embedHist = getMetricHistogram(latest, EMBED_LATENCY_S);
  const searchHist = getMetricHistogram(latest, SEARCH_LATENCY_S);
  const tmsHist = getMetricHistogram(latest, TMS_FORWARD_LATENCY_S);
  const chunksIngested = getMetricScalar(latest, CHUNKS_INGESTED);
  const indexSize = getMetricScalar(latest, INDEX_SIZE_VECTORS);
  const cacheHitRate = getMetricScalar(latest, EMBED_CACHE_HIT_RATE);
  const embedCalls = getMetricScalar(latest, EMBED_TOTAL_CALLS);
  const cmaScore = getMetricScalar(latest, CMA_SCORE);
  const miniLMReady = getMetricScalar(latest, MINILM_READY) === 1;
  const faissLoaded = getMetricScalar(latest, FAISS_LOADED) === 1;
  const llmAvoided = getMetricScalar(latest, LLM_AVOIDED_RATE);
  const uptime = latest?.uptime_s ?? 0;
  const kgNodes = getMetricScalar(latest, KG_NODES_TOTAL);
  const kgEdges = getMetricScalar(latest, KG_EDGES_TOTAL);
  const kgEnabled = kgNodes > 0 || kgEdges > 0;

  // ── Chart data from history ───────────────────────────────────────────
  const latencyChartData = history.map((snap, i) => {
    const eHist = snap.metrics[EMBED_LATENCY_S];
    const sHist = snap.metrics[SEARCH_LATENCY_S];
    return {
      t: i,
      embed_p50: eHist?.histogram?.p50 ? eHist.histogram.p50 * 1000 : 0,
      embed_p95: eHist?.histogram?.p95 ? eHist.histogram.p95 * 1000 : 0,
      search_p50: sHist?.histogram?.p50 ? sHist.histogram.p50 * 1000 : 0,
      search_p95: sHist?.histogram?.p95 ? sHist.histogram.p95 * 1000 : 0,
    };
  });

  const isLoading = !latest && !error;

  return (
    <>
      <PageHeader
        title="Engine Performance"
        description="Real-time C++ engine metrics and TMS observability"
      />

      {/* Connection status */}
      <div className="mb-4 flex items-center gap-2">
        {connected ? (
          <Badge variant="default" className="gap-1">
            <Wifi className="h-3 w-3" /> Live
          </Badge>
        ) : (
          <Badge variant="destructive" className="gap-1">
            <WifiOff className="h-3 w-3" /> Disconnected
          </Badge>
        )}
        {uptime > 0 && (
          <Badge variant="outline" className="gap-1">
            <Timer className="h-3 w-3" /> Uptime: {formatUptime(uptime)}
          </Badge>
        )}
        {error && (
          <Badge variant="destructive" className="gap-1">
            {error}
          </Badge>
        )}
      </div>

      {/* Stats Grid */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {isLoading ? (
          Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px]" />
          ))
        ) : (
          <>
            <StatsCard
              title="Embed Latency (p50)"
              value={embedHist ? `${(embedHist.p50 * 1000).toFixed(1)}ms` : "—"}
              description={
                embedHist
                  ? `p95: ${(embedHist.p95 * 1000).toFixed(1)}ms · p99: ${(embedHist.p99 * 1000).toFixed(1)}ms`
                  : "No data"
              }
              icon={Zap}
            />
            <StatsCard
              title="Search Latency (p50)"
              value={searchHist ? `${(searchHist.p50 * 1000).toFixed(1)}ms` : "—"}
              description={
                searchHist
                  ? `p95: ${(searchHist.p95 * 1000).toFixed(1)}ms · ${searchHist.count} queries`
                  : "No data"
              }
              icon={Search}
            />
            <StatsCard
              title="Chunks Ingested"
              value={chunksIngested.toLocaleString()}
              description={`FAISS index: ${indexSize.toLocaleString()} vectors`}
              icon={Database}
            />
            <StatsCard
              title="Embed Cache Hit"
              value={`${(cacheHitRate * 100).toFixed(1)}%`}
              description={`${embedCalls.toLocaleString()} total calls`}
              icon={Sparkles}
            />
            <StatsCard
              title="CMA Score"
              value={cmaScore.toFixed(3)}
              description="Cross-Memory Attention coherence"
              icon={Brain}
            />
            <StatsCard
              title="TMS Forward (p50)"
              value={tmsHist ? `${(tmsHist.p50 * 1000).toFixed(1)}ms` : "—"}
              description={
                tmsHist
                  ? `p95: ${(tmsHist.p95 * 1000).toFixed(1)}ms`
                  : "No data"
              }
              icon={Activity}
            />
            <StatsCard
              title="LLM Avoided Rate"
              value={`${(llmAvoided * 100).toFixed(1)}%`}
              description="Reviews using heuristics only"
              icon={Gauge}
            />
            <StatsCard
              title="Components"
              value={miniLMReady && faissLoaded ? "All Ready" : "Degraded"}
              description={`MiniLM: ${miniLMReady ? "✓" : "✗"} · FAISS: ${faissLoaded ? "✓" : "✗"} · KG: ${kgEnabled ? `${kgNodes}n/${kgEdges}e` : "off"}`}
              icon={Activity}
            />
          </>
        )}
      </div>

      {/* Latency Charts */}
      <div className="mt-6 grid gap-6 lg:grid-cols-2">
        {/* Embedding Latency Timeline */}
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">
              Embedding Latency (ms) — Last 60s
            </CardTitle>
          </CardHeader>
          <CardContent>
            {latencyChartData.length > 1 ? (
              <ResponsiveContainer width="100%" height={200}>
                <LineChart data={latencyChartData}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                  <XAxis dataKey="t" tick={false} />
                  <YAxis width={40} fontSize={12} />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "hsl(var(--popover))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: "var(--radius)",
                      fontSize: 12,
                    }}
                  />
                  <Line
                    type="monotone"
                    dataKey="embed_p50"
                    stroke="hsl(var(--chart-1))"
                    strokeWidth={2}
                    dot={false}
                    name="p50"
                  />
                  <Line
                    type="monotone"
                    dataKey="embed_p95"
                    stroke="hsl(var(--chart-2))"
                    strokeWidth={1}
                    strokeDasharray="4 2"
                    dot={false}
                    name="p95"
                  />
                </LineChart>
              </ResponsiveContainer>
            ) : (
              <div className="flex h-[200px] items-center justify-center text-muted-foreground">
                Waiting for data…
              </div>
            )}
          </CardContent>
        </Card>

        {/* Search Latency Timeline */}
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium">
              Search Latency (ms) — Last 60s
            </CardTitle>
          </CardHeader>
          <CardContent>
            {latencyChartData.length > 1 ? (
              <ResponsiveContainer width="100%" height={200}>
                <LineChart data={latencyChartData}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                  <XAxis dataKey="t" tick={false} />
                  <YAxis width={40} fontSize={12} />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "hsl(var(--popover))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: "var(--radius)",
                      fontSize: 12,
                    }}
                  />
                  <Line
                    type="monotone"
                    dataKey="search_p50"
                    stroke="hsl(var(--chart-3))"
                    strokeWidth={2}
                    dot={false}
                    name="p50"
                  />
                  <Line
                    type="monotone"
                    dataKey="search_p95"
                    stroke="hsl(var(--chart-4))"
                    strokeWidth={1}
                    strokeDasharray="4 2"
                    dot={false}
                    name="p95"
                  />
                </LineChart>
              </ResponsiveContainer>
            ) : (
              <div className="flex h-[200px] items-center justify-center text-muted-foreground">
                Waiting for data…
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Histogram Detail */}
      {embedHist && (
        <div className="mt-6">
          <Card>
            <CardHeader>
              <CardTitle className="text-sm font-medium">
                Embedding Latency Distribution
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-8">
                {[
                  { label: "Count", value: embedHist.count.toLocaleString() },
                  { label: "Min", value: `${(embedHist.min_val * 1000).toFixed(2)}ms` },
                  { label: "Avg", value: `${(embedHist.avg * 1000).toFixed(2)}ms` },
                  { label: "p50", value: `${(embedHist.p50 * 1000).toFixed(2)}ms` },
                  { label: "p90", value: `${(embedHist.p90 * 1000).toFixed(2)}ms` },
                  { label: "p95", value: `${(embedHist.p95 * 1000).toFixed(2)}ms` },
                  { label: "p99", value: `${(embedHist.p99 * 1000).toFixed(2)}ms` },
                  { label: "Max", value: `${(embedHist.max_val * 1000).toFixed(2)}ms` },
                ].map(({ label, value }) => (
                  <div key={label} className="text-center">
                    <div className="text-xs text-muted-foreground">{label}</div>
                    <div className="text-sm font-semibold">{value}</div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      {/* Memory Accounts Query Distribution */}
      <div className="mt-6">
        <MemoryAccountsChart history={history} />
      </div>
    </>
  );
}
