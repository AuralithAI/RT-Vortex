// ─── Index Sizes Per-Repo BarChart ────────────────────────────────────────────
// Displays a horizontal bar chart of per-repository FAISS index sizes (MB)
// sourced from the structured `index_sizes_bytes` field in EngineMetricsSnapshot.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Cell,
} from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { EngineMetricsSnapshot } from "@/types/api";

const COLORS = [
  "hsl(var(--chart-1))",
  "hsl(var(--chart-2))",
  "hsl(var(--chart-3))",
  "hsl(var(--chart-4))",
  "hsl(var(--chart-5))",
];

interface IndexSizesChartProps {
  latest: EngineMetricsSnapshot | null;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function shortRepoId(repoId: string): string {
  // "org/repo" → "repo", or last 20 chars if long
  const parts = repoId.split("/");
  const name = parts[parts.length - 1] || repoId;
  return name.length > 20 ? name.slice(-20) : name;
}

export function IndexSizesChart({ latest }: IndexSizesChartProps) {
  const sizes = latest?.index_sizes_bytes;
  if (!sizes || Object.keys(sizes).length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            Index Sizes by Repository
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex h-[200px] items-center justify-center text-muted-foreground">
            No indexed repositories yet
          </div>
        </CardContent>
      </Card>
    );
  }

  const chartData = Object.entries(sizes)
    .map(([repo, bytes]) => ({
      repo: shortRepoId(repo),
      fullRepo: repo,
      sizeMB: +(bytes / (1024 * 1024)).toFixed(2),
      sizeBytes: bytes,
    }))
    .sort((a, b) => b.sizeMB - a.sizeMB);

  const totalBytes = Object.values(sizes).reduce((a, b) => a + b, 0);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">
          Index Sizes by Repository — Total: {formatBytes(totalBytes)}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={Math.max(200, chartData.length * 40)}>
          <BarChart data={chartData} layout="vertical" margin={{ left: 20 }}>
            <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
            <XAxis
              type="number"
              fontSize={12}
              tickFormatter={(v: number) => `${v} MB`}
            />
            <YAxis
              type="category"
              dataKey="repo"
              width={120}
              fontSize={12}
              tick={{ fill: "hsl(var(--foreground))" }}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: "hsl(var(--popover))",
                border: "1px solid hsl(var(--border))",
                borderRadius: "var(--radius)",
                fontSize: 12,
              }}
              formatter={(value, _name, props) => {
                const payload = (props as unknown as { payload?: { fullRepo?: string; sizeBytes?: number } }).payload;
                return [
                  formatBytes(payload?.sizeBytes ?? 0),
                  payload?.fullRepo ?? "",
                ];
              }}
            />
            <Bar dataKey="sizeMB" radius={[0, 4, 4, 0]} name="Size">
              {chartData.map((_entry, index) => (
                <Cell
                  key={`cell-${index}`}
                  fill={COLORS[index % COLORS.length]}
                />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}
