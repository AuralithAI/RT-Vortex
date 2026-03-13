// ─── Task Pipeline Board ─────────────────────────────────────────────────────
// Kanban-style board showing tasks across all statuses.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import {
  ArrowRight,
  Clock,
  CheckCircle,
  XCircle,
  AlertTriangle,
  Loader2,
  RefreshCw,
  Trash2,
} from "lucide-react";
import type { SwarmTask, TaskStatus } from "@/types/swarm";

interface PipelineColumn {
  key: TaskStatus;
  label: string;
  color: string;
  icon: React.ReactNode;
}

const COLUMNS: PipelineColumn[] = [
  {
    key: "submitted",
    label: "Submitted",
    color: "border-blue-400",
    icon: <Clock className="h-4 w-4 text-blue-500" />,
  },
  {
    key: "planning",
    label: "Planning",
    color: "border-yellow-400",
    icon: <Loader2 className="h-4 w-4 animate-spin text-yellow-500" />,
  },
  {
    key: "plan_review",
    label: "Plan Review",
    color: "border-purple-400",
    icon: <ArrowRight className="h-4 w-4 text-purple-500" />,
  },
  {
    key: "implementing",
    label: "Implementing",
    color: "border-orange-400",
    icon: <Loader2 className="h-4 w-4 animate-spin text-orange-500" />,
  },
  {
    key: "diff_review",
    label: "Diff Review",
    color: "border-indigo-400",
    icon: <ArrowRight className="h-4 w-4 text-indigo-500" />,
  },
  {
    key: "pr_creating",
    label: "PR Creating",
    color: "border-teal-400",
    icon: <Loader2 className="h-4 w-4 animate-spin text-teal-500" />,
  },
  {
    key: "completed",
    label: "Completed",
    color: "border-green-400",
    icon: <CheckCircle className="h-4 w-4 text-green-500" />,
  },
  {
    key: "failed",
    label: "Failed",
    color: "border-red-400",
    icon: <XCircle className="h-4 w-4 text-red-500" />,
  },
  {
    key: "timed_out",
    label: "Timed Out",
    color: "border-amber-400",
    icon: <AlertTriangle className="h-4 w-4 text-amber-500" />,
  },
];

export function TaskPipelineBoard() {
  const [tasks, setTasks] = useState<SwarmTask[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchTasks = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/swarm/tasks?limit=100");
      if (res.ok) {
        const data = await res.json();
        setTasks(data || []);
      }
    } catch {
      /* silently retry on next poll */
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchTasks();
    const interval = setInterval(fetchTasks, 5000);
    return () => clearInterval(interval);
  }, [fetchTasks]);

  const tasksByStatus = (status: TaskStatus) =>
    tasks.filter((t) => t.status === status);

  if (loading) {
    return (
      <div className="flex items-center justify-center p-12">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <div className="flex min-w-[1400px] gap-3 p-4">
        {COLUMNS.map((col) => {
          const columnTasks = tasksByStatus(col.key);
          return (
            <div
              key={col.key}
              className={`flex w-48 flex-shrink-0 flex-col rounded-lg border-t-4 bg-muted/30 ${col.color}`}
            >
              {/* Column header */}
              <div className="flex items-center gap-2 p-3 pb-2">
                {col.icon}
                <span className="text-sm font-semibold">{col.label}</span>
                <span className="ml-auto rounded-full bg-muted px-2 py-0.5 text-xs font-medium">
                  {columnTasks.length}
                </span>
              </div>

              {/* Task cards */}
              <div className="flex-1 space-y-2 overflow-y-auto p-2 pt-0">
                {columnTasks.map((task) => (
                  <TaskCard key={task.id} task={task} />
                ))}
                {columnTasks.length === 0 && (
                  <p className="py-4 text-center text-xs text-muted-foreground">
                    No tasks
                  </p>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function TaskCard({ task }: { task: SwarmTask }) {
  const [retrying, setRetrying] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const handleRetry = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setRetrying(true);
    try {
      await fetch(`/api/v1/swarm/tasks/${task.id}/retry`, { method: "POST" });
    } finally {
      setRetrying(false);
    }
  };

  const handleDelete = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!confirm("Delete this task? This cannot be undone.")) return;
    setDeleting(true);
    try {
      await fetch(`/api/v1/swarm/tasks/${task.id}`, { method: "DELETE" });
    } finally {
      setDeleting(false);
    }
  };

  const isRetryable =
    (task.status === "failed" || task.status === "timed_out") &&
    task.retry_count < 3;

  return (
    <Link href={`/swarm/tasks/${task.id}`}>
      <div className="group cursor-pointer rounded-md border bg-card p-3 shadow-sm transition-colors hover:border-primary/50">
        <div className="flex items-start justify-between gap-1">
          <p className="line-clamp-2 text-xs font-medium leading-tight">
            {task.description}
          </p>
          <button
            onClick={handleDelete}
            disabled={deleting}
            className="shrink-0 rounded p-0.5 opacity-0 transition-opacity hover:bg-destructive/10 hover:text-destructive group-hover:opacity-100 disabled:opacity-50"
            title="Delete task"
          >
            {deleting ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Trash2 className="h-3 w-3" />
            )}
          </button>
        </div>
        <div className="mt-2 flex items-center justify-between">
          <span className="text-[10px] text-muted-foreground">
            {new Date(task.created_at).toLocaleDateString()}
          </span>
          {task.retry_count > 0 && (
            <span className="rounded bg-amber-100 px-1 text-[10px] font-medium text-amber-700 dark:bg-amber-900 dark:text-amber-300">
              retry {task.retry_count}
            </span>
          )}
        </div>
        {task.failure_reason && (
          <p className="mt-1 line-clamp-1 text-[10px] text-red-500">
            {task.failure_reason}
          </p>
        )}
        {isRetryable && (
          <button
            onClick={handleRetry}
            disabled={retrying}
            className="mt-2 flex w-full items-center justify-center gap-1 rounded bg-primary/10 py-1 text-[10px] font-medium text-primary hover:bg-primary/20 disabled:opacity-50"
          >
            {retrying ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <RefreshCw className="h-3 w-3" />
            )}
            Retry
          </button>
        )}
      </div>
    </Link>
  );
}
