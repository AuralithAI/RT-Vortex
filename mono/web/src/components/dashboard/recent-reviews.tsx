// ─── Recent Reviews List ─────────────────────────────────────────────────────
// Shows the latest reviews in a compact list on the dashboard.
// ─────────────────────────────────────────────────────────────────────────────

import Link from "next/link";
import { GitPullRequest } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { timeAgo } from "@/lib/utils";
import type { Review } from "@/types/api";

const statusVariant: Record<string, "default" | "secondary" | "destructive" | "success" | "warning" | "outline"> = {
  completed: "success",
  in_progress: "warning",
  pending: "secondary",
  failed: "destructive",
};

interface RecentReviewsProps {
  reviews: Review[];
}

export function RecentReviews({ reviews }: RecentReviewsProps) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="text-base">Recent Reviews</CardTitle>
        <Link
          href="/reviews"
          className="text-xs font-medium text-primary hover:underline"
        >
          View all →
        </Link>
      </CardHeader>
      <CardContent>
        {reviews.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            No reviews yet. Connect a repo to get started.
          </p>
        ) : (
          <div className="space-y-4">
            {reviews.map((review) => (
              <Link
                key={review.id}
                href={`/reviews/${review.id}`}
                className="flex items-start gap-3 rounded-lg p-2 transition-colors hover:bg-muted/50"
              >
                <GitPullRequest className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">
                    {review.pr_title || `PR #${review.pr_number}`}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {review.repo_name} · {timeAgo(review.created_at)}
                  </p>
                </div>
                <Badge variant={statusVariant[review.status] ?? "outline"}>
                  {review.status.replace("_", " ")}
                </Badge>
              </Link>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
