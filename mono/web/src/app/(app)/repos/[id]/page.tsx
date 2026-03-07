// ─── Repository Detail Page ──────────────────────────────────────────────────
// Shows repo info, indexing status, and recent reviews for the repo.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { use } from "react";
import Link from "next/link";
import { ArrowLeft, RefreshCw, Trash2, FolderGit2, CheckCircle2, XCircle, Loader2, Clock } from "lucide-react";
import { useRepo, useIndexStatus, useReviews } from "@/lib/api/queries";
import { useTriggerIndex, useDeleteRepo } from "@/lib/api/mutations";
import { useIndexProgress, formatETA } from "@/hooks/use-index-progress";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { useUIStore } from "@/lib/stores/ui";
import { useRouter } from "next/navigation";
import { formatDate, timeAgo } from "@/lib/utils";

export default function RepoDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const router = useRouter();
  const { data: repo, isLoading } = useRepo(id);
  const { data: indexStatus } = useIndexStatus(id);
  const { data: reviews } = useReviews({ limit: 5, offset: 0, repo_id: id });
  const triggerIndex = useTriggerIndex();
  const deleteRepo = useDeleteRepo();
  const { showConfirm, addToast } = useUIStore();

  // Real-time indexing progress via WebSocket
  const isIndexing = indexStatus?.status === "indexing";
  const { event: wsEvent, connected: wsConnected } = useIndexProgress(id, {
    enabled: isIndexing,
  });

  // Merge WS event into display state (WS is more current than polling)
  const displayProgress = wsEvent?.progress ?? indexStatus?.progress ?? 0;
  const displayPhase = wsEvent?.phase ?? indexStatus?.phase ?? "";
  const displayFilesProcessed = wsEvent?.files_processed ?? indexStatus?.files_processed ?? 0;
  const displayFilesTotal = wsEvent?.files_total ?? indexStatus?.files_total ?? 0;
  const displayCurrentFile = wsEvent?.current_file ?? indexStatus?.current_file ?? "";
  const displayETA = wsEvent?.eta_seconds ?? indexStatus?.eta_seconds ?? -1;
  const displayState = wsEvent?.state ?? indexStatus?.status ?? "idle";
  const displayError = wsEvent?.error ?? indexStatus?.error ?? null;

  const handleDelete = () => {
    showConfirm(
      "Delete Repository",
      `Are you sure you want to remove "${repo?.name}"? This action cannot be undone.`,
      async () => {
        try {
          await deleteRepo.mutateAsync(id);
          addToast({ title: "Repository deleted", variant: "success" });
          router.push("/repos");
        } catch {
          addToast({ title: "Failed to delete repository", variant: "error" });
        }
      },
    );
  };

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-[200px]" />
      </div>
    );
  }

  if (!repo) {
    return (
      <div className="flex flex-col items-center gap-4 py-20">
        <p className="text-lg font-medium">Repository not found</p>
        <Button variant="outline" asChild>
          <Link href="/repos">Back to Repositories</Link>
        </Button>
      </div>
    );
  }

  return (
    <>
      <PageHeader
        title={repo.full_name || repo.name}
        description={`Platform: ${repo.platform} · Branch: ${repo.default_branch}`}
        actions={
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" asChild>
              <Link href="/repos">
                <ArrowLeft className="mr-1 h-4 w-4" />
                Back
              </Link>
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => triggerIndex.mutate(id)}
              disabled={triggerIndex.isPending}
            >
              <RefreshCw className="mr-1 h-4 w-4" />
              Re-index
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={handleDelete}
            >
              <Trash2 className="mr-1 h-4 w-4" />
              Delete
            </Button>
          </div>
        }
      />

      <div className="grid gap-4 md:grid-cols-2">
        {/* Info */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <FolderGit2 className="h-4 w-4" />
              Repository Info
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Created</span>
              <span>{formatDate(repo.created_at)}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Indexed</span>
              <Badge variant={repo.is_indexed ? "success" : "secondary"}>
                {repo.is_indexed ? "Yes" : "No"}
              </Badge>
            </div>
            {repo.last_indexed_at && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Last Indexed</span>
                <span>{timeAgo(repo.last_indexed_at)}</span>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Indexing Status */}
        {indexStatus && (
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Indexing Status</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="flex items-center gap-2">
                {displayState === "completed" && (
                  <CheckCircle2 className="h-4 w-4 text-green-500" />
                )}
                {displayState === "failed" && (
                  <XCircle className="h-4 w-4 text-red-500" />
                )}
                {(displayState === "running" || displayState === "indexing") && (
                  <Loader2 className="h-4 w-4 animate-spin text-yellow-500" />
                )}
                {(displayState === "pending" || displayState === "queued") && (
                  <Clock className="h-4 w-4 text-muted-foreground" />
                )}
                <Badge
                  variant={
                    displayState === "completed"
                      ? "success"
                      : displayState === "failed"
                        ? "destructive"
                        : displayState === "running" || displayState === "indexing"
                          ? "warning"
                          : "secondary"
                  }
                >
                  {displayState === "running" || displayState === "indexing"
                    ? `Indexing — ${displayPhase}`
                    : displayState}
                </Badge>
                {wsConnected && isIndexing && (
                  <span className="text-[10px] text-green-500">● live</span>
                )}
              </div>

              {/* Progress bar — shown during active indexing */}
              {(displayState === "running" || displayState === "indexing") && (
                <>
                  <Progress value={displayProgress} className="h-2" />
                  <div className="flex justify-between text-xs text-muted-foreground">
                    <span>
                      {displayFilesProcessed} / {displayFilesTotal || "?"} files
                    </span>
                    <span className="font-medium">{displayProgress}%</span>
                  </div>
                  {displayCurrentFile && (
                    <p className="truncate text-xs text-muted-foreground" title={displayCurrentFile}>
                      📄 {displayCurrentFile}
                    </p>
                  )}
                  {displayETA !== undefined && (
                    <p className="text-xs text-muted-foreground">
                      ⏱ {formatETA(displayETA)}
                    </p>
                  )}
                </>
              )}

              {/* Queued state */}
              {(displayState === "pending" || displayState === "queued") && (
                <p className="text-xs text-muted-foreground">
                  Waiting for available indexing slot...
                </p>
              )}

              {/* Completed state */}
              {displayState === "completed" && (
                <p className="text-xs text-green-600 font-medium">
                  ✓ Indexing complete
                </p>
              )}

              {/* Error state */}
              {displayError && (
                <p className="text-sm text-red-500">{displayError}</p>
              )}
            </CardContent>
          </Card>
        )}
      </div>

      {/* Recent Reviews for this repo */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-base">Recent Reviews</CardTitle>
          <Link
            href={`/reviews?repo_id=${id}`}
            className="text-xs font-medium text-primary hover:underline"
          >
            View all →
          </Link>
        </CardHeader>
        <CardContent>
          {!reviews?.data?.length ? (
            <p className="py-4 text-center text-sm text-muted-foreground">
              No reviews yet for this repository.
            </p>
          ) : (
            <div className="space-y-2">
              {reviews.data?.map((r) => (
                <Link
                  key={r.id}
                  href={`/reviews/${r.id}`}
                  className="flex items-center justify-between rounded-lg p-2 hover:bg-muted/50"
                >
                  <span className="text-sm font-medium">
                    {r.pr_title || `PR #${r.pr_number}`}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {timeAgo(r.created_at)}
                  </span>
                </Link>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </>
  );
}
