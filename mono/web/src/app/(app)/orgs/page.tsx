// ─── Organizations List Page ─────────────────────────────────────────────────
// Shows all organizations the user belongs to.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import Link from "next/link";
import { Building2, Plus, Users } from "lucide-react";
import { useOrgs } from "@/lib/api/queries";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";

export default function OrgsPage() {
  const { data, isLoading } = useOrgs({ limit: 50, offset: 0 });

  return (
    <>
      <PageHeader
        title="Organizations"
        description="Manage your organizations and team members"
        actions={
          <Button asChild>
            <Link href="/orgs/create">
              <Plus className="mr-1 h-4 w-4" />
              Create Org
            </Link>
          </Button>
        }
      />

      {isLoading ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-[150px]" />
          ))}
        </div>
      ) : !data?.data?.length ? (
        <div className="flex flex-col items-center gap-4 py-20">
          <Building2 className="h-12 w-12 text-muted-foreground" />
          <p className="text-lg font-medium">No organizations yet</p>
          <p className="text-sm text-muted-foreground">
            Create an organization to start collaborating.
          </p>
          <Button asChild>
            <Link href="/orgs/create">
              <Plus className="mr-1 h-4 w-4" />
              Create Organization
            </Link>
          </Button>
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {data?.data?.map((org) => (
            <Link key={org.id} href={`/orgs/${org.id}`}>
              <Card className="h-full transition-shadow hover:shadow-md">
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <CardTitle className="text-base">{org.name}</CardTitle>
                    {org.plan && (
                      <Badge variant="secondary">{org.plan}</Badge>
                    )}
                  </div>
                  <CardDescription>@{org.slug}</CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Users className="h-4 w-4" />
                    <span>View members & settings</span>
                  </div>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </>
  );
}
