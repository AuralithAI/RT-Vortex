// ─── Swarm Summary Card ──────────────────────────────────────────────────────
// Compact swarm overview for the main dashboard. Fetches /api/v1/swarm/overview
// and shows active tasks, agents, teams, and recent completions with a link
// to the full swarm dashboard.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import {
  Bot,
  ListChecks,
  Users,
  CheckCircle2,
  AlertTriangle,
  Clock,
  ArrowRight,
} from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import type { SwarmOverview, SwarmTask } from "@/types/swarm";

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  return `${(seconds / 3600).toFixed(1)}h`;
}

export function SwarmSummaryCard() {
  const [overview, setOverview] = useState<SwarmOverview | null>(null);
  const [recentTasks, setRecentTasks] = useState<SwarmTask[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchData = useCallback(async () => {
    try {
      const [ovRes, taskRes] = await Promise.all([
        fetch("/api/v1/swarm/overview"),
        fetch("/api/v1/swarm/tasks?limit=3"),
      ]);
      if (ovRes.ok) setOverview(await ovRes.json());
      if (taskRes.ok) {
        const data = await taskRes.json();
        // API may return { tasks: [...] } or [...] depending on endpoint
        setRecentTasks(Array.isArray(data) ? data : data.tasks ?? data.data ?? []);
      }
    } catch {
      /* will retry */
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
    const iv = setInterval(fetchData, 15_000);
    return () => clearInterval(iv);
  }, [fetchData]);

  const statusBadge = (status: string) => {
    const map: Record<string, "default" | "secondary" | "destructive" | "success" | "warning" | "outline"> = {
      submitted: "secondary",
      planning: "warning",
      plan_review: "warning",
      implementing: "warning",
      diff_review: "warning",
      completed: "success",
      failed: "destructive",
      timed_out: "destructive",
      cancelled: "outline",
    };
    return map[status] ?? "outline";
  };

  if (loading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Agent Swarm</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-8">
            <Bot className="h-6 w-6 animate-pulse text-muted-foreground" />
          </div>
        </CardContent>
      </Card>
    );
  }

  const hasActivity =
    overview &&
    (overview.active_tasks > 0 ||
      overview.completed_all_time > 0 ||
      overview.online_agents > 0);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="text-base">Agent Swarm</CardTitle>
        <Link
          href="/dashboard/swarm"
          className="text-xs font-medium text-primary hover:underline"
        >
          Open Swarm →
        </Link>
      </CardHeader>
      <CardContent>
        {!hasActivity ? (
          <div className="space-y-3 py-2">
            <p className="text-sm text-muted-foreground">
              No swarm activity yet. Submit a task to get agents working.
            </p>
            <Link
              href="/dashboard/swarm"
              className="inline-flex items-center gap-1.5 text-sm font-medium text-primary hover:underline"
            >
              <Bot className="h-4 w-4" />
              Go to Swarm Dashboard
              <ArrowRight className="h-3 w-3" />
            </Link>
          </div>
        ) : (
          <div className="space-y-4">
            {/* Mini stats row */}
            <div className="grid grid-cols-4 gap-3">
              <MiniStat
                icon={ListChecks}
                label="Active"
                value={overview!.active_tasks}
                color="text-blue-600 dark:text-blue-400"
              />
              <MiniStat
                icon={CheckCircle2}
                label="Done"
                value={overview!.completed_all_time}
                color="text-green-600 dark:text-green-400"
              />
              <MiniStat
                icon={Bot}
                label="Agents"
                value={overview!.online_agents}
                color="text-purple-600 dark:text-purple-400"
              />
              <MiniStat
                icon={Users}
                label="Teams"
                value={overview!.active_teams}
                color="text-amber-600 dark:text-amber-400"
              />
            </div>

            {/* Failures / Avg duration row */}
            {(overview!.failed_all_time > 0 ||
              overview!.avg_duration_seconds > 0) && (
              <div className="flex items-center gap-4 text-xs text-muted-foreground">
                {overview!.failed_all_time > 0 && (
                  <span className="flex items-center gap-1">
                    <AlertTriangle className="h-3 w-3 text-red-500" />
                    {overview!.failed_all_time} failed
                  </span>
                )}
                {overview!.avg_duration_seconds > 0 && (
                  <span className="flex items-center gap-1">
                    <Clock className="h-3 w-3" />
                    Avg {formatDuration(overview!.avg_duration_seconds)}
                  </span>
                )}
              </div>
            )}

            {/* Recent tasks */}
            {recentTasks.length > 0 && (
              <div className="space-y-2">
                <p className="text-xs font-medium text-muted-foreground">
                  Recent Tasks
                </p>
                {recentTasks.map((task) => (
                  <Link
                    key={task.id}
                    href={`/swarm/tasks/${task.id}`}
                    className="flex items-center gap-2 rounded-md p-1.5 text-sm transition-colors hover:bg-muted/50"
                  >
                    <span className="min-w-0 flex-1 truncate">
                      {task.description}
                    </span>
                    <Badge
                      variant={statusBadge(task.status)}
                      className="shrink-0 text-[10px]"
                    >
                      {task.status.replace(/_/g, " ")}
                    </Badge>
                  </Link>
                ))}
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ── Mini stat pill ───────────────────────────────────────────────────────────

function MiniStat({
  icon: Icon,
  label,
  value,
  color,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: number;
  color: string;
}) {
  return (
    <div className="flex flex-col items-center gap-0.5 text-center">
      <Icon className={`h-4 w-4 ${color}`} />
      <span className="text-lg font-semibold leading-none">{value}</span>
      <span className="text-[10px] text-muted-foreground">{label}</span>
    </div>
  );
}
