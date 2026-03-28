// ─── Memory Accounts Chart ───────────────────────────────────────────────────
// Stacked BarChart showing query distribution across memory accounts.
// Data sourced from counters: aipr_account_queries_*_total
// Derives per-snapshot deltas → queries/min per account.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import type { EngineMetricsSnapshot } from "@/types/api";

// Metric keys (match C++ metrics.h constants)
const ACCOUNT_DEV = "aipr_account_queries_dev_total";
const ACCOUNT_OPS = "aipr_account_queries_ops_total";
const ACCOUNT_SECURITY = "aipr_account_queries_security_total";
const ACCOUNT_HISTORY = "aipr_account_queries_history_total";

interface MemoryAccountsChartProps {
  history: EngineMetricsSnapshot[];
}

function scalar(snap: EngineMetricsSnapshot, key: string): number {
  return snap.metrics[key]?.scalar ?? 0;
}

function delta(
  curr: EngineMetricsSnapshot,
  prev: EngineMetricsSnapshot,
  key: string,
): number {
  return Math.max(0, scalar(curr, key) - scalar(prev, key));
}

function formatTime(ms: number): string {
  const d = new Date(ms);
  return `${d.getHours().toString().padStart(2, "0")}:${d
    .getMinutes()
    .toString()
    .padStart(2, "0")}:${d.getSeconds().toString().padStart(2, "0")}`;
}

export function MemoryAccountsChart({ history }: MemoryAccountsChartProps) {
  if (history.length < 2) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            Memory Account Query Distribution
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex h-[200px] items-center justify-center text-muted-foreground">
            Waiting for data…
          </div>
        </CardContent>
      </Card>
    );
  }

  const accountData = history.slice(1).map((snap, i) => {
    const prev = history[i]; // previous snapshot
    return {
      time: formatTime(snap.timestamp_ms),
      dev: delta(snap, prev, ACCOUNT_DEV),
      ops: delta(snap, prev, ACCOUNT_OPS),
      security: delta(snap, prev, ACCOUNT_SECURITY),
      history: delta(snap, prev, ACCOUNT_HISTORY),
    };
  });

  // Check if queries have EVER been routed (cumulative totals from
  // latest snapshot).  This keeps the chart visible between bursts of
  // activity instead of flipping back to the empty-state placeholder.
  const latest = history[history.length - 1];
  const hasEverRouted =
    scalar(latest, ACCOUNT_DEV) +
      scalar(latest, ACCOUNT_OPS) +
      scalar(latest, ACCOUNT_SECURITY) +
      scalar(latest, ACCOUNT_HISTORY) >
    0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          Memory Account Query Distribution
        </CardTitle>
      </CardHeader>
      <CardContent>
        {hasEverRouted ? (
          <ResponsiveContainer width="100%" height={200}>
            <BarChart data={accountData}>
              <CartesianGrid
                strokeDasharray="3 3"
                className="stroke-muted"
              />
              <XAxis dataKey="time" tick={false} />
              <YAxis width={30} fontSize={12} allowDecimals={false} />
              <Tooltip
                contentStyle={{
                  backgroundColor: "hsl(var(--popover))",
                  border: "1px solid hsl(var(--border))",
                  borderRadius: "var(--radius)",
                  fontSize: 12,
                }}
              />
              <Legend wrapperStyle={{ fontSize: 12 }} />
              <Bar
                dataKey="dev"
                stackId="a"
                fill="#3b82f6"
                name="Dev"
              />
              <Bar
                dataKey="ops"
                stackId="a"
                fill="#f59e0b"
                name="Ops"
              />
              <Bar
                dataKey="security"
                stackId="a"
                fill="#ef4444"
                name="Security"
              />
              <Bar
                dataKey="history"
                stackId="a"
                fill="#8b5cf6"
                name="History"
              />
            </BarChart>
          </ResponsiveContainer>
        ) : (
          <div className="flex h-[200px] items-center justify-center text-muted-foreground">
            No account-routed queries yet
          </div>
        )}
      </CardContent>
    </Card>
  );
}
