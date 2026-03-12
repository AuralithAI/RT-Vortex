// ─── Team Grid ───────────────────────────────────────────────────────────────
// Live grid of active teams showing each team's agents, current activity,
// and status.  Powered by WebSocket events via use-swarm-events.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useMemo } from "react";
import {
  Bot,
  Users,
  Cpu,
  Loader2,
  WifiOff,
  CheckCircle2,
  AlertCircle,
  Code,
  Search,
  Shield,
  FileText,
  Settings,
  Zap,
  BrainCircuit,
} from "lucide-react";
import type { SwarmAgent, SwarmTeam, AgentRole, AgentStatus } from "@/types/swarm";

// ── Props ────────────────────────────────────────────────────────────────────

interface TeamGridProps {
  teams: SwarmTeam[];
  agents: SwarmAgent[];
  className?: string;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

const roleIcon: Record<AgentRole, typeof Bot> = {
  orchestrator: BrainCircuit,
  architect: Search,
  senior_dev: Code,
  junior_dev: Zap,
  qa: CheckCircle2,
  security: Shield,
  docs: FileText,
  ops: Settings,
};

const roleLabel: Record<AgentRole, string> = {
  orchestrator: "Orchestrator",
  architect: "Architect",
  senior_dev: "Senior Dev",
  junior_dev: "Junior Dev",
  qa: "QA",
  security: "Security",
  docs: "Docs",
  ops: "Ops",
};

const statusColor: Record<AgentStatus, string> = {
  offline: "bg-gray-400",
  idle: "bg-emerald-400",
  busy: "bg-amber-400 animate-pulse",
  errored: "bg-red-500",
};

const statusIcon: Record<AgentStatus, typeof Bot> = {
  offline: WifiOff,
  idle: CheckCircle2,
  busy: Loader2,
  errored: AlertCircle,
};

function teamStatusBadge(status: SwarmTeam["status"]) {
  switch (status) {
    case "busy":
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-800 dark:bg-amber-900/30 dark:text-amber-300">
          <Cpu className="h-3 w-3 animate-spin" /> Working
        </span>
      );
    case "idle":
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300">
          <CheckCircle2 className="h-3 w-3" /> Idle
        </span>
      );
    case "offline":
    default:
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-400">
          <WifiOff className="h-3 w-3" /> Offline
        </span>
      );
  }
}

// ── Agent Chip ───────────────────────────────────────────────────────────────

function AgentChip({ agent }: { agent: SwarmAgent }) {
  const Icon = roleIcon[agent.role] ?? Bot;
  const StatusIcon = statusIcon[agent.status] ?? Bot;

  return (
    <div className="flex items-center gap-2 rounded-lg border bg-card p-2 text-xs shadow-sm">
      <div className="relative">
        <Icon className="h-4 w-4 text-muted-foreground" />
        <span
          className={`absolute -bottom-0.5 -right-0.5 h-2 w-2 rounded-full ring-1 ring-background ${statusColor[agent.status]}`}
        />
      </div>
      <div className="flex flex-col min-w-0">
        <span className="truncate font-medium">{roleLabel[agent.role]}</span>
        <span className="truncate text-muted-foreground text-[10px]">
          ELO {Math.round(agent.elo_score)}
        </span>
      </div>
      {agent.status === "busy" && (
        <Loader2 className="ml-auto h-3 w-3 animate-spin text-amber-500" />
      )}
    </div>
  );
}

// ── Team Card ────────────────────────────────────────────────────────────────

function TeamCard({
  team,
  members,
}: {
  team: SwarmTeam;
  members: SwarmAgent[];
}) {
  const busyCount = members.filter((a) => a.status === "busy").length;

  return (
    <div className="rounded-xl border bg-card p-4 shadow-sm transition-shadow hover:shadow-md">
      {/* Header */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <Users className="h-4 w-4 text-primary" />
          <h3 className="text-sm font-semibold truncate max-w-[160px]">
            {team.name || `Team ${team.id.slice(0, 8)}`}
          </h3>
        </div>
        {teamStatusBadge(team.status)}
      </div>

      {/* Stats row */}
      <div className="flex items-center gap-4 text-xs text-muted-foreground mb-3">
        <span className="flex items-center gap-1">
          <Bot className="h-3 w-3" /> {members.length} agents
        </span>
        {busyCount > 0 && (
          <span className="flex items-center gap-1 text-amber-600 dark:text-amber-400">
            <Cpu className="h-3 w-3" /> {busyCount} active
          </span>
        )}
      </div>

      {/* Agent grid */}
      <div className="grid grid-cols-2 gap-1.5">
        {members.map((agent) => (
          <AgentChip key={agent.id} agent={agent} />
        ))}
        {members.length === 0 && (
          <p className="col-span-2 text-xs text-muted-foreground italic">
            No agents
          </p>
        )}
      </div>
    </div>
  );
}

// ── Main Component ───────────────────────────────────────────────────────────

export function TeamGrid({ teams, agents, className }: TeamGridProps) {
  // Map agents by team_id for fast lookup.
  const agentsByTeam = useMemo(() => {
    const map = new Map<string, SwarmAgent[]>();
    for (const agent of agents) {
      if (agent.team_id) {
        const list = map.get(agent.team_id) ?? [];
        list.push(agent);
        map.set(agent.team_id, list);
      }
    }
    return map;
  }, [agents]);

  if (teams.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
        <Users className="h-10 w-10 mb-2 opacity-40" />
        <p className="text-sm">No active teams</p>
      </div>
    );
  }

  return (
    <div className={`grid gap-4 sm:grid-cols-2 lg:grid-cols-3 ${className ?? ""}`}>
      {teams.map((team) => (
        <TeamCard
          key={team.id}
          team={team}
          members={agentsByTeam.get(team.id) ?? []}
        />
      ))}
    </div>
  );
}
