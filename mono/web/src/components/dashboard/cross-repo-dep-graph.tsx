// ─── Cross-Repo Dependency Graph (Repo-level) ───────────────────────────────
// Interactive React Flow visualization of cross-repo dependency edges
// scoped to a single repository. Transforms CrossRepoDependency[] data
// into a node-per-repo graph with symbol-level edge detail.
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
  type EdgeMouseHandler,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import {
  Network,
  X,
  ArrowRight,
  FileCode,
  GitFork,
  Info,
  Play,
  Loader2,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useCrossRepoDeps, queryKeys } from "@/lib/api/queries";
import { useBuildGraph } from "@/lib/api/mutations";
import { useUIStore } from "@/lib/stores/ui";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import type { CrossRepoDependency } from "@/types/api";

// ── Props ───────────────────────────────────────────────────────────────────

interface CrossRepoDepGraphProps {
  repoId: string;
  /** orgId is required to trigger graph builds from the repo page */
  orgId?: string;
}

// ── Node data shape ─────────────────────────────────────────────────────────

interface DepNodeData {
  repoId: string;
  label: string;
  /** Number of symbols exported / imported through this node */
  symbolCount: number;
  /** "source" = current repo, "target" = linked repo */
  role: "source" | "target";
  /** Unique files referenced */
  files: string[];
  [key: string]: unknown;
}

// ── Constants ───────────────────────────────────────────────────────────────

const ROLE_COLORS = {
  source: {
    bg: "bg-blue-50 dark:bg-blue-950/40",
    border: "border-blue-300 dark:border-blue-700",
    text: "text-blue-700 dark:text-blue-300",
    dot: "#3b82f6",
    mini: "rgb(59,130,246)",
  },
  target: {
    bg: "bg-emerald-50 dark:bg-emerald-950/40",
    border: "border-emerald-300 dark:border-emerald-700",
    text: "text-emerald-700 dark:text-emerald-300",
    dot: "#10b981",
    mini: "rgb(16,185,129)",
  },
} as const;

const DEP_TYPE_COLORS: Record<string, string> = {
  import: "#3b82f6",
  dependency: "#8b5cf6",
  api: "#f97316",
  artifact: "#ec4899",
  default: "#64748b",
};

// ── Custom Node ─────────────────────────────────────────────────────────────

function DepRepoNode({ data }: NodeProps<Node<DepNodeData>>) {
  const role = (data.role as "source" | "target") ?? "target";
  const colors = ROLE_COLORS[role];

  return (
    <div
      className={`rounded-lg border-2 ${colors.border} ${colors.bg} px-4 py-3 shadow-sm transition-shadow hover:shadow-md`}
      style={{ minWidth: 180 }}
    >
      <Handle type="target" position={Position.Left} className="!bg-muted-foreground/50" />
      <Handle type="source" position={Position.Right} className="!bg-muted-foreground/50" />

      {/* Title */}
      <div className="flex items-center gap-2">
        <span
          className="inline-block h-2.5 w-2.5 rounded-full"
          style={{ background: colors.dot }}
        />
        <span className={`text-sm font-semibold ${colors.text} truncate max-w-[160px]`}>
          {data.label}
        </span>
      </div>

      {/* Stats row */}
      <div className="mt-1.5 flex items-center gap-2 text-[10px] text-muted-foreground">
        <Badge variant="outline" className="h-4 px-1 text-[10px]">
          {data.role === "source" ? "source" : "target"}
        </Badge>
        <span>{data.symbolCount} symbols</span>
        <span>·</span>
        <span>{data.files.length} files</span>
      </div>
    </div>
  );
}

const nodeTypes = { depRepo: DepRepoNode };

// ── Edge Detail Panel ───────────────────────────────────────────────────────

interface SelectedEdgeInfo {
  sourceLabel: string;
  targetLabel: string;
  deps: CrossRepoDependency[];
}

function EdgeDetailPanel({
  info,
  onClose,
}: {
  info: SelectedEdgeInfo;
  onClose: () => void;
}) {
  return (
    <div className="absolute right-3 top-3 z-20 w-80 rounded-lg border bg-background/95 p-3 shadow-lg backdrop-blur-sm">
      <div className="mb-2 flex items-center justify-between">
        <h4 className="text-xs font-semibold">
          {info.sourceLabel} → {info.targetLabel}
        </h4>
        <button
          onClick={onClose}
          className="rounded-sm p-0.5 hover:bg-muted"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>
      <p className="mb-2 text-[10px] text-muted-foreground">
        {info.deps.length} dependency edge{info.deps.length !== 1 ? "s" : ""}
      </p>
      <div className="max-h-56 space-y-1.5 overflow-y-auto">
        {info.deps.map((dep, i) => (
          <div
            key={i}
            className="rounded border bg-muted/40 px-2 py-1.5 text-[11px]"
          >
            <div className="flex items-center gap-1">
              <FileCode className="h-3 w-3 shrink-0 text-muted-foreground" />
              <span className="font-medium truncate">{dep.source_symbol}</span>
              <ArrowRight className="h-3 w-3 shrink-0 text-muted-foreground" />
              <span className="font-medium truncate">{dep.target_symbol}</span>
            </div>
            <div className="mt-0.5 flex items-center gap-2 text-muted-foreground">
              <Badge variant="outline" className="h-4 px-1 text-[9px]">
                {dep.dependency_type}
              </Badge>
              <span>{Math.round(dep.confidence * 100)}% conf</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Layout helper ───────────────────────────────────────────────────────────

function buildGraph(
  deps: CrossRepoDependency[],
  currentRepoId: string,
): { nodes: Node<DepNodeData>[]; edges: Edge[] } {
  // Aggregate by repo — group all deps per unique repo pair
  const repoMap = new Map<
    string,
    {
      repoId: string;
      label: string;
      role: "source" | "target";
      symbols: Set<string>;
      files: Set<string>;
    }
  >();

  const edgeMap = new Map<string, CrossRepoDependency[]>();

  for (const dep of deps) {
    // Source repo node
    const srcId = dep.source_repo_id;
    if (!repoMap.has(srcId)) {
      repoMap.set(srcId, {
        repoId: srcId,
        label: srcId === currentRepoId ? "This Repo" : `Repo ${srcId.slice(0, 8)}`,
        role: srcId === currentRepoId ? "source" : "target",
        symbols: new Set(),
        files: new Set(),
      });
    }
    repoMap.get(srcId)!.symbols.add(dep.source_symbol);
    repoMap.get(srcId)!.files.add(dep.source_file);

    // Target repo node
    const tgtId = dep.target_repo_id;
    if (!repoMap.has(tgtId)) {
      repoMap.set(tgtId, {
        repoId: tgtId,
        label: tgtId === currentRepoId ? "This Repo" : `Repo ${tgtId.slice(0, 8)}`,
        role: tgtId === currentRepoId ? "source" : "target",
        symbols: new Set(),
        files: new Set(),
      });
    }
    repoMap.get(tgtId)!.symbols.add(dep.target_symbol);
    repoMap.get(tgtId)!.files.add(dep.target_file);

    // Edge aggregation
    const edgeKey = `${srcId}→${tgtId}`;
    if (!edgeMap.has(edgeKey)) edgeMap.set(edgeKey, []);
    edgeMap.get(edgeKey)!.push(dep);
  }

  // Separate source (left) and target (right) nodes
  const sources = [...repoMap.values()].filter((n) => n.role === "source");
  const targets = [...repoMap.values()].filter((n) => n.role === "target");

  const X_SOURCE = 50;
  const X_TARGET = 400;
  const Y_GAP = 110;

  const nodes: Node<DepNodeData>[] = [];

  sources.forEach((s, i) => {
    nodes.push({
      id: s.repoId,
      type: "depRepo",
      position: { x: X_SOURCE, y: i * Y_GAP + 30 },
      data: {
        repoId: s.repoId,
        label: s.label,
        symbolCount: s.symbols.size,
        role: s.role,
        files: [...s.files],
      },
    });
  });

  targets.forEach((t, i) => {
    nodes.push({
      id: t.repoId,
      type: "depRepo",
      position: { x: X_TARGET, y: i * Y_GAP + 30 },
      data: {
        repoId: t.repoId,
        label: t.label,
        symbolCount: t.symbols.size,
        role: t.role,
        files: [...t.files],
      },
    });
  });

  const edges: Edge[] = [...edgeMap.entries()].map(([key, depList]) => {
    const [srcId, tgtId] = key.split("→");
    const mainType = depList[0]?.dependency_type ?? "default";
    const color = DEP_TYPE_COLORS[mainType] ?? DEP_TYPE_COLORS.default;
    return {
      id: key,
      source: srcId,
      target: tgtId,
      animated: true,
      style: {
        stroke: color,
        strokeWidth: Math.min(1 + depList.length * 0.5, 5),
      },
      markerEnd: { type: MarkerType.ArrowClosed, color },
      label: `${depList.length}`,
      labelStyle: { fontSize: 10, fill: color },
      data: { deps: depList },
    };
  });

  return { nodes, edges };
}

// ── Main Component ──────────────────────────────────────────────────────────

function DepGraphCanvas({
  deps,
  repoId,
}: {
  deps: CrossRepoDependency[];
  repoId: string;
}) {
  const { nodes: initNodes, edges: initEdges } = useMemo(
    () => buildGraph(deps, repoId),
    [deps, repoId],
  );

  const [nodes, setNodes, onNodesChange] = useNodesState(initNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initEdges);
  const [selectedEdge, setSelectedEdge] = useState<SelectedEdgeInfo | null>(
    null,
  );

  // Sync when deps change
  useEffect(() => {
    const { nodes: n, edges: e } = buildGraph(deps, repoId);
    setNodes(n);
    setEdges(e);
    setSelectedEdge(null);
  }, [deps, repoId, setNodes, setEdges]);

  const onEdgeClick: EdgeMouseHandler = useCallback(
    (_evt: React.MouseEvent, edge: Edge) => {
      const srcNode = nodes.find((n: Node<DepNodeData>) => n.id === edge.source);
      const tgtNode = nodes.find((n: Node<DepNodeData>) => n.id === edge.target);
      const depList =
        (edge.data as { deps?: CrossRepoDependency[] })?.deps ?? [];
      setSelectedEdge({
        sourceLabel: (srcNode?.data as DepNodeData)?.label ?? edge.source,
        targetLabel: (tgtNode?.data as DepNodeData)?.label ?? edge.target,
        deps: depList,
      });
    },
    [nodes],
  );

  return (
    <div className="relative h-[420px] w-full rounded-lg border bg-background">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onEdgeClick={onEdgeClick}
        onPaneClick={() => setSelectedEdge(null)}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.3 }}
        proOptions={{ hideAttribution: true }}
        minZoom={0.3}
        maxZoom={2}
      >
        <Background gap={16} size={1} />
        <Controls showInteractive={false} />
        <MiniMap
          nodeColor={(n: Node) => {
            const role = (n.data as DepNodeData)?.role ?? "target";
            return ROLE_COLORS[role as "source" | "target"]?.mini ?? "#94a3b8";
          }}
          maskColor="rgba(0,0,0,0.08)"
          className="!bottom-2 !right-2"
        />
        <Panel position="top-left">
          <div className="flex items-center gap-3 rounded-md border bg-background/80 px-3 py-1.5 text-[10px] backdrop-blur-sm">
            <span className="flex items-center gap-1">
              <span
                className="inline-block h-2 w-2 rounded-full"
                style={{ background: ROLE_COLORS.source.dot }}
              />
              Source (this repo)
            </span>
            <span className="flex items-center gap-1">
              <span
                className="inline-block h-2 w-2 rounded-full"
                style={{ background: ROLE_COLORS.target.dot }}
              />
              Linked repos
            </span>
            <span className="text-muted-foreground">
              Click an edge for details
            </span>
          </div>
        </Panel>
      </ReactFlow>
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

export function CrossRepoDepGraph({ repoId, orgId }: CrossRepoDepGraphProps) {
  const { data: deps, isLoading } = useCrossRepoDeps(repoId);
  const buildGraphMut = useBuildGraph();
  const { addToast } = useUIStore();
  const qc = useQueryClient();
  const [building, setBuilding] = useState(false);

  const hasDeps = (deps?.dependencies?.length ?? 0) > 0;

  const handleBuild = async (forceRescan = false) => {
    if (!orgId) return;
    setBuilding(true);
    try {
      const result = await buildGraphMut.mutateAsync({
        orgId,
        data: { force_rescan: forceRescan },
      });
      // Refresh repo-level deps after org graph is built
      qc.invalidateQueries({ queryKey: queryKeys.crossRepoDeps(repoId) });
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
      setBuilding(false);
    }
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="flex items-center gap-2 text-base">
          <Network className="h-4 w-4" />
          Dependency Graph
          {deps && hasDeps && (
            <Badge variant="secondary" className="ml-1 text-xs">
              {deps.total_edges} edges · {deps.repos_authorized} repos
            </Badge>
          )}
        </CardTitle>
        {orgId && (
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => handleBuild(true)}
              disabled={building}
            >
              {building ? (
                <Loader2 className="mr-1 h-4 w-4 animate-spin" />
              ) : (
                <Play className="mr-1 h-4 w-4" />
              )}
              Force Rescan
            </Button>
            <Button size="sm" onClick={() => handleBuild()} disabled={building}>
              {building ? (
                <Loader2 className="mr-1 h-4 w-4 animate-spin" />
              ) : (
                <Play className="mr-1 h-4 w-4" />
              )}
              Build Graph
            </Button>
          </div>
        )}
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <Skeleton className="h-[420px] w-full rounded-lg" />
        ) : !hasDeps ? (
          <div className="flex flex-col items-center gap-3 py-12 text-center">
            <GitFork className="h-8 w-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">
              No cross-repo dependencies detected yet.
              {orgId
                ? " Link repos above, then click Build Graph to discover dependency edges."
                : " Link repos and re-build the org graph to discover dependency edges."}
            </p>
            {orgId && (
              <Button
                size="sm"
                onClick={() => handleBuild()}
                disabled={building}
              >
                {building ? (
                  <Loader2 className="mr-1 h-4 w-4 animate-spin" />
                ) : (
                  <Play className="mr-1 h-4 w-4" />
                )}
                Build Graph
              </Button>
            )}
          </div>
        ) : (
          <ReactFlowProvider>
            <DepGraphCanvas deps={deps!.dependencies} repoId={repoId} />
            {deps!.repos_denied > 0 && (
              <p className="mt-2 flex items-center gap-1 text-xs text-orange-600">
                <Info className="h-3 w-3" />
                {deps!.repos_denied} linked repo(s) not accessible — some
                edges may be missing.
              </p>
            )}
          </ReactFlowProvider>
        )}
      </CardContent>
    </Card>
  );
}
