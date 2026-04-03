"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Graph, PointShape } from "@cosmos.gl/graph";
import type { GraphConfigInterface } from "@cosmos.gl/graph";
import {
  AlertTriangle,
  Filter,
  ChevronDown,
  Maximize2,
  ZoomIn,
  ZoomOut,
  Search,
  X,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuCheckboxItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { KGNode, KGEdge } from "@/types/api";
import { GraphTooltip, SelectionPanel } from "./graph-tooltip";

const NODE_COLORS: Record<string, string> = {
  file: "#3b82f6",
  file_summary: "#3b82f6",
  function: "#f59e0b",
  class: "#8b5cf6",
  module: "#10b981",
};
const FALLBACK_NODE_COLOR = "#64748b";

// Language-based colors for file_summary nodes where language is the key differentiator.
const LANG_COLORS: Record<string, string> = {
  python: "#3572A5",
  javascript: "#f1e05a",
  typescript: "#3178c6",
  java: "#b07219",
  go: "#00ADD8",
  rust: "#dea584",
  c: "#555555",
  "c++": "#f34b7d",
  cpp: "#f34b7d",
  ruby: "#701516",
  php: "#4F5D95",
  swift: "#F05138",
  kotlin: "#A97BFF",
  bash: "#89e051",
  shell: "#89e051",
  yaml: "#cb171e",
  toml: "#9c4221",
  json: "#292929",
  markdown: "#083fa1",
  html: "#e34c26",
  css: "#563d7c",
  sql: "#e38c00",
  unknown: "#94a3b8",
};

const EDGE_COLORS: Record<string, string> = {
  IMPORTS: "#3b82f6",
  REFERENCES: "#f97316",
  CONTAINS: "#8b5cf6",
  CALLS: "#ec4899",
  INHERITS: "#10b981",
  IMPLEMENTS: "#06b6d4",
};
const FALLBACK_EDGE_COLOR = "#94a3b8";

const SHAPE_MAP: Record<string, number> = {
  file: PointShape.Circle,
  file_summary: PointShape.Circle,
  function: PointShape.Triangle,
  class: PointShape.Square,
  module: PointShape.Diamond,
};

function nColor(t: string, lang?: string) {
  // For file_summary nodes, color by programming language instead of node type.
  if ((t === "file_summary" || t === "file") && lang) {
    return LANG_COLORS[lang.toLowerCase()] ?? NODE_COLORS[t.toLowerCase()] ?? FALLBACK_NODE_COLOR;
  }
  return NODE_COLORS[t.toLowerCase()] ?? FALLBACK_NODE_COLOR;
}

function eColor(t: string) {
  return EDGE_COLORS[t.toUpperCase()] ?? FALLBACK_EDGE_COLOR;
}

function hexRGBA(hex: string, a = 1): [number, number, number, number] {
  // cosmos.gl v2 setPointColors/setLinkColors: RGB in 0–255, alpha in 0–1
  return [
    parseInt(hex.slice(1, 3), 16) / 255,
    parseInt(hex.slice(3, 5), 16) / 255,
    parseInt(hex.slice(5, 7), 16) / 255,
    a,
  ];
}

function isDark() {
  if (typeof document === "undefined") return false;
  return document.documentElement.classList.contains("dark");
}

export interface WebGLGraphCanvasProps {
  kgNodes: KGNode[];
  kgEdges: KGEdge[];
  truncated: boolean;
  totalNodes: number;
}

interface PreparedGraph {
  nodes: KGNode[];
  edges: KGEdge[];
  idToIndex: Map<string, number>;
  positions: Float32Array;
  pointColors: Float32Array;
  pointSizes: Float32Array;
  pointShapes: Float32Array;
  links: Float32Array;
  linkColors: Float32Array;
  linkWidths: Float32Array;
  clusters: (number | undefined)[];
  degree: Uint32Array;
  /** Directory name → list of node indices in that directory */
  dirGroups: Map<string, number[]>;
}

function prepareGraph(
  kgNodes: KGNode[],
  kgEdges: KGEdge[],
  visibleNodeTypes: Set<string>,
  visibleEdgeTypes: Set<string>,
): PreparedGraph {
  const nodes = kgNodes.filter((n) => visibleNodeTypes.has(n.node_type.toLowerCase()));
  const idToIndex = new Map<string, number>();
  nodes.forEach((n, i) => idToIndex.set(n.id, i));

  const edges = kgEdges.filter(
    (e) =>
      visibleEdgeTypes.has(e.edge_type.toUpperCase()) &&
      idToIndex.has(e.src_id) &&
      idToIndex.has(e.dst_id),
  );

  const degree = new Uint32Array(nodes.length);
  for (const e of edges) {
    degree[idToIndex.get(e.src_id)!]++;
    degree[idToIndex.get(e.dst_id)!]++;
  }
  const maxDeg = Math.max(1, ...degree);

  const dirOf = (fp: string) => {
    if (!fp) return "(root)";
    const idx = fp.lastIndexOf("/");
    return idx > 0 ? fp.slice(0, idx) : "(root)";
  };

  const dirGroups = new Map<string, number[]>();
  nodes.forEach((n, i) => {
    const d = dirOf(n.file_path);
    if (!dirGroups.has(d)) dirGroups.set(d, []);
    dirGroups.get(d)!.push(i);
  });

  const dirs = [...dirGroups.keys()].sort();
  const SPACE = 8192;
  const CX = SPACE / 2;
  const CY = SPACE / 2;
  const R = SPACE * 0.32;

  const positions = new Float32Array(nodes.length * 2);
  const clusters: (number | undefined)[] = new Array(nodes.length);
  const dirIndex = new Map<string, number>();
  dirs.forEach((d, i) => dirIndex.set(d, i));

  dirs.forEach((dir, di) => {
    const a = (2 * Math.PI * di) / Math.max(dirs.length, 1);
    const cx = CX + R * Math.cos(a);
    const cy = CY + R * Math.sin(a);
    const members = dirGroups.get(dir)!;
    const cols = Math.max(1, Math.ceil(Math.sqrt(members.length)));
    const spacing = 32;
    members.forEach((idx, mi) => {
      const col = mi % cols;
      const row = Math.floor(mi / cols);
      positions[idx * 2] = cx + (col - cols / 2) * spacing + (Math.random() - 0.5) * 8;
      positions[idx * 2 + 1] = cy + (row - cols / 2) * spacing + (Math.random() - 0.5) * 8;
      clusters[idx] = di;
    });
  });

  const pointColors = new Float32Array(nodes.length * 4);
  const pointSizes = new Float32Array(nodes.length);
  const pointShapes = new Float32Array(nodes.length);

  nodes.forEach((n, i) => {
    const [r, g, b, alpha] = hexRGBA(nColor(n.node_type, n.language));
    pointColors[i * 4] = r;
    pointColors[i * 4 + 1] = g;
    pointColors[i * 4 + 2] = b;
    pointColors[i * 4 + 3] = alpha;

    // Size: base 3, scale up to 18 by degree, min 3 so isolated nodes are visible
    const t = degree[i] / maxDeg;
    pointSizes[i] = 3 + t * 15;

    pointShapes[i] = SHAPE_MAP[n.node_type.toLowerCase()] ?? PointShape.Circle;
  });

  const links = new Float32Array(edges.length * 2);
  const linkColors = new Float32Array(edges.length * 4);
  const linkWidths = new Float32Array(edges.length);

  edges.forEach((e, i) => {
    links[i * 2] = idToIndex.get(e.src_id)!;
    links[i * 2 + 1] = idToIndex.get(e.dst_id)!;

    const [r, g, b] = hexRGBA(eColor(e.edge_type), 0.6);
    linkColors[i * 4] = r;
    linkColors[i * 4 + 1] = g;
    linkColors[i * 4 + 2] = b;
    linkColors[i * 4 + 3] = 0.6;

    linkWidths[i] = Math.min(0.5 + e.weight * 0.25, 2.5);
  });

  return {
    nodes, edges, idToIndex, positions,
    pointColors, pointSizes, pointShapes,
    links, linkColors, linkWidths, clusters, degree,
    dirGroups,
  };
}

export function WebGLGraphCanvas({
  kgNodes,
  kgEdges,
  truncated,
  totalNodes,
}: WebGLGraphCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const labelsRef = useRef<HTMLDivElement>(null);
  const graphRef = useRef<Graph | null>(null);
  const rafRef = useRef<number>(0);

  const [tooltip, setTooltip] = useState<{
    node: KGNode; x: number; y: number; inDegree: number; outDegree: number;
  } | null>(null);

  const [selection, setSelection] = useState<{
    node: KGNode; inEdges: KGEdge[]; outEdges: KGEdge[];
  } | null>(null);

  const [simProgress, setSimProgress] = useState(0);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");

  // ── Listen for dark/light theme changes ─────────────────────────────────
  const [dark, setDark] = useState(isDark);
  useEffect(() => {
    const el = document.documentElement;
    const observer = new MutationObserver(() => setDark(el.classList.contains("dark")));
    observer.observe(el, { attributes: true, attributeFilter: ["class"] });
    return () => observer.disconnect();
  }, []);

  const availableNodeTypes = useMemo(
    () => [...new Set(kgNodes.map((n) => n.node_type.toLowerCase()))].sort(),
    [kgNodes],
  );
  const availableEdgeTypes = useMemo(
    () => [...new Set(kgEdges.map((e) => e.edge_type.toUpperCase()))].sort(),
    [kgEdges],
  );

  const [visibleNodeTypes, setVisibleNodeTypes] = useState(() => new Set(availableNodeTypes));
  const [visibleEdgeTypes, setVisibleEdgeTypes] = useState(() => new Set(availableEdgeTypes));

  const prepared = useMemo(
    () => prepareGraph(kgNodes, kgEdges, visibleNodeTypes, visibleEdgeTypes),
    [kgNodes, kgEdges, visibleNodeTypes, visibleEdgeTypes],
  );

  const nodeMap = useMemo(() => {
    const m = new Map<string, KGNode>();
    for (const n of prepared.nodes) m.set(n.id, n);
    return m;
  }, [prepared.nodes]);

  const edgeLookup = useMemo(() => {
    const out = new Map<string, KGEdge[]>();
    const inMap = new Map<string, KGEdge[]>();
    for (const e of prepared.edges) {
      if (!out.has(e.src_id)) out.set(e.src_id, []);
      out.get(e.src_id)!.push(e);
      if (!inMap.has(e.dst_id)) inMap.set(e.dst_id, []);
      inMap.get(e.dst_id)!.push(e);
    }
    return { out, in: inMap };
  }, [prepared.edges]);

  const searchResults = useMemo(() => {
    if (!searchQuery.trim()) return [];
    const q = searchQuery.toLowerCase();
    return prepared.nodes
      .map((n, i) => ({ node: n, index: i }))
      .filter(({ node }) =>
        node.name.toLowerCase().includes(q) ||
        node.file_path.toLowerCase().includes(q),
      )
      .slice(0, 20);
  }, [searchQuery, prepared.nodes]);

  const preparedRef = useRef(prepared);
  preparedRef.current = prepared;
  const edgeLookupRef = useRef(edgeLookup);
  edgeLookupRef.current = edgeLookup;
  const nodeMapRef = useRef(nodeMap);
  nodeMapRef.current = nodeMap;
  const darkRef = useRef(dark);
  darkRef.current = dark;

  const updateLabels = useCallback(() => {
    const graph = graphRef.current;
    const el = labelsRef.current;
    if (!graph || !el) return;

    const prep = preparedRef.current;
    const fragments: string[] = [];

    // ── 1. Cluster / directory labels ────────────────────────────────────
    // For each directory group, compute the centroid in simulation space,
    // convert to screen space, and render the directory name (like Cosmograph).
    const allPos = graph.getPointPositions(); // [x0,y0,x1,y1,…] — sim space
    const dirCentroids = new Map<string, { sx: number; sy: number; n: number }>();
    for (const [dir, memberIndices] of prep.dirGroups) {
      for (const idx of memberIndices) {
        const px = allPos[idx * 2];
        const py = allPos[idx * 2 + 1];
        if (px === undefined || py === undefined) continue;
        const c = dirCentroids.get(dir) ?? { sx: 0, sy: 0, n: 0 };
        c.sx += px;
        c.sy += py;
        c.n += 1;
        dirCentroids.set(dir, c);
      }
    }
    for (const [dir, c] of dirCentroids) {
      if (c.n < 2) continue; // skip singletons
      const simX = c.sx / c.n;
      const simY = c.sy / c.n;
      const [cx, cy] = graph.spaceToScreenPosition([simX, simY]);
      // Use last segment of the directory path as the label
      const label = dir === "(root)" ? "(root)" : dir.split("/").pop() || dir;
      const size = Math.min(18, 10 + Math.log2(c.n) * 2);
      const clusterColor = darkRef.current ? "rgba(255,255,255,0.30)" : "rgba(0,0,0,0.25)";
      fragments.push(
        `<span style="position:absolute;left:${cx}px;top:${cy}px;transform:translate(-50%,-50%);color:${clusterColor};font-size:${size}px;font-weight:600;white-space:nowrap;pointer-events:none;letter-spacing:0.5px">${label}</span>`,
      );
    }

    // ── 2. Individual node labels (sampled high-degree nodes) ────────────
    const sampled = graph.getSampledPointPositionsMap();
    sampled.forEach((pos: [number, number], idx: number) => {
      const node = prep.nodes[idx];
      if (!node) return;
      const rawName = node.name || node.file_path.split("/").pop() || node.id;
      const name = rawName.length > 24 ? "…" + rawName.slice(-23) : rawName;
      const color = nColor(node.node_type, node.language);
      fragments.push(
        `<span style="position:absolute;left:${pos[0] + 8}px;top:${pos[1] - 6}px;color:${color};font-size:9px;font-weight:500;white-space:nowrap;pointer-events:none;text-shadow:0 0 3px rgba(0,0,0,.7),0 0 6px rgba(0,0,0,.5);opacity:.85">${name}</span>`,
      );
    });

    el.innerHTML = fragments.join("");
    rafRef.current = requestAnimationFrame(updateLabels);
  }, []);

  useLayoutEffect(() => {
    const div = containerRef.current;
    if (!div) return;

    const dark_ = dark;

    const config: GraphConfigInterface = {
      backgroundColor: dark_ ? "#09090b" : "#fafafa",
      spaceSize: 8192,
      fitViewOnInit: true,
      fitViewDelay: 800,
      fitViewPadding: 0.1,
      fitViewDuration: 600,

      // ── Force‑layout tuning (Cosmograph‑like tight clusters) ──────────
      simulationFriction: 0.85,
      simulationGravity: 0.25,
      simulationCenter: 0.0,
      simulationRepulsion: 1.0,
      simulationLinkSpring: 1.0,
      simulationLinkDistance: 10,
      simulationDecay: 6000,
      simulationCluster: 0.6,

      // ── Links / edges ─────────────────────────────────────────────────
      renderLinks: true,
      curvedLinks: true,
      curvedLinkSegments: 16,
      curvedLinkWeight: 0.8,
      curvedLinkControlPointDistance: 0.5,
      linkDefaultArrows: false,
      linkArrowsSizeScale: 0.5,
      linkDefaultWidth: 0.6,
      linkWidthScale: 1,
      linkOpacity: 0.6,
      linkGreyoutOpacity: 0.02,
      linkVisibilityDistanceRange: [80, 400],
      linkVisibilityMinTransparency: 0.08,
      scaleLinksOnZoom: true,

      // ── Points / nodes ────────────────────────────────────────────────
      pointDefaultSize: 4,
      pointSizeScale: 1,
      scalePointsOnZoom: true,
      renderHoveredPointRing: true,
      hoveredPointRingColor: dark_ ? "#ffffff" : "#000000",
      focusedPointRingColor: dark_ ? "#60a5fa" : "#2563eb",
      hoveredPointCursor: "pointer",
      pointSamplingDistance: 100,
      enableDrag: true,
      randomSeed: 42,
      pixelRatio: Math.min(window.devicePixelRatio, 2),
      pointGreyoutOpacity: 0.08,

      onSimulationTick: (alpha) => setSimProgress(Math.round((1 - alpha) * 100)),
      onSimulationEnd: () => setSimProgress(100),

      onPointMouseOver: (index, _pos, event) => {
        const prep = preparedRef.current;
        const node = prep.nodes[index];
        if (!node || !event || !("clientX" in event)) return;
        const rect = div.getBoundingClientRect();
        const me = event as MouseEvent;
        const inDeg = edgeLookupRef.current.in.get(node.id)?.length ?? 0;
        const outDeg = edgeLookupRef.current.out.get(node.id)?.length ?? 0;
        setTooltip({ node, x: me.clientX - rect.left, y: me.clientY - rect.top, inDegree: inDeg, outDegree: outDeg });
      },

      onPointMouseOut: () => setTooltip(null),

      onPointClick: (index) => {
        const graph = graphRef.current;
        const prep = preparedRef.current;
        const node = prep.nodes[index];
        if (!graph || !node) return;
        graph.selectPointByIndex(index, true);
        setSelection({
          node,
          inEdges: edgeLookupRef.current.in.get(node.id) ?? [],
          outEdges: edgeLookupRef.current.out.get(node.id) ?? [],
        });
        setTooltip(null);
      },

      onBackgroundClick: () => {
        graphRef.current?.unselectPoints();
        setSelection(null);
        setTooltip(null);
      },
    };

    const graph = new Graph(div, config);
    graphRef.current = graph;

    rafRef.current = requestAnimationFrame(updateLabels);

    return () => {
      cancelAnimationFrame(rafRef.current);
      graph.destroy();
      graphRef.current = null;
    };
  }, [updateLabels, dark]);

  useEffect(() => {
    const graph = graphRef.current;
    if (!graph) return;

    graph.setConfig({
      backgroundColor: dark ? "#09090b" : "#fafafa",
      hoveredPointRingColor: dark ? "#ffffff" : "#000000",
      focusedPointRingColor: dark ? "#60a5fa" : "#2563eb",
    });

    graph.setPointPositions(prepared.positions);
    graph.setPointColors(prepared.pointColors);
    graph.setPointSizes(prepared.pointSizes);
    graph.setPointShapes(prepared.pointShapes);
    graph.setLinks(prepared.links);
    graph.setLinkColors(prepared.linkColors);
    graph.setLinkWidths(prepared.linkWidths);
    graph.setPointClusters(prepared.clusters);

    graph.render();
    setSimProgress(0);
    setTooltip(null);
    setSelection(null);
  }, [prepared, dark]);

  const handleFitView = useCallback(() => graphRef.current?.fitView(400, 0.1), []);

  const handleZoomIn = useCallback(() => {
    const g = graphRef.current;
    if (g) g.setZoomLevel(g.getZoomLevel() * 1.5, 250);
  }, []);

  const handleZoomOut = useCallback(() => {
    const g = graphRef.current;
    if (g) g.setZoomLevel(g.getZoomLevel() / 1.5, 250);
  }, []);

  const navigateToNode = useCallback((nodeId: string) => {
    const graph = graphRef.current;
    const prep = preparedRef.current;
    if (!graph) return;
    const idx = prep.idToIndex.get(nodeId);
    if (idx === undefined) return;
    graph.zoomToPointByIndex(idx, 600, 4);
    graph.selectPointByIndex(idx, true);
    const node = prep.nodes[idx];
    if (node) {
      setSelection({
        node,
        inEdges: edgeLookupRef.current.in.get(node.id) ?? [],
        outEdges: edgeLookupRef.current.out.get(node.id) ?? [],
      });
    }
    setSearchOpen(false);
    setSearchQuery("");
  }, []);

  const toggleNodeType = useCallback((t: string) => {
    setVisibleNodeTypes((prev) => {
      const next = new Set(prev);
      next.has(t) ? next.delete(t) : next.add(t);
      return next;
    });
  }, []);

  const toggleEdgeType = useCallback((t: string) => {
    setVisibleEdgeTypes((prev) => {
      const next = new Set(prev);
      next.has(t) ? next.delete(t) : next.add(t);
      return next;
    });
  }, []);

  return (
    <div className="relative h-[700px] w-full rounded-lg border bg-background overflow-hidden">
      <div ref={containerRef} className="absolute inset-0" />
      <div ref={labelsRef} className="absolute inset-0 pointer-events-none overflow-hidden z-[5]" />

      {tooltip && !selection && (
        <GraphTooltip
          node={tooltip.node}
          x={tooltip.x}
          y={tooltip.y}
          inDegree={tooltip.inDegree}
          outDegree={tooltip.outDegree}
        />
      )}

      {selection && (
        <SelectionPanel
          node={selection.node}
          inEdges={selection.inEdges}
          outEdges={selection.outEdges}
          nodeMap={nodeMap}
          onClose={() => {
            setSelection(null);
            graphRef.current?.unselectPoints();
          }}
          onNavigate={navigateToNode}
        />
      )}

      <div className="absolute left-3 top-3 z-10">
        <div className="flex flex-wrap items-center gap-3 rounded-md border bg-background/80 px-3 py-1.5 text-[10px] backdrop-blur-sm">
          {/* Show language legend when file_summary nodes are present */}
          {availableNodeTypes.includes("file_summary")
            ? [...new Set(kgNodes.map((n) => n.language).filter(Boolean))].sort().map((lang) => (
                <span key={lang} className="flex items-center gap-1">
                  <span
                    className="inline-block h-2 w-2 rounded-full"
                    style={{ background: LANG_COLORS[lang.toLowerCase()] ?? FALLBACK_NODE_COLOR }}
                  />
                  {lang}
                </span>
              ))
            : availableNodeTypes.map((nt) => {
                const shape = SHAPE_MAP[nt];
                const shapeLabel = shape === PointShape.Triangle ? "▲"
                  : shape === PointShape.Square ? "■"
                  : shape === PointShape.Diamond ? "◆" : "●";
                return (
                  <span key={nt} className="flex items-center gap-1">
                    <span style={{ color: nColor(nt), fontSize: 10 }}>{shapeLabel}</span>
                    {nt.charAt(0).toUpperCase() + nt.slice(1)}
                  </span>
                );
              })
          }
          <span className="text-muted-foreground">
            {prepared.nodes.length.toLocaleString()} nodes · {prepared.edges.length.toLocaleString()} edges
          </span>
        </div>
      </div>

      <div className="absolute right-3 top-3 z-10 flex items-center gap-1.5">
        {searchOpen ? (
          <div className="flex items-center gap-1 rounded-md border bg-background/90 px-2 py-1 backdrop-blur-sm animate-in slide-in-from-right-2 duration-150">
            <Search className="h-3 w-3 text-muted-foreground shrink-0" />
            <Input
              autoFocus
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search nodes…"
              className="h-6 w-44 border-0 bg-transparent p-0 text-xs shadow-none focus-visible:ring-0"
            />
            <button onClick={() => { setSearchOpen(false); setSearchQuery(""); }} className="p-0.5 hover:bg-muted rounded-sm">
              <X className="h-3 w-3" />
            </button>
            {searchResults.length > 0 && (
              <div className="absolute right-0 top-9 w-64 max-h-60 overflow-y-auto rounded-md border bg-background/95 p-1 shadow-lg backdrop-blur-sm">
                {searchResults.map(({ node, index }) => (
                  <button
                    key={node.id}
                    onClick={() => navigateToNode(node.id)}
                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[11px] hover:bg-muted/60 transition-colors"
                  >
                    <span
                      className="h-2 w-2 rounded-full shrink-0"
                      style={{ background: nColor(node.node_type, node.language) }}
                    />
                    <span className="truncate font-medium">{node.name || node.file_path.split("/").pop() || node.id}</span>
                    <span className="ml-auto text-[9px] text-muted-foreground shrink-0">
                      {node.language || node.node_type}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </div>
        ) : (
          <Button
            variant="outline"
            size="icon"
            className="h-7 w-7 bg-background/80 backdrop-blur-sm"
            onClick={() => setSearchOpen(true)}
            title="Search nodes"
          >
            <Search className="h-3.5 w-3.5" />
          </Button>
        )}

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" className="h-7 text-[10px] bg-background/80 backdrop-blur-sm">
              <Filter className="mr-1 h-3 w-3" />
              Filter
              <ChevronDown className="ml-1 h-3 w-3" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuLabel className="text-[10px]">Node Types</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {availableNodeTypes.map((nt) => (
              <DropdownMenuCheckboxItem
                key={nt}
                checked={visibleNodeTypes.has(nt)}
                onCheckedChange={() => toggleNodeType(nt)}
                className="text-xs"
              >
                <span className="mr-1.5 inline-block h-2 w-2 rounded-full" style={{ background: nColor(nt) }} />
                {nt.charAt(0).toUpperCase() + nt.slice(1)}
              </DropdownMenuCheckboxItem>
            ))}
            <DropdownMenuSeparator />
            <DropdownMenuLabel className="text-[10px]">Edge Types</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {availableEdgeTypes.map((et) => (
              <DropdownMenuCheckboxItem
                key={et}
                checked={visibleEdgeTypes.has(et)}
                onCheckedChange={() => toggleEdgeType(et)}
                className="text-xs"
              >
                <span className="mr-1.5 inline-block h-2 w-2 rounded-full" style={{ background: eColor(et) }} />
                {et}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>

        <Button variant="outline" size="icon" className="h-7 w-7 bg-background/80 backdrop-blur-sm" onClick={handleZoomIn} title="Zoom in">
          <ZoomIn className="h-3.5 w-3.5" />
        </Button>
        <Button variant="outline" size="icon" className="h-7 w-7 bg-background/80 backdrop-blur-sm" onClick={handleZoomOut} title="Zoom out">
          <ZoomOut className="h-3.5 w-3.5" />
        </Button>
        <Button variant="outline" size="icon" className="h-7 w-7 bg-background/80 backdrop-blur-sm" onClick={handleFitView} title="Fit view">
          <Maximize2 className="h-3.5 w-3.5" />
        </Button>
      </div>

      {simProgress < 100 && (
        <div className="absolute bottom-3 left-3 z-10">
          <div className="flex items-center gap-2 rounded-md border bg-background/80 px-3 py-1.5 text-[10px] backdrop-blur-sm">
            <div className="h-1.5 w-20 rounded-full bg-muted overflow-hidden">
              <div
                className="h-full rounded-full bg-blue-500 transition-all duration-300"
                style={{ width: `${simProgress}%` }}
              />
            </div>
            <span className="text-muted-foreground">Simulating… {simProgress}%</span>
          </div>
        </div>
      )}

      {truncated && (
        <div className="absolute bottom-3 left-1/2 z-10 -translate-x-1/2">
          <div className="flex items-center gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-1.5 text-[11px] text-amber-800 dark:border-amber-700 dark:bg-amber-950/60 dark:text-amber-300">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
            Showing {kgNodes.length.toLocaleString()} of {totalNodes.toLocaleString()} nodes — server capped response. Use &quot;Files only&quot; or filters to narrow down.
          </div>
        </div>
      )}

      <div className="absolute bottom-3 right-3 z-10">
        <Badge variant="outline" className="bg-background/80 text-[9px] backdrop-blur-sm">
          WebGL · GPU Accelerated
        </Badge>
      </div>
    </div>
  );
}
