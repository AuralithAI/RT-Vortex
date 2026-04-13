// ─── Agent Performance Card ──────────────────────────────────────────────────
// Card showing aggregate performance metrics for a team of agents. Displays
// ELO distribution, task completion stats, and per-role breakdowns.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import {
  Bot,
  Star,
  TrendingUp,
  Target,
  Users,
  Shield,
  Code,
  FileText,
  Settings,
  Search,
  Zap,
} from "lucide-react";
import type { SwarmAgent, AgentRole, SwarmTeam } from "@/types/swarm";

interface AgentPerformanceCardProps {
  team: SwarmTeam;
  agents: SwarmAgent[];
  className?: string;
}

function roleColor(role: AgentRole): string {
  const colors: Record<AgentRole, string> = {
    orchestrator: "bg-purple-500/10 text-purple-500 border-purple-500/20",
    architect: "bg-blue-500/10 text-blue-500 border-blue-500/20",
    senior_dev: "bg-green-600/10 text-green-600 border-green-600/20",
    junior_dev: "bg-green-400/10 text-green-400 border-green-400/20",
    qa: "bg-yellow-500/10 text-yellow-500 border-yellow-500/20",
    security: "bg-red-500/10 text-red-500 border-red-500/20",
    docs: "bg-cyan-500/10 text-cyan-500 border-cyan-500/20",
    ops: "bg-orange-500/10 text-orange-500 border-orange-500/20",
    ui_ux: "bg-pink-500/10 text-pink-500 border-pink-500/20",
    builder: "bg-yellow-500/10 text-yellow-500 border-yellow-500/20",
  };
  return colors[role] ?? "bg-muted text-muted-foreground border-border";
}

function roleIcon(role: AgentRole) {
  const iconClass = "h-3.5 w-3.5";
  switch (role) {
    case "orchestrator":
      return <Users className={iconClass} />;
    case "architect":
      return <Search className={iconClass} />;
    case "senior_dev":
    case "junior_dev":
      return <Code className={iconClass} />;
    case "qa":
      return <Zap className={iconClass} />;
    case "security":
      return <Shield className={iconClass} />;
    case "docs":
      return <FileText className={iconClass} />;
    case "ops":
      return <Settings className={iconClass} />;
    case "ui_ux":
      return <Zap className={iconClass} />;
    default:
      return <Bot className={iconClass} />;
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

interface RoleSummary {
  role: AgentRole;
  count: number;
  avgElo: number;
  totalTasks: number;
  avgRating: number;
}

function computeRoleSummaries(agents: SwarmAgent[]): RoleSummary[] {
  const groups = new Map<AgentRole, SwarmAgent[]>();
  for (const agent of agents) {
    const list = groups.get(agent.role) ?? [];
    list.push(agent);
    groups.set(agent.role, list);
  }

  const summaries: RoleSummary[] = [];
  for (const [role, roleAgents] of groups) {
    const avgElo =
      roleAgents.reduce((sum, a) => sum + a.elo_score, 0) / roleAgents.length;
    const totalTasks = roleAgents.reduce((sum, a) => sum + a.tasks_done, 0);
    const ratedAgents = roleAgents.filter((a) => a.tasks_rated > 0);
    const avgRating =
      ratedAgents.length > 0
        ? ratedAgents.reduce((sum, a) => sum + a.avg_rating, 0) /
          ratedAgents.length
        : 0;

    summaries.push({
      role,
      count: roleAgents.length,
      avgElo: Math.round(avgElo),
      totalTasks,
      avgRating,
    });
  }

  return summaries.sort((a, b) => b.avgElo - a.avgElo);
}

export function AgentPerformanceCard({
  team,
  agents,
  className,
}: AgentPerformanceCardProps) {
  const teamAgents = agents.filter((a) =>
    team.agent_ids.includes(a.id),
  );

  if (teamAgents.length === 0) {
    return (
      <div
        className={`rounded-lg border bg-card p-4 ${className ?? ""}`}
      >
        <div className="flex items-center gap-2 text-muted-foreground">
          <Users className="h-4 w-4" />
          <span className="text-sm font-medium">{team.name}</span>
          <span className="ml-auto text-xs">No agents</span>
        </div>
      </div>
    );
  }

  // Aggregate stats
  const totalElo =
    teamAgents.reduce((sum, a) => sum + a.elo_score, 0) / teamAgents.length;
  const totalTasks = teamAgents.reduce((sum, a) => sum + a.tasks_done, 0);
  const ratedAgents = teamAgents.filter((a) => a.tasks_rated > 0);
  const teamAvgRating =
    ratedAgents.length > 0
      ? ratedAgents.reduce((sum, a) => sum + a.avg_rating, 0) /
        ratedAgents.length
      : 0;
  const busyCount = teamAgents.filter((a) => a.status === "busy").length;

  const roleSummaries = computeRoleSummaries(teamAgents);

  return (
    <div className={`rounded-lg border bg-card ${className ?? ""}`}>
      {/* Header */}
      <div className="flex items-center gap-2 border-b px-4 py-3">
        <Users className="h-4 w-4 text-blue-500" />
        <h4 className="font-semibold">{team.name}</h4>
        <span
          className={`ml-auto rounded-full px-2 py-0.5 text-xs font-medium ${
            team.status === "busy"
              ? "bg-yellow-500/10 text-yellow-500"
              : team.status === "idle"
                ? "bg-green-500/10 text-green-500"
                : "bg-muted text-muted-foreground"
          }`}
        >
          {team.status}
        </span>
      </div>

      {/* Aggregate Stats */}
      <div className="grid grid-cols-4 gap-px border-b bg-border">
        <div className="flex flex-col items-center bg-card px-3 py-2.5">
          <span className="text-xs text-muted-foreground">Avg ELO</span>
          <span className="mt-0.5 text-lg font-bold tabular-nums">
            {Math.round(totalElo)}
          </span>
        </div>
        <div className="flex flex-col items-center bg-card px-3 py-2.5">
          <span className="text-xs text-muted-foreground">Tasks</span>
          <span className="mt-0.5 text-lg font-bold tabular-nums">
            {totalTasks}
          </span>
        </div>
        <div className="flex flex-col items-center bg-card px-3 py-2.5">
          <span className="text-xs text-muted-foreground">Avg Rating</span>
          <span className="mt-0.5 text-lg font-bold tabular-nums">
            {teamAvgRating > 0 ? teamAvgRating.toFixed(1) : "—"}
          </span>
        </div>
        <div className="flex flex-col items-center bg-card px-3 py-2.5">
          <span className="text-xs text-muted-foreground">Active</span>
          <span className="mt-0.5 text-lg font-bold tabular-nums">
            {busyCount}/{teamAgents.length}
          </span>
        </div>
      </div>

      {/* Per-role breakdown */}
      <div className="px-4 py-3">
        <p className="mb-2 text-xs font-medium text-muted-foreground">
          Role Breakdown
        </p>
        <div className="space-y-1.5">
          {roleSummaries.map((rs) => (
            <div
              key={rs.role}
              className="flex items-center gap-2 text-sm"
            >
              <span
                className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-xs font-medium ${roleColor(rs.role)}`}
              >
                {roleIcon(rs.role)}
                {roleLabel(rs.role)}
              </span>
              <span className="text-xs text-muted-foreground">
                ×{rs.count}
              </span>
              <span className="ml-auto flex items-center gap-3 text-xs tabular-nums text-muted-foreground">
                <span className="inline-flex items-center gap-1" title="ELO">
                  <TrendingUp className="h-3 w-3" />
                  {rs.avgElo}
                </span>
                <span className="inline-flex items-center gap-1" title="Tasks">
                  <Target className="h-3 w-3" />
                  {rs.totalTasks}
                </span>
                {rs.avgRating > 0 && (
                  <span className="inline-flex items-center gap-1" title="Rating">
                    <Star className="h-3 w-3 fill-yellow-400 text-yellow-400" />
                    {rs.avgRating.toFixed(1)}
                  </span>
                )}
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
