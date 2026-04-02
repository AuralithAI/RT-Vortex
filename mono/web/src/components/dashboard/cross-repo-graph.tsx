// ─── Cross-Repo Dependency Graph ─────────────────────────────────────────────
// Visualizes the org-level dependency graph as a node list + edge table.
// A canvas-based graph visualisation can be added later; this provides the
// essential data view plus a build-graph trigger.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import {
  Network,
  Play,
  Loader2,
  CircleDot,
  ArrowRight,
  BarChart3,
} from "lucide-react";
import { useBuildGraph } from "@/lib/api/mutations";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { useUIStore } from "@/lib/stores/ui";
import type { BuildGraphResponse, DepGraphNode, DepGraphEdge } from "@/types/api";

interface CrossRepoGraphProps {
  orgId: string;
}

const nodeTypeColor: Record<string, string> = {
  library: "bg-blue-500/10 text-blue-600",
  service: "bg-green-500/10 text-green-600",
  executable: "bg-purple-500/10 text-purple-600",
  module: "bg-orange-500/10 text-orange-600",
  unknown: "bg-gray-500/10 text-gray-600",
};

export function CrossRepoGraph({ orgId }: CrossRepoGraphProps) {
  const buildGraph = useBuildGraph();
  const { addToast } = useUIStore();
  const [graph, setGraph] = useState<BuildGraphResponse | null>(null);
  const [loading, setLoading] = useState(false);

  const handleBuild = async (forceRescan = false) => {
    setLoading(true);
    try {
      const result = await buildGraph.mutateAsync({
        orgId,
        data: { force_rescan: forceRescan },
      });
      setGraph(result);
      addToast({
        title: "Dependency graph built",
        description: `${result.total_nodes} nodes, ${result.total_edges} edges across ${result.repos_scanned} repos`,
        variant: "success",
      });
    } catch (err) {
      addToast({
        title: "Failed to build graph",
        description: err instanceof Error ? err.message : "Unknown error",
        variant: "error",
      });
    } finally {
      setLoading(false);
    }
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="flex items-center gap-2 text-base">
          <Network className="h-4 w-4" />
          Dependency Graph
        </CardTitle>
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={() => handleBuild(true)}
            disabled={loading}
          >
            {loading ? (
              <Loader2 className="mr-1 h-4 w-4 animate-spin" />
            ) : (
              <Play className="mr-1 h-4 w-4" />
            )}
            Force Rescan
          </Button>
          <Button size="sm" onClick={() => handleBuild()} disabled={loading}>
            {loading ? (
              <Loader2 className="mr-1 h-4 w-4 animate-spin" />
            ) : (
              <Play className="mr-1 h-4 w-4" />
            )}
            Build Graph
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {loading && !graph ? (
          <div className="space-y-2">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : !graph ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <Network className="h-8 w-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">
              Click &quot;Build Graph&quot; to scan all linked repos and
              generate the org-level dependency graph.
            </p>
          </div>
        ) : (
          <div className="space-y-6">
            {/* Summary stats */}
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
              <StatCard
                label="Repos Scanned"
                value={graph.repos_scanned}
              />
              <StatCard label="Nodes" value={graph.total_nodes} />
              <StatCard label="Edges" value={graph.total_edges} />
              <StatCard
                label="Duration"
                value={`${(graph.duration / 1e6).toFixed(0)}ms`}
              />
            </div>

            {/* Nodes */}
            {graph.nodes?.length > 0 && (
              <div>
                <h3 className="mb-2 flex items-center gap-2 text-sm font-medium">
                  <CircleDot className="h-4 w-4" />
                  Nodes ({graph.nodes.length})
                </h3>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Label</TableHead>
                      <TableHead>Type</TableHead>
                      <TableHead>Language</TableHead>
                      <TableHead>Repo Type</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {graph.nodes.map((node: DepGraphNode) => (
                      <TableRow key={node.id}>
                        <TableCell className="font-medium">
                          {node.label}
                        </TableCell>
                        <TableCell>
                          <span
                            className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${nodeTypeColor[node.node_type] ?? nodeTypeColor.unknown}`}
                          >
                            {node.node_type}
                          </span>
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {node.language || "—"}
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {node.repo_type || "—"}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}

            {/* Edges */}
            {graph.edges?.length > 0 && (
              <div>
                <h3 className="mb-2 flex items-center gap-2 text-sm font-medium">
                  <ArrowRight className="h-4 w-4" />
                  Edges ({graph.edges.length})
                </h3>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Source → Target</TableHead>
                      <TableHead>Type</TableHead>
                      <TableHead>Weight</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {graph.edges.map((edge: DepGraphEdge, idx: number) => (
                      <TableRow key={`${edge.source_node_id}-${edge.target_node_id}-${idx}`}>
                        <TableCell>
                          <div className="flex items-center gap-2 text-sm">
                            <code className="rounded bg-muted px-1 text-xs">
                              {edge.source_node_id.substring(0, 8)}
                            </code>
                            <ArrowRight className="h-3 w-3 text-muted-foreground" />
                            <code className="rounded bg-muted px-1 text-xs">
                              {edge.target_node_id.substring(0, 8)}
                            </code>
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline">{edge.edge_type}</Badge>
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {edge.weight.toFixed(2)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ── Tiny stat card ──────────────────────────────────────────────────────────

function StatCard({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-lg border bg-card p-3 text-center">
      <p className="text-2xl font-bold">{value}</p>
      <p className="text-xs text-muted-foreground">{label}</p>
    </div>
  );
}
