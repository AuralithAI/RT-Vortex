"use client";

import { useMemo } from "react";
import {
  FileCode,
  FunctionSquare,
  Box,
  Layers,
  X,
  ArrowRight,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { KGNode, KGEdge } from "@/types/api";

const NODE_META: Record<
  string,
  { icon: typeof FileCode; label: string; accent: string; dot: string }
> = {
  file: { icon: FileCode, label: "File", accent: "text-blue-400", dot: "#3b82f6" },
  file_summary: { icon: FileCode, label: "File", accent: "text-blue-400", dot: "#3b82f6" },
  function: { icon: FunctionSquare, label: "Function", accent: "text-amber-400", dot: "#f59e0b" },
  class: { icon: Box, label: "Class", accent: "text-violet-400", dot: "#8b5cf6" },
  module: { icon: Layers, label: "Module", accent: "text-emerald-400", dot: "#10b981" },
};

const FALLBACK_META = { icon: FileCode, label: "Node", accent: "text-gray-400", dot: "#64748b" };

const EDGE_COLORS: Record<string, string> = {
  IMPORTS: "#3b82f6",
  REFERENCES: "#f97316",
  CONTAINS: "#8b5cf6",
  CALLS: "#ec4899",
  INHERITS: "#10b981",
  IMPLEMENTS: "#06b6d4",
};

function meta(t: string) {
  return NODE_META[t.toLowerCase()] ?? FALLBACK_META;
}

function shortName(name: string, max = 40) {
  return name.length > max ? "…" + name.slice(-(max - 1)) : name;
}

interface GraphTooltipProps {
  node: KGNode;
  x: number;
  y: number;
  inDegree: number;
  outDegree: number;
}

export function GraphTooltip({ node, x, y, inDegree, outDegree }: GraphTooltipProps) {
  const m = meta(node.node_type);
  const Icon = m.icon;
  const rawName = node.name || node.file_path?.split("/").pop() || node.id;
  const label = useMemo(() => shortName(rawName), [rawName]);

  return (
    <div
      className="pointer-events-none absolute z-30 max-w-[280px] rounded-lg border bg-background/95 p-2.5 shadow-lg backdrop-blur-sm"
      style={{ left: x + 14, top: y + 14 }}
    >
      <div className="mb-1 flex items-center gap-1.5">
        <Icon className={`h-3.5 w-3.5 shrink-0 ${m.accent}`} />
        <span className={`text-xs font-semibold ${m.accent} truncate`} title={node.file_path || node.name}>
          {label}
        </span>
      </div>
      <div className="flex flex-wrap items-center gap-1.5 text-[10px] text-muted-foreground">
        <Badge variant="outline" className="h-4 px-1 text-[9px]">{m.label}</Badge>
        {node.language && <span>{node.language}</span>}
        <span>↑{inDegree} ↓{outDegree}</span>
        <span className="text-muted-foreground/60">{inDegree + outDegree} edges</span>
      </div>
      {node.file_path && (
        <p className="mt-1 truncate text-[9px] text-muted-foreground/80" title={node.file_path}>
          📄 {node.file_path}
        </p>
      )}
    </div>
  );
}

interface SelectionPanelProps {
  node: KGNode;
  inEdges: KGEdge[];
  outEdges: KGEdge[];
  nodeMap: Map<string, KGNode>;
  onClose: () => void;
  onNavigate: (nodeId: string) => void;
}

export function SelectionPanel({ node, inEdges, outEdges, nodeMap, onClose, onNavigate }: SelectionPanelProps) {
  const m = meta(node.node_type);
  const Icon = m.icon;

  return (
    <div className="absolute right-3 top-3 z-20 w-80 rounded-lg border bg-background/95 p-3 shadow-xl backdrop-blur-sm animate-in slide-in-from-right-2 duration-200">
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-1.5 min-w-0">
          <Icon className={`h-4 w-4 shrink-0 ${m.accent}`} />
          <h4 className="text-xs font-semibold truncate">{node.name || node.file_path?.split("/").pop() || node.id}</h4>
        </div>
        <button onClick={onClose} className="rounded-sm p-0.5 hover:bg-muted shrink-0">
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="mb-2 space-y-1 text-[11px] text-muted-foreground">
        {node.file_path && (
          <p className="truncate" title={node.file_path}>📄 {node.file_path}</p>
        )}
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="h-4 px-1 text-[9px]">{node.node_type}</Badge>
          {node.language && <span>{node.language}</span>}
        </div>
      </div>

      {inEdges.length > 0 && (
        <EdgeList
          label="← Imported by"
          edges={inEdges}
          getNodeId={(e) => e.src_id}
          nodeMap={nodeMap}
          onNavigate={onNavigate}
        />
      )}
      {outEdges.length > 0 && (
        <EdgeList
          label="→ Depends on"
          edges={outEdges}
          getNodeId={(e) => e.dst_id}
          nodeMap={nodeMap}
          onNavigate={onNavigate}
        />
      )}
    </div>
  );
}

function EdgeList({
  label,
  edges,
  getNodeId,
  nodeMap,
  onNavigate,
}: {
  label: string;
  edges: KGEdge[];
  getNodeId: (e: KGEdge) => string;
  nodeMap: Map<string, KGNode>;
  onNavigate: (id: string) => void;
}) {
  const CAP = 20;
  return (
    <div className="mb-2">
      <p className="mb-1 text-[10px] font-medium text-muted-foreground">
        {label} ({edges.length})
      </p>
      <div className="max-h-28 space-y-0.5 overflow-y-auto">
        {edges.slice(0, CAP).map((e, i) => {
          const nId = getNodeId(e);
          const n = nodeMap.get(nId);
          return (
            <button
              key={i}
              onClick={() => onNavigate(nId)}
              className="flex w-full items-center gap-1 rounded bg-muted/40 px-2 py-0.5 text-[10px] text-left hover:bg-muted/70 transition-colors"
            >
              <Badge
                variant="outline"
                className="h-3.5 px-1 text-[8px] shrink-0"
                style={{ color: EDGE_COLORS[e.edge_type.toUpperCase()] ?? "#94a3b8" }}
              >
                {e.edge_type}
              </Badge>
              <ArrowRight className="h-2.5 w-2.5 shrink-0 text-muted-foreground/50" />
              <span className="truncate">{n?.name || n?.file_path?.split("/").pop() || nId.slice(0, 12)}</span>
            </button>
          );
        })}
        {edges.length > CAP && (
          <p className="text-[9px] text-muted-foreground">… and {edges.length - CAP} more</p>
        )}
      </div>
    </div>
  );
}
