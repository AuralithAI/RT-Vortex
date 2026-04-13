// ─── Agent Leaderboard ───────────────────────────────────────────────────────
// ELO leaderboard showing agent performance rankings. Sortable by ELO score,
// tasks completed, average rating, and role. Used on the swarm dashboard.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useMemo } from "react";
import {
  Trophy,
  ArrowUpDown,
  Bot,
  Star,
  Zap,
  Shield,
  FileText,
  Settings,
  Code,
  Search,
  Users,
  ChevronUp,
  ChevronDown,
} from "lucide-react";
import type { SwarmAgent, AgentRole } from "@/types/swarm";

interface AgentLeaderboardProps {
  agents: SwarmAgent[];
  className?: string;
}

type SortField = "elo_score" | "tasks_done" | "avg_rating" | "role";
type SortDir = "asc" | "desc";

function roleIcon(role: AgentRole) {
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

function roleLabel(role: AgentRole): string {
  const labels: Record<AgentRole, string> = {
    orchestrator: "Orchestrator",
    architect: "Architect",
    senior_dev: "Senior Dev",
    junior_dev: "Junior Dev",
    qa: "QA",
    security: "Security",
    docs: "Docs",
    ops: "Ops",
    ui_ux: "UI/UX Designer",
    builder: "Builder",
  };
  return labels[role] ?? role;
}

function eloTier(elo: number): { label: string; color: string } {
  if (elo >= 1600) return { label: "Expert", color: "text-yellow-500" };
  if (elo >= 1400) return { label: "Advanced", color: "text-blue-500" };
  if (elo >= 1200) return { label: "Proficient", color: "text-green-500" };
  if (elo >= 1000) return { label: "Learning", color: "text-orange-500" };
  return { label: "Novice", color: "text-muted-foreground" };
}

function statusDot(status: string) {
  const colors: Record<string, string> = {
    idle: "bg-green-500",
    busy: "bg-yellow-500",
    offline: "bg-gray-400",
    errored: "bg-red-500",
  };
  return (
    <span
      className={`inline-block h-2 w-2 rounded-full ${colors[status] ?? "bg-gray-400"}`}
      title={status}
    />
  );
}

function renderStars(rating: number) {
  const full = Math.floor(rating);
  const half = rating - full >= 0.5;
  const stars: React.ReactNode[] = [];

  for (let i = 0; i < full; i++) {
    stars.push(
      <Star key={`f-${i}`} className="h-3.5 w-3.5 fill-yellow-400 text-yellow-400" />
    );
  }
  if (half) {
    stars.push(
      <Star key="half" className="h-3.5 w-3.5 fill-yellow-400/50 text-yellow-400" />
    );
  }
  const remaining = 5 - full - (half ? 1 : 0);
  for (let i = 0; i < remaining; i++) {
    stars.push(
      <Star key={`e-${i}`} className="h-3.5 w-3.5 text-muted-foreground/30" />
    );
  }
  return <span className="inline-flex items-center gap-0.5">{stars}</span>;
}

export function AgentLeaderboard({ agents, className }: AgentLeaderboardProps) {
  const [sortField, setSortField] = useState<SortField>("elo_score");
  const [sortDir, setSortDir] = useState<SortDir>("desc");

  const toggleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      setSortDir("desc");
    }
  };

  const sorted = useMemo(() => {
    const copy = [...agents];
    copy.sort((a, b) => {
      let cmp = 0;
      switch (sortField) {
        case "elo_score":
          cmp = a.elo_score - b.elo_score;
          break;
        case "tasks_done":
          cmp = a.tasks_done - b.tasks_done;
          break;
        case "avg_rating":
          cmp = a.avg_rating - b.avg_rating;
          break;
        case "role":
          cmp = a.role.localeCompare(b.role);
          break;
      }
      return sortDir === "asc" ? cmp : -cmp;
    });
    return copy;
  }, [agents, sortField, sortDir]);

  const SortIcon = ({ field }: { field: SortField }) => {
    if (sortField !== field)
      return <ArrowUpDown className="ml-1 h-3 w-3 text-muted-foreground/50" />;
    return sortDir === "asc" ? (
      <ChevronUp className="ml-1 h-3 w-3" />
    ) : (
      <ChevronDown className="ml-1 h-3 w-3" />
    );
  };

  if (agents.length === 0) {
    return (
      <div className={`rounded-lg border p-8 text-center text-muted-foreground ${className ?? ""}`}>
        <Bot className="mx-auto mb-2 h-8 w-8" />
        <p>No agents registered yet.</p>
      </div>
    );
  }

  return (
    <div className={`rounded-lg border bg-card ${className ?? ""}`}>
      {/* Header */}
      <div className="flex items-center gap-2 border-b px-4 py-3">
        <Trophy className="h-5 w-5 text-yellow-500" />
        <h3 className="font-semibold">Agent Leaderboard</h3>
        <span className="ml-auto text-xs text-muted-foreground">
          {agents.length} agent{agents.length !== 1 ? "s" : ""}
        </span>
      </div>

      {/* Table */}
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b text-left text-xs text-muted-foreground">
              <th className="px-4 py-2 font-medium">#</th>
              <th className="px-4 py-2 font-medium">Agent</th>
              <th
                className="cursor-pointer select-none px-4 py-2 font-medium"
                onClick={() => toggleSort("role")}
              >
                <span className="inline-flex items-center">
                  Role
                  <SortIcon field="role" />
                </span>
              </th>
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
                  Avg Rating
                  <SortIcon field="avg_rating" />
                </span>
              </th>
              <th className="px-4 py-2 font-medium text-right">Status</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((agent, idx) => {
              const tier = eloTier(agent.elo_score);
              return (
                <tr
                  key={agent.id}
                  className="border-b last:border-0 hover:bg-muted/50 transition-colors"
                >
                  <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                    {idx + 1}
                  </td>
                  <td className="px-4 py-2.5">
                    <div className="flex items-center gap-2">
                      {roleIcon(agent.role)}
                      <span className="font-mono text-xs" title={agent.id}>
                        {agent.id.substring(0, 8)}
                      </span>
                    </div>
                  </td>
                  <td className="px-4 py-2.5">
                    <span className="rounded-full bg-muted px-2 py-0.5 text-xs">
                      {roleLabel(agent.role)}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <span className={`font-semibold tabular-nums ${tier.color}`}>
                      {Math.round(agent.elo_score)}
                    </span>
                    <span className="ml-1.5 text-xs text-muted-foreground">
                      {tier.label}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums">
                    {agent.tasks_done}
                    {agent.tasks_rated > 0 && (
                      <span className="ml-1 text-xs text-muted-foreground">
                        ({agent.tasks_rated} rated)
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    {agent.tasks_rated > 0 ? (
                      <div className="flex items-center justify-end gap-1.5">
                        {renderStars(agent.avg_rating)}
                        <span className="ml-1 text-xs tabular-nums text-muted-foreground">
                          {agent.avg_rating.toFixed(1)}
                        </span>
                      </div>
                    ) : (
                      <span className="text-xs text-muted-foreground">—</span>
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <span className="inline-flex items-center gap-1.5 text-xs">
                      {statusDot(agent.status)}
                      {agent.status}
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
