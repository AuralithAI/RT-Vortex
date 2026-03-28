// ─── Plan Review Card ────────────────────────────────────────────────────────
// Enhanced plan display with approve/reject, comment support, and step-by-step
// progress tracking.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import {
  CheckCircle,
  XCircle,
  FileCode,
  MessageSquare,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  Clock,
  Send,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import type { PlanDocument } from "@/types/swarm";

interface PlanReviewCardProps {
  plan: PlanDocument;
  taskId: string;
  status: string;
  onApprove?: () => void;
  onReject?: () => void;
  onComment?: (comment: string) => void;
}

function complexityBadge(complexity: string) {
  switch (complexity) {
    case "small":
      return (
        <span className="rounded-full bg-green-100 px-2.5 py-0.5 text-xs font-medium text-green-800 dark:bg-green-900 dark:text-green-200">
          Small
        </span>
      );
    case "large":
      return (
        <span className="rounded-full bg-red-100 px-2.5 py-0.5 text-xs font-medium text-red-800 dark:bg-red-900 dark:text-red-200">
          Large
        </span>
      );
    default:
      return (
        <span className="rounded-full bg-yellow-100 px-2.5 py-0.5 text-xs font-medium text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200">
          Medium
        </span>
      );
  }
}

export function PlanReviewCard({
  plan,
  taskId,
  status,
  onApprove,
  onReject,
  onComment,
}: PlanReviewCardProps) {
  const [showComment, setShowComment] = useState(false);
  const [comment, setComment] = useState("");
  const [expandedSteps, setExpandedSteps] = useState<Set<number>>(
    new Set(plan.steps.map((_, i) => i))
  );

  const toggleStep = (index: number) => {
    setExpandedSteps((prev) => {
      const next = new Set(prev);
      if (next.has(index)) {
        next.delete(index);
      } else {
        next.add(index);
      }
      return next;
    });
  };

  const handleSubmitComment = () => {
    if (comment.trim() && onComment) {
      onComment(comment.trim());
      setComment("");
      setShowComment(false);
    }
  };

  const canReview = status === "plan_review";

  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      {/* Header */}
      <div className="flex items-center justify-between border-b bg-muted/30 px-6 py-4">
        <div className="space-y-1">
          <h3 className="text-lg font-semibold">Implementation Plan</h3>
          <div className="flex items-center gap-3 text-sm text-muted-foreground">
            {complexityBadge(plan.estimated_complexity)}
            <span>{plan.steps.length} steps</span>
            <span>•</span>
            <span>{plan.affected_files.length} files</span>
            {plan.agents_needed > 0 && (
              <>
                <span>•</span>
                <span>{plan.agents_needed} agents</span>
              </>
            )}
          </div>
        </div>
        {canReview && (
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => setShowComment(!showComment)}
            >
              <MessageSquare className="mr-2 h-4 w-4" />
              Comment
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={onReject}
            >
              <XCircle className="mr-2 h-4 w-4" />
              Reject
            </Button>
            <Button size="sm" onClick={onApprove}>
              <CheckCircle className="mr-2 h-4 w-4" />
              Approve
            </Button>
          </div>
        )}
      </div>

      {/* Comment Input */}
      {showComment && (
        <div className="border-b bg-muted/10 px-6 py-3">
          <div className="flex gap-2">
            <textarea
              className="flex-1 rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground"
              rows={2}
              placeholder="Add a comment or request changes…"
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                  handleSubmitComment();
                }
              }}
            />
            <Button
              size="sm"
              className="self-end"
              onClick={handleSubmitComment}
              disabled={!comment.trim()}
            >
              <Send className="h-4 w-4" />
            </Button>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            Press ⌘+Enter to submit
          </p>
        </div>
      )}

      {/* Summary */}
      <div className="border-b px-6 py-4">
        <h4 className="mb-2 text-sm font-medium text-muted-foreground">Summary</h4>
        <p className="text-sm">{plan.summary}</p>
      </div>

      {/* Steps */}
      <div className="border-b px-6 py-4">
        <h4 className="mb-3 text-sm font-medium text-muted-foreground">Steps</h4>
        <div className="space-y-2">
          {plan.steps.map((step, i) => (
            <div key={i} className="rounded-md border">
              <button
                className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm hover:bg-muted/30"
                onClick={() => toggleStep(i)}
              >
                <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-medium">
                  {i + 1}
                </span>
                {expandedSteps.has(i) ? (
                  <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                ) : (
                  <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                )}
                <span className="flex-1">{step.description}</span>
                {step.files && step.files.length > 0 && (
                  <span className="text-xs text-muted-foreground">
                    {step.files.length} file{step.files.length !== 1 ? "s" : ""}
                  </span>
                )}
              </button>
              {expandedSteps.has(i) && step.files && step.files.length > 0 && (
                <div className="border-t bg-muted/10 px-4 py-2">
                  <ul className="space-y-1">
                    {step.files.map((f, j) => (
                      <li
                        key={j}
                        className="flex items-center gap-2 font-mono text-xs text-muted-foreground"
                      >
                        <FileCode className="h-3 w-3" />
                        {f}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          ))}
        </div>
      </div>

      {/* Affected Files */}
      <div className="px-6 py-4">
        <h4 className="mb-3 text-sm font-medium text-muted-foreground">
          All Affected Files ({plan.affected_files.length})
        </h4>
        <div className="grid gap-1 sm:grid-cols-2">
          {plan.affected_files.map((f, i) => (
            <div
              key={i}
              className="flex items-center gap-2 rounded px-2 py-1 font-mono text-xs hover:bg-muted/30"
            >
              <FileCode className="h-3 w-3 shrink-0 text-muted-foreground" />
              <span className="truncate">{f}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
