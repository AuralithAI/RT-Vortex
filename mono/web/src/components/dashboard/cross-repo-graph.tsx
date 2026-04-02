// ─── Cross-Repo Dependency Graph ─────────────────────────────────────────────
// Production-grade visual dependency graph using React Flow (@xyflow/react).
// Two view modes: interactive canvas (default) and table fallback.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  Handle,
  Position,
  MarkerType,
  useNodesState,
  useEdgesState,
  Panel,
  type Node,
  type Edge,
  type NodeProps,
  type NodeMouseHandler,
  type EdgeMouseHandler,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import {
  Network,
  Play,
  Loader2,
  CircleDot,
  ArrowRight,
  Table2,
  X,
  Package,
  GitFork,
  Clock,
  Info,
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
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";
import { useUIStore } from "@/lib/stores/ui";
import type {
  BuildGraphResponse,
  DepGraphNode,
  DepGraphEdge,
} from "@/types/api";

// ── Props ───────────────────────────────────────────────────────────────────

interface CrossRepoGraphProps {
  orgId: string;
}

// ── Constants ───────────────────────────────────────────────────────────────

const NODE_TYPE_COLORS: Record<
  string,
  { bg: string; border: string; text: string; dot: string }
> = {
  library: {
    bg: "bg-blue-50 dark:bg-blue-950/40",
    border: "border-blue-300 dark:border-blue-700",
    text: "text-blue-700 dark:text-blue-300",
    dot: "#3b82f6",
  },
  service: {
    bg: "bg-green-50 dark:bg-green-950/40",
    border: "border-green-300 dark:border-green-700",
    text: "text-green-700 dark:text-green-300",
    dot: "#22c55e",
  },
  executable: {
    bg: "bg-purple-50 dark:bg-purple-950/40",
    border: "border-purple-300 dark:border-purple-700",
    text: "text-purple-700 dark:text-purple-300",
    dot: "#a855f7",
  },
  module: {
    bg: "bg-orange-50 dark:bg-orange-950/40",
    border: "border-orange-300 dark:border-orange-700",
    text: "text-orange-700 dark:text-orange-300",
    dot: "#f97316",
  },
  unknown: {
    bg: "bg-gray-50 dark:bg-gray-900/40",
    border: "border-gray-300 dark:border-gray-700",
    text: "text-gray-700 dark:text-gray-300",
    dot: "#6b7280",
  },
};

const EDGE_TYPE_COLORS: Record<string, string> = {
  dependency: "#3b82f6",
  api: "#22c55e",
  artifact: "#a855f7",
  import: "#f97316",
  default: "#6b7280",
};

const TABLE_NODE_TYPE_COLORS: Record<string, string> = {
  library: "bg-blue-500/10 text-blue-600",
  service: "bg-green-500/10 text-green-600",
  executable: "bg-purple-500/10 text-purple-600",
  module: "bg-orange-500/10 text-orange-600",
  unknown: "bg-gray-500/10 text-gray-600",
};

// ── Custom React Flow Node ──────────────────────────────────────────────────

interface RepoNodeData extends Record<string, unknown> {
  label: string;
  nodeType: string;
  language: string;
  repoType: string;
  depCount: number;
  metadata?: Record<string, string>;
}

function RepoNode({ data }: NodeProps<Node<RepoNodeData>>) {
  const colors = NODE_TYPE_COLORS[data.nodeType] ?? NODE_TYPE_COLORS.unknown;

  return (
    <div
      className={`
        relative rounded-xl border-2 shadow-md px-4 py-3 min-w-[200px] max-w-[260px]
        transition-shadow hover:shadow-lg cursor-pointer
        ${colors.bg} ${colors.border}
      `}
    >
      {/* Left handle (target) */}
      <Handle
        type="target"
        position={Position.Left}
        className="!w-2.5 !h-2.5 !border-2 !border-background"
        style={{ background: colors.dot }}
      />

      {/* Content */}
      <div className="flex items-start gap-3">
        <div
          className="mt-1 h-3 w-3 shrink-0 rounded-full ring-2 ring-background"
          style={{ backgroundColor: colors.dot }}
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold leading-tight">
            {data.label}
          </p>
          <div className="mt-1 flex flex-wrap items-center gap-1">
            {data.language && (
              <span
                className={`inline-flex rounded-full px-1.5 py-0.5 text-[10px] font-medium ${colors.text} bg-white/60 dark:bg-black/20`}
              >
                {data.language}
              </span>
            )}
            <span className="inline-flex rounded-full px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground bg-white/60 dark:bg-black/20">
              {data.nodeType}
            </span>
          </div>
          {data.depCount > 0 && (
            <p className="mt-1 text-[10px] text-muted-foreground">
              {data.depCount} connection{data.depCount !== 1 ? "s" : ""}
            </p>
          )}
        </div>
      </div>

      {/* Right handle (source) */}
      <Handle
        type="source"
        position={Position.Right}
        className="!w-2.5 !h-2.5 !border-2 !border-background"
        style={{ background: colors.dot }}
      />
    </div>
  );
}

const nodeTypes = { repo: RepoNode };

// ── Auto-Layout (layered / topological) ─────────────────────────────────────

/**
 * Computes a hierarchical layout for nodes based on edges.
 * Nodes with no incoming edges are placed in column 0, their targets in
 * column 1, etc. Within each column nodes are spread vertically.
 */
function computeLayout(
  rawNodes: DepGraphNode[],
  rawEdges: DepGraphEdge[],
): { nodes: Node<RepoNodeData>[]; edges: Edge[] } {
  const X_GAP = 320;
  const Y_GAP = 120;
  const BASE_X = 40;
  const BASE_Y = 40;

  // Build adjacency data
  const incomingCount = new Map<string, number>();
  const outgoing = new Map<string, string[]>();
  for (const n of rawNodes) {
    incomingCount.set(n.id, 0);
    outgoing.set(n.id, []);
  }
  for (const e of rawEdges) {
    incomingCount.set(
      e.target_node_id,
      (incomingCount.get(e.target_node_id) ?? 0) + 1,
    );
    outgoing.get(e.source_node_id)?.push(e.target_node_id);
  }

  // Compute dep counts per node (total edges involving this node)
  const depCount = new Map<string, number>();
  for (const n of rawNodes) depCount.set(n.id, 0);
  for (const e of rawEdges) {
    depCount.set(
      e.source_node_id,
      (depCount.get(e.source_node_id) ?? 0) + 1,
    );
    depCount.set(
      e.target_node_id,
      (depCount.get(e.target_node_id) ?? 0) + 1,
    );
  }

  // Topological sort (Kahn's algorithm) for layering
  const layers = new Map<string, number>();
  const queue: string[] = [];

  for (const [id, count] of incomingCount) {
    if (count === 0) {
      queue.push(id);
      layers.set(id, 0);
    }
  }

  let head = 0;
  while (head < queue.length) {
    const current = queue[head++];
    const currentLayer = layers.get(current) ?? 0;

    for (const target of outgoing.get(current) ?? []) {
      const prevLayer = layers.get(target);
      const newLayer = currentLayer + 1;
      if (prevLayer === undefined || newLayer > prevLayer) {
        layers.set(target, newLayer);
      }
      incomingCount.set(target, (incomingCount.get(target) ?? 1) - 1);
      if (incomingCount.get(target) === 0) {
        queue.push(target);
      }
    }
  }

  // Handle nodes not reached (cycles → place them in layer 0)
  for (const n of rawNodes) {
    if (!layers.has(n.id)) {
      layers.set(n.id, 0);
    }
  }

  // Group by layer
  const layerBuckets = new Map<number, DepGraphNode[]>();
  for (const n of rawNodes) {
    const layer = layers.get(n.id) ?? 0;
    if (!layerBuckets.has(layer)) layerBuckets.set(layer, []);
    layerBuckets.get(layer)!.push(n);
  }

  // Position nodes
  const flowNodes: Node<RepoNodeData>[] = [];
  for (const [layer, nodesInLayer] of layerBuckets) {
    const totalHeight = (nodesInLayer.length - 1) * Y_GAP;
    const startY = BASE_Y + (totalHeight > 0 ? -totalHeight / 2 : 0);

    nodesInLayer.forEach((n, i) => {
      flowNodes.push({
        id: n.id,
        type: "repo",
        position: {
          x: BASE_X + layer * X_GAP,
          y: startY + i * Y_GAP + (layer % 2 === 1 ? Y_GAP / 2 : 0),
        },
        data: {
          label: n.label,
          nodeType: n.node_type,
          language: n.language,
          repoType: n.repo_type,
          depCount: depCount.get(n.id) ?? 0,
          metadata: n.metadata,
        },
      });
    });
  }

  // Build edges with styling
  const flowEdges: Edge[] = rawEdges.map((e, idx) => {
    const color = EDGE_TYPE_COLORS[e.edge_type] ?? EDGE_TYPE_COLORS.default;
    const strokeWidth = Math.max(1.5, Math.min(e.weight * 5, 6));

    return {
      id: `${e.source_node_id}-${e.target_node_id}-${idx}`,
      source: e.source_node_id,
      target: e.target_node_id,
      label: e.edge_type,
      animated: e.weight >= 0.7,
      markerEnd: { type: MarkerType.ArrowClosed, color },
      style: { stroke: color, strokeWidth },
      labelStyle: { fontSize: 10, fill: color, fontWeight: 500 },
      labelBgStyle: { fill: "var(--background)", fillOpacity: 0.8 },
      data: { ...e },
    };
  });

  return { nodes: flowNodes, edges: flowEdges };
}

// ── Edge Detail Panel ───────────────────────────────────────────────────────

function EdgeDetailPanel({
  edge,
  nodes,
  onClose,
}: {
  edge: DepGraphEdge;
  nodes: DepGraphNode[];
  onClose: () => void;
}) {
  const sourceNode = nodes.find((n) => n.id === edge.source_node_id);
  const targetNode = nodes.find((n) => n.id === edge.target_node_id);

  return (
    <div className="absolute right-4 top-4 z-20 w-72 rounded-xl border bg-card p-4 shadow-xl">
      <div className="mb-3 flex items-center justify-between">
        <h4 className="flex items-center gap-2 text-sm font-semibold">
          <GitFork className="h-4 w-4" />
          Edge Detail
        </h4>
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6"
          onClick={onClose}
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="space-y-2 text-sm">
        <div>
          <span className="text-xs text-muted-foreground">Source</span>
          <p className="font-medium">
            {sourceNode?.label ?? edge.source_node_id.substring(0, 12)}
          </p>
        </div>
        <div className="flex justify-center">
          <ArrowRight className="h-4 w-4 text-muted-foreground" />
        </div>
        <div>
          <span className="text-xs text-muted-foreground">Target</span>
          <p className="font-medium">
            {targetNode?.label ?? edge.target_node_id.substring(0, 12)}
          </p>
        </div>

        <div className="my-2 border-t" />

        <div className="grid grid-cols-2 gap-2">
          <div>
            <span className="text-[10px] text-muted-foreground">Type</span>
            <p>
              <Badge variant="outline" className="text-xs">
                {edge.edge_type}
              </Badge>
            </p>
          </div>
          <div>
            <span className="text-[10px] text-muted-foreground">Weight</span>
            <p className="font-medium">{edge.weight.toFixed(3)}</p>
          </div>
        </div>

        {edge.metadata && Object.keys(edge.metadata).length > 0 && (
          <div className="mt-2">
            <span className="text-[10px] text-muted-foreground">Metadata</span>
            <div className="mt-1 space-y-1">
              {Object.entries(edge.metadata).map(([key, val]) => (
                <div
                  key={key}
                  className="flex justify-between rounded bg-muted/50 px-2 py-1 text-xs"
                >
                  <span className="text-muted-foreground">{key}</span>
                  <span className="font-medium">{val}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// ── Visual Graph (React Flow canvas) ────────────────────────────────────────

function VisualGraph({
  graphData,
  loading,
}: {
  graphData: BuildGraphResponse;
  loading: boolean;
}) {
  const [selectedEdge, setSelectedEdge] = useState<DepGraphEdge | null>(null);

  const { nodes: initialNodes, edges: initialEdges } = useMemo(
    () => computeLayout(graphData.nodes ?? [], graphData.edges ?? []),
    [graphData],
  );

  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  // Sync when graphData changes
  useEffect(() => {
    const layout = computeLayout(graphData.nodes ?? [], graphData.edges ?? []);
    setNodes(layout.nodes);
    setEdges(layout.edges);
    setSelectedEdge(null);
  }, [graphData, setNodes, setEdges]);

  const onNodeClick: NodeMouseHandler = useCallback(() => {
    // Future: navigate to repo detail page
  }, []);

  const onEdgeClick: EdgeMouseHandler = useCallback(
    (_event, edge) => {
      const rawEdge = (graphData.edges ?? []).find(
        (e) =>
          e.source_node_id === edge.source &&
          e.target_node_id === edge.target,
      );
      if (rawEdge) {
        setSelectedEdge(rawEdge);
      }
    },
    [graphData.edges],
  );

  const onPaneClick = useCallback(() => {
    setSelectedEdge(null);
  }, []);

  return (
    <div className="relative h-[600px] w-full overflow-hidden rounded-xl border bg-background">
      {loading && (
        <div className="absolute inset-0 z-30 flex items-center justify-center bg-background/70 backdrop-blur-sm">
          <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      )}

      <ReactFlowProvider>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={onNodeClick}
          onEdgeClick={onEdgeClick}
          onPaneClick={onPaneClick}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          minZoom={0.2}
          maxZoom={2.5}
          proOptions={{ hideAttribution: true }}
        >
          <Background gap={20} size={1} />
          <Controls
            showInteractive={false}
            className="!rounded-lg !border !bg-card !shadow-md [&>button]:!border-b [&>button]:!bg-card [&>button]:!text-foreground hover:[&>button]:!bg-muted"
          />
          <MiniMap
            nodeStrokeWidth={3}
            nodeColor={(n: Node) => {
              const nt =
                (n.data as RepoNodeData | undefined)?.nodeType ?? "unknown";
              return NODE_TYPE_COLORS[nt]?.dot ?? "#6b7280";
            }}
            className="!rounded-lg !border !bg-card !shadow-md"
          />

          {/* Legend */}
          <Panel position="top-left" className="!m-3">
            <div className="flex flex-wrap gap-3 rounded-lg border bg-card/90 px-3 py-2 text-[10px] shadow-sm backdrop-blur">
              {Object.entries(NODE_TYPE_COLORS)
                .filter(([k]) => k !== "unknown")
                .map(([type, c]) => (
                  <div key={type} className="flex items-center gap-1.5">
                    <div
                      className="h-2.5 w-2.5 rounded-full"
                      style={{ backgroundColor: c.dot }}
                    />
                    <span className="capitalize text-muted-foreground">
                      {type}
                    </span>
                  </div>
                ))}
            </div>
          </Panel>
        </ReactFlow>
      </ReactFlowProvider>

      {/* Edge Detail Flyout */}
      {selectedEdge && (
        <EdgeDetailPanel
          edge={selectedEdge}
          nodes={graphData.nodes ?? []}
          onClose={() => setSelectedEdge(null)}
        />
      )}
    </div>
  );
}

// ── Main Component ──────────────────────────────────────────────────────────

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
          <div className="flex flex-col items-center gap-3 py-12 text-center">
            <div className="rounded-full bg-muted p-4">
              <Network className="h-10 w-10 text-muted-foreground" />
            </div>
            <div>
              <p className="text-sm font-medium">No graph data yet</p>
              <p className="mt-1 text-xs text-muted-foreground">
                Click &quot;Build Graph&quot; to scan all linked repos and
                generate the org-level dependency graph.
              </p>
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            {/* Summary stats */}
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <StatCard
                icon={<Package className="h-4 w-4 text-blue-500" />}
                label="Repos Scanned"
                value={graph.repos_scanned}
              />
              <StatCard
                icon={<CircleDot className="h-4 w-4 text-green-500" />}
                label="Nodes"
                value={graph.total_nodes}
              />
              <StatCard
                icon={<GitFork className="h-4 w-4 text-purple-500" />}
                label="Edges"
                value={graph.total_edges}
              />
              <StatCard
                icon={<Clock className="h-4 w-4 text-orange-500" />}
                label="Duration"
                value={`${(graph.duration / 1e6).toFixed(0)}ms`}
              />
            </div>

            {/* Tabbed view: Visual | Table */}
            <Tabs defaultValue="visual">
              <TabsList className="w-full justify-start">
                <TabsTrigger value="visual" className="gap-1.5">
                  <Network className="h-3.5 w-3.5" />
                  Visual Graph
                </TabsTrigger>
                <TabsTrigger value="table" className="gap-1.5">
                  <Table2 className="h-3.5 w-3.5" />
                  Table View
                </TabsTrigger>
              </TabsList>

              {/* ── Visual (React Flow) ──────────────────────────────────── */}
              <TabsContent value="visual" className="mt-4">
                {(graph.nodes?.length ?? 0) === 0 ? (
                  <div className="flex flex-col items-center gap-2 py-8 text-center">
                    <Info className="h-6 w-6 text-muted-foreground" />
                    <p className="text-sm text-muted-foreground">
                      The graph has no nodes. Link more repos and rebuild.
                    </p>
                  </div>
                ) : (
                  <VisualGraph graphData={graph} loading={loading} />
                )}
              </TabsContent>

              {/* ── Table Fallback ────────────────────────────────────────── */}
              <TabsContent value="table" className="mt-4 space-y-6">
                {/* Nodes Table */}
                {(graph.nodes?.length ?? 0) > 0 && (
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
                                className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${TABLE_NODE_TYPE_COLORS[node.node_type] ?? TABLE_NODE_TYPE_COLORS.unknown}`}
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

                {/* Edges Table */}
                {(graph.edges?.length ?? 0) > 0 && (
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
                        {graph.edges.map((edge: DepGraphEdge, idx: number) => {
                          const sourceNode = graph.nodes?.find(
                            (n: DepGraphNode) => n.id === edge.source_node_id,
                          );
                          const targetNode = graph.nodes?.find(
                            (n: DepGraphNode) => n.id === edge.target_node_id,
                          );
                          return (
                            <TableRow
                              key={`${edge.source_node_id}-${edge.target_node_id}-${idx}`}
                            >
                              <TableCell>
                                <div className="flex items-center gap-2 text-sm">
                                  <span className="font-medium">
                                    {sourceNode?.label ??
                                      edge.source_node_id.substring(0, 8)}
                                  </span>
                                  <ArrowRight className="h-3 w-3 text-muted-foreground" />
                                  <span className="font-medium">
                                    {targetNode?.label ??
                                      edge.target_node_id.substring(0, 8)}
                                  </span>
                                </div>
                              </TableCell>
                              <TableCell>
                                <Badge variant="outline">
                                  {edge.edge_type}
                                </Badge>
                              </TableCell>
                              <TableCell className="text-muted-foreground">
                                {edge.weight.toFixed(2)}
                              </TableCell>
                            </TableRow>
                          );
                        })}
                      </TableBody>
                    </Table>
                  </div>
                )}
              </TabsContent>
            </Tabs>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ── Stat Card ───────────────────────────────────────────────────────────────

function StatCard({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: string | number;
}) {
  return (
    <div className="flex items-center gap-3 rounded-lg border bg-card p-3">
      <div className="rounded-md bg-muted p-2">{icon}</div>
      <div>
        <p className="text-lg font-bold leading-none">{value}</p>
        <p className="mt-0.5 text-[10px] text-muted-foreground">{label}</p>
      </div>
    </div>
  );
}
