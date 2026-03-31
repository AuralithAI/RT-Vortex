"use client";

// ─── RepoChat — Advanced AI Chat Interface ──────────────────────────────────
// Features:
//   ✦ RAG-powered: retrieves from C++ engine index, synthesizes with LLM
//   ✦ Streaming SSE: real-time token-by-token display
//   ✦ Voice input: Web Speech API (browser-native, zero cost)
//   ✦ Drag & drop: files/code snippets as attachments
//   ✦ Markdown + syntax highlighted code blocks
//   ✦ Citation links to specific file:line references
//   ✦ Thinking indicators: shows search/retrieval/synthesis phases
//   ✦ Session management: sidebar with history
// ─────────────────────────────────────────────────────────────────────────────

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  MessageSquare,
  Send,
  Mic,
  MicOff,
  Plus,
  Trash2,
  FileCode2,
  Paperclip,
  X,
  Search,
  BookOpen,
  Sparkles,
  Square,
  Code2,
  Clock,
  Cpu,
  Hash,
  ChevronDown,
  Copy,
  Check,
  Loader2,
  PenLine,
  Image as ImageIcon,
  FileText,
  Globe,
  Link2,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useChatSessions, useChatMessages } from "@/lib/api/queries";
import { useCreateChatSession, useDeleteChatSession } from "@/lib/api/mutations";
import { useChatStream } from "@/hooks/use-chat-stream";
import { cn } from "@/lib/utils";
import type { ChatAttachment, ChatCitation, ChatMessage, ChatSession } from "@/types/api";

// Web Speech API types (not all TS configs include them).
/* eslint-disable @typescript-eslint/no-explicit-any */
type SpeechRecognitionType = any;
/* eslint-enable @typescript-eslint/no-explicit-any */

// ═══════════════════════════════════════════════════════════════════════════
// Props
// ═══════════════════════════════════════════════════════════════════════════

interface RepoChatProps {
  repoId: string;
  repoName: string;
}

// ═══════════════════════════════════════════════════════════════════════════
// Main Component
// ═══════════════════════════════════════════════════════════════════════════

export function RepoChat({ repoId, repoName }: RepoChatProps) {
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [showSidebar, setShowSidebar] = useState(true);

  // Data
  const { data: sessions = [], isLoading: loadingSessions } = useChatSessions(repoId);
  const createSession = useCreateChatSession();
  const deleteSession = useDeleteChatSession();

  // Auto-select first session.
  useEffect(() => {
    if (!activeSessionId && sessions.length > 0) {
      setActiveSessionId(sessions[0].id);
    }
  }, [sessions, activeSessionId]);

  const handleNewSession = async () => {
    try {
      const session = await createSession.mutateAsync({ repoId });
      setActiveSessionId(session.id);
    } catch {
      // Error is handled by mutation.
    }
  };

  const handleDeleteSession = async (sessionId: string) => {
    try {
      await deleteSession.mutateAsync({ repoId, sessionId });
      if (activeSessionId === sessionId) {
        const remaining = sessions.filter((s) => s.id !== sessionId);
        setActiveSessionId(remaining[0]?.id ?? null);
      }
    } catch {
      // Error handled by mutation.
    }
  };

  return (
    <div className="flex h-[calc(100vh-8rem)] rounded-xl border border-border bg-background overflow-hidden">
      {/* ── Sidebar ─────────────────────────────────────────────────────── */}
      {showSidebar && (
        <div className="w-64 border-r border-border flex flex-col bg-muted/30">
          <div className="p-3 border-b border-border">
            <Button
              variant="outline"
              className="w-full justify-start gap-2 text-sm"
              onClick={handleNewSession}
              disabled={createSession.isPending}
            >
              <Plus className="h-4 w-4" />
              New Chat
            </Button>
          </div>

          <ScrollArea className="flex-1">
            <div className="p-2 space-y-1">
              {loadingSessions ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full rounded-lg" />
                ))
              ) : sessions.length === 0 ? (
                <p className="text-xs text-muted-foreground px-3 py-6 text-center">
                  No chats yet. Start a new conversation!
                </p>
              ) : (
                sessions.map((session) => (
                  <SessionItem
                    key={session.id}
                    session={session}
                    isActive={session.id === activeSessionId}
                    onClick={() => setActiveSessionId(session.id)}
                    onDelete={() => handleDeleteSession(session.id)}
                  />
                ))
              )}
            </div>
          </ScrollArea>
        </div>
      )}

      {/* ── Main Chat Area ──────────────────────────────────────────────── */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Header */}
        <div className="flex items-center gap-2 px-4 py-2.5 border-b border-border">
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8"
            onClick={() => setShowSidebar(!showSidebar)}
          >
            <MessageSquare className="h-4 w-4" />
          </Button>
          <div className="flex-1 min-w-0">
            <h3 className="text-sm font-medium text-foreground truncate">
              {sessions.find((s) => s.id === activeSessionId)?.title ?? "RTVortex Chat"}
            </h3>
            <p className="text-xs text-muted-foreground">{repoName}</p>
          </div>
          <Badge variant="outline" className="text-xs text-emerald-400 border-emerald-400/30">
            <Sparkles className="h-3 w-3 mr-1" />
            RAG
          </Badge>
        </div>

        {/* Messages + Input */}
        {activeSessionId ? (
          <ChatPanel repoId={repoId} sessionId={activeSessionId} />
        ) : (
          <EmptyState onNewChat={handleNewSession} repoName={repoName} />
        )}
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Session Item
// ═══════════════════════════════════════════════════════════════════════════

function SessionItem({
  session,
  isActive,
  onClick,
  onDelete,
}: {
  session: ChatSession;
  isActive: boolean;
  onClick: () => void;
  onDelete: () => void;
}) {
  return (
    <div
      className={cn(
        "group flex items-center gap-2 px-3 py-2 rounded-lg cursor-pointer text-sm transition-colors",
        isActive
          ? "bg-accent text-accent-foreground"
          : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
      )}
      onClick={onClick}
    >
      <MessageSquare className="h-3.5 w-3.5 shrink-0" />
      <span className="truncate flex-1">{session.title}</span>
      {session.message_count > 0 && (
        <span className="text-[10px] text-muted-foreground">{session.message_count}</span>
      )}
      <button
        className="opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-red-400 transition-opacity"
        onClick={(e) => {
          e.stopPropagation();
          onDelete();
        }}
      >
        <Trash2 className="h-3 w-3" />
      </button>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Chat Panel (Messages + Input)
// ═══════════════════════════════════════════════════════════════════════════

function ChatPanel({ repoId, sessionId }: { repoId: string; sessionId: string }) {
  const { data: messages = [], isLoading } = useChatMessages(repoId, sessionId);
  const stream = useChatStream(repoId, sessionId);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [pendingUserMessage, setPendingUserMessage] = useState<{
    content: string;
    attachments?: ChatAttachment[];
  } | null>(null);

  // Stable references from the stream for dependency arrays.
  const isStreaming = stream.isStreaming;
  const streamedContent = stream.streamedContent;
  const thinkingMessage = stream.thinkingMessage;
  const resetStream = stream.reset;

  // Auto-scroll to bottom on new content.
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages, streamedContent, thinkingMessage, pendingUserMessage]);

  // Clear the optimistic message once server messages include it.
  // Also reset stream state to prevent the duplicate — the streamed content
  // and the persisted message would both render at the same time.
  useEffect(() => {
    if (pendingUserMessage && messages.length > 0) {
      const lastUserMsg = [...messages].reverse().find((m) => m.role === "user");
      if (lastUserMsg && lastUserMsg.content === pendingUserMessage.content) {
        setPendingUserMessage(null);
      }
    }

    // Once streaming is done and the server messages now contain the
    // assistant response, clear the streaming bubble to avoid duplicates.
    if (!isStreaming && streamedContent && messages.length > 0) {
      const lastMsg = messages[messages.length - 1];
      if (lastMsg.role === "assistant") {
        resetStream();
      }
    }
  }, [messages, pendingUserMessage, isStreaming, streamedContent, resetStream]);

  // Reset stream state when session changes.
  useEffect(() => {
    resetStream();
    setPendingUserMessage(null);
  }, [sessionId, resetStream]);

  const handleSend = (content: string, attachments?: ChatAttachment[]) => {
    // Show the user message optimistically — no waiting for SSE.
    setPendingUserMessage({ content, attachments });
    stream.sendMessage(content, attachments);
  };

  // Determine if we should show the "thinking" state: streaming has started
  // but no content or explicit thinking event has arrived yet.
  const showAutoThinking =
    isStreaming && !streamedContent && !thinkingMessage;

  return (
    <>
      {/* Messages */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto px-4 py-4 space-y-4">
        {isLoading ? (
          <MessagesSkeleton />
        ) : messages.length === 0 && !isStreaming && !pendingUserMessage ? (
          <WelcomeMessage />
        ) : (
          <>
            {messages.map((msg) => (
              <MessageBubble key={msg.id} message={msg} />
            ))}

            {/* Optimistic user bubble (shown before server confirms) */}
            {pendingUserMessage && (
              <div className="flex gap-3 justify-end">
                <div className="max-w-[75%] space-y-2 items-end">
                  <div className="rounded-2xl px-4 py-3 text-sm leading-relaxed bg-blue-600 text-white rounded-br-md">
                    <p className="whitespace-pre-wrap">{pendingUserMessage.content}</p>
                  </div>
                  {pendingUserMessage.attachments && pendingUserMessage.attachments.length > 0 && (
                    <div className="flex flex-wrap gap-2">
                      {pendingUserMessage.attachments.map((att, i) => (
                        <div key={i} className="flex items-center gap-1.5">
                          {att.type === "image" && att.data_uri ? (
                            <img src={att.data_uri} alt={att.filename} className="h-16 w-16 rounded-lg object-cover border border-white/20" />
                          ) : (
                            <Badge key={i} variant="outline" className="text-xs gap-1 border-white/30 text-white/80">
                              {att.type === "image" ? <ImageIcon className="h-3 w-3" /> :
                               att.type === "audio" ? <Mic className="h-3 w-3" /> :
                               att.type === "pdf" ? <FileText className="h-3 w-3" /> :
                               att.type === "url" ? <Link2 className="h-3 w-3" /> :
                               <FileCode2 className="h-3 w-3" />}
                              {att.filename}
                            </Badge>
                          )}
                        </div>
                      ))}
                    </div>
                  )}
                </div>
                <div className="w-8 h-8 rounded-full bg-blue-600 flex items-center justify-center shrink-0">
                  <PenLine className="h-4 w-4 text-white" />
                </div>
              </div>
            )}

            {/* Thinking — either from SSE event or auto-generated while waiting */}
            {thinkingMessage && (
              <ThinkingIndicator
                phase={stream.thinkingPhase}
                message={thinkingMessage}
              />
            )}
            {showAutoThinking && (
              <ThinkingIndicator
                phase="searching"
                message="Searching codebase..."
              />
            )}

            {stream.citations.length > 0 && !streamedContent && (
              <CitationList citations={stream.citations} />
            )}

            {streamedContent && (
              <StreamingBubble
                content={streamedContent}
                citations={stream.citations}
                isStreaming={isStreaming}
                usage={stream.usage}
                searchTimeMs={stream.searchTimeMs}
                chunksRetrieved={stream.chunksRetrieved}
              />
            )}

            {stream.error && (
              <div className="flex justify-center">
                <p className="text-sm text-red-400 bg-red-400/10 px-4 py-2 rounded-lg">
                  {stream.error}
                </p>
              </div>
            )}
          </>
        )}
      </div>

      {/* Input */}
      <ChatInput
        onSend={handleSend}
        onCancel={stream.cancelStream}
        isStreaming={isStreaming}
      />
    </>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Message Bubble
// ═══════════════════════════════════════════════════════════════════════════

function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === "user";

  return (
    <div className={cn("flex gap-3", isUser ? "justify-end" : "justify-start")}>
      {!isUser && (
        <div className="w-8 h-8 rounded-full bg-gradient-to-br from-emerald-500 to-cyan-500 flex items-center justify-center shrink-0">
          <Sparkles className="h-4 w-4 text-white" />
        </div>
      )}

      <div className={cn("max-w-[75%] space-y-2", isUser ? "items-end" : "items-start")}>
        <div
          className={cn(
            "rounded-2xl px-4 py-3 text-sm leading-relaxed",
            isUser
              ? "bg-blue-600 text-white rounded-br-md"
              : "bg-card text-card-foreground border border-border rounded-bl-md",
          )}
        >
          {isUser ? (
            <p className="whitespace-pre-wrap">{message.content}</p>
          ) : (
            <MarkdownContent content={message.content} />
          )}
        </div>

        {/* Attachments */}
        {message.attachments && message.attachments.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {message.attachments.map((att, i) => (
              <div key={i} className="flex items-center gap-1.5">
                {att.type === "image" && att.data_uri ? (
                  <img src={att.data_uri} alt={att.filename} className="h-16 w-16 rounded-lg object-cover border border-border" />
                ) : (
                  <Badge variant="outline" className="text-xs gap-1">
                    {att.type === "image" ? <ImageIcon className="h-3 w-3 text-violet-400" /> :
                     att.type === "audio" ? <Mic className="h-3 w-3 text-amber-400" /> :
                     att.type === "pdf" ? <FileText className="h-3 w-3 text-red-400" /> :
                     att.type === "url" ? <Link2 className="h-3 w-3 text-blue-400" /> :
                     <FileCode2 className="h-3 w-3" />}
                    {att.filename}
                  </Badge>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Citations */}
        {message.citations && message.citations.length > 0 && (
          <CitationList citations={message.citations} />
        )}

        {/* Usage stats for assistant messages */}
        {!isUser && (message.prompt_tokens > 0 || message.search_time_ms > 0) && (
          <UsageStats
            promptTokens={message.prompt_tokens}
            completionTokens={message.completion_tokens}
            searchTimeMs={message.search_time_ms}
            chunksRetrieved={message.chunks_retrieved}
          />
        )}
      </div>

      {isUser && (
        <div className="w-8 h-8 rounded-full bg-blue-600 flex items-center justify-center shrink-0">
          <PenLine className="h-4 w-4 text-white" />
        </div>
      )}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Streaming Bubble
// ═══════════════════════════════════════════════════════════════════════════

function StreamingBubble({
  content,
  citations,
  isStreaming,
  usage,
  searchTimeMs,
  chunksRetrieved,
}: {
  content: string;
  citations: ChatCitation[];
  isStreaming: boolean;
  usage: { prompt: number; completion: number } | null;
  searchTimeMs: number | null;
  chunksRetrieved: number | null;
}) {
  return (
    <div className="flex gap-3 justify-start">
      <div className="w-8 h-8 rounded-full bg-gradient-to-br from-emerald-500 to-cyan-500 flex items-center justify-center shrink-0">
        <Sparkles className="h-4 w-4 text-white" />
      </div>

      <div className="max-w-[75%] space-y-2">
        <div className="rounded-2xl rounded-bl-md px-4 py-3 text-sm leading-relaxed bg-card text-card-foreground border border-border">
          <MarkdownContent content={content} />
          {isStreaming && (
            <span className="inline-block w-2 h-4 bg-emerald-400 animate-pulse ml-0.5" />
          )}
        </div>

        {citations.length > 0 && <CitationList citations={citations} />}

        {!isStreaming && usage && (
          <UsageStats
            promptTokens={usage.prompt}
            completionTokens={usage.completion}
            searchTimeMs={searchTimeMs ?? 0}
            chunksRetrieved={chunksRetrieved ?? 0}
          />
        )}
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Markdown Renderer (with syntax highlighted code blocks)
// ═══════════════════════════════════════════════════════════════════════════

function MarkdownContent({ content }: { content: string }) {
  // Simple markdown rendering — handles code blocks, bold, italic, headers, links, lists.
  // We use a lightweight custom renderer to avoid heavy dependencies.
  const blocks = useMemo(() => parseMarkdown(content), [content]);

  return (
    <div className="space-y-2 markdown-content">
      {blocks.map((block, i) => (
        <MarkdownBlock key={i} block={block} />
      ))}
    </div>
  );
}

interface MdBlock {
  type: "paragraph" | "code" | "heading" | "list" | "blockquote" | "hr";
  content: string;
  language?: string;
  level?: number;
  items?: string[];
}

function parseMarkdown(text: string): MdBlock[] {
  const blocks: MdBlock[] = [];
  const lines = text.split("\n");
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    // Fenced code block.
    if (line.startsWith("```")) {
      const lang = line.slice(3).trim();
      const codeLines: string[] = [];
      i++;
      while (i < lines.length && !lines[i].startsWith("```")) {
        codeLines.push(lines[i]);
        i++;
      }
      blocks.push({ type: "code", content: codeLines.join("\n"), language: lang || undefined });
      i++; // skip closing ```
      continue;
    }

    // Heading.
    const headingMatch = line.match(/^(#{1,6})\s+(.+)/);
    if (headingMatch) {
      blocks.push({
        type: "heading",
        level: headingMatch[1].length,
        content: headingMatch[2],
      });
      i++;
      continue;
    }

    // HR.
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(line.trim())) {
      blocks.push({ type: "hr", content: "" });
      i++;
      continue;
    }

    // List items.
    if (/^[\s]*[-*+]\s/.test(line) || /^[\s]*\d+\.\s/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && (/^[\s]*[-*+]\s/.test(lines[i]) || /^[\s]*\d+\.\s/.test(lines[i]))) {
        items.push(lines[i].replace(/^[\s]*[-*+]\s|^[\s]*\d+\.\s/, ""));
        i++;
      }
      blocks.push({ type: "list", content: "", items });
      continue;
    }

    // Blockquote.
    if (line.startsWith(">")) {
      const quoteLines: string[] = [];
      while (i < lines.length && lines[i].startsWith(">")) {
        quoteLines.push(lines[i].replace(/^>\s?/, ""));
        i++;
      }
      blocks.push({ type: "blockquote", content: quoteLines.join("\n") });
      continue;
    }

    // Empty line — skip.
    if (line.trim() === "") {
      i++;
      continue;
    }

    // Paragraph — collect consecutive non-special lines.
    const paraLines: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !lines[i].startsWith("```") &&
      !lines[i].startsWith("#") &&
      !lines[i].startsWith(">") &&
      !/^[\s]*[-*+]\s/.test(lines[i]) &&
      !/^[\s]*\d+\.\s/.test(lines[i]) &&
      !/^(-{3,}|\*{3,}|_{3,})$/.test(lines[i].trim())
    ) {
      paraLines.push(lines[i]);
      i++;
    }
    blocks.push({ type: "paragraph", content: paraLines.join("\n") });
  }

  return blocks;
}

function MarkdownBlock({ block }: { block: MdBlock }) {
  switch (block.type) {
    case "code":
      return <CodeBlock code={block.content} language={block.language} />;

    case "heading": {
      const Tag = `h${Math.min(block.level ?? 3, 6)}` as keyof React.JSX.IntrinsicElements;
      const sizes: Record<number, string> = {
        1: "text-lg font-bold",
        2: "text-base font-bold",
        3: "text-sm font-semibold",
        4: "text-sm font-medium",
        5: "text-xs font-medium",
        6: "text-xs font-medium text-muted-foreground",
      };
      return (
        <Tag className={cn(sizes[block.level ?? 3], "text-foreground mt-3 mb-1")}>
          <InlineMarkdown text={block.content} />
        </Tag>
      );
    }

    case "list":
      return (
        <ul className="list-disc list-inside space-y-0.5 text-foreground">
          {block.items?.map((item, i) => (
            <li key={i}>
              <InlineMarkdown text={item} />
            </li>
          ))}
        </ul>
      );

    case "blockquote":
      return (
        <blockquote className="border-l-2 border-muted-foreground/30 pl-3 text-muted-foreground italic">
          <InlineMarkdown text={block.content} />
        </blockquote>
      );

    case "hr":
      return <hr className="border-border" />;

    case "paragraph":
    default:
      return (
        <p className="text-foreground leading-relaxed">
          <InlineMarkdown text={block.content} />
        </p>
      );
  }
}

/** Handles inline markdown: **bold**, *italic*, `code`, [links](url) */
function InlineMarkdown({ text }: { text: string }) {
  // Split by inline patterns and reconstruct.
  const parts: React.ReactNode[] = [];
  let remaining = text;
  let key = 0;

  while (remaining.length > 0) {
    // Bold **text**
    const boldMatch = remaining.match(/\*\*(.+?)\*\*/);
    // Italic *text*
    const italicMatch = remaining.match(/(?<!\*)\*(?!\*)(.+?)(?<!\*)\*(?!\*)/);
    // Inline code `code`
    const codeMatch = remaining.match(/`([^`]+)`/);
    // Link [text](url)
    const linkMatch = remaining.match(/\[([^\]]+)\]\(([^)]+)\)/);

    // Find the earliest match.
    const matches = [
      boldMatch && { type: "bold", match: boldMatch },
      italicMatch && { type: "italic", match: italicMatch },
      codeMatch && { type: "code", match: codeMatch },
      linkMatch && { type: "link", match: linkMatch },
    ].filter(Boolean) as { type: string; match: RegExpMatchArray }[];

    if (matches.length === 0) {
      parts.push(remaining);
      break;
    }

    // Sort by index.
    matches.sort((a, b) => (a.match.index ?? 0) - (b.match.index ?? 0));
    const first = matches[0];
    const idx = first.match.index ?? 0;

    // Push text before the match.
    if (idx > 0) {
      parts.push(remaining.slice(0, idx));
    }

    switch (first.type) {
      case "bold":
        parts.push(<strong key={key++} className="font-semibold">{first.match[1]}</strong>);
        break;
      case "italic":
        parts.push(<em key={key++} className="italic text-muted-foreground">{first.match[1]}</em>);
        break;
      case "code":
        parts.push(
          <code key={key++} className="px-1.5 py-0.5 rounded bg-muted text-emerald-600 dark:text-emerald-400 text-xs font-mono">
            {first.match[1]}
          </code>,
        );
        break;
      case "link":
        parts.push(
          <a key={key++} href={first.match[2]} target="_blank" rel="noopener noreferrer" className="text-primary hover:underline">
            {first.match[1]}
          </a>,
        );
        break;
    }

    remaining = remaining.slice(idx + first.match[0].length);
  }

  return <>{parts}</>;
}

// ═══════════════════════════════════════════════════════════════════════════
// Code Block with Copy Button
// ═══════════════════════════════════════════════════════════════════════════

function CodeBlock({ code, language }: { code: string; language?: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="relative group rounded-lg overflow-hidden border border-border bg-muted/50 dark:bg-zinc-950">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-1.5 bg-muted dark:bg-zinc-900 border-b border-border">
        <div className="flex items-center gap-1.5">
          <Code2 className="h-3 w-3 text-muted-foreground" />
          <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wide">
            {language || "code"}
          </span>
        </div>
        <button
          onClick={handleCopy}
          className="text-muted-foreground hover:text-foreground transition-colors"
        >
          {copied ? (
            <Check className="h-3.5 w-3.5 text-emerald-400" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </button>
      </div>

      {/* Code */}
      <pre className="p-3 overflow-x-auto text-xs font-mono text-foreground/80 leading-relaxed">
        <code>{code}</code>
      </pre>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Citation List
// ═══════════════════════════════════════════════════════════════════════════

function CitationList({ citations }: { citations: ChatCitation[] }) {
  const [expanded, setExpanded] = useState(false);
  const [viewingCitation, setViewingCitation] = useState<ChatCitation | null>(null);
  const visibleCitations = expanded ? citations : citations.slice(0, 3);

  return (
    <div className="space-y-1">
      <div className="flex items-center gap-1 text-[10px] text-muted-foreground uppercase tracking-wider font-medium">
        <BookOpen className="h-3 w-3" />
        Sources ({citations.length})
      </div>
      <TooltipProvider delayDuration={200}>
        <div className="flex flex-wrap gap-1.5">
          {visibleCitations.map((c, i) => (
            <Tooltip key={i}>
              <TooltipTrigger asChild>
                <button
                  onClick={() => setViewingCitation(viewingCitation?.file_path === c.file_path && viewingCitation?.start_line === c.start_line ? null : c)}
                  className={cn(
                    "inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg border text-[11px] font-medium shadow-sm transition-all cursor-pointer",
                    viewingCitation?.file_path === c.file_path && viewingCitation?.start_line === c.start_line
                      ? "bg-emerald-500/15 border-emerald-500/40 text-emerald-300 ring-1 ring-emerald-500/20"
                      : "bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 border-zinc-200 dark:border-zinc-700 text-zinc-800 dark:text-zinc-200",
                  )}
                >
                  <FileCode2 className="h-3 w-3 text-emerald-500" />
                  <span className="font-mono truncate max-w-[160px]">
                    {c.file_path.split("/").pop()}
                  </span>
                  <span className="text-zinc-500 dark:text-zinc-400 font-normal">
                    :{c.start_line}–{c.end_line}
                  </span>
                </button>
              </TooltipTrigger>
              <TooltipContent
                side="top"
                className="max-w-sm bg-zinc-900 dark:bg-zinc-900 border-zinc-700 text-zinc-100 shadow-xl"
              >
                <p className="font-mono text-xs font-medium text-zinc-100">{c.file_path}</p>
                <p className="text-xs text-zinc-400 mt-1">
                  Lines {c.start_line}–{c.end_line} • {c.language}
                  {c.relevance_score > 0 && ` • ${(c.relevance_score * 100).toFixed(0)}% relevant`}
                </p>
                {c.symbols && c.symbols.length > 0 && (
                  <p className="text-xs text-emerald-400 mt-1">
                    Symbols: {c.symbols.join(", ")}
                  </p>
                )}
                <p className="text-[10px] text-zinc-500 mt-1.5 italic">Click to view source</p>
              </TooltipContent>
            </Tooltip>
          ))}
        </div>
      </TooltipProvider>

      {citations.length > 3 && (
        <button
          onClick={() => setExpanded(!expanded)}
          className="text-[10px] text-muted-foreground hover:text-foreground transition-colors flex items-center gap-1"
        >
          <ChevronDown className={cn("h-3 w-3 transition-transform", expanded && "rotate-180")} />
          {expanded ? "Show less" : `+${citations.length - 3} more`}
        </button>
      )}

      {/* Inline code viewer */}
      {viewingCitation && (
        <div className="mt-2 rounded-lg border border-zinc-700 bg-zinc-900 overflow-hidden shadow-lg">
          {/* Header */}
          <div className="flex items-center justify-between px-3 py-2 bg-zinc-800 border-b border-zinc-700">
            <div className="flex items-center gap-2 min-w-0">
              <FileCode2 className="h-3.5 w-3.5 text-emerald-500 shrink-0" />
              <span className="font-mono text-xs text-zinc-200 truncate">{viewingCitation.file_path}</span>
              <Badge variant="outline" className="text-[10px] shrink-0 border-zinc-600 text-zinc-400">
                {viewingCitation.language}
              </Badge>
              <span className="text-[10px] text-zinc-500 shrink-0">
                L{viewingCitation.start_line}–{viewingCitation.end_line}
              </span>
            </div>
            <div className="flex items-center gap-1 shrink-0">
              <CopyCodeButton text={viewingCitation.content} />
              <button
                onClick={() => setViewingCitation(null)}
                className="p-1 rounded hover:bg-zinc-700 text-zinc-400 hover:text-zinc-200 transition-colors"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
          {/* Code content */}
          <div className="overflow-auto max-h-[400px]">
            <pre className="p-3 text-xs leading-relaxed">
              <code className="text-zinc-300 font-mono">
                {viewingCitation.content.split("\n").map((line, lineIdx) => (
                  <div key={lineIdx} className="flex">
                    <span className="select-none text-zinc-600 w-10 text-right pr-3 shrink-0">
                      {viewingCitation.start_line + lineIdx}
                    </span>
                    <span className="flex-1 whitespace-pre-wrap break-all">{line}</span>
                  </div>
                ))}
              </code>
            </pre>
          </div>
          {/* Footer with symbols */}
          {viewingCitation.symbols && viewingCitation.symbols.length > 0 && (
            <div className="px-3 py-2 border-t border-zinc-700 bg-zinc-800/50">
              <p className="text-[10px] text-zinc-500">
                <span className="text-emerald-400 font-medium">Symbols:</span>{" "}
                {viewingCitation.symbols.join(", ")}
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** Small copy button used inside the code viewer. */
function CopyCodeButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      onClick={() => {
        navigator.clipboard.writeText(text);
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      }}
      className="p-1 rounded hover:bg-zinc-700 text-zinc-400 hover:text-zinc-200 transition-colors"
    >
      {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
    </button>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Usage Stats
// ═══════════════════════════════════════════════════════════════════════════

function UsageStats({
  promptTokens,
  completionTokens,
  searchTimeMs,
  chunksRetrieved,
}: {
  promptTokens: number;
  completionTokens: number;
  searchTimeMs: number;
  chunksRetrieved: number;
}) {
  return (
    <div className="flex flex-wrap gap-3 text-[10px] text-muted-foreground">
      {searchTimeMs > 0 && (
        <span className="flex items-center gap-1">
          <Search className="h-3 w-3" /> {searchTimeMs}ms
        </span>
      )}
      {chunksRetrieved > 0 && (
        <span className="flex items-center gap-1">
          <Hash className="h-3 w-3" /> {chunksRetrieved} chunks
        </span>
      )}
      {(promptTokens > 0 || completionTokens > 0) && (
        <span className="flex items-center gap-1">
          <Cpu className="h-3 w-3" /> {promptTokens + completionTokens} tokens
        </span>
      )}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Thinking Indicator
// ═══════════════════════════════════════════════════════════════════════════

function ThinkingIndicator({ phase, message }: { phase: string | null; message: string | null }) {
  const icons: Record<string, React.ReactNode> = {
    searching: <Search className="h-4 w-4 text-cyan-400 animate-pulse" />,
    retrieving: <BookOpen className="h-4 w-4 text-amber-400 animate-pulse" />,
    synthesizing: <Sparkles className="h-4 w-4 text-emerald-400 animate-pulse" />,
  };

  return (
    <div className="flex gap-3 justify-start">
      <div className="w-8 h-8 rounded-full bg-gradient-to-br from-emerald-500 to-cyan-500 flex items-center justify-center shrink-0 animate-pulse">
        <Sparkles className="h-4 w-4 text-white" />
      </div>
      <div className="flex items-center gap-2 px-4 py-2.5 rounded-2xl rounded-bl-md bg-card border border-border">
        {icons[phase ?? ""] ?? <Loader2 className="h-4 w-4 text-muted-foreground animate-spin" />}
        <span className="text-sm text-muted-foreground">{message}</span>
        <span className="flex gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-bounce" style={{ animationDelay: "0ms" }} />
          <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-bounce" style={{ animationDelay: "150ms" }} />
          <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-bounce" style={{ animationDelay: "300ms" }} />
        </span>
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Chat Input (with voice + drag & drop)
// ═══════════════════════════════════════════════════════════════════════════

function ChatInput({
  onSend,
  onCancel,
  isStreaming,
}: {
  onSend: (content: string, attachments?: ChatAttachment[]) => void;
  onCancel: () => void;
  isStreaming: boolean;
}) {
  const [text, setText] = useState("");
  const [attachments, setAttachments] = useState<ChatAttachment[]>([]);
  const [isListening, setIsListening] = useState(false);
  const [isDragging, setIsDragging] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const recognitionRef = useRef<SpeechRecognitionType>(null);

  // Auto-resize textarea.
  useEffect(() => {
    const el = textareaRef.current;
    if (el) {
      el.style.height = "auto";
      el.style.height = Math.min(el.scrollHeight, 200) + "px";
    }
  }, [text]);

  const handleSend = () => {
    const trimmed = text.trim();
    if (!trimmed && attachments.length === 0) return;
    onSend(trimmed, attachments.length > 0 ? attachments : undefined);
    setText("");
    setAttachments([]);
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      if (!isStreaming) handleSend();
    }
  };

  // ── Voice Input (Web Speech API) ──────────────────────────────────────
  const supportsVoice =
    typeof window !== "undefined" &&
    ("SpeechRecognition" in window || "webkitSpeechRecognition" in window);

  const toggleVoice = () => {
    if (isListening) {
      recognitionRef.current?.stop();
      setIsListening(false);
      return;
    }

    const SpeechRecognitionCtor =
      (window as unknown as Record<string, unknown>).SpeechRecognition ??
      (window as unknown as Record<string, unknown>).webkitSpeechRecognition;

    if (!SpeechRecognitionCtor) return;

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const recognition = new (SpeechRecognitionCtor as any)();
    recognition.continuous = true;
    recognition.interimResults = true;
    recognition.lang = "en-US";

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    recognition.onresult = (event: any) => {
      let transcript = "";
      for (let i = event.resultIndex; i < event.results.length; i++) {
        transcript += event.results[i][0].transcript;
      }
      setText((prev) => prev + transcript);
    };

    recognition.onend = () => setIsListening(false);
    recognition.onerror = () => setIsListening(false);

    recognition.start();
    recognitionRef.current = recognition;
    setIsListening(true);
  };

  // ── Drag & Drop ───────────────────────────────────────────────────────
  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(true);
  };

  const handleDragLeave = () => setIsDragging(false);

  const handleDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);

    const files = Array.from(e.dataTransfer.files);
    const droppedText = e.dataTransfer.getData("text/plain");

    // If the dropped text looks like a URL, create a URL attachment.
    if (droppedText) {
      const isUrl = /^https?:\/\/\S+$/i.test(droppedText.trim());
      if (isUrl) {
        setAttachments((prev) => [
          ...prev,
          {
            type: "url",
            filename: new URL(droppedText.trim()).hostname,
            content: droppedText.trim(),
          },
        ]);
      } else {
        setAttachments((prev) => [
          ...prev,
          {
            type: "code_snippet",
            filename: "dropped-snippet.txt",
            content: droppedText,
          },
        ]);
      }
      return;
    }

    for (const file of files) {
      const mime = file.type.toLowerCase();
      const ext = file.name.split(".").pop()?.toLowerCase() ?? "";

      // ── Images: read as data URI for inline preview ───────────────────
      if (mime.startsWith("image/")) {
        if (file.size > 10 * 1024 * 1024) continue; // 10 MB limit for images
        const dataUri = await new Promise<string>((resolve) => {
          const reader = new FileReader();
          reader.onload = () => resolve(reader.result as string);
          reader.readAsDataURL(file);
        });
        setAttachments((prev) => [
          ...prev,
          {
            type: "image",
            filename: file.name,
            content: `[Image: ${file.name}]`,
            mime_type: mime,
            size: file.size,
            data_uri: dataUri,
          },
        ]);
        continue;
      }

      // ── Audio files ───────────────────────────────────────────────────
      if (mime.startsWith("audio/")) {
        if (file.size > 25 * 1024 * 1024) continue; // 25 MB limit for audio
        setAttachments((prev) => [
          ...prev,
          {
            type: "audio",
            filename: file.name,
            content: `[Audio: ${file.name}, ${file.size} bytes]`,
            mime_type: mime,
            size: file.size,
          },
        ]);
        continue;
      }

      // ── PDF files ─────────────────────────────────────────────────────
      if (mime === "application/pdf" || ext === "pdf") {
        if (file.size > 50 * 1024 * 1024) continue; // 50 MB limit for PDFs
        setAttachments((prev) => [
          ...prev,
          {
            type: "pdf",
            filename: file.name,
            content: `[PDF: ${file.name}, ${file.size} bytes]`,
            mime_type: mime,
            size: file.size,
          },
        ]);
        continue;
      }

      // ── Text/code files (existing behavior) ──────────────────────────
      if (file.size > 1024 * 1024) continue; // 1 MB limit for text

      const content = await file.text();
      const langMap: Record<string, string> = {
        ts: "typescript",
        tsx: "typescript",
        js: "javascript",
        jsx: "javascript",
        py: "python",
        rs: "rust",
        go: "go",
        cpp: "cpp",
        c: "c",
        h: "c",
        hpp: "cpp",
        java: "java",
        rb: "ruby",
        sh: "bash",
        yml: "yaml",
        yaml: "yaml",
        json: "json",
        md: "markdown",
        sql: "sql",
        css: "css",
        html: "html",
      };

      setAttachments((prev) => [
        ...prev,
        {
          type: "file",
          filename: file.name,
          content,
          language: langMap[ext],
          mime_type: file.type,
          size: file.size,
        },
      ]);
    }
  };

  const removeAttachment = (index: number) => {
    setAttachments((prev) => prev.filter((_, i) => i !== index));
  };

  return (
    <div
      className={cn(
        "border-t border-border px-4 py-3 transition-colors",
        isDragging && "bg-blue-500/5 border-blue-500/30",
      )}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {/* Attachments preview */}
      {attachments.length > 0 && (
        <div className="flex flex-wrap gap-2 mb-2">
          {attachments.map((att, i) => (
            <div
              key={i}
              className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-muted border border-border text-xs text-foreground overflow-hidden"
            >
              {/* Inline image thumbnail */}
              {att.type === "image" && att.data_uri ? (
                <img src={att.data_uri} alt={att.filename} className="h-8 w-8 rounded object-cover shrink-0" />
              ) : att.type === "image" ? (
                <ImageIcon className="h-3 w-3 text-violet-400 shrink-0" />
              ) : att.type === "audio" ? (
                <Mic className="h-3 w-3 text-amber-400 shrink-0" />
              ) : att.type === "pdf" ? (
                <FileText className="h-3 w-3 text-red-400 shrink-0" />
              ) : att.type === "url" ? (
                <Link2 className="h-3 w-3 text-blue-400 shrink-0" />
              ) : (
                <FileCode2 className="h-3 w-3 text-emerald-400 shrink-0" />
              )}
              <span className="truncate max-w-[120px]">{att.filename}</span>
              {att.size != null && att.size > 0 && (
                <span className="text-[10px] text-muted-foreground shrink-0">
                  {att.size < 1024 ? `${att.size}B` : att.size < 1048576 ? `${(att.size / 1024).toFixed(0)}K` : `${(att.size / 1048576).toFixed(1)}M`}
                </span>
              )}
              <button onClick={() => removeAttachment(i)} className="text-muted-foreground hover:text-red-400 shrink-0">
                <X className="h-3 w-3" />
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Input row */}
      <TooltipProvider delayDuration={0}>
      <div className="flex items-end gap-2">
        {/* File attach button */}
        <Tooltip>
          <TooltipTrigger asChild>
            <label className="cursor-pointer text-muted-foreground hover:text-foreground transition-colors p-1.5">
              <Paperclip className="h-4 w-4" />
              <input
                type="file"
                className="hidden"
                multiple
                accept="image/*,audio/*,application/pdf,.ts,.tsx,.js,.jsx,.py,.rs,.go,.cpp,.c,.h,.hpp,.java,.rb,.sh,.yml,.yaml,.json,.md,.sql,.css,.html,.txt"
                onChange={async (e) => {
                  const files = Array.from(e.target.files ?? []);
                  for (const file of files) {
                    const mime = file.type.toLowerCase();
                    const ext = file.name.split(".").pop()?.toLowerCase() ?? "";

                    if (mime.startsWith("image/")) {
                      if (file.size > 10 * 1024 * 1024) continue;
                      const dataUri = await new Promise<string>((resolve) => {
                        const reader = new FileReader();
                        reader.onload = () => resolve(reader.result as string);
                        reader.readAsDataURL(file);
                      });
                      setAttachments((prev) => [
                        ...prev,
                        { type: "image", filename: file.name, content: `[Image: ${file.name}]`, mime_type: mime, size: file.size, data_uri: dataUri },
                      ]);
                    } else if (mime.startsWith("audio/")) {
                      if (file.size > 25 * 1024 * 1024) continue;
                      setAttachments((prev) => [
                        ...prev,
                        { type: "audio", filename: file.name, content: `[Audio: ${file.name}]`, mime_type: mime, size: file.size },
                      ]);
                    } else if (mime === "application/pdf" || ext === "pdf") {
                      if (file.size > 50 * 1024 * 1024) continue;
                      setAttachments((prev) => [
                        ...prev,
                        { type: "pdf", filename: file.name, content: `[PDF: ${file.name}]`, mime_type: mime, size: file.size },
                      ]);
                    } else {
                      if (file.size > 1024 * 1024) continue;
                      const content = await file.text();
                      setAttachments((prev) => [
                        ...prev,
                        { type: "file", filename: file.name, content, size: file.size },
                      ]);
                    }
                  }
                  e.target.value = "";
                }}
              />
            </label>
          </TooltipTrigger>
          <TooltipContent>Attach file</TooltipContent>
        </Tooltip>

        {/* Textarea */}
        <div className="flex-1 relative">
          <textarea
            ref={textareaRef}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={isDragging ? "Drop files here..." : "Ask about this codebase..."}
            className={cn(
              "w-full resize-none rounded-xl bg-background border border-input px-4 py-2.5",
              "text-sm text-foreground placeholder-muted-foreground",
              "focus:outline-none focus:border-emerald-500/50 focus:ring-1 focus:ring-emerald-500/20",
              "max-h-[200px] min-h-[44px]",
            )}
            rows={1}
            disabled={isStreaming}
          />
        </div>

        {/* Voice button */}
        {supportsVoice && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className={cn(
                  "h-9 w-9 shrink-0",
                  isListening && "text-red-400 bg-red-400/10",
                )}
                onClick={toggleVoice}
                disabled={isStreaming}
              >
                {isListening ? <MicOff className="h-4 w-4" /> : <Mic className="h-4 w-4" />}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{isListening ? "Stop recording" : "Voice input"}</TooltipContent>
          </Tooltip>
        )}

        {/* Send / Stop */}
        {isStreaming ? (
          <Button
            variant="ghost"
            size="icon"
            className="h-9 w-9 shrink-0 text-red-400 hover:bg-red-400/10"
            onClick={onCancel}
          >
            <Square className="h-4 w-4" />
          </Button>
        ) : (
          <Button
            variant="default"
            size="icon"
            className="h-9 w-9 shrink-0 bg-emerald-600 hover:bg-emerald-500"
            onClick={handleSend}
            disabled={!text.trim() && attachments.length === 0}
          >
            <Send className="h-4 w-4" />
          </Button>
        )}
      </div>
      </TooltipProvider>

      {/* Keyboard hint */}
      <p className="text-[10px] text-muted-foreground mt-1.5 px-1">
        Press <kbd className="px-1 py-0.5 rounded bg-muted text-muted-foreground">Enter</kbd> to send,{" "}
        <kbd className="px-1 py-0.5 rounded bg-muted text-muted-foreground">Shift+Enter</kbd> for new
        line. Drop files, images, audio, PDFs, or URLs to attach.
      </p>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Helper Components
// ═══════════════════════════════════════════════════════════════════════════

function WelcomeMessage() {
  return (
    <div className="flex flex-col items-center justify-center h-full text-center px-8 py-12 space-y-4">
      <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-emerald-500/20 to-cyan-500/20 flex items-center justify-center">
        <Sparkles className="h-8 w-8 text-emerald-400" />
      </div>
      <h3 className="text-lg font-semibold text-foreground">Ask anything about this repository</h3>
      <p className="text-sm text-muted-foreground max-w-md">
        I have access to the repository&apos;s semantic index. Ask about architecture, code patterns,
        specific functions, or anything else — I&apos;ll find the relevant code and give you precise answers.
      </p>
      <div className="flex flex-wrap gap-2 justify-center mt-2">
        {[
          "What is the main architecture?",
          "How does the build system work?",
          "Show me the entry point",
          "What dependencies are used?",
        ].map((q) => (
          <button
            key={q}
            className="text-xs px-3 py-1.5 rounded-full border border-border text-muted-foreground hover:text-foreground hover:border-foreground/30 transition-colors"
          >
            {q}
          </button>
        ))}
      </div>
    </div>
  );
}

function EmptyState({ onNewChat, repoName }: { onNewChat: () => void; repoName: string }) {
  return (
    <div className="flex flex-col items-center justify-center flex-1 text-center px-8 space-y-4">
      <MessageSquare className="h-12 w-12 text-muted-foreground/40" />
      <h3 className="text-base font-medium text-foreground">Chat with {repoName}</h3>
      <p className="text-sm text-muted-foreground">Start a conversation to explore the codebase</p>
      <Button onClick={onNewChat} className="gap-2">
        <Plus className="h-4 w-4" />
        Start New Chat
      </Button>
    </div>
  );
}

function MessagesSkeleton() {
  return (
    <div className="space-y-4">
      {[...Array(3)].map((_, i) => (
        <div key={i} className={cn("flex gap-3", i % 2 === 0 ? "justify-end" : "justify-start")}>
          {i % 2 !== 0 && <Skeleton className="w-8 h-8 rounded-full shrink-0" />}
          <Skeleton className={cn("h-16 rounded-2xl", i % 2 === 0 ? "w-1/3" : "w-2/3")} />
          {i % 2 === 0 && <Skeleton className="w-8 h-8 rounded-full shrink-0" />}
        </div>
      ))}
    </div>
  );
}
