// ─── Swarm Dashboard ─────────────────────────────────────────────────────────
// Overview page: task submission form, task list, team grid, agent leaderboard.
// Phase 0 stub — will be expanded with live WebSocket data in Phase 1.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import { Bot, ListChecks, Users, Plus, Send, Loader2 } from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import type { SwarmTask, TaskSubmission } from "@/types/swarm";

export default function SwarmDashboardPage() {
  const [tasks, setTasks] = useState<SwarmTask[]>([]);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [repoId, setRepoId] = useState("");
  const [description, setDescription] = useState("");
  const [showForm, setShowForm] = useState(false);

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

      {/* Stats Overview */}
      <div className="grid gap-4 md:grid-cols-3">
        <div className="rounded-lg border bg-card p-6">
          <div className="flex items-center gap-3">
            <div className="rounded-md bg-blue-100 p-2 dark:bg-blue-900">
              <ListChecks className="h-5 w-5 text-blue-600 dark:text-blue-400" />
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Total Tasks</p>
              <p className="text-2xl font-bold">{tasks.length}</p>
            </div>
          </div>
        </div>
        <div className="rounded-lg border bg-card p-6">
          <div className="flex items-center gap-3">
            <div className="rounded-md bg-green-100 p-2 dark:bg-green-900">
              <Users className="h-5 w-5 text-green-600 dark:text-green-400" />
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Active Teams</p>
              <p className="text-2xl font-bold">0</p>
            </div>
          </div>
        </div>
        <div className="rounded-lg border bg-card p-6">
          <div className="flex items-center gap-3">
            <div className="rounded-md bg-purple-100 p-2 dark:bg-purple-900">
              <Bot className="h-5 w-5 text-purple-600 dark:text-purple-400" />
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Active Agents</p>
              <p className="text-2xl font-bold">0</p>
            </div>
          </div>
        </div>
      </div>

      {/* Task List */}
      <div className="rounded-lg border bg-card">
        <div className="border-b px-6 py-4">
          <h3 className="text-lg font-semibold">Tasks</h3>
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
