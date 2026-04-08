// ─── Activity Feed ───────────────────────────────────────────────────────────
// Real-time feed of agent activity for a swarm task. Shows tool calls,
// status changes, diff submissions, and plan events as they happen.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import {
  Bot,
  FileCode,
  CheckCircle,
  XCircle,
  Search,
  FileText,
  Zap,
  Clock,
  Users,
  GitPullRequest,
  MessageSquare,
  Scale,
  Gavel,
  Brain,
} from "lucide-react";
import type { SwarmWsEvent } from "@/hooks/use-swarm-events";

interface ActivityFeedProps {
  events: SwarmWsEvent[];
  maxItems?: number;
}

function eventIcon(event: SwarmWsEvent) {
  const type = event.type;
  const eventName = event.event;

  if (type === "swarm_task") {
    switch (eventName) {
      case "created":
        return <Zap className="h-4 w-4 text-blue-500" />;
      case "status_changed":
        return <Clock className="h-4 w-4 text-yellow-500" />;
      case "completed":
        return <CheckCircle className="h-4 w-4 text-green-500" />;
      case "failed":
        return <XCircle className="h-4 w-4 text-red-500" />;
      default:
        return <Zap className="h-4 w-4 text-muted-foreground" />;
    }
  }

  if (type === "swarm_agent") {
    switch (eventName) {
      case "registered":
        return <Bot className="h-4 w-4 text-blue-500" />;
      case "tool_call":
        return <Search className="h-4 w-4 text-purple-500" />;
      case "thinking":
        return <Bot className="h-4 w-4 text-yellow-500 animate-pulse" />;
      default:
        return <Bot className="h-4 w-4 text-muted-foreground" />;
    }
  }

  if (type === "swarm_diff") {
    return <FileCode className="h-4 w-4 text-green-500" />;
  }

  if (type === "swarm_plan") {
    switch (eventName) {
      case "plan_submitted":
        return <FileText className="h-4 w-4 text-blue-500" />;
      case "plan_approved":
        return <CheckCircle className="h-4 w-4 text-green-500" />;
      case "plan_rejected":
        return <XCircle className="h-4 w-4 text-red-500" />;
      default:
        return <FileText className="h-4 w-4 text-muted-foreground" />;
    }
  }

  if (type === "swarm_discussion") {
    switch (eventName) {
      case "thread_opened":
        return <MessageSquare className="h-4 w-4 text-blue-500" />;
      case "provider_response":
        return <Brain className="h-4 w-4 text-purple-500" />;
      case "thread_completed":
        return <CheckCircle className="h-4 w-4 text-green-500" />;
      case "thread_synthesised":
        return <Scale className="h-4 w-4 text-emerald-500" />;
      case "consensus_result":
        return <Gavel className="h-4 w-4 text-amber-500" />;
      default:
        return <MessageSquare className="h-4 w-4 text-muted-foreground" />;
    }
  }

  return <Zap className="h-4 w-4 text-muted-foreground" />;
}

function eventMessage(event: SwarmWsEvent): string {
  const data = event.data || {};

  if (event.type === "swarm_task") {
    switch (event.event) {
      case "created":
        return "Task created";
      case "status_changed":
        return `Status changed to ${(data.new_status as string) || "unknown"}`;
      case "completed":
        return "Task completed";
      case "failed":
        return `Task failed: ${(data.reason as string) || "unknown"}`;
      default:
        return `Task event: ${event.event}`;
    }
  }

  if (event.type === "swarm_agent") {
    const agentId = event.agent_id
      ? event.agent_id.substring(0, 8)
      : "unknown";

    switch (event.event) {
      case "registered":
        return `Agent ${agentId} registered (${(data.role as string) || "unknown"})`;
      case "tool_call":
        return `Agent ${agentId} called ${(data.tool_name as string) || "tool"}`;
      case "thinking":
        return `Agent ${agentId} is thinking…`;
      case "completed":
        return `Agent ${agentId} finished`;
      default:
        return `Agent ${agentId}: ${event.event}`;
    }
  }

  if (event.type === "swarm_diff") {
    const filePath = (data.file_path as string) || (data.diff_id as string) || "file";
    return `Diff submitted for ${filePath}`;
  }

  if (event.type === "swarm_plan") {
    switch (event.event) {
      case "plan_submitted":
        return "Plan submitted for review";
      case "plan_approved":
        return "Plan approved — implementation starting";
      case "plan_rejected":
        return "Plan rejected";
      case "plan_commented":
        return "Comment added to plan";
      default:
        return `Plan event: ${event.event}`;
    }
  }

  if (event.type === "swarm_discussion") {
    const provider = (data.provider as string) || "";
    const model = (data.model as string) || "";
    const strategy = (data.strategy as string) || "";
    const threadId = (data.thread_id as string) || "";
    const shortThread = threadId ? threadId.substring(0, 8) : "";

    switch (event.event) {
      case "thread_opened": {
        const count = (data.provider_count as number) || 0;
        return `Discussion thread ${shortThread} opened — querying ${count} LLM${count !== 1 ? "s" : ""}`;
      }
      case "provider_response": {
        const latency = (data.latency_ms as number) || 0;
        const label = provider && model ? `${provider}/${model}` : provider || "LLM";
        return latency
          ? `${label} responded (${latency}ms)`
          : `${label} responded`;
      }
      case "thread_completed": {
        const success = (data.success_count as number) || 0;
        const total = (data.provider_count as number) || 0;
        return `Thread ${shortThread} complete — ${success}/${total} providers responded`;
      }
      case "thread_synthesised": {
        const winner = (data.selected_provider as string) || (data.provider as string) || "unknown";
        return `Thread synthesised — selected ${winner}`;
      }
      case "consensus_result": {
        const winner = (data.provider as string) || "unknown";
        const confidence = (data.confidence as number) || 0;
        const confPct = Math.round(confidence * 100);
        return strategy
          ? `Consensus (${strategy}): ${winner} selected (${confPct}% confidence)`
          : `Consensus: ${winner} selected (${confPct}% confidence)`;
      }
      default:
        return `Discussion: ${event.event}`;
    }
  }

  return event.event;
}

function formatTime(timestamp: string): string {
  try {
    const d = new Date(timestamp);
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  } catch {
    return "";
  }
}

export function ActivityFeed({ events, maxItems = 50 }: ActivityFeedProps) {
  // Filter out low-value noise events that would flood the feed.
  // provider_streaming_start is internal plumbing (spinner trigger);
  // provider_streaming_chunk is legacy (server no longer sends it).
  const meaningful = events.filter(
    (e) =>
      e.event !== "provider_streaming_start" &&
      e.event !== "provider_streaming_chunk",
  );
  const displayed = meaningful.slice(0, maxItems);

  if (displayed.length === 0) {
    return (
      <div className="rounded-lg border bg-card p-6 text-center">
        <Bot className="mx-auto mb-2 h-8 w-8 text-muted-foreground/50" />
        <p className="text-sm text-muted-foreground">
          No activity yet. Events will appear here in real time.
        </p>
      </div>
    );
  }

  return (
    <div className="rounded-lg border bg-card">
      <div className="border-b px-4 py-3">
        <h3 className="text-sm font-semibold">Activity Feed</h3>
      </div>
      <div className="max-h-96 divide-y overflow-y-auto">
        {displayed.map((event, i) => (
          <div
            key={i}
            className="flex items-start gap-3 px-4 py-2.5 text-sm"
          >
            <div className="mt-0.5 shrink-0">{eventIcon(event)}</div>
            <div className="min-w-0 flex-1">
              <p className="text-foreground">{eventMessage(event)}</p>
              {event.data?.detail != null && (
                <p className="mt-0.5 truncate text-xs text-muted-foreground">
                  {`${event.data.detail}`}
                </p>
              )}
            </div>
            <span className="shrink-0 text-xs text-muted-foreground">
              {formatTime(event.timestamp)}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
