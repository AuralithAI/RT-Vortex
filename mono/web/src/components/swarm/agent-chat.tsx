// ─── Agent Chat ──────────────────────────────────────────────────────────────
// Live conversation feed showing agents talking to each other in real time.
// Displays agent thinking, tool calls, file edits, and errors as a chat-like
// transcript — similar to Grok's expert mode.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useRef, useEffect } from "react";
import {
  Bot,
  BrainCircuit,
  Code,
  CheckCircle2,
  Shield,
  FileText,
  Settings,
  Zap,
  Search,
  FileEdit,
  FilePlus,
  Trash2,
  FolderOpen,
  Terminal,
  AlertCircle,
} from "lucide-react";
import type { SwarmWsEvent } from "@/hooks/use-swarm-events";

// ── Role styling ────────────────────────────────────────────────────────────

const roleStyles: Record<
  string,
  { icon: typeof Bot; label: string; color: string; bg: string; border: string }
> = {
  orchestrator: {
    icon: BrainCircuit,
    label: "Orchestrator",
    color: "text-violet-700 dark:text-violet-300",
    bg: "bg-violet-50 dark:bg-violet-950/40",
    border: "border-violet-200 dark:border-violet-800",
  },
  senior_dev: {
    icon: Code,
    label: "Senior Dev",
    color: "text-blue-700 dark:text-blue-300",
    bg: "bg-blue-50 dark:bg-blue-950/40",
    border: "border-blue-200 dark:border-blue-800",
  },
  junior_dev: {
    icon: Zap,
    label: "Junior Dev",
    color: "text-amber-700 dark:text-amber-300",
    bg: "bg-amber-50 dark:bg-amber-950/40",
    border: "border-amber-200 dark:border-amber-800",
  },
  architect: {
    icon: Search,
    label: "Architect",
    color: "text-cyan-700 dark:text-cyan-300",
    bg: "bg-cyan-50 dark:bg-cyan-950/40",
    border: "border-cyan-200 dark:border-cyan-800",
  },
  qa: {
    icon: CheckCircle2,
    label: "QA",
    color: "text-green-700 dark:text-green-300",
    bg: "bg-green-50 dark:bg-green-950/40",
    border: "border-green-200 dark:border-green-800",
  },
  security: {
    icon: Shield,
    label: "Security",
    color: "text-red-700 dark:text-red-300",
    bg: "bg-red-50 dark:bg-red-950/40",
    border: "border-red-200 dark:border-red-800",
  },
  docs: {
    icon: FileText,
    label: "Docs",
    color: "text-teal-700 dark:text-teal-300",
    bg: "bg-teal-50 dark:bg-teal-950/40",
    border: "border-teal-200 dark:border-teal-800",
  },
  ops: {
    icon: Settings,
    label: "Ops",
    color: "text-orange-700 dark:text-orange-300",
    bg: "bg-orange-50 dark:bg-orange-950/40",
    border: "border-orange-200 dark:border-orange-800",
  },
};

const defaultStyle = {
  icon: Bot,
  label: "Agent",
  color: "text-gray-700 dark:text-gray-300",
  bg: "bg-gray-50 dark:bg-gray-950/40",
  border: "border-gray-200 dark:border-gray-800",
};

// ── Kind icon ───────────────────────────────────────────────────────────────

function kindIcon(kind: string, metadata?: Record<string, unknown>) {
  const tool = metadata?.tool as string | undefined;

  if (kind === "tool_call") {
    if (tool?.includes("read") || tool?.includes("get_file")) {
      return <FolderOpen className="h-3.5 w-3.5" />;
    }
    if (tool?.includes("edit")) {
      return <FileEdit className="h-3.5 w-3.5" />;
    }
    if (tool?.includes("create")) {
      return <FilePlus className="h-3.5 w-3.5" />;
    }
    if (tool?.includes("delete")) {
      return <Trash2 className="h-3.5 w-3.5" />;
    }
    if (tool?.includes("search") || tool?.includes("grep")) {
      return <Search className="h-3.5 w-3.5" />;
    }
    if (tool?.includes("list")) {
      return <FolderOpen className="h-3.5 w-3.5" />;
    }
    if (tool?.includes("command") || tool?.includes("run")) {
      return <Terminal className="h-3.5 w-3.5" />;
    }
    return <Zap className="h-3.5 w-3.5" />;
  }
  if (kind === "edit") {
    return <FileEdit className="h-3.5 w-3.5" />;
  }
  if (kind === "error") {
    return <AlertCircle className="h-3.5 w-3.5" />;
  }
  return null; // thinking — no extra icon
}

// ── Chat message ────────────────────────────────────────────────────────────

interface ChatMessage {
  agentId: string;
  agentRole: string;
  kind: string;
  content: string;
  metadata?: Record<string, unknown>;
  timestamp: string;
}

function extractChatMessages(events: SwarmWsEvent[]): ChatMessage[] {
  const messages: ChatMessage[] = [];
  for (const evt of events) {
    if (evt.type !== "swarm_agent") continue;
    // Filter to chat-like events (thinking, tool_call, edit, error).
    const kind = evt.event;
    if (!["thinking", "tool_call", "edit", "error"].includes(kind)) continue;

    messages.push({
      agentId: evt.agent_id ?? "unknown",
      agentRole: (evt.data?.agent_role as string) ?? "agent",
      kind,
      content: (evt.data?.content as string) ?? "",
      metadata: evt.data?.metadata as Record<string, unknown> | undefined,
      timestamp: evt.timestamp,
    });
  }
  return messages;
}

function formatTime(ts: string): string {
  try {
    const d = new Date(ts);
    return d.toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return "";
  }
}

// ── Component ───────────────────────────────────────────────────────────────

interface AgentChatProps {
  events: SwarmWsEvent[];
  maxMessages?: number;
}

export function AgentChat({ events, maxMessages = 200 }: AgentChatProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const messages = extractChatMessages(events).slice(-maxMessages);

  // Auto-scroll to bottom on new messages.
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages.length]);

  if (messages.length === 0) {
    return (
      <div className="rounded-lg border bg-card p-8 text-center">
        <BrainCircuit className="mx-auto mb-3 h-10 w-10 text-muted-foreground/40" />
        <p className="text-sm font-medium text-muted-foreground">
          Agent conversation will appear here
        </p>
        <p className="mt-1 text-xs text-muted-foreground/70">
          Watch agents discuss, plan, and implement changes in real time
        </p>
      </div>
    );
  }

  // Group consecutive messages from the same agent.
  const grouped = groupMessages(messages);

  return (
    <div className="rounded-lg border bg-card">
      <div className="border-b px-4 py-3">
        <h3 className="text-sm font-semibold">Agent Conversation</h3>
        <p className="text-xs text-muted-foreground">
          {messages.length} messages from {countUniqueAgents(messages)} agents
        </p>
      </div>
      <div ref={scrollRef} className="max-h-[600px] overflow-y-auto p-3 space-y-3">
        {grouped.map((group, gi) => {
          const style = roleStyles[group.agentRole] ?? defaultStyle;
          const Icon = style.icon;

          return (
            <div key={gi} className={`rounded-lg border p-3 ${style.bg} ${style.border}`}>
              {/* Agent header */}
              <div className="mb-2 flex items-center gap-2">
                <div className={`rounded-md bg-background p-1.5 ${style.color}`}>
                  <Icon className="h-4 w-4" />
                </div>
                <span className={`text-sm font-semibold ${style.color}`}>
                  {style.label}
                </span>
                <span className="text-xs text-muted-foreground">
                  {group.agentId.substring(0, 8)}
                </span>
                <span className="ml-auto text-xs text-muted-foreground">
                  {formatTime(group.messages[0].timestamp)}
                </span>
              </div>

              {/* Messages */}
              <div className="space-y-1.5">
                {group.messages.map((msg, mi) => (
                  <div key={mi} className="flex items-start gap-2">
                    {msg.kind !== "thinking" && (
                      <span className="mt-0.5 text-muted-foreground">
                        {kindIcon(msg.kind, msg.metadata)}
                      </span>
                    )}
                    <p
                      className={`text-sm leading-relaxed ${
                        msg.kind === "error"
                          ? "text-red-600 dark:text-red-400"
                          : msg.kind === "tool_call"
                            ? "text-muted-foreground font-mono text-xs"
                            : "text-foreground"
                      }`}
                    >
                      {msg.content}
                    </p>
                  </div>
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ── Utilities ───────────────────────────────────────────────────────────────

interface MessageGroup {
  agentId: string;
  agentRole: string;
  messages: ChatMessage[];
}

function groupMessages(messages: ChatMessage[]): MessageGroup[] {
  const groups: MessageGroup[] = [];
  let current: MessageGroup | null = null;

  for (const msg of messages) {
    if (current && current.agentId === msg.agentId) {
      current.messages.push(msg);
    } else {
      current = {
        agentId: msg.agentId,
        agentRole: msg.agentRole,
        messages: [msg],
      };
      groups.push(current);
    }
  }

  return groups;
}

function countUniqueAgents(messages: ChatMessage[]): number {
  return new Set(messages.map((m) => m.agentId)).size;
}
