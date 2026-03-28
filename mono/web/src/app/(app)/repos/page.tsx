// ─── Repositories List Page ──────────────────────────────────────────────────
// Paginated list of connected repositories with indexing status.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import Link from "next/link";
import { FolderGit2, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useRepos } from "@/lib/api/queries";
import { useTriggerIndex, useDeleteRepo } from "@/lib/api/mutations";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { timeAgo } from "@/lib/utils";
import type { Repo } from "@/types/api";
import { useUIStore } from "@/lib/stores/ui";

export default function ReposPage() {
  const [offset, setOffset] = useState(0);
  const limit = 20;
  const { data, isLoading } = useRepos({ limit, offset });
  const triggerIndex = useTriggerIndex();
  const deleteRepo = useDeleteRepo();
  const { showConfirm, addToast } = useUIStore();

  const handleDelete = (repo: Repo) => {
    showConfirm(
      "Delete Repository",
      `Are you sure you want to remove "${repo.full_name || repo.name}"? This will also delete its index data. This action cannot be undone.`,
      async () => {
        try {
          await deleteRepo.mutateAsync(repo.id);
          addToast({ title: "Repository deleted", variant: "success" });
        } catch {
          addToast({ title: "Failed to delete repository", variant: "error" });
        }
      },
    );
  };

  return (
    <>
      <PageHeader
        title="Repositories"
        description="Manage your connected repositories"
        actions={
          <Button asChild>
            <Link href="/repos/connect">
              <Plus className="mr-1 h-4 w-4" />
              Connect Repo
            </Link>
          </Button>
        }
      />

      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Repository</TableHead>
              <TableHead>Platform</TableHead>
              <TableHead>Indexed</TableHead>
              <TableHead>Last Indexed</TableHead>
              <TableHead className="w-[100px]">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading
              ? Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 5 }).map((_, j) => (
                      <TableCell key={j}>
                        <Skeleton className="h-5 w-full" />
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              : data?.data?.map((repo: Repo) => (
                  <TableRow key={repo.id}>
                    <TableCell>
                      <Link
                        href={`/repos/${repo.id}`}
                        className="flex items-center gap-2 font-medium hover:underline"
                      >
                        <FolderGit2 className="h-4 w-4 text-muted-foreground" />
                        {repo.full_name || repo.name}
                      </Link>
                    </TableCell>
                    <TableCell className="capitalize text-muted-foreground">
                      {repo.platform}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={repo.is_indexed ? "success" : "secondary"}
                      >
                        {repo.is_indexed ? "Yes" : "No"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {repo.last_indexed_at
                        ? timeAgo(repo.last_indexed_at)
                        : "Never"}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => triggerIndex.mutate({ repoId: repo.id, action: "reindex" })}
                          disabled={triggerIndex.isPending}
                          title="Re-index"
                        >
                          <RefreshCw className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="text-red-500 hover:text-red-600"
                          onClick={() => handleDelete(repo)}
                          title="Delete"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
          </TableBody>
        </Table>
      </div>

      {data && (
        <div className="flex items-center justify-between">
          <p className="text-sm text-muted-foreground">
            Showing {offset + 1}–{Math.min(offset + limit, data.total)} of{" "}
            {data.total}
          </p>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - limit))}
            >
              Previous
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!data.has_more}
              onClick={() => setOffset(offset + limit)}
            >
              Next
            </Button>
          </div>
        </div>
      )}
    </>
  );
}
