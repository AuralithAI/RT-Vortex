// ─── Intra-Repo File Map (Knowledge Graph Visualization) ────────────────────
// Interactive React Flow graph showing how files within a single repo
// depend on each other — imports, references, containment edges from the
// engine's knowledge graph. Sits above the Cross-Repo section on the
// repo detail page and only renders after the repo is indexed.
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
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import {
  FileCode,
  FunctionSquare,
  Box,
  Layers,
  X,
  ArrowRight,
  Loader2,
  Share2,
  ChevronDown,
  Filter,
} from "lucide-react";
import { useRepoFileMap } from "@/lib/api/queries";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuCheckboxItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { KGNode, KGEdge } from "@/types/api";

// ── Props ───────────────────────────────────────────────────────────────────

interface IntraRepoFileMapProps {
  repoId: string;
}

// ── Node-type styling ───────────────────────────────────────────────────────

const NODE_TYPE_STYLES: Record<
  string,
  {
    bg: string;
    border: string;
    text: string;
    dot: string;
    mini: string;
    icon: typeof FileCode;
    label: string;
  }
> = {
  file: {
    bg: "bg-blue-50 dark:bg-blue-950/40",
    border: "border-blue-300 dark:border-blue-700",
    text: "text-blue-700 dark:text-blue-300",
    dot: "#3b82f6",
    mini: "rgb(59,130,246)",
    icon: FileCode,
    label: "File",
  },
  function: {
    bg: "bg-amber-50 dark:bg-amber-950/40",
    border: "border-amber-300 dark:border-amber-700",
    text: "text-amber-700 dark:text-amber-300",
    dot: "#f59e0b",
    mini: "rgb(245,158,11)",
    icon: FunctionSquare,
    label: "Function",
  },
  class: {
    bg: "bg-violet-50 dark:bg-violet-950/40",
    border: "border-violet-300 dark:border-violet-700",
    text: "text-violet-700 dark:text-violet-300",
    dot: "#8b5cf6",
    mini: "rgb(139,92,246)",
    icon: Box,
    label: "Class",
  },
  module: {
    bg: "bg-emerald-50 dark:bg-emerald-950/40",
    border: "border-emerald-300 dark:border-emerald-700",
    text: "text-emerald-700 dark:text-emerald-300",
    dot: "#10b981",
    mini: "rgb(16,185,129)",
    icon: Layers,
    label: "Module",
  },
};

const DEFAULT_STYLE = {
  bg: "bg-gray-50 dark:bg-gray-950/40",
  border: "border-gray-300 dark:border-gray-700",
  text: "text-gray-700 dark:text-gray-300",
  dot: "#64748b",
  mini: "rgb(100,116,139)",
  icon: FileCode,
  label: "Node",
};

function getNodeStyle(nodeType: string) {
  return NODE_TYPE_STYLES[nodeType.toLowerCase()] ?? DEFAULT_STYLE;
}

// ── Edge-type colors ────────────────────────────────────────────────────────

const EDGE_TYPE_COLORS: Record<string, string> = {
  IMPORTS: "#3b82f6",
  REFERENCES: "#f97316",
  CONTAINS: "#8b5cf6",
  CALLS: "#ec4899",
  INHERITS: "#10b981",
  IMPLEMENTS: "#06b6d4",
};

const DEFAULT_EDGE_COLOR = "#94a3b8";

function getEdgeColor(edgeType: string): string {
  return EDGE_TYPE_COLORS[edgeType.toUpperCase()] ?? DEFAULT_EDGE_COLOR;
}

// ── Custom Node Data ────────────────────────────────────────────────────────

interface FileNodeData {
  kgId: string;
  name: string;
  filePath: string;
  nodeType: string;
  language: string;
  metadata: string;
  /** Number of outgoing edges */
  outDegree: number;
  /** Number of incoming edges */
  inDegree: number;
  [key: string]: unknown;
}

// ── Custom Node Component ───────────────────────────────────────────────────

function FileNode({ data }: NodeProps<Node<FileNodeData>>) {
  const style = getNodeStyle(data.nodeType);
  const Icon = style.icon;
  const shortName =
    data.name.length > 28 ? "…" + data.name.slice(-27) : data.name;

  return (
    <div
      className={`rounded-lg border-2 ${style.border} ${style.bg} px-3 py-2 shadow-sm transition-shadow hover:shadow-md`}
      style={{ minWidth: 160, maxWidth: 260 }}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="!bg-muted-foreground/50"
      />
      <Handle
        type="source"
        position={Position.Right}
        className="!bg-muted-foreground/50"
      />

      {/* Title row */}
      <div className="flex items-center gap-1.5">
        <Icon className={`h-3.5 w-3.5 shrink-0 ${style.text}`} />
        <span
          className={`text-xs font-semibold ${style.text} truncate`}
          title={data.filePath || data.name}
        >
          {shortName}
        </span>
      </div>

      {/* Stats row */}
      <div className="mt-1 flex items-center gap-2 text-[10px] text-muted-foreground">
        <Badge variant="outline" className="h-4 px-1 text-[9px]">
          {style.label}
        </Badge>
        {data.language && (
          <span className="truncate">{data.language}</span>
        )}
        <span>{data.inDegree + data.outDegree} edges</span>
      </div>
    </div>
  );
}

const nodeTypes = { fileNode: FileNode };

// ── Detail Panels ───────────────────────────────────────────────────────────

interface SelectedNodeInfo {
  node: KGNode;
  inEdges: KGEdge[];
  outEdges: KGEdge[];
  nodeMap: Map<string, KGNode>;
}

interface SelectedEdgeInfo {
  edge: KGEdge;
  srcNode?: KGNode;
  dstNode?: KGNode;
}

function NodeDetailPanel({
  info,
  onClose,
}: {
  info: SelectedNodeInfo;
  onClose: () => void;
}) {
  const style = getNodeStyle(info.node.node_type);
  const Icon = style.icon;

  return (
    <div className="absolute right-3 top-3 z-20 w-80 rounded-lg border bg-background/95 p-3 shadow-lg backdrop-blur-sm">
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <Icon className={`h-4 w-4 ${style.text}`} />
          <h4 className="text-xs font-semibold truncate max-w-[220px]">
            {info.node.name}
          </h4>
        </div>
        <button
          onClick={onClose}
          className="rounded-sm p-0.5 hover:bg-muted"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="mb-2 space-y-1 text-[11px] text-muted-foreground">
        {info.node.file_path && (
          <p className="truncate" title={info.node.file_path}>
            📄 {info.node.file_path}
          </p>
        )}
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="h-4 px-1 text-[9px]">
            {info.node.node_type}
          </Badge>
          {info.node.language && (
            <span>{info.node.language}</span>
          )}
        </div>
      </div>

      {/* Incoming edges */}
      {info.inEdges.length > 0 && (
        <div className="mb-2">
          <p className="mb-1 text-[10px] font-medium text-muted-foreground">
            ← Imported by ({info.inEdges.length})
          </p>
          <div className="max-h-24 space-y-0.5 overflow-y-auto">
            {info.inEdges.slice(0, 20).map((e, i) => {
              const src = info.nodeMap.get(e.src_id);
              return (
                <div
                  key={i}
                  className="flex items-center gap-1 rounded bg-muted/40 px-2 py-0.5 text-[10px]"
                >
                  <Badge
                    variant="outline"
                    className="h-3.5 px-1 text-[8px]"
                    style={{ color: getEdgeColor(e.edge_type) }}
                  >
                    {e.edge_type}
                  </Badge>
                  <span className="truncate">
                    {src?.name ?? e.src_id.slice(0, 12)}
                  </span>
                </div>
              );
            })}
            {info.inEdges.length > 20 && (
              <p className="text-[9px] text-muted-foreground">
                … and {info.inEdges.length - 20} more
              </p>
            )}
          </div>
        </div>
      )}

      {/* Outgoing edges */}
      {info.outEdges.length > 0 && (
        <div>
          <p className="mb-1 text-[10px] font-medium text-muted-foreground">
            → Depends on ({info.outEdges.length})
          </p>
          <div className="max-h-24 space-y-0.5 overflow-y-auto">
            {info.outEdges.slice(0, 20).map((e, i) => {
              const dst = info.nodeMap.get(e.dst_id);
              return (
                <div
                  key={i}
                  className="flex items-center gap-1 rounded bg-muted/40 px-2 py-0.5 text-[10px]"
                >
                  <Badge
                    variant="outline"
                    className="h-3.5 px-1 text-[8px]"
                    style={{ color: getEdgeColor(e.edge_type) }}
                  >
                    {e.edge_type}
                  </Badge>
                  <span className="truncate">
                    {dst?.name ?? e.dst_id.slice(0, 12)}
                  </span>
                </div>
              );
            })}
            {info.outEdges.length > 20 && (
              <p className="text-[9px] text-muted-foreground">
                … and {info.outEdges.length - 20} more
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function EdgeDetailPanel({
  info,
  onClose,
}: {
  info: SelectedEdgeInfo;
  onClose: () => void;
}) {
  const color = getEdgeColor(info.edge.edge_type);

  return (
    <div className="absolute right-3 top-3 z-20 w-72 rounded-lg border bg-background/95 p-3 shadow-lg backdrop-blur-sm">
      <div className="mb-2 flex items-center justify-between">
        <h4 className="text-xs font-semibold">Edge Detail</h4>
        <button
          onClick={onClose}
          className="rounded-sm p-0.5 hover:bg-muted"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>
      <div className="space-y-1.5 text-[11px]">
        <div className="flex items-center gap-1">
          <FileCode className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="truncate font-medium">
            {info.srcNode?.name ?? info.edge.src_id.slice(0, 12)}
          </span>
          <ArrowRight className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="truncate font-medium">
            {info.dstNode?.name ?? info.edge.dst_id.slice(0, 12)}
          </span>
        </div>
        <div className="flex items-center gap-2 text-muted-foreground">
          <Badge
            variant="outline"
            className="h-4 px-1 text-[9px]"
            style={{ color }}
          >
            {info.edge.edge_type}
          </Badge>
          <span>weight: {info.edge.weight.toFixed(2)}</span>
        </div>
      </div>
    </div>
  );
}

// ── Layout helpers ──────────────────────────────────────────────────────────

/**
 * Simple layered layout — groups nodes by "depth" (distance from root nodes
 * with no incoming edges) and arranges them in columns. This avoids an
 * external dagre dependency while producing a readable left-to-right flow.
 */
function layoutGraph(
  kgNodes: KGNode[],
  kgEdges: KGEdge[],
  visibleNodeTypes: Set<string>,
  visibleEdgeTypes: Set<string>,
): { nodes: Node<FileNodeData>[]; edges: Edge[] } {
  // Filter nodes / edges by visibility
  const filteredNodes = kgNodes.filter((n) =>
    visibleNodeTypes.has(n.node_type.toLowerCase()),
  );
  const nodeIdSet = new Set(filteredNodes.map((n) => n.id));

  const filteredEdges = kgEdges.filter(
    (e) =>
      visibleEdgeTypes.has(e.edge_type.toUpperCase()) &&
      nodeIdSet.has(e.src_id) &&
      nodeIdSet.has(e.dst_id),
  );

  // Build adjacency lists
  const outEdgeMap = new Map<string, KGEdge[]>();
  const inEdgeMap = new Map<string, KGEdge[]>();
  for (const e of filteredEdges) {
    if (!outEdgeMap.has(e.src_id)) outEdgeMap.set(e.src_id, []);
    outEdgeMap.get(e.src_id)!.push(e);
    if (!inEdgeMap.has(e.dst_id)) inEdgeMap.set(e.dst_id, []);
    inEdgeMap.get(e.dst_id)!.push(e);
  }

  // BFS layering from root nodes (in-degree 0)
  const depth = new Map<string, number>();
  const roots = filteredNodes.filter(
    (n) => !inEdgeMap.has(n.id) || inEdgeMap.get(n.id)!.length === 0,
  );

  // If no clear roots, pick the first few nodes
  if (roots.length === 0 && filteredNodes.length > 0) {
    roots.push(...filteredNodes.slice(0, Math.min(5, filteredNodes.length)));
  }

  const queue: string[] = [];
  for (const r of roots) {
    depth.set(r.id, 0);
    queue.push(r.id);
  }

  while (queue.length > 0) {
    const id = queue.shift()!;
    const d = depth.get(id)!;
    const outEdges = outEdgeMap.get(id) ?? [];
    for (const e of outEdges) {
      if (!depth.has(e.dst_id)) {
        depth.set(e.dst_id, d + 1);
        queue.push(e.dst_id);
      }
    }
  }

  // Assign depth 0 to any remaining unvisited nodes
  for (const n of filteredNodes) {
    if (!depth.has(n.id)) depth.set(n.id, 0);
  }

  // Group by depth layer
  const layers = new Map<number, KGNode[]>();
  for (const n of filteredNodes) {
    const d = depth.get(n.id)!;
    if (!layers.has(d)) layers.set(d, []);
    layers.get(d)!.push(n);
  }

  const COL_GAP = 280;
  const ROW_GAP = 90;

  const nodes: Node<FileNodeData>[] = [];
  const sortedLayers = [...layers.keys()].sort((a, b) => a - b);

  for (const layerIdx of sortedLayers) {
    const layerNodes = layers.get(layerIdx)!;
    // Sort within layer by name for stability
    layerNodes.sort((a, b) => a.name.localeCompare(b.name));

    layerNodes.forEach((kgNode, rowIdx) => {
      const outDegree = (outEdgeMap.get(kgNode.id) ?? []).length;
      const inDegree = (inEdgeMap.get(kgNode.id) ?? []).length;

      nodes.push({
        id: kgNode.id,
        type: "fileNode",
        position: {
          x: layerIdx * COL_GAP + 40,
          y: rowIdx * ROW_GAP + 30,
        },
        data: {
          kgId: kgNode.id,
          name: kgNode.name,
          filePath: kgNode.file_path,
          nodeType: kgNode.node_type,
          language: kgNode.language,
          metadata: kgNode.metadata,
          outDegree,
          inDegree,
        },
      });
    });
  }

  // Edges
  const edges: Edge[] = filteredEdges.map((kgEdge) => {
    const color = getEdgeColor(kgEdge.edge_type);
    return {
      id: `e-${kgEdge.src_id}-${kgEdge.dst_id}-${kgEdge.edge_type}`,
      source: kgEdge.src_id,
      target: kgEdge.dst_id,
      animated: kgEdge.edge_type.toUpperCase() === "IMPORTS",
      style: {
        stroke: color,
        strokeWidth: Math.min(1 + kgEdge.weight * 0.5, 4),
      },
      markerEnd: { type: MarkerType.ArrowClosed, color },
      label: kgEdge.edge_type,
      labelStyle: { fontSize: 9, fill: color },
      data: { kgEdge },
    };
  });

  return { nodes, edges };
}

// ── Canvas ──────────────────────────────────────────────────────────────────

function FileMapCanvas({
  kgNodes,
  kgEdges,
}: {
  kgNodes: KGNode[];
  kgEdges: KGEdge[];
}) {
  // Discover available node / edge types
  const availableNodeTypes = useMemo(
    () => [...new Set(kgNodes.map((n) => n.node_type.toLowerCase()))].sort(),
    [kgNodes],
  );
  const availableEdgeTypes = useMemo(
    () => [...new Set(kgEdges.map((e) => e.edge_type.toUpperCase()))].sort(),
    [kgEdges],
  );

  // Visibility filters
  const [visibleNodeTypes, setVisibleNodeTypes] = useState<Set<string>>(
    () => new Set(availableNodeTypes),
  );
  const [visibleEdgeTypes, setVisibleEdgeTypes] = useState<Set<string>>(
    () => new Set(availableEdgeTypes),
  );

  // Build lookup map
  const nodeMap = useMemo(() => {
    const m = new Map<string, KGNode>();
    for (const n of kgNodes) m.set(n.id, n);
    return m;
  }, [kgNodes]);

  // Build edge lookup
  const edgeLookup = useMemo(() => {
    const out = new Map<string, KGEdge[]>();
    const inMap = new Map<string, KGEdge[]>();
    for (const e of kgEdges) {
      if (!out.has(e.src_id)) out.set(e.src_id, []);
      out.get(e.src_id)!.push(e);
      if (!inMap.has(e.dst_id)) inMap.set(e.dst_id, []);
      inMap.get(e.dst_id)!.push(e);
    }
    return { out, in: inMap };
  }, [kgEdges]);

  // Layout
  const { nodes: initNodes, edges: initEdges } = useMemo(
    () => layoutGraph(kgNodes, kgEdges, visibleNodeTypes, visibleEdgeTypes),
    [kgNodes, kgEdges, visibleNodeTypes, visibleEdgeTypes],
  );

  const [nodes, setNodes, onNodesChange] = useNodesState(initNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initEdges);
  const [selectedNode, setSelectedNode] = useState<SelectedNodeInfo | null>(
    null,
  );
  const [selectedEdge, setSelectedEdge] = useState<SelectedEdgeInfo | null>(
    null,
  );

  // Re-sync when filters change
  useEffect(() => {
    const { nodes: n, edges: e } = layoutGraph(
      kgNodes,
      kgEdges,
      visibleNodeTypes,
      visibleEdgeTypes,
    );
    setNodes(n);
    setEdges(e);
    setSelectedNode(null);
    setSelectedEdge(null);
  }, [kgNodes, kgEdges, visibleNodeTypes, visibleEdgeTypes, setNodes, setEdges]);

  // Handlers
  const onNodeClick = useCallback(
    (_evt: React.MouseEvent, node: Node) => {
      const kgNode = nodeMap.get(node.id);
      if (!kgNode) return;
      setSelectedEdge(null);
      setSelectedNode({
        node: kgNode,
        inEdges: edgeLookup.in.get(node.id) ?? [],
        outEdges: edgeLookup.out.get(node.id) ?? [],
        nodeMap,
      });
    },
    [nodeMap, edgeLookup],
  );

  const onEdgeClick = useCallback(
    (_evt: React.MouseEvent, edge: Edge) => {
      const kgEdge = (edge.data as { kgEdge?: KGEdge })?.kgEdge;
      if (!kgEdge) return;
      setSelectedNode(null);
      setSelectedEdge({
        edge: kgEdge,
        srcNode: nodeMap.get(kgEdge.src_id),
        dstNode: nodeMap.get(kgEdge.dst_id),
      });
    },
    [nodeMap],
  );

  const toggleNodeType = (t: string) => {
    setVisibleNodeTypes((prev) => {
      const next = new Set(prev);
      if (next.has(t)) next.delete(t);
      else next.add(t);
      return next;
    });
  };

  const toggleEdgeType = (t: string) => {
    setVisibleEdgeTypes((prev) => {
      const next = new Set(prev);
      if (next.has(t)) next.delete(t);
      else next.add(t);
      return next;
    });
  };

  return (
    <div className="relative h-[500px] w-full rounded-lg border bg-background">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
        onEdgeClick={onEdgeClick}
        onPaneClick={() => {
          setSelectedNode(null);
          setSelectedEdge(null);
        }}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.25 }}
        proOptions={{ hideAttribution: true }}
        minZoom={0.15}
        maxZoom={2.5}
      >
        <Background gap={20} size={1} />
        <Controls showInteractive={false} />
        <MiniMap
          nodeColor={(n: Node) => {
            const nt =
              (n.data as FileNodeData)?.nodeType?.toLowerCase() ?? "";
            return getNodeStyle(nt).mini;
          }}
          maskColor="rgba(0,0,0,0.08)"
          className="!bottom-2 !right-2"
        />

        {/* Legend panel */}
        <Panel position="top-left">
          <div className="flex flex-wrap items-center gap-3 rounded-md border bg-background/80 px-3 py-1.5 text-[10px] backdrop-blur-sm">
            {availableNodeTypes.map((nt) => {
              const s = getNodeStyle(nt);
              return (
                <span key={nt} className="flex items-center gap-1">
                  <span
                    className="inline-block h-2 w-2 rounded-full"
                    style={{ background: s.dot }}
                  />
                  {s.label}
                </span>
              );
            })}
            <span className="text-muted-foreground">
              Click a node or edge for details
            </span>
          </div>
        </Panel>

        {/* Filter dropdown */}
        <Panel position="top-right">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-[10px]"
              >
                <Filter className="mr-1 h-3 w-3" />
                Filter
                <ChevronDown className="ml-1 h-3 w-3" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuLabel className="text-[10px]">
                Node Types
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              {availableNodeTypes.map((nt) => (
                <DropdownMenuCheckboxItem
                  key={nt}
                  checked={visibleNodeTypes.has(nt)}
                  onCheckedChange={() => toggleNodeType(nt)}
                  className="text-xs"
                >
                  <span
                    className="mr-1.5 inline-block h-2 w-2 rounded-full"
                    style={{ background: getNodeStyle(nt).dot }}
                  />
                  {getNodeStyle(nt).label}
                </DropdownMenuCheckboxItem>
              ))}
              <DropdownMenuSeparator />
              <DropdownMenuLabel className="text-[10px]">
                Edge Types
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              {availableEdgeTypes.map((et) => (
                <DropdownMenuCheckboxItem
                  key={et}
                  checked={visibleEdgeTypes.has(et)}
                  onCheckedChange={() => toggleEdgeType(et)}
                  className="text-xs"
                >
                  <span
                    className="mr-1.5 inline-block h-2 w-2 rounded-full"
                    style={{ background: getEdgeColor(et) }}
                  />
                  {et}
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </Panel>
      </ReactFlow>

      {/* Detail overlays */}
      {selectedNode && (
        <NodeDetailPanel
          info={selectedNode}
          onClose={() => setSelectedNode(null)}
        />
      )}
      {selectedEdge && (
        <EdgeDetailPanel
          info={selectedEdge}
          onClose={() => setSelectedEdge(null)}
        />
      )}
    </div>
  );
}

// ── Exported Component ──────────────────────────────────────────────────────

export function IntraRepoFileMap({ repoId }: IntraRepoFileMapProps) {
  const { data, isLoading, error } = useRepoFileMap(repoId);

  const hasData =
    data && (data.nodes?.length > 0 || data.edges?.length > 0);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Share2 className="h-4 w-4" />
          Internal File Map
          {hasData && (
            <Badge variant="secondary" className="ml-1 text-xs">
              {data!.total_nodes} nodes · {data!.total_edges} edges
            </Badge>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <Skeleton className="h-[500px] w-full rounded-lg" />
        ) : error ? (
          <div className="flex flex-col items-center gap-3 py-12 text-center">
            <Share2 className="h-8 w-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">
              Failed to load file dependency map.{" "}
              {error instanceof Error ? error.message : ""}
            </p>
          </div>
        ) : !hasData ? (
          <div className="flex flex-col items-center gap-3 py-12 text-center">
            <Share2 className="h-8 w-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">
              No file dependency data available yet. Index the repository
              to generate the knowledge graph.
            </p>
          </div>
        ) : (
          <ReactFlowProvider>
            <FileMapCanvas
              kgNodes={data!.nodes}
              kgEdges={data!.edges}
            />
          </ReactFlowProvider>
        )}
      </CardContent>
    </Card>
  );
}
