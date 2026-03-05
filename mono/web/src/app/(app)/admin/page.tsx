// ─── Admin Page ──────────────────────────────────────────────────────────────
// System stats, health checks — only visible to admin users.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import {
  Activity,
  Database,
  Server,
  Users,
  GitPullRequest,
  HardDrive,
} from "lucide-react";
import { useAdminStats, useDetailedHealth } from "@/lib/api/queries";
import { useAuth } from "@/lib/auth/context";
import { PageHeader } from "@/components/layout/page-header";
import { StatsCard } from "@/components/dashboard/stats-card";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

export default function AdminPage() {
  const { user, isLoading: authLoading } = useAuth();
  const router = useRouter();
  const { data: stats, isLoading: statsLoading } = useAdminStats();
  const { data: health, isLoading: healthLoading } = useDetailedHealth();

  // Redirect non-admins
  useEffect(() => {
    if (!authLoading && user?.role !== "admin") {
      router.replace("/dashboard");
    }
  }, [authLoading, user, router]);

  if (authLoading || user?.role !== "admin") {
    return null;
  }

  return (
    <>
      <PageHeader
        title="Admin Dashboard"
        description="System health and statistics"
      />

      {/* Stats Grid */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {statsLoading ? (
          Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px]" />
          ))
        ) : (
          <>
            <StatsCard
              title="Total Users"
              value={stats?.total_users ?? 0}
              icon={Users}
            />
            <StatsCard
              title="Total Reviews"
              value={stats?.total_reviews ?? 0}
              icon={GitPullRequest}
            />
            <StatsCard
              title="Total Repos"
              value={stats?.total_repos ?? 0}
              icon={Database}
            />
            <StatsCard
              title="Uptime"
              value={stats ? `${Math.floor((stats.avg_review_time_ms || 0) / 1000)}s avg` : "—"}
              icon={Activity}
            />
          </>
        )}
      </div>

      {/* Health Checks */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Server className="h-4 w-4" />
            System Health
          </CardTitle>
        </CardHeader>
        <CardContent>
          {healthLoading ? (
            <div className="space-y-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : (
            <div className="space-y-3">
              <div className="flex items-center justify-between rounded-lg border p-3">
                <div className="flex items-center gap-2">
                  <HardDrive className="h-4 w-4 text-muted-foreground" />
                  <span className="text-sm font-medium">Overall Status</span>
                </div>
                <Badge
                  variant={
                    health?.status === "healthy"
                      ? "success"
                      : health?.status === "degraded"
                        ? "warning"
                        : "destructive"
                  }
                >
                  {health?.status ?? "unknown"}
                </Badge>
              </div>

              {health?.components?.map((component) => (
                <div
                  key={component.name}
                  className="flex items-center justify-between rounded-lg border p-3"
                >
                  <div>
                    <p className="text-sm font-medium">{component.name}</p>
                    {component.message && (
                      <p className="text-xs text-muted-foreground">
                        {component.message}
                      </p>
                    )}
                  </div>
                  <Badge
                    variant={
                      component.status === "up"
                        ? "success"
                        : component.status === "degraded"
                          ? "warning"
                          : "destructive"
                    }
                  >
                    {component.status}
                  </Badge>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </>
  );
}
