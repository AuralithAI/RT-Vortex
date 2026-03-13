// ─── Task History ────────────────────────────────────────────────────────────
// Paginated table of completed/failed/timed-out tasks with stats.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import {
  CheckCircle,
  XCircle,
  AlertTriangle,
  Ban,
  Star,
  ChevronLeft,
  ChevronRight,
  RefreshCw,
  Loader2,
  ExternalLink,
  Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import type { TaskSummary, TaskHistoryResponse, TaskStatus } from "@/types/swarm";

const PAGE_SIZE = 25;

export function TaskHistory() {
  const [data, setData] = useState<TaskHistoryResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(0);

  const fetchHistory = useCallback(async () => {
    try {
      const res = await fetch(
        `/api/v1/swarm/tasks/history?limit=${PAGE_SIZE}&offset=${page * PAGE_SIZE}`
      );
      if (res.ok) {
        setData(await res.json());
      }
    } catch {
      /* retry on next action */
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => {
    setLoading(true);
    fetchHistory();
  }, [fetchHistory]);

  const handleRetry = async (taskId: string) => {
    try {
      const res = await fetch(`/api/v1/swarm/tasks/${taskId}/retry`, {
        method: "POST",
      });
      if (res.ok) {
        fetchHistory();
      }
    } catch {
      /* */
    }
  };

  const handleDelete = async (taskId: string) => {
    if (!confirm("Delete this task? This cannot be undone.")) return;
    try {
      const res = await fetch(`/api/v1/swarm/tasks/${taskId}`, {
        method: "DELETE",
      });
      if (res.ok) {
        fetchHistory();
      }
    } catch {
      /* */
    }
  };

  const totalPages = data ? Math.ceil(data.total / PAGE_SIZE) : 0;

  const statusIcon = (status: TaskStatus) => {
    switch (status) {
      case "completed":
        return <CheckCircle className="h-4 w-4 text-green-500" />;
      case "failed":
        return <XCircle className="h-4 w-4 text-red-500" />;
      case "timed_out":
        return <AlertTriangle className="h-4 w-4 text-amber-500" />;
      case "cancelled":
        return <Ban className="h-4 w-4 text-gray-400" />;
      default:
        return null;
    }
  };

  const formatDuration = (sec?: number) => {
    if (sec == null) return "—";
    if (sec < 60) return `${Math.round(sec)}s`;
    if (sec < 3600) return `${Math.round(sec / 60)}m`;
    return `${(sec / 3600).toFixed(1)}h`;
  };

  if (loading && !data) {
    return (
      <div className="flex items-center justify-center p-12">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="rounded-lg border bg-card">
      <div className="border-b p-4">
        <h3 className="text-lg font-semibold">Task History</h3>
        <p className="text-sm text-muted-foreground">
          {data?.total ?? 0} completed tasks
        </p>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="border-b text-left text-xs font-medium text-muted-foreground">
              <th className="p-3">Status</th>
              <th className="p-3">Description</th>
              <th className="p-3 text-center">Diffs</th>
              <th className="p-3 text-center">Agents</th>
              <th className="p-3 text-center">Duration</th>
              <th className="p-3 text-center">Rating</th>
              <th className="p-3 text-center">Retries</th>
              <th className="p-3">PR</th>
              <th className="p-3">Date</th>
              <th className="p-3"></th>
            </tr>
          </thead>
          <tbody>
            {data?.tasks?.map((task) => (
              <tr
                key={task.id}
                className="border-b transition-colors hover:bg-muted/50"
              >
                <td className="p-3">{statusIcon(task.status)}</td>
                <td className="max-w-xs p-3">
                  <Link
                    href={`/swarm/tasks/${task.id}`}
                    className="line-clamp-1 text-sm font-medium hover:underline"
                  >
                    {task.description}
                  </Link>
                </td>
                <td className="p-3 text-center text-sm">{task.diff_count}</td>
                <td className="p-3 text-center text-sm">{task.agent_count}</td>
                <td className="p-3 text-center text-sm">
                  {formatDuration(task.duration_sec)}
                </td>
                <td className="p-3 text-center">
                  {task.human_rating ? (
                    <span className="inline-flex items-center gap-1 text-sm">
                      {task.human_rating}
                      <Star className="h-3 w-3 fill-yellow-400 text-yellow-400" />
                    </span>
                  ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                  )}
                </td>
                <td className="p-3 text-center">
                  {task.retry_count > 0 ? (
                    <span className="rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/40 dark:text-amber-300">
                      {task.retry_count}
                    </span>
                  ) : (
                    <span className="text-xs text-muted-foreground">0</span>
                  )}
                </td>
                <td className="p-3">
                  {task.pr_url ? (
                    <a
                      href={task.pr_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1 text-xs text-primary hover:underline"
                    >
                      #{task.pr_number}
                      <ExternalLink className="h-3 w-3" />
                    </a>
                  ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                  )}
                </td>
                <td className="p-3 text-xs text-muted-foreground">
                  {new Date(task.created_at).toLocaleDateString()}
                </td>
                <td className="p-3">
                  <div className="flex items-center gap-1">
                    {(task.status === "failed" || task.status === "timed_out") &&
                      task.retry_count < 3 && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleRetry(task.id)}
                          className="h-7 px-2"
                        >
                          <RefreshCw className="mr-1 h-3 w-3" />
                          Retry
                        </Button>
                      )}
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleDelete(task.id)}
                      className="h-7 px-2 text-muted-foreground hover:text-destructive"
                    >
                      <Trash2 className="h-3 w-3" />
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between border-t p-3">
          <span className="text-xs text-muted-foreground">
            Page {page + 1} of {totalPages}
          </span>
          <div className="flex gap-1">
            <Button
              variant="ghost"
              size="sm"
              disabled={page === 0}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
            >
              <ChevronLeft className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={page >= totalPages - 1}
              onClick={() => setPage((p) => p + 1)}
            >
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
