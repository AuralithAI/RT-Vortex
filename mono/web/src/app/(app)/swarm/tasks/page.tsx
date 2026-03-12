// ─── Swarm Task List ─────────────────────────────────────────────────────────
// Filterable list of all swarm tasks with status badges.
// Phase 0 stub — will be expanded with WebSocket updates in Phase 1.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect } from "react";
import { Bot, Filter, ChevronRight } from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import type { SwarmTask, TaskStatus } from "@/types/swarm";

const STATUS_FILTERS: { label: string; value: TaskStatus | "all" }[] = [
  { label: "All", value: "all" },
  { label: "Submitted", value: "submitted" },
  { label: "Planning", value: "planning" },
  { label: "Plan Review", value: "plan_review" },
  { label: "Implementing", value: "implementing" },
  { label: "Diff Review", value: "diff_review" },
  { label: "Completed", value: "completed" },
  { label: "Failed", value: "failed" },
];

export default function SwarmTasksPage() {
  const [tasks, setTasks] = useState<SwarmTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState<TaskStatus | "all">("all");

  useEffect(() => {
    const fetchTasks = async () => {
      try {
        const params = new URLSearchParams();
        if (statusFilter !== "all") params.set("status", statusFilter);
        const res = await fetch(`/api/v1/swarm/tasks?${params}`);
        if (res.ok) {
          const data = await res.json();
          setTasks(data.tasks || []);
        }
      } catch {
        // Will be handled by error boundary in Phase 1.
      } finally {
        setLoading(false);
      }
    };
    fetchTasks();
  }, [statusFilter]);

  const statusColor = (status: string) => {
    switch (status) {
      case "submitted":
        return "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200";
      case "planning":
      case "implementing":
        return "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200";
      case "plan_review":
      case "diff_review":
        return "bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200";
      case "completed":
        return "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200";
      case "failed":
      case "timed_out":
        return "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200";
      default:
        return "bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200";
    }
  };

  return (
    <>
      <PageHeader
        title="Swarm Tasks"
        description="All agent tasks across repositories"
      />

      {/* Status Filters */}
      <div className="flex flex-wrap gap-2">
        {STATUS_FILTERS.map((f) => (
          <Button
            key={f.value}
            variant={statusFilter === f.value ? "default" : "outline"}
            size="sm"
            onClick={() => setStatusFilter(f.value)}
          >
            {f.label}
          </Button>
        ))}
      </div>

      {/* Task List */}
      <div className="rounded-lg border bg-card">
        <div className="divide-y">
          {loading ? (
            Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="px-6 py-4">
                <div className="h-5 w-3/4 animate-pulse rounded bg-muted" />
                <div className="mt-2 h-4 w-1/2 animate-pulse rounded bg-muted" />
              </div>
            ))
          ) : tasks.length === 0 ? (
            <div className="px-6 py-12 text-center text-muted-foreground">
              <Bot className="mx-auto mb-3 h-10 w-10 opacity-50" />
              <p>No tasks match the current filter.</p>
            </div>
          ) : (
            tasks.map((task) => (
              <a
                key={task.id}
                href={`/swarm/tasks/${task.id}`}
                className="flex items-center justify-between px-6 py-4 transition-colors hover:bg-muted/50"
              >
                <div className="space-y-1">
                  <p className="font-medium">{task.description}</p>
                  <p className="text-sm text-muted-foreground">
                    {task.repo_id} •{" "}
                    {new Date(task.created_at).toLocaleDateString()} •{" "}
                    {task.assigned_agents.length} agent
                    {task.assigned_agents.length !== 1 ? "s" : ""}
                  </p>
                </div>
                <div className="flex items-center gap-3">
                  <span
                    className={`rounded-full px-2.5 py-0.5 text-xs font-medium ${statusColor(task.status)}`}
                  >
                    {task.status.replace(/_/g, " ")}
                  </span>
                  <ChevronRight className="h-4 w-4 text-muted-foreground" />
                </div>
              </a>
            ))
          )}
        </div>
      </div>
    </>
  );
}
