// ─── Dashboard Page ──────────────────────────────────────────────────────────
// Main landing page after login: stats grid, review activity chart, recent.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import {
  GitPullRequest,
  FolderGit2,
  Building2,
  MessageSquare,
} from "lucide-react";
import { useReviews, useRepos, useOrgs } from "@/lib/api/queries";
import { PageHeader } from "@/components/layout/page-header";
import { StatsCard } from "@/components/dashboard/stats-card";
import { RecentReviews } from "@/components/dashboard/recent-reviews";
import { ReviewActivityChart } from "@/components/dashboard/review-activity-chart";
import { Skeleton } from "@/components/ui/skeleton";

export default function DashboardPage() {
  const { data: reviewsData, isLoading: loadingReviews } = useReviews({
    limit: 5,
    offset: 0,
  });
  const { data: reposData, isLoading: loadingRepos } = useRepos({
    limit: 1,
    offset: 0,
  });
  const { data: orgsData, isLoading: loadingOrgs } = useOrgs({
    limit: 1,
    offset: 0,
  });

  const isLoading = loadingReviews || loadingRepos || loadingOrgs;

  // Generate mock chart data (will be replaced with real stats endpoint)
  const chartData = Array.from({ length: 7 }, (_, i) => {
    const d = new Date();
    d.setDate(d.getDate() - (6 - i));
    return {
      date: d.toLocaleDateString("en-US", { month: "short", day: "numeric" }),
      reviews: Math.floor(Math.random() * 10) + 1,
    };
  });

  return (
    <>
      <PageHeader
        title="Dashboard"
        description="Overview of your code review activity"
      />

      {/* Stats Grid */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {isLoading ? (
          Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px]" />
          ))
        ) : (
          <>
            <StatsCard
              title="Total Reviews"
              value={reviewsData?.total ?? 0}
              description="All time"
              icon={GitPullRequest}
            />
            <StatsCard
              title="Repositories"
              value={reposData?.total ?? 0}
              description="Connected"
              icon={FolderGit2}
            />
            <StatsCard
              title="Organizations"
              value={orgsData?.total ?? 0}
              description="Active"
              icon={Building2}
            />
            <StatsCard
              title="Comments"
              value={
                reviewsData?.data?.reduce(
                  (sum: number, r) => sum + (r.stats?.total_comments ?? 0),
                  0,
                ) ?? 0
              }
              description="Generated feedback"
              icon={MessageSquare}
            />
          </>
        )}
      </div>

      {/* Charts + Recent */}
      <div className="grid gap-6 lg:grid-cols-2">
        <ReviewActivityChart data={chartData} />
        <RecentReviews reviews={reviewsData?.data ?? []} />
      </div>
    </>
  );
}
