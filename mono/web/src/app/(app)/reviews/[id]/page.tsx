// ─── Review Detail Page ──────────────────────────────────────────────────────
// Shows a single review with comments, progress (if active), and stats.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { use } from "react";
import Link from "next/link";
import {
  ArrowLeft,
  ExternalLink,
  GitPullRequest,
} from "lucide-react";
import { useReview, useReviewComments } from "@/lib/api/queries";
import { useReviewStream } from "@/lib/ws/useReviewStream";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import { ReviewProgress } from "@/components/reviews/review-progress";
import {
  ReviewDiffView,
  ReviewFileSummary,
} from "@/components/reviews/review-diff-view";
import { formatDate, formatDuration } from "@/lib/utils";

const statusVariant: Record<string, "default" | "secondary" | "destructive" | "success" | "warning" | "outline"> = {
  completed: "success",
  in_progress: "warning",
  pending: "secondary",
  failed: "destructive",
};

export default function ReviewDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { data: review, isLoading } = useReview(id);
  const { data: comments } = useReviewComments(id);

  const isActive =
    review?.status === "in_progress" || review?.status === "pending";
  const stream = useReviewStream({
    reviewId: id,
    enabled: isActive,
  });

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-[200px]" />
        <Skeleton className="h-[400px]" />
      </div>
    );
  }

  if (!review) {
    return (
      <div className="flex flex-col items-center gap-4 py-20">
        <p className="text-lg font-medium">Review not found</p>
        <Button variant="outline" asChild>
          <Link href="/reviews">Back to Reviews</Link>
        </Button>
      </div>
    );
  }

  return (
    <>
      <PageHeader
        title={review.pr_title || `PR #${review.pr_number}`}
        description={`${review.repo_name} · Review ${review.id.slice(0, 8)}`}
        actions={
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" asChild>
              <Link href="/reviews">
                <ArrowLeft className="mr-1 h-4 w-4" />
                Back
              </Link>
            </Button>
            {review.pr_url && (
              <Button variant="outline" size="sm" asChild>
                <a
                  href={review.pr_url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <ExternalLink className="mr-1 h-4 w-4" />
                  View PR
                </a>
              </Button>
            )}
          </div>
        }
      />

      {/* Status + Stats */}
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Status</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <GitPullRequest className="h-5 w-5 text-muted-foreground" />
              <Badge
                variant={statusVariant[review.status] ?? "outline"}
                className="text-sm"
              >
                {review.status.replace("_", " ")}
              </Badge>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Comments</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {review.stats?.total_comments ?? 0}
            </div>
            <p className="text-xs text-muted-foreground">
              {review.stats?.critical ?? 0} critical ·{" "}
              {review.stats?.warnings ?? 0} warnings
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Timing</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-sm">
              <p>Started: {formatDate(review.created_at)}</p>
              {review.completed_at && (
                <p className="text-muted-foreground">
                  Duration:{" "}
                  {formatDuration(
                    (new Date(review.completed_at).getTime() -
                      new Date(review.created_at).getTime()) /
                      1000,
                  )}
                </p>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Live Progress */}
      {isActive && (
        <ReviewProgress events={stream.events} connected={stream.connected} />
      )}

      {/* Comments — Diff View */}
      <div>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold">
            Review Comments ({comments?.length ?? 0})
          </h2>
          {comments && comments.length > 0 && (
            <ReviewFileSummary comments={comments} />
          )}
        </div>
        <Separator className="mb-4" />
        {!comments || comments.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            {isActive
              ? "Comments will appear here as the review progresses…"
              : "No comments were generated for this review."}
          </p>
        ) : (
          <ReviewDiffView comments={comments} />
        )}
      </div>
    </>
  );
}
