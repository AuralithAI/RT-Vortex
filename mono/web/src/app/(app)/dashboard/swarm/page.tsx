// ─── Swarm Dashboard ─────────────────────────────────────────────────────────
// Overview page: live stats, task submission, pipeline board, task history.
// Production-grade with live overview, Prometheus-backed metrics,
// retry controls, and tabbed navigation.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Bot,
  ListChecks,
  Users,
  Plus,
  Send,
  Loader2,
  Activity,
  Clock,
  AlertTriangle,
  RefreshCw,
  Percent,
  Kanban,
  History,
} from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { TaskPipelineBoard } from "@/components/swarm/task-pipeline-board";
import { TaskHistory } from "@/components/swarm/task-history";
import type { SwarmTask, SwarmOverview, TaskSubmission } from "@/types/swarm";

type Tab = "pipeline" | "history";

export default function SwarmDashboardPage() {
  const [tasks, setTasks] = useState<SwarmTask[]>([]);
  const [overview, setOverview] = useState<SwarmOverview | null>(null);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [repoId, setRepoId] = useState("");
  const [description, setDescription] = useState("");
  const [showForm, setShowForm] = useState(false);
  const [activeTab, setActiveTab] = useState<Tab>("pipeline");

  // Fetch live overview stats every 10 seconds
  const fetchOverview = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/swarm/overview");
      if (res.ok) {
        setOverview(await res.json());
      }
    } catch {
      /* will retry on next interval */
    }
  }, []);

  useEffect(() => {
    fetchOverview();
    const iv = setInterval(fetchOverview, 10_000);
    return () => clearInterval(iv);
  }, [fetchOverview]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!repoId.trim() || !description.trim()) return;

    setSubmitting(true);
    try {
      const res = await fetch("/api/v1/swarm/tasks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ repo_id: repoId, description }),
      });
      if (res.ok) {
        const task = await res.json();
        setTasks((prev) => [task, ...prev]);
        setRepoId("");
        setDescription("");
        setShowForm(false);
      }
    } finally {
      setSubmitting(false);
    }
  };

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
        title="Agent Swarm"
        description="AI agent teams that implement code changes from natural language descriptions"
        actions={
          <Button onClick={() => setShowForm(!showForm)}>
            <Plus className="mr-2 h-4 w-4" />
            New Task
          </Button>
        }
      />

      {/* Task Submission Form */}
      {showForm && (
        <div className="rounded-lg border bg-card p-6">
          <h3 className="mb-4 text-lg font-semibold">Submit a New Task</h3>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="mb-1 block text-sm font-medium">
                Repository
              </label>
              <Input
                placeholder="e.g. my-org/my-repo"
                value={repoId}
                onChange={(e) => setRepoId(e.target.value)}
                disabled={submitting}
              />
            </div>
            <div>
              <label className="mb-1 block text-sm font-medium">
                Task Description
              </label>
              <textarea
                className="w-full rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                rows={3}
                placeholder="Describe what you want the agents to implement…"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                disabled={submitting}
              />
            </div>
            <div className="flex gap-2">
              <Button type="submit" disabled={submitting}>
                {submitting ? (
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                ) : (
                  <Send className="mr-2 h-4 w-4" />
                )}
                Submit Task
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => setShowForm(false)}
              >
                Cancel
              </Button>
            </div>
          </form>
        </div>
      )}

      {/* Stats Overview — live from /overview */}
      <div className="grid gap-4 md:grid-cols-3 lg:grid-cols-6">
        <StatCard
          icon={<ListChecks className="h-5 w-5 text-blue-600 dark:text-blue-400" />}
          bgClass="bg-blue-100 dark:bg-blue-900"
          label="Active Tasks"
          value={overview?.active_tasks ?? "—"}
        />
        <StatCard
          icon={<Clock className="h-5 w-5 text-amber-600 dark:text-amber-400" />}
          bgClass="bg-amber-100 dark:bg-amber-900"
          label="Pending"
          value={overview?.pending_tasks ?? "—"}
        />
        <StatCard
          icon={<Users className="h-5 w-5 text-green-600 dark:text-green-400" />}
          bgClass="bg-green-100 dark:bg-green-900"
          label="Active Teams"
          value={
            overview
              ? `${overview.busy_teams} / ${overview.active_teams}`
              : "—"
          }
        />
        <StatCard
          icon={<Bot className="h-5 w-5 text-purple-600 dark:text-purple-400" />}
          bgClass="bg-purple-100 dark:bg-purple-900"
          label="Online Agents"
          value={
            overview
              ? `${overview.busy_agents} / ${overview.online_agents}`
              : "—"
          }
        />
        <StatCard
          icon={<Activity className="h-5 w-5 text-teal-600 dark:text-teal-400" />}
          bgClass="bg-teal-100 dark:bg-teal-900"
          label="Avg Duration"
          value={
            overview
              ? overview.avg_duration_seconds < 60
                ? `${Math.round(overview.avg_duration_seconds)}s`
                : `${Math.round(overview.avg_duration_seconds / 60)}m`
              : "—"
          }
        />
        <StatCard
          icon={<Percent className="h-5 w-5 text-indigo-600 dark:text-indigo-400" />}
          bgClass="bg-indigo-100 dark:bg-indigo-900"
          label="LLM Utilisation"
          value={
            overview
              ? `${overview.llm_percentage.toFixed(0)}%`
              : "—"
          }
        />
      </div>

      {/* Secondary stats row */}
      <div className="grid gap-4 md:grid-cols-3">
        <div className="flex items-center gap-3 rounded-lg border bg-card p-4">
          <AlertTriangle className="h-5 w-5 text-red-500" />
          <div>
            <p className="text-sm text-muted-foreground">Failed (all time)</p>
            <p className="text-xl font-bold">{overview?.failed_all_time ?? "—"}</p>
          </div>
        </div>
        <div className="flex items-center gap-3 rounded-lg border bg-card p-4">
          <RefreshCw className="h-5 w-5 text-amber-500" />
          <div>
            <p className="text-sm text-muted-foreground">Total Retries</p>
            <p className="text-xl font-bold">{overview?.total_retries ?? "—"}</p>
          </div>
        </div>
        <div className="flex items-center gap-3 rounded-lg border bg-card p-4">
          <ListChecks className="h-5 w-5 text-green-500" />
          <div>
            <p className="text-sm text-muted-foreground">
              Completed (all time)
            </p>
            <p className="text-xl font-bold">
              {overview?.completed_all_time ?? "—"}
            </p>
          </div>
        </div>
      </div>

      {/* Tabs: Pipeline Board / History */}
      <div className="flex gap-2 border-b pb-2">
        <Button
          variant={activeTab === "pipeline" ? "default" : "ghost"}
          size="sm"
          onClick={() => setActiveTab("pipeline")}
        >
          <Kanban className="mr-2 h-4 w-4" />
          Pipeline Board
        </Button>
        <Button
          variant={activeTab === "history" ? "default" : "ghost"}
          size="sm"
          onClick={() => setActiveTab("history")}
        >
          <History className="mr-2 h-4 w-4" />
          Task History
        </Button>
      </div>

      {activeTab === "pipeline" && <TaskPipelineBoard />}
      {activeTab === "history" && <TaskHistory />}

      {/* Task List */}
      <div className="rounded-lg border bg-card">
        <div className="border-b px-6 py-4">
          <h3 className="text-lg font-semibold">Recent Tasks</h3>
        </div>
        <div className="divide-y">
          {tasks.length === 0 ? (
            <div className="px-6 py-12 text-center text-muted-foreground">
              <Bot className="mx-auto mb-3 h-10 w-10 opacity-50" />
              <p>No tasks yet. Submit a task to get started.</p>
            </div>
          ) : (
            tasks.map((task) => (
              <a
                key={task.id}
                href={`/swarm/tasks/${task.id}`}
                className="block px-6 py-4 transition-colors hover:bg-muted/50"
              >
                <div className="flex items-center justify-between">
                  <div className="space-y-1">
                    <p className="font-medium">{task.description}</p>
                    <p className="text-sm text-muted-foreground">
                      {task.repo_id} •{" "}
                      {new Date(task.created_at).toLocaleDateString()}
                    </p>
                  </div>
                  <span
                    className={`rounded-full px-2.5 py-0.5 text-xs font-medium ${statusColor(task.status)}`}
                  >
                    {task.status.replace(/_/g, " ")}
                  </span>
                </div>
              </a>
            ))
          )}
        </div>
      </div>
    </>
  );
}

// ── Stat Card ──────────────────────────────────────────────────────────────────

function StatCard({
  icon,
  bgClass,
  label,
  value,
}: {
  icon: React.ReactNode;
  bgClass: string;
  label: string;
  value: string | number;
}) {
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="flex items-center gap-3">
        <div className={`rounded-md p-2 ${bgClass}`}>{icon}</div>
        <div>
          <p className="text-xs text-muted-foreground">{label}</p>
          <p className="text-lg font-bold">{value}</p>
        </div>
      </div>
    </div>
  );
}
