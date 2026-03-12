// ─── Swarm Task Detail ───────────────────────────────────────────────────────
// Task detail page: plan display, approve/reject, rating, diff list.
// Phase 0 stub — will be expanded with Monaco diff viewer in Phase 1.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect } from "react";
import { useParams } from "next/navigation";
import {
  Bot,
  CheckCircle,
  XCircle,
  MessageSquare,
  Star,
  FileCode,
  ArrowLeft,
} from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import type { SwarmTask, SwarmDiff, PlanDocument } from "@/types/swarm";

export default function SwarmTaskDetailPage() {
  const params = useParams<{ id: string }>();
  const [task, setTask] = useState<SwarmTask | null>(null);
  const [diffs, setDiffs] = useState<SwarmDiff[]>([]);
  const [loading, setLoading] = useState(true);
  const [rating, setRating] = useState(0);
  const [comment, setComment] = useState("");

  useEffect(() => {
    const fetchTask = async () => {
      try {
        const res = await fetch(`/api/v1/swarm/tasks/${params.id}`);
        if (res.ok) {
          const data = await res.json();
          setTask(data);
        }

        // Fetch diffs if available.
        const diffsRes = await fetch(
          `/api/v1/swarm/tasks/${params.id}/diffs`
        );
        if (diffsRes.ok) {
          const diffsData = await diffsRes.json();
          setDiffs(diffsData.diffs || []);
        }
      } catch {
        // Handled by error boundary in Phase 1.
      } finally {
        setLoading(false);
      }
    };
    fetchTask();
  }, [params.id]);

  const handlePlanAction = async (action: string) => {
    await fetch(`/api/v1/swarm/tasks/${params.id}/plan-action`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action }),
    });
    // Refresh task.
    const res = await fetch(`/api/v1/swarm/tasks/${params.id}`);
    if (res.ok) setTask(await res.json());
  };

  const handleRate = async () => {
    if (rating === 0) return;
    await fetch(`/api/v1/swarm/tasks/${params.id}/rate`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ rating, comment }),
    });
    // Refresh.
    const res = await fetch(`/api/v1/swarm/tasks/${params.id}`);
    if (res.ok) setTask(await res.json());
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
    <>
      <PageHeader
        title={task.description}
        description={`${task.repo_id} • ${task.status.replace(/_/g, " ")}`}
        actions={
          <Button variant="outline" asChild>
            <a href="/swarm/tasks">
              <ArrowLeft className="mr-2 h-4 w-4" />
              Back to Tasks
            </a>
          </Button>
        }
      />

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
                    className="text-blue-600 hover:underline"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    #{task.pr_number}
                  </a>
                </dd>
              </div>
            )}
          </dl>
        </div>

        {/* Rating Card (shown for completed tasks) */}
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
        <div className="rounded-lg border bg-card p-6">
          <div className="mb-4 flex items-center justify-between">
            <h3 className="text-lg font-semibold">Plan</h3>
            {task.status === "plan_review" && (
              <div className="flex gap-2">
                <Button
                  size="sm"
                  onClick={() => handlePlanAction("approve")}
                >
                  <CheckCircle className="mr-2 h-4 w-4" />
                  Approve
                </Button>
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() => handlePlanAction("reject")}
                >
                  <XCircle className="mr-2 h-4 w-4" />
                  Reject
                </Button>
              </div>
            )}
          </div>
          <p className="mb-4 text-sm">{plan.summary}</p>
          <div className="mb-4">
            <h4 className="mb-2 text-sm font-medium text-muted-foreground">
              Steps
            </h4>
            <ol className="list-inside list-decimal space-y-1 text-sm">
              {plan.steps?.map((step, i) => (
                <li key={i}>{step.description}</li>
              ))}
            </ol>
          </div>
          <div>
            <h4 className="mb-2 text-sm font-medium text-muted-foreground">
              Affected Files
            </h4>
            <ul className="space-y-1 text-sm font-mono">
              {plan.affected_files?.map((f, i) => (
                <li key={i} className="flex items-center gap-2">
                  <FileCode className="h-3 w-3 text-muted-foreground" />
                  {f}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}

      {/* Diffs Section (stub — Monaco viewer in Phase 1) */}
      {diffs.length > 0 && (
        <div className="rounded-lg border bg-card p-6">
          <h3 className="mb-4 text-lg font-semibold">
            Diffs ({diffs.length} file{diffs.length !== 1 ? "s" : ""})
          </h3>
          <div className="divide-y">
            {diffs.map((diff) => (
              <div key={diff.id} className="py-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <FileCode className="h-4 w-4 text-muted-foreground" />
                    <span className="font-mono text-sm">{diff.file_path}</span>
                    <span className="rounded-full bg-muted px-2 py-0.5 text-xs">
                      {diff.change_type}
                    </span>
                  </div>
                  <span className="text-xs text-muted-foreground">
                    {diff.status}
                  </span>
                </div>
                {/* Phase 1: Replace with Monaco diff editor */}
                {diff.unified_diff && (
                  <pre className="mt-2 max-h-64 overflow-auto rounded bg-muted p-3 font-mono text-xs">
                    {diff.unified_diff}
                  </pre>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </>
  );
}
