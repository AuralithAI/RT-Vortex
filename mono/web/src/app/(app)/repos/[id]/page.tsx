// ─── Repository Detail Page ──────────────────────────────────────────────────
// Shows repo info, indexing status, contextual actions, branch selector,
// and recent reviews for the repo.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { use, useState, useMemo, useCallback } from "react";
import Link from "next/link";
import {
  ArrowLeft,
  RefreshCw,
  Trash2,
  FolderGit2,
  CheckCircle2,
  XCircle,
  Loader2,
  Clock,
  MessageSquare,
  ChevronDown,
  GitBranch,
  Download,
  RotateCcw,
  Search,
  AlertTriangle,
} from "lucide-react";
import { useRepo, useIndexStatus, useReviews, useBranches } from "@/lib/api/queries";
import { useTriggerIndex, useDeleteRepo, useUpdateRepo } from "@/lib/api/mutations";
import { useIndexProgress, formatETA } from "@/hooks/use-index-progress";
import { PageHeader } from "@/components/layout/page-header";
import { PullRequestList } from "@/components/dashboard/pull-request-list";
import { AssetManager } from "@/components/dashboard/asset-manager";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { Input } from "@/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useUIStore } from "@/lib/stores/ui";
import { useRouter } from "next/navigation";
import { formatDate, timeAgo } from "@/lib/utils";
import { CrossRepoLinks } from "@/components/dashboard/cross-repo-links";
import { CrossRepoDeps } from "@/components/dashboard/cross-repo-deps";
import { CrossRepoSearch } from "@/components/dashboard/cross-repo-search";
import { CrossRepoDepGraph } from "@/components/dashboard/cross-repo-dep-graph";
import { IntraRepoFileMap } from "@/components/dashboard/intra-repo-file-map";
import { BuildSecretsManager } from "@/components/dashboard/build-secrets-manager";

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
  const updateRepo = useUpdateRepo();
  const { showConfirm, addToast } = useUIStore();

  // Branch selector state
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [branchSearch, setBranchSearch] = useState("");
  const [branchChangeDialog, setBranchChangeDialog] = useState<{
    open: boolean;
    branch: string;
  }>({ open: false, branch: "" });

  // Fetch remote branches only when the dropdown is opened
  const { data: branchData, isLoading: branchesLoading } = useBranches(
    id,
    branchMenuOpen,
  );

  // Filter branches by search input
  const filteredBranches = useMemo(() => {
    if (!branchData?.branches) return [];
    if (!branchSearch) return branchData.branches;
    const q = branchSearch.toLowerCase();
    return branchData.branches.filter((b) => b.toLowerCase().includes(q));
  }, [branchData?.branches, branchSearch]);

  // Real-time indexing progress via WebSocket
  const isIndexing = indexStatus?.status === "indexing";
  const { event: wsEvent, connected: wsConnected } = useIndexProgress(id, {
    enabled: isIndexing,
  });

  // Merge WS event into display state (WS is more current than polling)
  const displayProgress = wsEvent?.progress ?? indexStatus?.progress ?? 0;
  const displayPhase = wsEvent?.phase ?? indexStatus?.phase ?? "";
  const displayFilesProcessed =
    wsEvent?.files_processed ?? indexStatus?.files_processed ?? 0;
  const displayFilesTotal =
    wsEvent?.files_total ?? indexStatus?.files_total ?? 0;
  const displayCurrentFile =
    wsEvent?.current_file ?? indexStatus?.current_file ?? "";
  const displayETA = wsEvent?.eta_seconds ?? indexStatus?.eta_seconds ?? -1;
  const displayState = wsEvent?.state ?? indexStatus?.status ?? "idle";
  const displayError = wsEvent?.error ?? indexStatus?.error ?? null;

  const isOperationInProgress = triggerIndex.isPending || isIndexing;

  // ── Action handlers ─────────────────────────────────────────────────────

  const handleIndex = useCallback(() => {
    triggerIndex.mutate({ repoId: id, action: "index" });
    addToast({ title: "Indexing started (clone + index)", variant: "success" });
  }, [triggerIndex, id, addToast]);

  const handleReindex = useCallback(() => {
    triggerIndex.mutate({ repoId: id, action: "reindex" });
    addToast({
      title: "Re-indexing started (using existing clone)",
      variant: "success",
    });
  }, [triggerIndex, id, addToast]);

  const handleReclone = useCallback(() => {
    showConfirm(
      "Re-clone Repository",
      "This will delete the existing local clone and re-clone from the remote. This may take longer than a re-index. Continue?",
      () => {
        triggerIndex.mutate({ repoId: id, action: "reclone" });
        addToast({
          title: "Re-clone + index started",
          variant: "success",
        });
      },
    );
  }, [triggerIndex, id, showConfirm, addToast]);

  const handleBranchSelect = useCallback(
    (branch: string) => {
      setBranchMenuOpen(false);
      setBranchSearch("");
      if (branch === repo?.default_branch) return; // no change
      setBranchChangeDialog({ open: true, branch });
    },
    [repo?.default_branch],
  );

  const confirmBranchChange = useCallback(async () => {
    const branch = branchChangeDialog.branch;
    setBranchChangeDialog({ open: false, branch: "" });
    try {
      // Update the default branch in the DB
      await updateRepo.mutateAsync({
        id,
        data: { default_branch: branch },
      });
      // Trigger re-clone + index on the new branch
      triggerIndex.mutate({
        repoId: id,
        action: "reclone",
        targetBranch: branch,
      });
      addToast({
        title: `Branch changed to "${branch}" — re-indexing started`,
        variant: "success",
      });
    } catch {
      addToast({ title: "Failed to change branch", variant: "error" });
    }
  }, [branchChangeDialog.branch, id, updateRepo, triggerIndex, addToast]);

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
          addToast({
            title: "Failed to delete repository",
            variant: "error",
          });
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
            <Button variant="outline" size="sm" asChild>
              <Link href={`/repos/${id}/chat`}>
                <MessageSquare className="mr-1 h-4 w-4" />
                Chat
              </Link>
            </Button>

            {/* ── Indexing Actions Dropdown ──────────────────────────────── */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={isOperationInProgress}
                >
                  {triggerIndex.isPending ? (
                    <Loader2 className="mr-1 h-4 w-4 animate-spin" />
                  ) : (
                    <RefreshCw className="mr-1 h-4 w-4" />
                  )}
                  Index
                  <ChevronDown className="ml-1 h-3 w-3" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuLabel>Indexing Actions</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={handleReindex}>
                  <RefreshCw className="mr-2 h-4 w-4" />
                  <div>
                    <div className="font-medium">Re-index</div>
                    <p className="text-xs text-muted-foreground">
                      Re-parse existing local files (fast)
                    </p>
                  </div>
                </DropdownMenuItem>
                <DropdownMenuItem onClick={handleIndex}>
                  <Download className="mr-2 h-4 w-4" />
                  <div>
                    <div className="font-medium">Pull &amp; Index</div>
                    <p className="text-xs text-muted-foreground">
                      Pull latest changes, then index
                    </p>
                  </div>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={handleReclone}
                  className="text-orange-600 focus:text-orange-600"
                >
                  <RotateCcw className="mr-2 h-4 w-4" />
                  <div>
                    <div className="font-medium">Re-clone &amp; Index</div>
                    <p className="text-xs text-muted-foreground">
                      Delete local clone, fresh clone + index
                    </p>
                  </div>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            {/* ── Branch Selector Dropdown ────────────────────────────────── */}
            <DropdownMenu
              open={branchMenuOpen}
              onOpenChange={(open) => {
                setBranchMenuOpen(open);
                if (!open) setBranchSearch("");
              }}
            >
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm">
                  <GitBranch className="mr-1 h-4 w-4" />
                  {repo.default_branch || "main"}
                  <ChevronDown className="ml-1 h-3 w-3" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-64">
                <DropdownMenuLabel>Switch Branch</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {/* Search input */}
                <div className="px-2 pb-2">
                  <div className="relative">
                    <Search className="absolute left-2 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
                    <Input
                      placeholder="Filter branches..."
                      value={branchSearch}
                      onChange={(e) => setBranchSearch(e.target.value)}
                      className="h-8 pl-7 text-xs"
                      autoFocus
                    />
                  </div>
                </div>
                <DropdownMenuSeparator />
                <div className="max-h-60 overflow-y-auto">
                  {branchesLoading ? (
                    <div className="flex items-center justify-center py-4">
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                      <span className="ml-2 text-xs text-muted-foreground">
                        Fetching branches...
                      </span>
                    </div>
                  ) : filteredBranches.length === 0 ? (
                    <p className="py-4 text-center text-xs text-muted-foreground">
                      {branchSearch
                        ? "No branches match your search"
                        : "No branches found"}
                    </p>
                  ) : (
                    filteredBranches.map((branch) => (
                      <DropdownMenuItem
                        key={branch}
                        onClick={() => handleBranchSelect(branch)}
                        className="flex items-center justify-between"
                      >
                        <span className="truncate text-xs">{branch}</span>
                        {branch === repo.default_branch && (
                          <Badge
                            variant="success"
                            className="ml-2 text-[10px]"
                          >
                            current
                          </Badge>
                        )}
                      </DropdownMenuItem>
                    ))
                  )}
                </div>
              </DropdownMenuContent>
            </DropdownMenu>

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
              <span className="text-muted-foreground">Platform</span>
              <Badge variant="outline">{repo.platform}</Badge>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Default Branch</span>
              <div className="flex items-center gap-1">
                <GitBranch className="h-3 w-3 text-muted-foreground" />
                <span>{repo.default_branch}</span>
              </div>
            </div>
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
                {(displayState === "running" ||
                  displayState === "indexing") && (
                  <Loader2 className="h-4 w-4 animate-spin text-yellow-500" />
                )}
                {(displayState === "pending" ||
                  displayState === "queued") && (
                  <Clock className="h-4 w-4 text-muted-foreground" />
                )}
                <Badge
                  variant={
                    displayState === "completed"
                      ? "success"
                      : displayState === "failed"
                        ? "destructive"
                        : displayState === "running" ||
                            displayState === "indexing"
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
              {(displayState === "running" ||
                displayState === "indexing") && (
                <>
                  <Progress value={displayProgress} className="h-2" />
                  <div className="flex justify-between text-xs text-muted-foreground">
                    <span>
                      {displayFilesProcessed} /{" "}
                      {displayFilesTotal || "?"} files
                    </span>
                    <span className="font-medium">{displayProgress}%</span>
                  </div>
                  {displayCurrentFile && (
                    <p
                      className="truncate text-xs text-muted-foreground"
                      title={displayCurrentFile}
                    >
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
                <p className="text-xs font-medium text-green-600">
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

      {/* Multimodal Assets */}
      <AssetManager repoId={id} />

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

      {/* Tracked Pull Requests */}
      <PullRequestList repoId={id} />

      {/* ── Sandbox Build Secrets ────────────────────────────────────────── */}
      <BuildSecretsManager repoId={id} />

      {/* ── Intra-Repo File Map (Knowledge Graph) ────────────────────────── */}
      <IntraRepoFileMap repoId={id} />

      {/* ── Cross-Repo Observatory ───────────────────────────────────────── */}
      <CrossRepoLinks repoId={id} orgId={repo.org_id} />
      <CrossRepoDepGraph repoId={id} orgId={repo.org_id} />
      <CrossRepoDeps repoId={id} />
      <CrossRepoSearch repoId={id} />

      {/* ── Branch Change Warning Dialog ─────────────────────────────────── */}
      <Dialog
        open={branchChangeDialog.open}
        onOpenChange={(open) =>
          !open && setBranchChangeDialog({ open: false, branch: "" })
        }
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <AlertTriangle className="h-5 w-5 text-orange-500" />
              Change Default Branch
            </DialogTitle>
            <DialogDescription>
              Switching from{" "}
              <strong className="text-foreground">
                {repo.default_branch}
              </strong>{" "}
              to{" "}
              <strong className="text-foreground">
                {branchChangeDialog.branch}
              </strong>{" "}
              will:
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2 rounded-md bg-muted p-3 text-sm">
            <p>• Delete the existing local clone</p>
            <p>• Re-clone the repository on the new branch</p>
            <p>• Re-index all files from scratch</p>
            <p className="text-xs text-muted-foreground">
              This may take several minutes for large repositories.
            </p>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() =>
                setBranchChangeDialog({ open: false, branch: "" })
              }
            >
              Cancel
            </Button>
            <Button onClick={confirmBranchChange}>
              <GitBranch className="mr-1 h-4 w-4" />
              Change Branch &amp; Re-index
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
