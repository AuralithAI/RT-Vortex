// ─── Reviews List Page ───────────────────────────────────────────────────────
// Paginated list of all reviews with status badges and filters.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import Link from "next/link";
import { GitPullRequest, ExternalLink } from "lucide-react";
import { useReviews } from "@/lib/api/queries";
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
import type { Review } from "@/types/api";

const statusVariant: Record<string, "default" | "secondary" | "destructive" | "success" | "warning" | "outline"> = {
  completed: "success",
  in_progress: "warning",
  pending: "secondary",
  failed: "destructive",
};

export default function ReviewsPage() {
  const [offset, setOffset] = useState(0);
  const limit = 20;
  const { data, isLoading } = useReviews({ limit, offset });

  return (
    <>
      <PageHeader
        title="Reviews"
        description="AI-powered code review results for your pull requests"
      />

      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Pull Request</TableHead>
              <TableHead>Repository</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Comments</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="w-[50px]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading
              ? Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 6 }).map((_, j) => (
                      <TableCell key={j}>
                        <Skeleton className="h-5 w-full" />
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              : data?.data.map((review: Review) => (
                  <TableRow key={review.id}>
                    <TableCell>
                      <Link
                        href={`/reviews/${review.id}`}
                        className="flex items-center gap-2 font-medium hover:underline"
                      >
                        <GitPullRequest className="h-4 w-4 text-muted-foreground" />
                        {review.pr_title || `PR #${review.pr_number}`}
                      </Link>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {review.repo_name}
                    </TableCell>
                    <TableCell>
                      <Badge variant={statusVariant[review.status] ?? "outline"}>
                        {review.status.replace("_", " ")}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {review.stats?.total_comments ?? 0}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {timeAgo(review.created_at)}
                    </TableCell>
                    <TableCell>
                      {review.pr_url && (
                        <a
                          href={review.pr_url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-muted-foreground hover:text-foreground"
                        >
                          <ExternalLink className="h-4 w-4" />
                        </a>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
          </TableBody>
        </Table>
      </div>

      {/* Pagination */}
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
