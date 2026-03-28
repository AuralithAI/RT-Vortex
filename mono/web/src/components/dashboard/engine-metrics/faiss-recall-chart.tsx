// ─── FAISS Recall@10 Chart ───────────────────────────────────────────────────
// Displays a rolling time-series line chart of the FAISS recall@10 metric
// streamed from the C++ engine via WebSocket.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { EngineMetricsSnapshot } from "@/types/api";

const FAISS_RECALL_AT_10 = "aipr_faiss_recall_at_10";

interface FaissRecallChartProps {
  history: EngineMetricsSnapshot[];
}

export function FaissRecallChart({ history }: FaissRecallChartProps) {
  const chartData = history.map((snap, i) => {
    const recall = snap.metrics[FAISS_RECALL_AT_10]?.scalar ?? 0;
    return { t: i, recall: +(recall * 100).toFixed(1) };
  });

  const hasData = chartData.some((d) => d.recall > 0);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          FAISS Recall@10 (%) — Last 60s
        </CardTitle>
      </CardHeader>
      <CardContent>
        {hasData ? (
          <ResponsiveContainer width="100%" height={200}>
            <LineChart data={chartData}>
              <CartesianGrid
                strokeDasharray="3 3"
                className="stroke-muted"
              />
              <XAxis dataKey="t" tick={false} />
              <YAxis
                width={40}
                fontSize={12}
                domain={[0, 100]}
                tickFormatter={(v: number) => `${v}%`}
              />
              <Tooltip
                contentStyle={{
                  backgroundColor: "hsl(var(--popover))",
                  border: "1px solid hsl(var(--border))",
                  borderRadius: "var(--radius)",
                  fontSize: 12,
                }}
                formatter={(value: number) => [`${value}%`, "Recall@10"]}
              />
              <Line
                type="monotone"
                dataKey="recall"
                stroke="hsl(var(--chart-5))"
                strokeWidth={2}
                dot={false}
                name="Recall@10"
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
  );
}
