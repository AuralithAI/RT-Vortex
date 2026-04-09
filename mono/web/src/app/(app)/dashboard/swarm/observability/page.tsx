// ─── Swarm Observability Page ────────────────────────────────────────────────
// Full-page observability dashboard with system health, metrics time-series,
// provider performance comparison, and cost tracking.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { Activity } from "lucide-react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { ObservabilityDashboard } from "@/components/swarm/observability-dashboard";
import Link from "next/link";

export default function SwarmObservabilityPage() {
  return (
    <>
      <PageHeader
        title="Swarm Observability"
        description="System health, metrics, provider performance, and cost tracking"
        actions={
          <Link href="/dashboard/swarm">
            <Button variant="outline">← Back to Swarm</Button>
          </Link>
        }
      />
      <ObservabilityDashboard />
    </>
  );
}
