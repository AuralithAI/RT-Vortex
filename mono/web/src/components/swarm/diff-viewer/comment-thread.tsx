// ─── Comment Thread ──────────────────────────────────────────────────────────
// Inline comment thread attached to a diff line.  Supports both agent-authored
// and user-authored comments, with a reply input.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import { Bot, User, Send, MessageSquare, Clock } from "lucide-react";
import type { DiffComment } from "@/types/swarm";

// ── Types ────────────────────────────────────────────────────────────────────

interface CommentThreadProps {
  comments: DiffComment[];
  diffId: string;
  lineNumber: number;
  onSubmit: (diffId: string, lineNumber: number, content: string) => Promise<void>;
  className?: string;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function timeAgo(dateStr: string): string {
  const now = Date.now();
  const then = new Date(dateStr).getTime();
  const seconds = Math.floor((now - then) / 1000);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

// ── Component ────────────────────────────────────────────────────────────────

export function CommentThread({
  comments,
  diffId,
  lineNumber,
  onSubmit,
  className,
}: CommentThreadProps) {
  const [reply, setReply] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [showReplyBox, setShowReplyBox] = useState(false);

  const handleSubmit = async () => {
    const text = reply.trim();
    if (!text) return;
    setSubmitting(true);
    try {
      await onSubmit(diffId, lineNumber, text);
      setReply("");
      setShowReplyBox(false);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className={`rounded-lg border bg-muted/30 ${className ?? ""}`}>
      {/* Header */}
      <div className="flex items-center gap-2 border-b px-3 py-1.5 text-xs text-muted-foreground">
        <MessageSquare className="h-3 w-3" />
        {comments.length} comment{comments.length !== 1 ? "s" : ""} on line {lineNumber}
      </div>

      {/* Comments */}
      <div className="divide-y">
        {comments.map((c) => (
          <div key={c.id} className="px-3 py-2">
            <div className="flex items-center gap-2 mb-1">
              {c.author_type === "agent" ? (
                <Bot className="h-3.5 w-3.5 text-primary" />
              ) : (
                <User className="h-3.5 w-3.5 text-muted-foreground" />
              )}
              <span className="text-xs font-medium">
                {c.author_type === "agent" ? "Agent" : "Reviewer"}{" "}
                <span className="font-normal text-muted-foreground">
                  {c.author_id.slice(0, 8)}
                </span>
              </span>
              <span className="ml-auto flex items-center gap-1 text-[10px] text-muted-foreground">
                <Clock className="h-2.5 w-2.5" />
                {timeAgo(c.created_at)}
              </span>
            </div>
            <p className="text-xs leading-relaxed whitespace-pre-wrap">
              {c.content}
            </p>
          </div>
        ))}
      </div>

      {/* Reply */}
      {showReplyBox ? (
        <div className="border-t px-3 py-2">
          <textarea
            value={reply}
            onChange={(e) => setReply(e.target.value)}
            placeholder="Add a comment…"
            rows={2}
            className="w-full rounded-md border bg-background px-2 py-1.5 text-xs resize-none focus:outline-none focus:ring-1 focus:ring-primary"
          />
          <div className="flex justify-end gap-2 mt-1.5">
            <button
              onClick={() => {
                setShowReplyBox(false);
                setReply("");
              }}
              className="rounded px-2 py-1 text-xs text-muted-foreground hover:bg-muted"
            >
              Cancel
            </button>
            <button
              onClick={handleSubmit}
              disabled={submitting || !reply.trim()}
              className="inline-flex items-center gap-1 rounded bg-primary px-2.5 py-1 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              <Send className="h-3 w-3" />
              {submitting ? "Sending…" : "Reply"}
            </button>
          </div>
        </div>
      ) : (
        <button
          onClick={() => setShowReplyBox(true)}
          className="w-full border-t px-3 py-1.5 text-xs text-muted-foreground hover:bg-muted/50 text-left"
        >
          Reply…
        </button>
      )}
    </div>
  );
}
