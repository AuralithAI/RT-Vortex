// ─── Pull Request List Component ─────────────────────────────────────────────
// Displays tracked PRs for a repository with filters, sync, and one-click review.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useCallback } from "react";
import Link from "next/link";
import {
  GitPullRequest,
  GitMerge,
  RefreshCw,
  Play,
  ExternalLink,
  ChevronLeft,
  ChevronRight,
  Filter,
  CheckCircle2,
  Clock,
  AlertCircle,
  FileCode,
  Plus,
  Minus,
  Loader2,
  Zap,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { usePullRequests, usePullRequestStats } from "@/lib/api/queries";
import { useSyncPullRequests, useReviewPullRequest } from "@/lib/api/mutations";
import { usePREmbedProgress } from "@/hooks/use-pr-embed-progress";
import { formatETA } from "@/hooks/use-index-progress";
import { useUIStore } from "@/lib/stores/ui";
import { timeAgo } from "@/lib/utils";
import type { PRSyncStatus, PRReviewStatus, PRListFilter, TrackedPullRequest, PREmbedProgressEvent } from "@/types/api";

const PAGE_SIZE = 20;

// ── Status Badge helpers ────────────────────────────────────────────────────

function syncStatusBadge(status: PRSyncStatus) {
  const map: Record<PRSyncStatus, { label: string; variant: "default" | "secondary" | "success" | "warning" | "destructive" | "outline" }> = {
    open: { label: "Open", variant: "success" },
    closed: { label: "Closed", variant: "secondary" },
    merged: { label: "Merged", variant: "default" },
    draft: { label: "Draft", variant: "outline" },
    stale: { label: "Stale", variant: "secondary" },
    embedded: { label: "Embedded", variant: "success" },
    embedding: { label: "Embedding…", variant: "warning" },
    embed_error: { label: "Embed Error", variant: "destructive" },
  };
  const m = map[status] ?? { label: status, variant: "secondary" as const };
  return <Badge variant={m.variant}>{m.label}</Badge>;
}

function reviewStatusBadge(status: PRReviewStatus) {
  const map: Record<PRReviewStatus, { icon: React.ReactNode; label: string; className: string }> = {
    none: {
      icon: null,
      label: "Not Reviewed",
      className: "text-muted-foreground",
    },
    pending: {
      icon: <Loader2 className="h-3 w-3 animate-spin" />,
      label: "Reviewing…",
      className: "text-yellow-600 dark:text-yellow-400",
    },
    completed: {
      icon: <CheckCircle2 className="h-3 w-3" />,
      label: "Reviewed",
      className: "text-green-600 dark:text-green-400",
    },
    skipped: {
      icon: <AlertCircle className="h-3 w-3" />,
      label: "Skipped",
      className: "text-muted-foreground",
    },
  };
  const m = map[status] ?? { icon: null, label: status, className: "text-muted-foreground" };
  return (
    <span className={`inline-flex items-center gap-1 text-xs font-medium ${m.className}`}>
      {m.icon}
      {m.label}
    </span>
  );
}

// ── Stats Bar ───────────────────────────────────────────────────────────────

function PRStatsBar({ repoId }: { repoId: string }) {
  const { data: stats } = usePullRequestStats(repoId);
  if (!stats?.counts) return null;

  const c = stats.counts;
  const items = [
    { label: "Open", value: c["open"] ?? 0, color: "bg-green-500" },
    { label: "Embedding", value: c["embedding"] ?? 0, color: "bg-yellow-500" },
    { label: "Embedded", value: c["embedded"] ?? 0, color: "bg-blue-500" },
    { label: "Draft", value: c["draft"] ?? 0, color: "bg-gray-400" },
    { label: "Merged", value: c["merged"] ?? 0, color: "bg-purple-500" },
    { label: "Closed", value: c["closed"] ?? 0, color: "bg-red-400" },
    { label: "Embed Error", value: c["embed_error"] ?? 0, color: "bg-red-600" },
  ];

  const total = items.reduce((s, i) => s + i.value, 0);
  const embedQueue = stats.embed_queue ?? 0;

  return (
    <div className="flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
      {items.filter((i) => i.value > 0).map((item) => (
        <span key={item.label} className="flex items-center gap-1">
          <span className={`inline-block h-2 w-2 rounded-full ${item.color}`} />
          {item.value} {item.label}
        </span>
      ))}
      {total > 0 && (
        <span className="font-medium text-foreground">{total} total</span>
      )}
      {embedQueue > 0 && (
        <span className="flex items-center gap-1 rounded-full bg-amber-100 px-2 py-0.5 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
          <Loader2 className="h-3 w-3 animate-spin" />
          {embedQueue} in embed queue
        </span>
      )}
    </div>
  );
}

// ── Embed Activity Banner ───────────────────────────────────────────────────

function EmbedActivityBanner({ events }: { events: Map<number, PREmbedProgressEvent> }) {
  // Only show active (non-terminal) events
  const activeEvents = Array.from(events.values()).filter(
    (e) => e.state === "embedding",
  );

  if (activeEvents.length === 0) return null;

  // Show aggregate progress
  const avgProgress =
    activeEvents.reduce((sum, e) => sum + e.progress, 0) / activeEvents.length;

  return (
    <div className="mx-6 mb-3 rounded-lg border border-amber-200 bg-amber-50 p-3 dark:border-amber-800 dark:bg-amber-950/30">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-sm font-medium text-amber-700 dark:text-amber-400">
          <Loader2 className="h-4 w-4 animate-spin" />
          Embedding in progress — {activeEvents.length} PR{activeEvents.length > 1 ? "s" : ""}
        </div>
        <span className="text-xs text-amber-600 dark:text-amber-500">
          {Math.round(avgProgress)}%
        </span>
      </div>
      <Progress value={avgProgress} className="mt-2 h-1.5" />
      <div className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1 text-[10px] text-amber-600 dark:text-amber-500">
        {activeEvents.map((e) => (
          <span key={e.pr_number} className="flex items-center gap-1">
            <span className="font-mono">#{e.pr_number}</span>
            <span>{e.phase}</span>
            {e.current_file && <span className="truncate max-w-[120px]">— {e.current_file}</span>}
          </span>
        ))}
      </div>
    </div>
  );
}

// ── Embed Progress Inline ───────────────────────────────────────────────────

function EmbedProgressBar({ event }: { event: PREmbedProgressEvent }) {
  const isFailed = event.state === "failed";
  const isDone = event.state === "completed";

  return (
    <div className="mt-2 space-y-1">
      <div className="flex items-center justify-between text-xs">
        <span className={`font-medium ${isFailed ? "text-red-500" : isDone ? "text-green-600" : "text-blue-600"}`}>
          {isFailed ? "Embed failed" : isDone ? "Embedding complete" : event.phase}
        </span>
        <span className="text-muted-foreground">
          {isFailed
            ? event.error
            : isDone
              ? "✓"
              : `${Math.round(event.progress)}%`}
        </span>
      </div>
      {!isFailed && (
        <Progress
          value={event.progress}
          className="h-1.5"
        />
      )}
      {!isDone && !isFailed && (
        <div className="flex items-center justify-between text-[10px] text-muted-foreground">
          <span>
            {event.files_total > 0
              ? `${event.files_processed}/${event.files_total} files`
              : event.current_file ?? ""}
          </span>
          {event.eta_seconds > 0 && (
            <span>{formatETA(event.eta_seconds)}</span>
          )}
        </div>
      )}
    </div>
  );
}

// ── PR Row ──────────────────────────────────────────────────────────────────

function PRRow({
  pr,
  repoId,
  onReview,
  isReviewing,
  embedProgress,
}: {
  pr: TrackedPullRequest;
  repoId: string;
  onReview: (prId: string) => void;
  isReviewing: boolean;
  embedProgress?: PREmbedProgressEvent;
}) {
  const canReview = pr.review_status !== "pending" && pr.review_status !== "completed";
  const isEmbedded = pr.sync_status === "embedded";

  return (
    <div className="flex items-center gap-4 rounded-lg border p-3 transition-colors hover:bg-muted/40">
      {/* PR Icon */}
      <div className="shrink-0">
        {pr.sync_status === "merged" ? (
          <GitMerge className="h-5 w-5 text-purple-500" />
        ) : pr.sync_status === "closed" ? (
          <GitPullRequest className="h-5 w-5 text-red-400" />
        ) : (
          <GitPullRequest className="h-5 w-5 text-green-500" />
        )}
      </div>

      {/* Title + Meta */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{pr.title}</span>
          {syncStatusBadge(pr.sync_status)}
          {isEmbedded && (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger>
                  <Zap className="h-3.5 w-3.5 text-amber-500" />
                </TooltipTrigger>
                <TooltipContent>
                  <p className="text-xs">Pre-embedded by engine — faster review</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
          <span className="font-mono">#{pr.pr_number}</span>
          <span>{pr.author}</span>
          <span>
            {pr.source_branch} → {pr.target_branch}
          </span>
          <span className="inline-flex items-center gap-0.5">
            <FileCode className="h-3 w-3" />
            {pr.files_changed}
          </span>
          <span className="inline-flex items-center gap-0.5 text-green-600">
            <Plus className="h-3 w-3" />
            {pr.additions}
          </span>
          <span className="inline-flex items-center gap-0.5 text-red-500">
            <Minus className="h-3 w-3" />
            {pr.deletions}
          </span>
          <span>{timeAgo(pr.synced_at)}</span>
        </div>
        {/* Embed progress bar (shown when engine is embedding this PR) */}
        {embedProgress && <EmbedProgressBar event={embedProgress} />}
      </div>

      {/* Review Status */}
      <div className="shrink-0">{reviewStatusBadge(pr.review_status)}</div>

      {/* Actions */}
      <div className="flex shrink-0 items-center gap-1">
        {canReview && (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="sm"
                  variant={isEmbedded ? "default" : "outline"}
                  disabled={isReviewing}
                  onClick={(e) => {
                    e.preventDefault();
                    onReview(pr.id);
                  }}
                  className="gap-1"
                >
                  {isReviewing ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Play className="h-3.5 w-3.5" />
                  )}
                  Review
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p className="text-xs">
                  {isEmbedded
                    ? "Engine context available — minimal LLM usage"
                    : "Review using LLM analysis"}
                </p>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}

        {pr.last_review_id && (
          <Button size="sm" variant="ghost" asChild>
            <Link href={`/reviews/${pr.last_review_id}`}>
              View
            </Link>
          </Button>
        )}

        {pr.pr_url && (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button size="sm" variant="ghost" asChild>
                  <a
                    href={pr.pr_url}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <ExternalLink className="h-3.5 w-3.5" />
                  </a>
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p className="text-xs">Open on {pr.platform}</p>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
      </div>
    </div>
  );
}

// ── Main Component ──────────────────────────────────────────────────────────

export function PullRequestList({ repoId }: { repoId: string }) {
  const { addToast } = useUIStore();
  const [page, setPage] = useState(0);
  const [filter, setFilter] = useState<PRListFilter>({});
  const [reviewingId, setReviewingId] = useState<string | null>(null);

  const { data, isLoading, isFetching } = usePullRequests(
    repoId,
    { limit: PAGE_SIZE, offset: page * PAGE_SIZE },
    filter,
  );

  // Real-time embedding progress via WebSocket
  const embedProgress = usePREmbedProgress(repoId);

  const syncMutation = useSyncPullRequests();
  const reviewMutation = useReviewPullRequest();

  const handleSync = useCallback(() => {
    syncMutation.mutate(repoId, {
      onSuccess: () => {
        addToast({ title: "PR sync triggered", variant: "success" });
      },
      onError: () => {
        addToast({ title: "Failed to trigger PR sync", variant: "error" });
      },
    });
  }, [repoId, syncMutation, addToast]);

  const handleReview = useCallback(
    (prId: string) => {
      setReviewingId(prId);
      reviewMutation.mutate(
        { repoId, prId },
        {
          onSuccess: () => {
            addToast({ title: "Review queued", description: "The review pipeline has started.", variant: "success" });
            setReviewingId(null);
          },
          onError: () => {
            addToast({ title: "Failed to start review", variant: "error" });
            setReviewingId(null);
          },
        },
      );
    },
    [repoId, reviewMutation, addToast],
  );

  const prs = data?.data ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.ceil(total / PAGE_SIZE);

  return (
    <Card>
      <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-1">
          <CardTitle className="flex items-center gap-2 text-base">
            <GitPullRequest className="h-4 w-4" />
            Pull Requests
          </CardTitle>
          <PRStatsBar repoId={repoId} />
        </div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={handleSync}
            disabled={syncMutation.isPending}
          >
            {syncMutation.isPending ? (
              <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCw className="mr-1 h-3.5 w-3.5" />
            )}
            Sync
          </Button>
        </div>
      </CardHeader>

      {/* Filters */}
      <div className="border-b px-6 pb-3">
        <div className="flex flex-wrap items-center gap-2">
          <Filter className="h-3.5 w-3.5 text-muted-foreground" />
          <Select
            value={filter.sync_status ?? "__all__"}
            onValueChange={(v) => {
              setFilter((f) => ({
                ...f,
                sync_status: v === "__all__" ? undefined : (v as PRSyncStatus),
              }));
              setPage(0);
            }}
          >
            <SelectTrigger className="h-8 w-[130px] text-xs">
              <SelectValue placeholder="Sync Status" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">All Status</SelectItem>
              <SelectItem value="open">Open</SelectItem>
              <SelectItem value="draft">Draft</SelectItem>
              <SelectItem value="merged">Merged</SelectItem>
              <SelectItem value="closed">Closed</SelectItem>
              <SelectItem value="embedded">Embedded</SelectItem>
              <SelectItem value="embedding">Embedding</SelectItem>
              <SelectItem value="embed_error">Embed Error</SelectItem>
              <SelectItem value="stale">Stale</SelectItem>
            </SelectContent>
          </Select>

          <Select
            value={filter.review_status ?? "__all__"}
            onValueChange={(v) => {
              setFilter((f) => ({
                ...f,
                review_status: v === "__all__" ? undefined : (v as PRReviewStatus),
              }));
              setPage(0);
            }}
          >
            <SelectTrigger className="h-8 w-[140px] text-xs">
              <SelectValue placeholder="Review Status" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">All Reviews</SelectItem>
              <SelectItem value="none">Not Reviewed</SelectItem>
              <SelectItem value="pending">Pending</SelectItem>
              <SelectItem value="completed">Completed</SelectItem>
              <SelectItem value="skipped">Skipped</SelectItem>
            </SelectContent>
          </Select>

          {(filter.sync_status || filter.review_status) && (
            <Button
              size="sm"
              variant="ghost"
              className="h-8 text-xs"
              onClick={() => {
                setFilter({});
                setPage(0);
              }}
            >
              Clear
            </Button>
          )}
        </div>
      </div>

      {/* Embedding activity banner (live from WebSocket) */}
      <EmbedActivityBanner events={embedProgress.events} />

      <CardContent className="pt-3">
        {/* Loading skeleton */}
        {isLoading && (
          <div className="space-y-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-16" />
            ))}
          </div>
        )}

        {/* Empty state */}
        {!isLoading && prs.length === 0 && (
          <div className="flex flex-col items-center gap-3 py-12 text-center">
            <GitPullRequest className="h-10 w-10 text-muted-foreground/40" />
            <div>
              <p className="text-sm font-medium">No pull requests found</p>
              <p className="text-xs text-muted-foreground">
                {filter.sync_status || filter.review_status
                  ? "Try adjusting your filters or sync to discover new PRs."
                  : "Click Sync to discover pull requests from your connected VCS."}
              </p>
            </div>
            <Button size="sm" variant="outline" onClick={handleSync} disabled={syncMutation.isPending}>
              <RefreshCw className="mr-1 h-3.5 w-3.5" />
              Sync Now
            </Button>
          </div>
        )}

        {/* PR list */}
        {!isLoading && prs.length > 0 && (
          <div className="space-y-2">
            {prs.map((pr) => (
              <PRRow
                key={pr.id}
                pr={pr}
                repoId={repoId}
                onReview={handleReview}
                isReviewing={reviewingId === pr.id}
                embedProgress={embedProgress.events.get(pr.pr_number)}
              />
            ))}
          </div>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="mt-4 flex items-center justify-between text-xs text-muted-foreground">
            <span>
              Showing {page * PAGE_SIZE + 1}–{Math.min((page + 1) * PAGE_SIZE, total)} of {total}
            </span>
            <div className="flex items-center gap-1">
              <Button
                size="sm"
                variant="ghost"
                disabled={page === 0}
                onClick={() => setPage((p) => p - 1)}
              >
                <ChevronLeft className="h-4 w-4" />
              </Button>
              <span className="px-2">
                {page + 1} / {totalPages}
              </span>
              <Button
                size="sm"
                variant="ghost"
                disabled={page >= totalPages - 1}
                onClick={() => setPage((p) => p + 1)}
              >
                <ChevronRight className="h-4 w-4" />
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
