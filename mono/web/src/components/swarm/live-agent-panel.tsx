// ─── Live Agent Panel ────────────────────────────────────────────────────────
// Shows a real-time visualization of all online agents.
// The controller agent is always visible with a pulsing animation.
// As tasks start, agents dynamically appear with a spin-up animation.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useMemo } from "react";
import {
  Bot,
  BrainCircuit,
  Code,
  Zap,
  CheckCircle2,
  Shield,
  FileText,
  Settings,
  Search,
  Loader2,
  Wifi,
  Palette,
} from "lucide-react";
import { AgentAvatar } from "@/components/swarm/agent-avatar";
import type { AgentRole, AgentSnapshot } from "@/types/swarm";

// ── Role config ──────────────────────────────────────────────────────────────

const roleConfig: Record<
  AgentRole,
  { icon: typeof Bot; label: string; color: string; bg: string }
> = {
  orchestrator: {
    icon: BrainCircuit,
    label: "Orchestrator",
    color: "text-violet-600 dark:text-violet-400",
    bg: "bg-violet-100 dark:bg-violet-900/40",
  },
  architect: {
    icon: Search,
    label: "Architect",
    color: "text-cyan-600 dark:text-cyan-400",
    bg: "bg-cyan-100 dark:bg-cyan-900/40",
  },
  senior_dev: {
    icon: Code,
    label: "Senior Dev",
    color: "text-blue-600 dark:text-blue-400",
    bg: "bg-blue-100 dark:bg-blue-900/40",
  },
  junior_dev: {
    icon: Zap,
    label: "Junior Dev",
    color: "text-amber-600 dark:text-amber-400",
    bg: "bg-amber-100 dark:bg-amber-900/40",
  },
  qa: {
    icon: CheckCircle2,
    label: "QA",
    color: "text-green-600 dark:text-green-400",
    bg: "bg-green-100 dark:bg-green-900/40",
  },
  security: {
    icon: Shield,
    label: "Security",
    color: "text-red-600 dark:text-red-400",
    bg: "bg-red-100 dark:bg-red-900/40",
  },
  docs: {
    icon: FileText,
    label: "Docs",
    color: "text-teal-600 dark:text-teal-400",
    bg: "bg-teal-100 dark:bg-teal-900/40",
  },
  ops: {
    icon: Settings,
    label: "Ops",
    color: "text-orange-600 dark:text-orange-400",
    bg: "bg-orange-100 dark:bg-orange-900/40",
  },
  ui_ux: {
    icon: Palette,
    label: "UI/UX",
    color: "text-pink-600 dark:text-pink-400",
    bg: "bg-pink-100 dark:bg-pink-900/40",
  },
};

// ── Agent Node ───────────────────────────────────────────────────────────────

function AgentNode({
  agent,
  isController,
}: {
  agent: AgentSnapshot;
  isController: boolean;
}) {
  const config = roleConfig[agent.role] ?? roleConfig.orchestrator;
  const isBusy = agent.status === "busy";

  return (
    <div
      className={`
        relative flex flex-col items-center gap-1.5 rounded-xl border p-3
        transition-all duration-500 ease-out
        ${isBusy ? "border-amber-400/60 shadow-md shadow-amber-500/10" : "border-border"}
        ${isController ? "ring-2 ring-primary/20" : ""}
        animate-in fade-in slide-in-from-bottom-2
      `}
    >
      {/* Pulse ring for busy agents */}
      {isBusy && (
        <span className="absolute -inset-px rounded-xl animate-pulse border border-amber-400/40" />
      )}

      {/* Animated Avatar */}
      <AgentAvatar role={agent.role} size="md" busy={isBusy} />

      {/* Label */}
      <span className="text-[11px] font-medium leading-none">
        {config.label}
      </span>

      {/* Status indicator */}
      <div className="flex items-center gap-1">
        {isBusy ? (
          <Loader2 className="h-3 w-3 animate-spin text-amber-500" />
        ) : (
          <span className="h-2 w-2 rounded-full bg-emerald-400" />
        )}
        <span className="text-[10px] text-muted-foreground">
          {isBusy ? "Working" : "Idle"}
        </span>
      </div>

      {/* Controller badge */}
      {isController && (
        <span className="absolute -top-2 left-1/2 -translate-x-1/2 rounded-full bg-primary px-1.5 py-0.5 text-[9px] font-bold text-primary-foreground">
          CTRL
        </span>
      )}
    </div>
  );
}

// ── Main Component ───────────────────────────────────────────────────────────

interface LiveAgentPanelProps {
  agents: AgentSnapshot[];
  className?: string;
}

export function LiveAgentPanel({ agents, className }: LiveAgentPanelProps) {
  const controllerAgents = useMemo(
    () => agents.filter((a) => a.team_id === "" || a.team_id === "00000000-0000-0000-0000-000000000000"),
    [agents]
  );

  const taskAgents = useMemo(
    () => agents.filter((a) => a.team_id && a.team_id !== "" && a.team_id !== "00000000-0000-0000-0000-000000000000"),
    [agents]
  );

  // Group task agents by team
  const teamGroups = useMemo(() => {
    const map = new Map<string, AgentSnapshot[]>();
    for (const agent of taskAgents) {
      const list = map.get(agent.team_id!) ?? [];
      list.push(agent);
      map.set(agent.team_id!, list);
    }
    return map;
  }, [taskAgents]);

  return (
    <div className={`rounded-xl border bg-card ${className ?? ""}`}>
      <div className="border-b px-4 py-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Bot className="h-4 w-4 text-primary" />
            <h3 className="text-sm font-semibold">Live Agents</h3>
          </div>
          <div className="flex items-center gap-1.5">
            <Wifi className="h-3 w-3 text-emerald-500" />
            <span className="text-xs text-muted-foreground">
              {agents.length} online
            </span>
          </div>
        </div>
      </div>

      <div className="p-4 space-y-4">
        {/* Controller — always visible */}
        {controllerAgents.length > 0 ? (
          <div>
            <p className="mb-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              Controller
            </p>
            <div className="flex gap-2">
              {controllerAgents.map((a) => (
                <AgentNode key={a.id} agent={a} isController />
              ))}
            </div>
          </div>
        ) : (
          <div className="flex flex-col items-center py-4 text-muted-foreground">
            <Bot className="mb-2 h-8 w-8 opacity-30 animate-pulse" />
            <p className="text-xs">Waiting for controller…</p>
          </div>
        )}

        {/* Task agents — grouped by team */}
        {teamGroups.size > 0 && (
          <div>
            <p className="mb-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              Task Agents
            </p>
            {Array.from(teamGroups.entries()).map(([teamId, members]) => (
              <div key={teamId} className="mb-3 last:mb-0">
                <p className="mb-1.5 text-[10px] text-muted-foreground">
                  Team {teamId.slice(0, 8)}
                </p>
                <div className="grid grid-cols-2 gap-2">
                  {members.map((a) => (
                    <AgentNode key={a.id} agent={a} isController={false} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}

        {agents.length === 0 && (
          <div className="flex flex-col items-center py-8 text-muted-foreground">
            <Bot className="mb-2 h-10 w-10 opacity-20" />
            <p className="text-sm">No agents online</p>
            <p className="text-xs">Start the swarm to see agents here</p>
          </div>
        )}
      </div>
    </div>
  );
}
