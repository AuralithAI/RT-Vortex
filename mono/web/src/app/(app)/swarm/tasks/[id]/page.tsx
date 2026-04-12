// ─── Swarm Task Detail ───────────────────────────────────────────────────────
// Task detail page with a stunning multi-LLM orchestration UI. Shows a hero
// panel with real-time parallel model responses, LIVE badge, provider avatars,
// broadcast stats, plus the plan, agent chat, code changes, and consensus
// panels. Designed to look like a modern parallel-task execution interface.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import { useParams } from "next/navigation";
import {
  MessageSquare,
  Star,
  FileCode,
  ArrowLeft,
  ExternalLink,
  Eye,
  Activity,
  Brain,
  Hammer,
} from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { PlanReviewCard } from "@/components/swarm/plan-review-card";
import { DiffViewer } from "@/components/swarm/diff-viewer";
import { ActivityFeed } from "@/components/swarm/activity-feed";
import { LiveAgentChat } from "@/components/swarm/live-agent-chat";
import { TaskAgentList } from "@/components/swarm/task-agent-list";
import { OrchestrationHero } from "@/components/swarm/orchestration-hero";
import { ConsensusDecisionPanel } from "@/components/swarm/consensus-decision-panel";
import { InsightMemoryPanel } from "@/components/swarm/insight-memory-panel";
import { RoleELOLeaderboard } from "@/components/swarm/role-elo-leaderboard";
import { CISignalBadge } from "@/components/swarm/ci-signal-badge";
import { TeamFormationCard } from "@/components/swarm/team-formation-card";
import { BuildValidationCard } from "@/components/swarm/build-validation-card";
import { useSwarmEvents } from "@/hooks/use-swarm-events";
import { useDiscussionEvents } from "@/hooks/use-discussion-events";
import type { SwarmTask, SwarmDiff, PlanDocument } from "@/types/swarm";
import type { LLMProvider } from "@/types/api";

// ── Tab types for the main content area ─────────────────────────────────────
type ContentTab = "reasoning" | "conversation" | "diffs" | "builds";

// ── Derive agent label from the task's agents ───────────────────────────────
function deriveAgentLabel(task: SwarmTask | null): string {
  if (!task) return "AI Agent";
  const agentCount = (task.assigned_agents ?? []).length;
  if (agentCount === 1) return "Senior Dev Agent";
  if (agentCount > 1) return `Agent Swarm (${agentCount})`;
  return "Senior Dev Agent";
}

// ── Fetch configured LLM providers ──────────────────────────────────────────
function useActiveProviders() {
  const [providers, setProviders] = useState<string[]>([]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch("/api/v1/llm/providers");
        if (!res.ok) return;
        const data = await res.json();
        const list: LLMProvider[] = data.providers ?? [];
        if (!cancelled) {
          setProviders(
            list.filter((p) => p.configured && p.healthy).map((p) => p.name),
          );
        }
      } catch {
        // non-critical
      }
    })();
    return () => { cancelled = true; };
  }, []);

  return providers;
}

// ─────────────────────────────────────────────────────────────────────────────

export default function SwarmTaskDetailPage() {
  const params = useParams<{ id: string }>();
  const [task, setTask] = useState<SwarmTask | null>(null);
  const [diffs, setDiffs] = useState<SwarmDiff[]>([]);
  const [loading, setLoading] = useState(true);
  const [rating, setRating] = useState(0);
  const [comment, setComment] = useState("");
  const [activeTab, setActiveTab] = useState<ContentTab>("reasoning");

  const { events, connected } = useSwarmEvents(params.id);
  const { threads, consensusResults } = useDiscussionEvents(events);
  const activeProviders = useActiveProviders();

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
        title={task.title || task.description}
        description={`${task.repo_id} • ${task.status.replace(/_/g, " ")}`}
        actions={
          <div className="flex items-center gap-3">
            {/* Multi-LLM status indicator */}
            {threads.length > 0 && (
              <span className="flex items-center gap-1.5 rounded-full bg-violet-100 px-3 py-1 text-[11px] font-medium text-violet-700 dark:bg-violet-900/40 dark:text-violet-300">
                <Brain className="h-3.5 w-3.5" />
                {threads.length} LLM discussion{threads.length !== 1 ? "s" : ""}
                {threads.some((t) => t.status === "open") && (
                  <Activity className="h-3 w-3 animate-pulse text-yellow-500" />
                )}
              </span>
            )}
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
                  <dd>{(task.assigned_agents ?? []).length}</dd>
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
                {task.pr_url && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">CI Status</dt>
                    <dd>
                      <CISignalBadge taskId={params.id} compact />
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

          {/* Dynamic Team Formation — complexity analysis + ELO-aware team composition */}
          <TeamFormationCard taskId={params.id} refreshInterval={15000} />

          {/* ── Tabbed Content Area ─────────────────────────────────── */}
          <div className="space-y-4">
            {/* Tab bar */}
            <div className="flex items-center gap-1 rounded-xl border bg-muted/30 p-1">
              <button
                onClick={() => setActiveTab("reasoning")}
                className={`flex items-center gap-2 rounded-lg px-4 py-2.5 text-sm font-medium transition-all ${
                  activeTab === "reasoning"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <Eye className="h-4 w-4" />
                AI Reasoning
                {(threads.length > 0 || consensusResults.length > 0) && (
                  <span className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${
                    activeTab === "reasoning"
                      ? "bg-violet-100 text-violet-700 dark:bg-violet-900/40 dark:text-violet-300"
                      : "bg-muted text-muted-foreground"
                  }`}>
                    {threads.length + consensusResults.length}
                  </span>
                )}
                {threads.some((t) => t.status === "open") && (
                  <span className="h-2 w-2 rounded-full bg-yellow-500 animate-pulse" />
                )}
              </button>
              <button
                onClick={() => setActiveTab("conversation")}
                className={`flex items-center gap-2 rounded-lg px-4 py-2.5 text-sm font-medium transition-all ${
                  activeTab === "conversation"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <MessageSquare className="h-4 w-4" />
                Agent Chat
                {events.filter((e) => e.type === "swarm_agent").length > 0 && (
                  <span className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${
                    activeTab === "conversation"
                      ? "bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300"
                      : "bg-muted text-muted-foreground"
                  }`}>
                    {events.filter((e) => e.type === "swarm_agent").length}
                  </span>
                )}
              </button>
              <button
                onClick={() => setActiveTab("diffs")}
                className={`flex items-center gap-2 rounded-lg px-4 py-2.5 text-sm font-medium transition-all ${
                  activeTab === "diffs"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <FileCode className="h-4 w-4" />
                Code Changes
                {diffs.length > 0 && (
                  <span className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${
                    activeTab === "diffs"
                      ? "bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300"
                      : "bg-muted text-muted-foreground"
                  }`}>
                    {diffs.length}
                  </span>
                )}
              </button>
              <button
                onClick={() => setActiveTab("builds")}
                className={`flex items-center gap-2 rounded-lg px-4 py-2.5 text-sm font-medium transition-all ${
                  activeTab === "builds"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <Hammer className="h-4 w-4" />
                Builds
              </button>
            </div>

            {/* Tab content */}
            <div>
              {/* ── AI Reasoning tab ─────────────────────────────────── */}
              {activeTab === "reasoning" && (
                <div className="space-y-6">
                  {/* ★ Orchestration Hero — stunning multi-LLM parallel execution UI ★ */}
                  <OrchestrationHero
                    agentLabel={deriveAgentLabel(task)}
                    connected={connected}
                    threads={threads}
                    consensusResults={consensusResults}
                    activeProviders={activeProviders}
                  />

                  {/* Consensus Decisions — detailed judge verdicts & score comparison */}
                  <ConsensusDecisionPanel results={consensusResults} />

                  {/* Cross-Task Insights — learned from past consensus decisions */}
                  <InsightMemoryPanel taskId={params.id} />

                  {/* CI Signal Status — auto-ingested PR merge + CI check status */}
                  {task.pr_url && <CISignalBadge taskId={params.id} />}

                  {/* Role ELO Leaderboard — scoped to this task's repo */}
                  <RoleELOLeaderboard repoId={task.repo_id} />
                </div>
              )}

              {/* ── Agent Chat tab ───────────────────────────────────── */}
              {activeTab === "conversation" && (
                <LiveAgentChat events={events} />
              )}

              {/* ── Code Changes tab ─────────────────────────────────── */}
              {activeTab === "diffs" && (
                <div className="space-y-4">
                  {diffs.length > 0 ? (
                    <>
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
                    </>
                  ) : (
                    <div className="flex flex-col items-center justify-center rounded-xl border border-dashed py-16">
                      <FileCode className="mb-3 h-10 w-10 text-muted-foreground/40" />
                      <p className="text-sm font-semibold text-muted-foreground">
                        No code changes yet
                      </p>
                      <p className="mt-1 text-xs text-muted-foreground/70">
                        Diffs will appear here as agents make changes
                      </p>
                    </div>
                  )}
                </div>
              )}

              {activeTab === "builds" && (
                <BuildValidationCard
                  taskId={params.id}
                  refreshInterval={10000}
                />
              )}
            </div>
          </div>
        </div>

        {/* Sidebar */}
        <div className="space-y-4">
          <TaskAgentList taskId={params.id} />
          <ActivityFeed events={events} />
        </div>
      </div>
    </div>
  );
}
