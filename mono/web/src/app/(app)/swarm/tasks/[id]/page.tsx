// ─── Swarm Task Detail ───────────────────────────────────────────────────────
// Task detail page: plan display, approve/reject, rating, diff list with
// real-time WebSocket updates and links to full review page.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import { useParams } from "next/navigation";
import {
  Bot,
  CheckCircle,
  XCircle,
  MessageSquare,
  Star,
  FileCode,
  ArrowLeft,
  ExternalLink,
} from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { PlanReviewCard } from "@/components/swarm/plan-review-card";
import { DiffViewer } from "@/components/swarm/diff-viewer";
import { ActivityFeed } from "@/components/swarm/activity-feed";
import { useSwarmEvents } from "@/hooks/use-swarm-events";
import type { SwarmTask, SwarmDiff, PlanDocument } from "@/types/swarm";

export default function SwarmTaskDetailPage() {
  const params = useParams<{ id: string }>();
  const [task, setTask] = useState<SwarmTask | null>(null);
  const [diffs, setDiffs] = useState<SwarmDiff[]>([]);
  const [loading, setLoading] = useState(true);
  const [rating, setRating] = useState(0);
  const [comment, setComment] = useState("");

  const { events, connected } = useSwarmEvents(params.id);

  const fetchTask = useCallback(async () => {
    try {
      const res = await fetch(`/api/v1/swarm/tasks/${params.id}`);
      if (res.ok) {
        const data = await res.json();
        setTask(data);
      }

      const diffsRes = await fetch(
        `/api/v1/swarm/tasks/${params.id}/diffs`
      );
      if (diffsRes.ok) {
        const diffsData = await diffsRes.json();
        setDiffs(diffsData.diffs || []);
      }
    } catch {
      // Handled by error boundary
    } finally {
      setLoading(false);
    }
  }, [params.id]);

  useEffect(() => {
    fetchTask();
  }, [fetchTask]);

  // Refresh on relevant WS events
  useEffect(() => {
    if (!events.length) return;
    const last = events[0];
    if (
      last.type === "swarm_diff" ||
      last.type === "swarm_plan" ||
      (last.type === "swarm_task" && last.event === "status_changed")
    ) {
      fetchTask();
    }
  }, [events, fetchTask]);

  const handlePlanAction = async (action: string) => {
    await fetch(`/api/v1/swarm/tasks/${params.id}/plan-action`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action }),
    });
    await fetchTask();
  };

  const handlePlanComment = async (commentText: string) => {
    await fetch(`/api/v1/swarm/tasks/${params.id}/plan-comment`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ comment: commentText }),
    });
  };

  const handleRate = async () => {
    if (rating === 0) return;
    await fetch(`/api/v1/swarm/tasks/${params.id}/rate`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ rating, comment }),
    });
    await fetchTask();
  };

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="h-8 w-1/3 animate-pulse rounded bg-muted" />
        <div className="h-64 animate-pulse rounded bg-muted" />
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

  return (
    <div className="space-y-6">
      <PageHeader
        title={task.description}
        description={`${task.repo_id} • ${task.status.replace(/_/g, " ")}`}
        actions={
          <div className="flex items-center gap-3">
            {connected && (
              <span className="flex items-center gap-1.5 text-xs text-green-600 dark:text-green-400">
                <span className="h-2 w-2 animate-pulse rounded-full bg-green-500" />
                Live
              </span>
            )}
            {diffs.length > 0 && (
              <Button variant="outline" asChild>
                <a href={`/swarm/tasks/${params.id}/review`}>
                  <FileCode className="mr-2 h-4 w-4" />
                  Review Diffs ({diffs.length})
                </a>
              </Button>
            )}
            <Button variant="outline" asChild>
              <a href="/swarm/tasks">
                <ArrowLeft className="mr-2 h-4 w-4" />
                Back to Tasks
              </a>
            </Button>
          </div>
        }
      />

      <div className="grid gap-6 lg:grid-cols-[1fr_320px]">
        {/* Main content */}
        <div className="space-y-6">
          {/* Task Info */}
          <div className="grid gap-4 md:grid-cols-2">
            <div className="rounded-lg border bg-card p-6">
              <h3 className="mb-3 font-semibold">Details</h3>
              <dl className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Status</dt>
                  <dd className="font-medium">{task.status.replace(/_/g, " ")}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Created</dt>
                  <dd>{new Date(task.created_at).toLocaleString()}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Agents</dt>
                  <dd>{task.assigned_agents.length}</dd>
                </div>
                {task.pr_url && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">PR</dt>
                    <dd>
                      <a
                        href={task.pr_url}
                        className="inline-flex items-center gap-1 text-blue-600 hover:underline"
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        #{task.pr_number}
                        <ExternalLink className="h-3 w-3" />
                      </a>
                    </dd>
                  </div>
                )}
              </dl>
            </div>

            {/* Rating Card */}
            {task.status === "completed" && (
              <div className="rounded-lg border bg-card p-6">
                <h3 className="mb-3 font-semibold">Rate This Work</h3>
                <div className="mb-3 flex gap-1">
                  {[1, 2, 3, 4, 5].map((n) => (
                    <button
                      key={n}
                      onClick={() => setRating(n)}
                      className="transition-colors"
                    >
                      <Star
                        className={`h-6 w-6 ${n <= rating ? "fill-yellow-400 text-yellow-400" : "text-muted-foreground"}`}
                      />
                    </button>
                  ))}
                </div>
                <textarea
                  className="mb-3 w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground"
                  rows={2}
                  placeholder="Optional comment…"
                  value={comment}
                  onChange={(e) => setComment(e.target.value)}
                />
                <Button size="sm" onClick={handleRate} disabled={rating === 0}>
                  Submit Rating
                </Button>
              </div>
            )}
          </div>

          {/* Plan Section */}
          {plan && (
            <PlanReviewCard
              plan={plan}
              taskId={params.id}
              status={task.status}
              onApprove={() => handlePlanAction("approve")}
              onReject={() => handlePlanAction("reject")}
              onComment={handlePlanComment}
            />
          )}

          {/* Diffs Section */}
          {diffs.length > 0 && (
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-lg font-semibold">
                  Diffs ({diffs.length} file{diffs.length !== 1 ? "s" : ""})
                </h3>
                <Button variant="outline" size="sm" asChild>
                  <a href={`/swarm/tasks/${params.id}/review`}>
                    Full Review View →
                  </a>
                </Button>
              </div>
              {diffs.slice(0, 5).map((diff) => (
                <DiffViewer key={diff.id} diff={diff} readOnly />
              ))}
              {diffs.length > 5 && (
                <p className="text-center text-sm text-muted-foreground">
                  +{diffs.length - 5} more files.{" "}
                  <a
                    href={`/swarm/tasks/${params.id}/review`}
                    className="text-blue-600 hover:underline"
                  >
                    View all in review mode
                  </a>
                </p>
              )}
            </div>
          )}
        </div>

        {/* Sidebar */}
        <div className="space-y-4">
          <ActivityFeed events={events} />
        </div>
      </div>
    </div>
  );
}
