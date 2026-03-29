// ─── Real-Time Agent Progress Feed ───────────────────────────────────────────
// WebSocket-powered live feed showing what each agent is doing right now.
// Displays messages like "SeniorDev is reviewing line 42… QA found 2 issues".
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useRef } from "react";
import {
  Bot,
  CheckCircle,
  AlertTriangle,
  Search,
  FileCode,
  MessageSquare,
  Zap,
  Clock,
} from "lucide-react";
import { useSwarmEvents, type SwarmWsEvent } from "@/hooks/use-swarm-events";

interface AgentProgressFeedProps {
  taskId: string;
  maxItems?: number;
}

interface AgentActivity {
  agentId: string;
  role: string;
  action: string;
  detail: string;
  timestamp: string;
  status: "active" | "completed" | "warning" | "idle";
}

const roleColors: Record<string, string> = {
  orchestrator: "text-purple-600 dark:text-purple-400",
  senior_dev: "text-blue-600 dark:text-blue-400",
  junior_dev: "text-cyan-600 dark:text-cyan-400",
  architect: "text-indigo-600 dark:text-indigo-400",
  qa: "text-green-600 dark:text-green-400",
  security: "text-red-600 dark:text-red-400",
  docs: "text-amber-600 dark:text-amber-400",
  ops: "text-orange-600 dark:text-orange-400",
};

const roleIcons: Record<string, typeof Bot> = {
  orchestrator: Zap,
  senior_dev: FileCode,
  junior_dev: FileCode,
  architect: Search,
  qa: CheckCircle,
  security: AlertTriangle,
  docs: MessageSquare,
  ops: Clock,
};

function formatRole(role: string): string {
  return role
    .split("_")
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join("");
}

function parseAgentEvent(event: SwarmWsEvent): AgentActivity | null {
  if (event.type !== "swarm_agent") return null;

  const data = event.data ?? {};
  const role = (data.role as string) || "agent";
  const action = event.event || "working";

  let detail = "";
  let status: AgentActivity["status"] = "active";

  switch (action) {
    case "reviewing_file":
      detail = `is reviewing ${data.file || "a file"}${data.line ? ` around line ${data.line}` : ""}…`;
      break;
    case "found_issues":
      detail = `found ${data.count || "some"} issue${(data.count as number) !== 1 ? "s" : ""}`;
      status = "warning";
      break;
    case "generating_diff":
      detail = `is generating changes for ${data.file || "files"}…`;
      break;
    case "completed":
      detail = "finished their review";
      status = "completed";
      break;
    case "thinking":
      detail = `is analyzing ${data.context || "the codebase"}…`;
      break;
    case "tool_call":
      detail = `is using tool: ${data.tool || "unknown"}`;
      break;
    case "waiting_human":
      detail = "is waiting for human input…";
      status = "idle";
      break;
    case "memory_recall":
      detail = "is recalling relevant context from memory…";
      break;
    case "self_critique":
      detail = "is self-reviewing their output…";
      break;
    default:
      detail = (data.message as string) || `is ${action}…`;
  }

  return {
    agentId: event.agent_id || "",
    role,
    action,
    detail,
    timestamp: event.timestamp,
    status,
  };
}

export function AgentProgressFeed({ taskId, maxItems = 50 }: AgentProgressFeedProps) {
  const { events, connected } = useSwarmEvents(taskId);
  const [activities, setActivities] = useState<AgentActivity[]>([]);
  const feedRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const newActivities: AgentActivity[] = [];
    for (const event of events) {
      const activity = parseAgentEvent(event);
      if (activity) {
        newActivities.push(activity);
      }
    }
    setActivities(newActivities.slice(0, maxItems));
  }, [events, maxItems]);

  useEffect(() => {
    if (feedRef.current) {
      feedRef.current.scrollTop = 0;
    }
  }, [activities]);

  // Aggregate current agent states
  const currentAgents = new Map<string, AgentActivity>();
  for (const a of [...activities].reverse()) {
    if (!currentAgents.has(a.role)) {
      currentAgents.set(a.role, a);
    }
  }

  return (
    <div className="space-y-3">
      {/* Connection status */}
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span
          className={`inline-block w-2 h-2 rounded-full ${
            connected ? "bg-green-500" : "bg-red-500"
          }`}
        />
        {connected ? "Live" : "Reconnecting…"}
      </div>

      {/* Active agent summary bar */}
      {currentAgents.size > 0 && (
        <div className="flex flex-wrap gap-2">
          {Array.from(currentAgents.entries()).map(([role, activity]) => {
            const Icon = roleIcons[role] || Bot;
            const color = roleColors[role] || "text-muted-foreground";
            return (
              <div
                key={role}
                className="flex items-center gap-1.5 text-xs border rounded-full px-2.5 py-1 bg-muted/30"
              >
                <Icon className={`h-3 w-3 ${color}`} />
                <span className={`font-medium ${color}`}>{formatRole(role)}</span>
                <span className="text-muted-foreground truncate max-w-[200px]">
                  {activity.detail}
                </span>
                {activity.status === "active" && (
                  <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-500 animate-pulse" />
                )}
              </div>
            );
          })}
        </div>
      )}

      {/* Activity feed */}
      <div
        ref={feedRef}
        className="space-y-1 max-h-64 overflow-auto scrollbar-thin"
      >
        {activities.length === 0 && (
          <p className="text-sm text-muted-foreground py-4 text-center">
            Waiting for agent activity…
          </p>
        )}
        {activities.map((a, i) => {
          const Icon = roleIcons[a.role] || Bot;
          const color = roleColors[a.role] || "text-muted-foreground";
          const time = new Date(a.timestamp).toLocaleTimeString([], {
            hour: "2-digit",
            minute: "2-digit",
            second: "2-digit",
          });

          return (
            <div
              key={i}
              className="flex items-start gap-2 text-sm py-1 px-1 rounded hover:bg-muted/30 transition-colors"
            >
              <Icon className={`h-4 w-4 mt-0.5 shrink-0 ${color}`} />
              <div className="flex-1 min-w-0">
                <span className={`font-medium ${color}`}>
                  {formatRole(a.role)}
                </span>{" "}
                <span className="text-foreground">{a.detail}</span>
              </div>
              <span className="text-xs text-muted-foreground shrink-0 tabular-nums">
                {time}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
