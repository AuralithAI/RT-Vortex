// ─── Task Agent List ─────────────────────────────────────────────────────────
// Shows all agents working on a specific task with their roles and statuses.
// Polls GET /api/v1/swarm/tasks/{id}/agents every 5 seconds.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
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
} from "lucide-react";
import type { AgentRole, AgentSnapshot } from "@/types/swarm";

const roleConfig: Record<
  AgentRole,
  { icon: typeof Bot; label: string; color: string; bgColor: string }
> = {
  orchestrator: {
    icon: BrainCircuit,
    label: "Orchestrator",
    color: "text-violet-600 dark:text-violet-400",
    bgColor: "bg-violet-100 dark:bg-violet-900/40",
  },
  architect: {
    icon: Search,
    label: "Architect",
    color: "text-cyan-600 dark:text-cyan-400",
    bgColor: "bg-cyan-100 dark:bg-cyan-900/40",
  },
  senior_dev: {
    icon: Code,
    label: "Senior Dev",
    color: "text-blue-600 dark:text-blue-400",
    bgColor: "bg-blue-100 dark:bg-blue-900/40",
  },
  junior_dev: {
    icon: Zap,
    label: "Junior Dev",
    color: "text-amber-600 dark:text-amber-400",
    bgColor: "bg-amber-100 dark:bg-amber-900/40",
  },
  qa: {
    icon: CheckCircle2,
    label: "QA",
    color: "text-green-600 dark:text-green-400",
    bgColor: "bg-green-100 dark:bg-green-900/40",
  },
  security: {
    icon: Shield,
    label: "Security",
    color: "text-red-600 dark:text-red-400",
    bgColor: "bg-red-100 dark:bg-red-900/40",
  },
  docs: {
    icon: FileText,
    label: "Docs",
    color: "text-teal-600 dark:text-teal-400",
    bgColor: "bg-teal-100 dark:bg-teal-900/40",
  },
  ops: {
    icon: Settings,
    label: "Ops",
    color: "text-orange-600 dark:text-orange-400",
    bgColor: "bg-orange-100 dark:bg-orange-900/40",
  },
};

const statusLabels: Record<string, { label: string; dotColor: string }> = {
  busy: { label: "Working", dotColor: "bg-amber-400 animate-pulse" },
  idle: { label: "Idle", dotColor: "bg-emerald-400" },
  offline: { label: "Offline", dotColor: "bg-gray-400" },
  errored: { label: "Error", dotColor: "bg-red-500" },
};

interface TaskAgentListProps {
  taskId: string;
}

export function TaskAgentList({ taskId }: TaskAgentListProps) {
  const [agents, setAgents] = useState<AgentSnapshot[]>([]);

  const fetchAgents = useCallback(async () => {
    try {
      const res = await fetch(`/api/v1/swarm/tasks/${taskId}/agents`);
      if (res.ok) {
        const data = await res.json();
        setAgents(data.agents ?? []);
      }
    } catch {
      /* retry on next interval */
    }
  }, [taskId]);

  useEffect(() => {
    fetchAgents();
    const iv = setInterval(fetchAgents, 5_000);
    return () => clearInterval(iv);
  }, [fetchAgents]);

  if (agents.length === 0) {
    return (
      <div className="rounded-lg border bg-card p-6">
        <h3 className="mb-3 font-semibold flex items-center gap-2">
          <Bot className="h-4 w-4" />
          Agents
        </h3>
        <p className="text-sm text-muted-foreground">
          No agents assigned yet.
        </p>
      </div>
    );
  }

  const busyCount = agents.filter((a) => a.status === "busy").length;

  return (
    <div className="rounded-lg border bg-card p-6">
      <h3 className="mb-1 font-semibold flex items-center gap-2">
        <Bot className="h-4 w-4" />
        Agents Working
      </h3>
      <p className="mb-4 text-xs text-muted-foreground">
        {busyCount} of {agents.length} active
      </p>

      <div className="space-y-2">
        {agents.map((agent) => {
          const config = roleConfig[agent.role] ?? roleConfig.orchestrator;
          const Icon = config.icon;
          const status = statusLabels[agent.status] ?? statusLabels.idle;

          return (
            <div
              key={agent.id}
              className="flex items-center gap-3 rounded-lg border bg-background p-3 transition-all"
            >
              <div className={`rounded-md p-1.5 ${config.bgColor}`}>
                <Icon className={`h-4 w-4 ${config.color}`} />
              </div>

              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium">{config.label}</p>
                <p className="text-[11px] text-muted-foreground truncate">
                  {agent.id.slice(0, 8)}
                </p>
              </div>

              <div className="flex items-center gap-1.5">
                {agent.status === "busy" ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin text-amber-500" />
                ) : (
                  <span className={`h-2 w-2 rounded-full ${status.dotColor}`} />
                )}
                <span className="text-xs text-muted-foreground">
                  {status.label}
                </span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
