// ─── Diff Review Page ────────────────────────────────────────────────────────
// Full-page diff review for a swarm task: shows all diffs with the DiffViewer,
// plan context, activity feed, and batch approve/reject controls.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import { useParams } from "next/navigation";
import {
  ArrowLeft,
  CheckCircle,
  XCircle,
  FileCode,
  Bot,
  Loader2,
} from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { DiffViewer } from "@/components/swarm/diff-viewer";
import { PlanReviewCard } from "@/components/swarm/plan-review-card";
import { ActivityFeed } from "@/components/swarm/activity-feed";
import { useSwarmEvents } from "@/hooks/use-swarm-events";
import type {
  SwarmTask,
  SwarmDiff,
  PlanDocument,
} from "@/types/swarm";

export default function SwarmDiffReviewPage() {
  const params = useParams<{ id: string }>();
  const [task, setTask] = useState<SwarmTask | null>(null);
  const [diffs, setDiffs] = useState<SwarmDiff[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);

  const { events, connected } = useSwarmEvents(params.id);

  // ── Fetch data ──────────────────────────────────────────────────────────

  const fetchData = useCallback(async () => {
    try {
      const [taskRes, diffsRes] = await Promise.all([
        fetch(`/api/v1/swarm/tasks/${params.id}`),
        fetch(`/api/v1/swarm/tasks/${params.id}/diffs`),
      ]);

      if (taskRes.ok) setTask(await taskRes.json());
      if (diffsRes.ok) {
        const data = await diffsRes.json();
        setDiffs(data.diffs || []);
      }
    } catch {
      // Error handling deferred to Phase 2 error boundary
    } finally {
      setLoading(false);
    }
  }, [params.id]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // Refresh on relevant WS events
  useEffect(() => {
    if (!events.length) return;
    const last = events[0];
    if (
      last.type === "swarm_diff" ||
      last.type === "swarm_plan" ||
      (last.type === "swarm_task" && last.event === "status_changed")
    ) {
      fetchData();
    }
  }, [events, fetchData]);

  // ── Actions ─────────────────────────────────────────────────────────────

  const handleDiffAction = async (diffId: string, action: "approve" | "reject") => {
    setActionLoading(true);
    try {
      await fetch(`/api/v1/swarm/tasks/${params.id}/diffs/${diffId}/${action}`, {
        method: "POST",
      });
      await fetchData();
    } finally {
      setActionLoading(false);
    }
  };

  const handleBatchAction = async (action: "approve" | "reject") => {
    setActionLoading(true);
    try {
      const pending = diffs.filter((d) => d.status === "pending");
      await Promise.all(
        pending.map((d) =>
          fetch(`/api/v1/swarm/tasks/${params.id}/diffs/${d.id}/${action}`, {
            method: "POST",
          })
        )
      );
      await fetchData();
    } finally {
      setActionLoading(false);
    }
  };

  const handlePlanApprove = async () => {
    await fetch(`/api/v1/swarm/tasks/${params.id}/plan-action`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "approve" }),
    });
    await fetchData();
  };

  const handlePlanReject = async () => {
    await fetch(`/api/v1/swarm/tasks/${params.id}/plan-action`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "reject" }),
    });
    await fetchData();
  };

  const handlePlanComment = async (comment: string) => {
    await fetch(`/api/v1/swarm/tasks/${params.id}/plan-comment`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ comment }),
    });
  };

  // ── Render ──────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="flex items-center justify-center py-24">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!task) {
    return (
      <div className="py-12 text-center text-muted-foreground">
        Task not found.
      </div>
    );
  }

  const plan = task.plan_document as PlanDocument | undefined;
  const pendingCount = diffs.filter((d) => d.status === "pending").length;
  const approvedCount = diffs.filter((d) => d.status === "approved").length;
  const rejectedCount = diffs.filter((d) => d.status === "rejected").length;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Review Changes"
        description={`${task.description} • ${task.repo_id}`}
        actions={
          <div className="flex items-center gap-3">
            {connected && (
              <span className="flex items-center gap-1.5 text-xs text-green-600 dark:text-green-400">
                <span className="h-2 w-2 animate-pulse rounded-full bg-green-500" />
                Live
              </span>
            )}
            <Button variant="outline" asChild>
              <a href={`/swarm/tasks/${params.id}`}>
                <ArrowLeft className="mr-2 h-4 w-4" />
                Back to Task
              </a>
            </Button>
          </div>
        }
      />

      <div className="grid gap-6 lg:grid-cols-[1fr_320px]">
        {/* Main Content */}
        <div className="space-y-6">
          {/* Plan (if in review) */}
          {plan && (task.status === "plan_review" || task.status === "planning") && (
            <PlanReviewCard
              plan={plan}
              taskId={params.id}
              status={task.status}
              onApprove={handlePlanApprove}
              onReject={handlePlanReject}
              onComment={handlePlanComment}
            />
          )}

          {/* Diff Summary Bar */}
          {diffs.length > 0 && (
            <div className="flex items-center justify-between rounded-lg border bg-card px-4 py-3">
              <div className="flex items-center gap-4 text-sm">
                <span className="flex items-center gap-1.5 font-medium">
                  <FileCode className="h-4 w-4" />
                  {diffs.length} file{diffs.length !== 1 ? "s" : ""} changed
                </span>
                {pendingCount > 0 && (
                  <span className="text-muted-foreground">
                    {pendingCount} pending
                  </span>
                )}
                {approvedCount > 0 && (
                  <span className="text-green-600 dark:text-green-400">
                    {approvedCount} approved
                  </span>
                )}
                {rejectedCount > 0 && (
                  <span className="text-red-600 dark:text-red-400">
                    {rejectedCount} rejected
                  </span>
                )}
              </div>
              {pendingCount > 0 && (
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={actionLoading}
                    onClick={() => handleBatchAction("reject")}
                  >
                    <XCircle className="mr-1 h-3.5 w-3.5" />
                    Reject All
                  </Button>
                  <Button
                    size="sm"
                    disabled={actionLoading}
                    onClick={() => handleBatchAction("approve")}
                  >
                    <CheckCircle className="mr-1 h-3.5 w-3.5" />
                    Approve All
                  </Button>
                </div>
              )}
            </div>
          )}

          {/* Individual Diffs */}
          {diffs.map((diff) => (
            <DiffViewer
              key={diff.id}
              diff={diff}
              onApprove={(id) => handleDiffAction(id, "approve")}
              onReject={(id) => handleDiffAction(id, "reject")}
            />
          ))}

          {diffs.length === 0 && task.status !== "plan_review" && (
            <div className="rounded-lg border bg-card py-12 text-center">
              <Bot className="mx-auto mb-3 h-10 w-10 text-muted-foreground/50" />
              <p className="text-sm text-muted-foreground">
                {task.status === "implementing"
                  ? "Agents are working… diffs will appear here."
                  : "No diffs submitted for this task."}
              </p>
            </div>
          )}
        </div>

        {/* Sidebar: Activity Feed */}
        <div className="space-y-4">
          <ActivityFeed events={events} />

          {/* Plan Summary (sidebar, when viewing diffs) */}
          {plan && task.status !== "plan_review" && (
            <div className="rounded-lg border bg-card p-4">
              <h4 className="mb-2 text-sm font-semibold">Plan Summary</h4>
              <p className="text-xs text-muted-foreground">{plan.summary}</p>
              <div className="mt-3 space-y-1">
                {plan.affected_files.slice(0, 8).map((f, i) => (
                  <div
                    key={i}
                    className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground"
                  >
                    <FileCode className="h-3 w-3 shrink-0" />
                    <span className="truncate">{f}</span>
                  </div>
                ))}
                {plan.affected_files.length > 8 && (
                  <p className="text-xs text-muted-foreground">
                    +{plan.affected_files.length - 8} more files
                  </p>
                )}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
