// ─── Approval Toolbar ────────────────────────────────────────────────────────
// Toolbar for batch-approving, rejecting, or requesting changes on a set of
// diffs.  Appears at the top of the diff review page.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import {
  Check,
  X,
  MessageSquare,
  Loader2,
  CheckCheck,
  AlertTriangle,
} from "lucide-react";

// ── Types ────────────────────────────────────────────────────────────────────

export type ApprovalAction = "approve_all" | "reject_all" | "request_changes";

interface ApprovalToolbarProps {
  totalDiffs: number;
  approvedCount: number;
  rejectedCount: number;
  pendingCount: number;
  onAction: (action: ApprovalAction, comment?: string) => Promise<void>;
  disabled?: boolean;
  className?: string;
}

// ── Component ────────────────────────────────────────────────────────────────

export function ApprovalToolbar({
  totalDiffs,
  approvedCount,
  rejectedCount,
  pendingCount,
  onAction,
  disabled = false,
  className,
}: ApprovalToolbarProps) {
  const [loading, setLoading] = useState<ApprovalAction | null>(null);
  const [showCommentBox, setShowCommentBox] = useState(false);
  const [comment, setComment] = useState("");

  const handleAction = async (action: ApprovalAction) => {
    setLoading(action);
    try {
      await onAction(action, comment || undefined);
      setComment("");
      setShowCommentBox(false);
    } finally {
      setLoading(null);
    }
  };

  const allApproved = approvedCount === totalDiffs && totalDiffs > 0;
  const hasRejections = rejectedCount > 0;

  return (
    <div className={`rounded-lg border bg-card p-3 ${className ?? ""}`}>
      {/* Summary row */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-4 text-sm">
          <span className="font-medium">{totalDiffs} file{totalDiffs !== 1 ? "s" : ""} changed</span>
          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            {approvedCount > 0 && (
              <span className="flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
                <Check className="h-3 w-3" /> {approvedCount} approved
              </span>
            )}
            {rejectedCount > 0 && (
              <span className="flex items-center gap-1 text-red-600 dark:text-red-400">
                <X className="h-3 w-3" /> {rejectedCount} rejected
              </span>
            )}
            {pendingCount > 0 && (
              <span className="flex items-center gap-1">
                <AlertTriangle className="h-3 w-3" /> {pendingCount} pending
              </span>
            )}
          </div>
        </div>

        {/* Status badge */}
        {allApproved && (
          <span className="inline-flex items-center gap-1 rounded-full bg-emerald-100 px-2.5 py-0.5 text-xs font-medium text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300">
            <CheckCheck className="h-3 w-3" /> All approved
          </span>
        )}
      </div>

      {/* Action buttons */}
      <div className="flex items-center gap-2 flex-wrap">
        <button
          onClick={() => handleAction("approve_all")}
          disabled={disabled || loading !== null || pendingCount === 0}
          className="inline-flex items-center gap-1.5 rounded-md bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-emerald-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          {loading === "approve_all" ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Check className="h-3.5 w-3.5" />
          )}
          Approve All
        </button>

        <button
          onClick={() => setShowCommentBox(!showCommentBox)}
          disabled={disabled || loading !== null}
          className="inline-flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-xs font-medium hover:bg-muted disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          <MessageSquare className="h-3.5 w-3.5" />
          Request Changes
        </button>

        <button
          onClick={() => handleAction("reject_all")}
          disabled={disabled || loading !== null || pendingCount === 0}
          className="inline-flex items-center gap-1.5 rounded-md border border-red-200 px-3 py-1.5 text-xs font-medium text-red-600 hover:bg-red-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-900/20 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          {loading === "reject_all" ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <X className="h-3.5 w-3.5" />
          )}
          Reject All
        </button>
      </div>

      {/* Comment box for request-changes */}
      {showCommentBox && (
        <div className="mt-3 border-t pt-3">
          <textarea
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder="Describe what changes are needed…"
            rows={3}
            className="w-full rounded-md border bg-background px-3 py-2 text-sm resize-none focus:outline-none focus:ring-1 focus:ring-primary"
          />
          <div className="flex justify-end gap-2 mt-2">
            <button
              onClick={() => {
                setShowCommentBox(false);
                setComment("");
              }}
              className="rounded-md px-3 py-1.5 text-xs text-muted-foreground hover:bg-muted"
            >
              Cancel
            </button>
            <button
              onClick={() => handleAction("request_changes")}
              disabled={!comment.trim() || loading !== null}
              className="inline-flex items-center gap-1 rounded-md bg-primary px-3 py-1.5 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {loading === "request_changes" ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <MessageSquare className="h-3 w-3" />
              )}
              Submit
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
