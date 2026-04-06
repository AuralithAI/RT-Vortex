// ─── Role ELO Leaderboard ────────────────────────────────────────────────────
// Displays role-level ELO rankings per (role, repo) pair.
// Self-contained component that fetches GET /api/v1/swarm/role-elo and renders
// a sortable, filterable leaderboard showing tier badges, ELO scores, win/loss
// records, average ratings, training probes, and best strategies.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useMemo, useCallback } from "react";
import {
  Trophy,
  ArrowUpDown,
  ChevronUp,
  ChevronDown,
  Shield,
  Star,
  Zap,
  Crown,
  AlertTriangle,
  Target,
  RefreshCw,
  Loader2,
  Search,
  Code,
  FileText,
  Settings,
  Users,
  Bot,
} from "lucide-react";
import type { RoleELOData, RoleELOTier } from "@/types/swarm";

// ── Helpers ──────────────────────────────────────────────────────────────────

interface TierBadge {
  label: string;
  color: string;
  bgColor: string;
  icon: React.ReactNode;
}

function tierBadge(tier: RoleELOTier): TierBadge {
  switch (tier) {
    case "expert":
      return {
        label: "Expert",
        color: "text-yellow-600 dark:text-yellow-400",
        bgColor: "bg-yellow-100 dark:bg-yellow-900/50",
        icon: <Crown className="h-3.5 w-3.5" />,
      };
    case "restricted":
      return {
        label: "Restricted",
        color: "text-red-600 dark:text-red-400",
        bgColor: "bg-red-100 dark:bg-red-900/50",
        icon: <AlertTriangle className="h-3.5 w-3.5" />,
      };
    default:
      return {
        label: "Standard",
        color: "text-blue-600 dark:text-blue-400",
        bgColor: "bg-blue-100 dark:bg-blue-900/50",
        icon: <Shield className="h-3.5 w-3.5" />,
      };
  }
}

function roleIcon(role: string) {
  switch (role) {
    case "orchestrator":
      return <Users className="h-4 w-4 text-purple-500" />;
    case "architect":
      return <Search className="h-4 w-4 text-blue-500" />;
    case "senior_dev":
      return <Code className="h-4 w-4 text-green-600" />;
    case "junior_dev":
      return <Code className="h-4 w-4 text-green-400" />;
    case "qa":
      return <Zap className="h-4 w-4 text-yellow-500" />;
    case "security":
      return <Shield className="h-4 w-4 text-red-500" />;
    case "docs":
      return <FileText className="h-4 w-4 text-cyan-500" />;
    case "ops":
      return <Settings className="h-4 w-4 text-orange-500" />;
    default:
      return <Bot className="h-4 w-4 text-muted-foreground" />;
  }
}

function roleLabel(role: string): string {
  const labels: Record<string, string> = {
    orchestrator: "Orchestrator",
    architect: "Architect",
    senior_dev: "Senior Dev",
    junior_dev: "Junior Dev",
    qa: "QA",
    security: "Security",
    docs: "Docs",
    ops: "Ops",
    ui_ux: "UI/UX",
  };
  return labels[role] ?? role;
}

function renderStars(rating: number) {
  const full = Math.floor(rating);
  const half = rating - full >= 0.5;
  const stars: React.ReactNode[] = [];
  for (let i = 0; i < full; i++) {
    stars.push(
      <Star key={`f-${i}`} className="h-3 w-3 fill-yellow-400 text-yellow-400" />
    );
  }
  if (half) {
    stars.push(
      <Star key="half" className="h-3 w-3 fill-yellow-400/50 text-yellow-400" />
    );
  }
  const remaining = 5 - full - (half ? 1 : 0);
  for (let i = 0; i < remaining; i++) {
    stars.push(
      <Star key={`e-${i}`} className="h-3 w-3 text-muted-foreground/30" />
    );
  }
  return <span className="inline-flex items-center gap-0.5">{stars}</span>;
}

function eloDelta(elo: number): { label: string; color: string } {
  if (elo >= 1400) return { label: `+${(elo - 1200).toFixed(0)}`, color: "text-green-500" };
  if (elo >= 1200) return { label: `+${(elo - 1200).toFixed(0)}`, color: "text-green-400" };
  if (elo >= 1100) return { label: `${(elo - 1200).toFixed(0)}`, color: "text-orange-500" };
  return { label: `${(elo - 1200).toFixed(0)}`, color: "text-red-500" };
}

function formatStrategy(strategy: string): string {
  if (!strategy) return "—";
  return strategy.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

// ── Props ────────────────────────────────────────────────────────────────────

interface RoleELOLeaderboardProps {
  /** Optional repo_id filter. If provided, only shows roles for that repo. */
  repoId?: string;
  /** CSS class name. */
  className?: string;
  /** Polling interval in ms. Default 30000 (30s). */
  pollInterval?: number;
}

// ── Sort ─────────────────────────────────────────────────────────────────────

type SortField = "elo_score" | "role" | "tasks_done" | "avg_rating" | "wins" | "training_probes";
type SortDir = "asc" | "desc";

// ── Component ────────────────────────────────────────────────────────────────

export function RoleELOLeaderboard({
  repoId,
  className,
  pollInterval = 30_000,
}: RoleELOLeaderboardProps) {
  const [data, setData] = useState<RoleELOData[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [sortField, setSortField] = useState<SortField>("elo_score");
  const [sortDir, setSortDir] = useState<SortDir>("desc");
  const [filterTier, setFilterTier] = useState<RoleELOTier | "all">("all");

  const fetchData = useCallback(async () => {
    try {
      const params = new URLSearchParams();
      if (repoId) params.set("repo_id", repoId);
      const url = `/api/v1/swarm/role-elo${params.toString() ? `?${params}` : ""}`;
      const res = await fetch(url);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const json = await res.json();
      setData(json.records ?? json.entries ?? (Array.isArray(json) ? json : []));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch");
    } finally {
      setLoading(false);
    }
  }, [repoId]);

  useEffect(() => {
    fetchData();
    const iv = setInterval(fetchData, pollInterval);
    return () => clearInterval(iv);
  }, [fetchData, pollInterval]);

  const toggleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      setSortDir("desc");
    }
  };

  const filtered = useMemo(() => {
    if (filterTier === "all") return data;
    return data.filter((r) => r.tier === filterTier);
  }, [data, filterTier]);

  const sorted = useMemo(() => {
    const copy = [...filtered];
    copy.sort((a, b) => {
      let cmp = 0;
      switch (sortField) {
        case "elo_score":
          cmp = a.elo_score - b.elo_score;
          break;
        case "role":
          cmp = a.role.localeCompare(b.role);
          break;
        case "tasks_done":
          cmp = a.tasks_done - b.tasks_done;
          break;
        case "avg_rating":
          cmp = a.avg_rating - b.avg_rating;
          break;
        case "wins":
          cmp = a.wins - b.wins;
          break;
        case "training_probes":
          cmp = a.training_probes - b.training_probes;
          break;
      }
      return sortDir === "asc" ? cmp : -cmp;
    });
    return copy;
  }, [filtered, sortField, sortDir]);

  // Summary stats
  const expertCount = data.filter((r) => r.tier === "expert").length;
  const restrictedCount = data.filter((r) => r.tier === "restricted").length;
  const avgElo = data.length > 0
    ? data.reduce((sum, r) => sum + r.elo_score, 0) / data.length
    : 0;

  const SortIcon = ({ field }: { field: SortField }) => {
    if (sortField !== field)
      return <ArrowUpDown className="ml-1 h-3 w-3 text-muted-foreground/50" />;
    return sortDir === "asc" ? (
      <ChevronUp className="ml-1 h-3 w-3" />
    ) : (
      <ChevronDown className="ml-1 h-3 w-3" />
    );
  };

  // Loading state
  if (loading) {
    return (
      <div className={`rounded-lg border bg-card p-6 ${className ?? ""}`}>
        <div className="flex items-center gap-2 mb-4">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          <span className="text-sm text-muted-foreground">Loading role ELO data…</span>
        </div>
        <div className="space-y-3">
          {[...Array(5)].map((_, i) => (
            <div key={i} className="h-10 animate-pulse rounded bg-muted" />
          ))}
        </div>
      </div>
    );
  }

  // Error state
  if (error) {
    return (
      <div className={`rounded-lg border border-red-200 dark:border-red-800 bg-card p-6 ${className ?? ""}`}>
        <div className="flex items-center gap-2 text-red-500">
          <AlertTriangle className="h-5 w-5" />
          <span className="text-sm">Failed to load role ELO: {error}</span>
        </div>
        <button
          onClick={fetchData}
          className="mt-2 text-xs text-primary underline"
        >
          Retry
        </button>
      </div>
    );
  }

  // Empty state
  if (data.length === 0) {
    return (
      <div className={`rounded-lg border p-8 text-center text-muted-foreground ${className ?? ""}`}>
        <Target className="mx-auto mb-2 h-8 w-8" />
        <p>No role ELO data yet.</p>
        <p className="mt-1 text-xs">
          Ratings will appear after agents complete tasks and receive feedback.
        </p>
      </div>
    );
  }

  return (
    <div className={`rounded-lg border bg-card ${className ?? ""}`}>
      {/* Header */}
      <div className="flex items-center gap-2 border-b px-4 py-3">
        <Trophy className="h-5 w-5 text-yellow-500" />
        <h3 className="font-semibold">Role ELO Leaderboard</h3>
        <span className="ml-auto flex items-center gap-3 text-xs text-muted-foreground">
          {/* Tier filter pills */}
          {(["all", "expert", "standard", "restricted"] as const).map((t) => (
            <button
              key={t}
              onClick={() => setFilterTier(t)}
              className={`rounded-full px-2 py-0.5 text-xs transition-colors ${
                filterTier === t
                  ? "bg-primary text-primary-foreground"
                  : "bg-muted hover:bg-muted/80"
              }`}
            >
              {t === "all" ? `All (${data.length})` : `${t.charAt(0).toUpperCase() + t.slice(1)}`}
            </button>
          ))}
          <button onClick={fetchData} title="Refresh">
            <RefreshCw className="h-3.5 w-3.5 hover:text-foreground transition-colors" />
          </button>
        </span>
      </div>

      {/* Summary stats */}
      <div className="grid grid-cols-4 gap-4 border-b px-4 py-3 text-center">
        <div>
          <p className="text-xs text-muted-foreground">Total Roles</p>
          <p className="text-lg font-bold">{data.length}</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">Avg ELO</p>
          <p className="text-lg font-bold tabular-nums">{Math.round(avgElo)}</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">Expert</p>
          <p className="text-lg font-bold text-yellow-500">{expertCount}</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">Restricted</p>
          <p className="text-lg font-bold text-red-500">{restrictedCount}</p>
        </div>
      </div>

      {/* Table */}
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b text-left text-xs text-muted-foreground">
              <th className="px-4 py-2 font-medium">#</th>
              <th
                className="cursor-pointer select-none px-4 py-2 font-medium"
                onClick={() => toggleSort("role")}
              >
                <span className="inline-flex items-center">
                  Role
                  <SortIcon field="role" />
                </span>
              </th>
              <th className="px-4 py-2 font-medium">Repo</th>
              <th className="px-4 py-2 font-medium">Tier</th>
              <th
                className="cursor-pointer select-none px-4 py-2 font-medium text-right"
                onClick={() => toggleSort("elo_score")}
              >
                <span className="inline-flex items-center justify-end">
                  ELO
                  <SortIcon field="elo_score" />
                </span>
              </th>
              <th
                className="cursor-pointer select-none px-4 py-2 font-medium text-right"
                onClick={() => toggleSort("wins")}
              >
                <span className="inline-flex items-center justify-end">
                  W/L
                  <SortIcon field="wins" />
                </span>
              </th>
              <th
                className="cursor-pointer select-none px-4 py-2 font-medium text-right"
                onClick={() => toggleSort("tasks_done")}
              >
                <span className="inline-flex items-center justify-end">
                  Tasks
                  <SortIcon field="tasks_done" />
                </span>
              </th>
              <th
                className="cursor-pointer select-none px-4 py-2 font-medium text-right"
                onClick={() => toggleSort("avg_rating")}
              >
                <span className="inline-flex items-center justify-end">
                  Rating
                  <SortIcon field="avg_rating" />
                </span>
              </th>
              <th
                className="cursor-pointer select-none px-4 py-2 font-medium text-right"
                onClick={() => toggleSort("training_probes")}
              >
                <span className="inline-flex items-center justify-end">
                  Probes
                  <SortIcon field="training_probes" />
                </span>
              </th>
              <th className="px-4 py-2 font-medium text-right">Strategy</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((entry, idx) => {
              const tier = tierBadge(entry.tier);
              const delta = eloDelta(entry.elo_score);
              return (
                <tr
                  key={entry.id}
                  className="border-b last:border-0 hover:bg-muted/50 transition-colors"
                >
                  {/* Rank */}
                  <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                    {idx + 1}
                  </td>

                  {/* Role */}
                  <td className="px-4 py-2.5">
                    <div className="flex items-center gap-2">
                      {roleIcon(entry.role)}
                      <span className="font-medium">
                        {roleLabel(entry.role)}
                      </span>
                    </div>
                  </td>

                  {/* Repo */}
                  <td className="px-4 py-2.5">
                    <span
                      className="max-w-[180px] truncate text-xs text-muted-foreground"
                      title={entry.repo_id}
                    >
                      {entry.repo_id || "global"}
                    </span>
                  </td>

                  {/* Tier badge */}
                  <td className="px-4 py-2.5">
                    <span
                      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${tier.color} ${tier.bgColor}`}
                    >
                      {tier.icon}
                      {tier.label}
                    </span>
                  </td>

                  {/* ELO score */}
                  <td className="px-4 py-2.5 text-right">
                    <span className="font-semibold tabular-nums">
                      {Math.round(entry.elo_score)}
                    </span>
                    <span className={`ml-1.5 text-xs ${delta.color}`}>
                      {delta.label}
                    </span>
                  </td>

                  {/* Win / Loss */}
                  <td className="px-4 py-2.5 text-right tabular-nums">
                    <span className="text-green-500">{entry.wins}</span>
                    <span className="mx-0.5 text-muted-foreground">/</span>
                    <span className="text-red-500">{entry.losses}</span>
                  </td>

                  {/* Tasks */}
                  <td className="px-4 py-2.5 text-right tabular-nums">
                    {entry.tasks_done}
                    {entry.tasks_rated > 0 && (
                      <span className="ml-1 text-xs text-muted-foreground">
                        ({entry.tasks_rated} rated)
                      </span>
                    )}
                  </td>

                  {/* Rating */}
                  <td className="px-4 py-2.5 text-right">
                    {entry.tasks_rated > 0 ? (
                      <div className="flex items-center justify-end gap-1.5">
                        {renderStars(entry.avg_rating)}
                        <span className="ml-1 text-xs tabular-nums text-muted-foreground">
                          {entry.avg_rating.toFixed(1)}
                        </span>
                      </div>
                    ) : (
                      <span className="text-xs text-muted-foreground">—</span>
                    )}
                  </td>

                  {/* Training probes */}
                  <td className="px-4 py-2.5 text-right">
                    <span
                      className={`inline-flex items-center gap-1 tabular-nums text-xs ${
                        entry.training_probes >= 5
                          ? "text-red-500 font-medium"
                          : entry.training_probes >= 3
                            ? "text-muted-foreground"
                            : "text-green-500 font-medium"
                      }`}
                    >
                      <Target className="h-3 w-3" />
                      {entry.training_probes}
                    </span>
                  </td>

                  {/* Best strategy */}
                  <td className="px-4 py-2.5 text-right">
                    <span className="rounded bg-muted px-1.5 py-0.5 text-xs">
                      {formatStrategy(entry.best_strategy)}
                    </span>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
