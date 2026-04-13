// ─── Live Agent Chat ─────────────────────────────────────────────────────────
// Enhanced live conversation feed showing agents talking to each other in real
// time. Each message shows the agent's animated avatar, role, thinking state,
// tool calls, file edits, and errors as a beautiful chat-like transcript.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useRef, useState, useEffect } from "react";
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
  ChevronDown,
  ChevronUp,
  Activity,
  Eye,
  Filter,
  MessageCircle,
} from "lucide-react";
import { AgentAvatar } from "@/components/swarm/agent-avatar";
import { LLMMarkdown } from "@/components/ui/llm-markdown";
import { sanitizeLLMContent } from "@/lib/sanitize-llm-content";
import type { SwarmWsEvent } from "@/hooks/use-swarm-events";

// ── Role styling ────────────────────────────────────────────────────────────

const roleStyles: Record<
  string,
  { icon: typeof Bot; label: string; color: string; bg: string; border: string; accent: string }
> = {
  orchestrator: {
    icon: BrainCircuit,
    label: "Orchestrator",
    color: "text-violet-700 dark:text-violet-300",
    bg: "bg-violet-50/80 dark:bg-violet-950/40",
    border: "border-violet-200/60 dark:border-violet-800/60",
    accent: "from-violet-500 to-purple-500",
  },
  senior_dev: {
    icon: Code,
    label: "Senior Dev",
    color: "text-blue-700 dark:text-blue-300",
    bg: "bg-blue-50/80 dark:bg-blue-950/40",
    border: "border-blue-200/60 dark:border-blue-800/60",
    accent: "from-blue-500 to-indigo-500",
  },
  junior_dev: {
    icon: Zap,
    label: "Junior Dev",
    color: "text-amber-700 dark:text-amber-300",
    bg: "bg-amber-50/80 dark:bg-amber-950/40",
    border: "border-amber-200/60 dark:border-amber-800/60",
    accent: "from-amber-500 to-orange-500",
  },
  architect: {
    icon: Search,
    label: "Architect",
    color: "text-cyan-700 dark:text-cyan-300",
    bg: "bg-cyan-50/80 dark:bg-cyan-950/40",
    border: "border-cyan-200/60 dark:border-cyan-800/60",
    accent: "from-cyan-500 to-teal-500",
  },
  qa: {
    icon: CheckCircle2,
    label: "QA",
    color: "text-green-700 dark:text-green-300",
    bg: "bg-green-50/80 dark:bg-green-950/40",
    border: "border-green-200/60 dark:border-green-800/60",
    accent: "from-green-500 to-emerald-500",
  },
  security: {
    icon: Shield,
    label: "Security",
    color: "text-red-700 dark:text-red-300",
    bg: "bg-red-50/80 dark:bg-red-950/40",
    border: "border-red-200/60 dark:border-red-800/60",
    accent: "from-red-500 to-rose-500",
  },
  docs: {
    icon: FileText,
    label: "Docs",
    color: "text-teal-700 dark:text-teal-300",
    bg: "bg-teal-50/80 dark:bg-teal-950/40",
    border: "border-teal-200/60 dark:border-teal-800/60",
    accent: "from-teal-500 to-cyan-500",
  },
  ops: {
    icon: Settings,
    label: "Ops",
    color: "text-orange-700 dark:text-orange-300",
    bg: "bg-orange-50/80 dark:bg-orange-950/40",
    border: "border-orange-200/60 dark:border-orange-800/60",
    accent: "from-orange-500 to-red-500",
  },
  ui_ux: {
    icon: Zap,
    label: "UI/UX",
    color: "text-pink-700 dark:text-pink-300",
    bg: "bg-pink-50/80 dark:bg-pink-950/40",
    border: "border-pink-200/60 dark:border-pink-800/60",
    accent: "from-pink-500 to-rose-500",
  },
  builder: {
    icon: Settings,
    label: "Builder",
    color: "text-yellow-700 dark:text-yellow-300",
    bg: "bg-yellow-50/80 dark:bg-yellow-950/40",
    border: "border-yellow-200/60 dark:border-yellow-800/60",
    accent: "from-yellow-500 to-amber-500",
  },
};

const defaultStyle = {
  icon: Bot,
  label: "Agent",
  color: "text-gray-700 dark:text-gray-300",
  bg: "bg-gray-50/80 dark:bg-gray-950/40",
  border: "border-gray-200/60 dark:border-gray-800/60",
  accent: "from-gray-500 to-gray-600",
};

// ── Kind badge ──────────────────────────────────────────────────────────────

function kindBadge(kind: string, metadata?: Record<string, unknown>) {
  const tool = metadata?.tool as string | undefined;

  if (kind === "thinking") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-yellow-100 px-2 py-0.5 text-[10px] font-medium text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300">
        <BrainCircuit className="h-2.5 w-2.5" />
        Thinking
      </span>
    );
  }

  if (kind === "tool_call") {
    let icon = <Zap className="h-2.5 w-2.5" />;
    let label = tool || "Tool Call";

    if (tool?.includes("read") || tool?.includes("get_file")) {
      icon = <FolderOpen className="h-2.5 w-2.5" />;
      label = "Reading File";
    } else if (tool?.includes("edit")) {
      icon = <FileEdit className="h-2.5 w-2.5" />;
      label = "Editing File";
    } else if (tool?.includes("create")) {
      icon = <FilePlus className="h-2.5 w-2.5" />;
      label = "Creating File";
    } else if (tool?.includes("delete")) {
      icon = <Trash2 className="h-2.5 w-2.5" />;
      label = "Deleting";
    } else if (tool?.includes("search") || tool?.includes("grep")) {
      icon = <Search className="h-2.5 w-2.5" />;
      label = "Searching";
    } else if (tool?.includes("command") || tool?.includes("run")) {
      icon = <Terminal className="h-2.5 w-2.5" />;
      label = "Running Command";
    }

    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-purple-100 px-2 py-0.5 text-[10px] font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">
        {icon}
        {label}
      </span>
    );
  }

  if (kind === "edit") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-blue-100 px-2 py-0.5 text-[10px] font-medium text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
        <FileEdit className="h-2.5 w-2.5" />
        File Edit
      </span>
    );
  }

  if (kind === "error") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-red-100 px-2 py-0.5 text-[10px] font-medium text-red-700 dark:bg-red-900/40 dark:text-red-300">
        <AlertCircle className="h-2.5 w-2.5" />
        Error
      </span>
    );
  }

  return null;
}

// ── Chat message type ───────────────────────────────────────────────────────

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

// ── Message Group ───────────────────────────────────────────────────────────

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

// ── Filter types ────────────────────────────────────────────────────────────

type MessageFilter = "all" | "thinking" | "tool_call" | "edit" | "error";

// ── Main Component ──────────────────────────────────────────────────────────

interface LiveAgentChatProps {
  events: SwarmWsEvent[];
  maxMessages?: number;
}

export function LiveAgentChat({ events, maxMessages = 200 }: LiveAgentChatProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [filter, setFilter] = useState<MessageFilter>("all");
  const [autoScroll, setAutoScroll] = useState(true);
  const prevCountRef = useRef(0);

  const allMessages = extractChatMessages(events).slice(-maxMessages);
  const reversed = [...allMessages].reverse();
  const filtered =
    filter === "all" ? reversed : reversed.filter((m) => m.kind === filter);
  const grouped = groupMessages(filtered);

  const uniqueAgents = countUniqueAgents(allMessages);
  const hasNewMessages = allMessages.length > prevCountRef.current;

  useEffect(() => {
    prevCountRef.current = allMessages.length;
    if (autoScroll && scrollRef.current && hasNewMessages) {
      scrollRef.current.scrollTop = 0;
    }
  }, [allMessages.length, autoScroll, hasNewMessages]);

  if (allMessages.length === 0) {
    return (
      <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
        <div className="flex flex-col items-center justify-center py-16">
          <div className="mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-100 to-purple-100 dark:from-violet-900/40 dark:to-purple-900/40">
            <BrainCircuit className="h-8 w-8 text-violet-400" />
          </div>
          <p className="text-sm font-semibold text-muted-foreground">
            Agent conversation will appear here
          </p>
          <p className="mt-1 text-xs text-muted-foreground/70">
            Watch agents discuss, plan, and implement changes in real time
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
      {/* Header */}
      <div className="flex items-center justify-between border-b px-5 py-3">
        <div className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-violet-100 to-purple-100 dark:from-violet-900/40 dark:to-purple-900/40">
            <MessageCircle className="h-4 w-4 text-violet-600 dark:text-violet-400" />
          </div>
          <div>
            <h3 className="text-sm font-semibold">Agent Conversation</h3>
            <p className="text-[11px] text-muted-foreground">
              {allMessages.length} messages from {uniqueAgents} agent{uniqueAgents !== 1 ? "s" : ""}
              {hasNewMessages && (
                <span className="ml-1.5 inline-flex items-center gap-1 text-green-600 dark:text-green-400">
                  <Activity className="h-2.5 w-2.5 animate-pulse" />
                  Live
                </span>
              )}
            </p>
          </div>
        </div>

        {/* Filter buttons */}
        <div className="flex items-center gap-1">
          {(
            [
              { key: "all", label: "All" },
              { key: "thinking", label: "Thinking" },
              { key: "tool_call", label: "Tools" },
              { key: "error", label: "Errors" },
            ] as { key: MessageFilter; label: string }[]
          ).map((f) => (
            <button
              key={f.key}
              onClick={() => setFilter(f.key)}
              className={`rounded-md px-2.5 py-1 text-[11px] font-medium transition-colors ${
                filter === f.key
                  ? "bg-violet-100 text-violet-700 dark:bg-violet-900/40 dark:text-violet-300"
                  : "text-muted-foreground hover:bg-muted"
              }`}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      {/* Messages */}
      <div ref={scrollRef} className="max-h-[600px] overflow-y-auto p-4 space-y-3">
        {grouped.map((group, gi) => {
          const style = roleStyles[group.agentRole] ?? defaultStyle;

          return (
            <div
              key={gi}
              className={`overflow-hidden rounded-xl border transition-all duration-200 ${style.bg} ${style.border}`}
            >
              {/* Role accent bar */}
              <div className={`h-0.5 bg-gradient-to-r ${style.accent}`} />

              {/* Agent header */}
              <div className="flex items-center gap-2.5 px-4 py-2.5">
                <AgentAvatar role={group.agentRole} size="sm" busy={false} />
                <span className={`text-sm font-semibold ${style.color}`}>
                  {style.label}
                </span>
                <span className="rounded-md bg-black/5 px-1.5 py-0.5 text-[10px] font-mono text-muted-foreground dark:bg-white/5">
                  {group.agentId.substring(0, 8)}
                </span>
                <span className="ml-auto text-[11px] text-muted-foreground">
                  {formatTime(group.messages[0].timestamp)}
                </span>
              </div>

              {/* Messages */}
              <div className="space-y-2 px-4 pb-3">
                {group.messages.map((msg, mi) => (
                  <div key={mi} className="flex items-start gap-2">
                    {/* Kind badge */}
                    <div className="flex-shrink-0 pt-0.5">
                      {kindBadge(msg.kind, msg.metadata)}
                    </div>

                    {/* Content */}
                    {msg.kind === "tool_call" ? (
                      <p className="font-mono text-xs text-muted-foreground leading-relaxed">
                        {msg.content}
                      </p>
                    ) : msg.kind === "error" ? (
                      <p className="text-[13px] leading-relaxed text-red-600 dark:text-red-400">
                        {msg.content}
                      </p>
                    ) : (
                      (() => {
                        const cleaned = sanitizeLLMContent(msg.content);
                        return cleaned ? (
                          <LLMMarkdown
                            content={cleaned}
                            variant="light"
                            className="text-[13px]"
                          />
                        ) : null;
                      })()
                    )}
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
