// ─── Organization Detail Page ────────────────────────────────────────────────
// Shows org info and member list with invite/remove capabilities.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { use, useState } from "react";
import Link from "next/link";
import { ArrowLeft, UserPlus, Trash2, Link2, ArrowRight, Shield } from "lucide-react";
import { useOrg, useOrgMembers, useOrgCrossRepoLinks } from "@/lib/api/queries";
import { useInviteMember, useRemoveMember } from "@/lib/api/mutations";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { useUIStore } from "@/lib/stores/ui";
import { formatDate } from "@/lib/utils";
import { CrossRepoGraph } from "@/components/dashboard/cross-repo-graph";

export default function OrgDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { data: org, isLoading: loadingOrg } = useOrg(id);
  const { data: membersData, isLoading: loadingMembers } = useOrgMembers(id);
  const inviteMember = useInviteMember();
  const removeMember = useRemoveMember();
  const { showConfirm, addToast } = useUIStore();

  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState("member");

  const handleInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!inviteEmail.trim()) return;

    try {
      await inviteMember.mutateAsync({
        orgId: id,
        email: inviteEmail,
        role: inviteRole,
      });
      addToast({ title: "Invitation sent", variant: "success" });
      setInviteEmail("");
    } catch (err) {
      addToast({
        title: "Failed to invite member",
        description: err instanceof Error ? err.message : "Unknown error",
        variant: "error",
      });
    }
  };

  const handleRemove = (userId: string, name: string) => {
    showConfirm(
      "Remove Member",
      `Remove ${name} from this organization?`,
      async () => {
        try {
          await removeMember.mutateAsync({ orgId: id, userId });
          addToast({ title: "Member removed", variant: "success" });
        } catch {
          addToast({ title: "Failed to remove member", variant: "error" });
        }
      },
    );
  };

  if (loadingOrg) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-[200px]" />
      </div>
    );
  }

  if (!org) {
    return (
      <div className="flex flex-col items-center gap-4 py-20">
        <p className="text-lg font-medium">Organization not found</p>
        <Button variant="outline" asChild>
          <Link href="/orgs">Back to Organizations</Link>
        </Button>
      </div>
    );
  }

  return (
    <>
      <PageHeader
        title={org.name}
        description={`@${org.slug} · Created ${formatDate(org.created_at)}`}
        actions={
          <Button variant="outline" size="sm" asChild>
            <Link href="/orgs">
              <ArrowLeft className="mr-1 h-4 w-4" />
              Back
            </Link>
          </Button>
        }
      />

      {/* Info */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Organization Info</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <div className="flex justify-between">
            <span className="text-muted-foreground">Plan</span>
            <Badge variant="secondary">{org.plan ?? "Free"}</Badge>
          </div>
        </CardContent>
      </Card>

      {/* Invite Form */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <UserPlus className="h-4 w-4" />
            Invite Member
          </CardTitle>
          <CardDescription>
            Send an invitation to a team member by email.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            onSubmit={handleInvite}
            className="flex items-end gap-3"
          >
            <div className="flex-1 space-y-1">
              <Input
                placeholder="user@company.com"
                type="email"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
              />
            </div>
            <Select value={inviteRole} onValueChange={setInviteRole}>
              <SelectTrigger className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="member">Member</SelectItem>
                <SelectItem value="admin">Admin</SelectItem>
              </SelectContent>
            </Select>
            <Button type="submit" disabled={inviteMember.isPending}>
              Invite
            </Button>
          </form>
        </CardContent>
      </Card>

      {/* Members Table */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">
            Members ({membersData?.total ?? 0})
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>User</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Joined</TableHead>
                <TableHead className="w-[50px]" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {loadingMembers
                ? Array.from({ length: 3 }).map((_, i) => (
                    <TableRow key={i}>
                      {Array.from({ length: 4 }).map((_, j) => (
                        <TableCell key={j}>
                          <Skeleton className="h-5 w-full" />
                        </TableCell>
                      ))}
                    </TableRow>
                  ))
                : membersData?.data?.map((member) => (
                    <TableRow key={member.user_id}>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Avatar className="h-7 w-7">
                            <AvatarFallback className="text-xs">
                              {(member.user?.name ?? member.user?.email ?? "?")
                                .charAt(0)
                                .toUpperCase()}
                            </AvatarFallback>
                          </Avatar>
                          <div>
                            <p className="text-sm font-medium">
                              {member.user?.name ?? "—"}
                            </p>
                            <p className="text-xs text-muted-foreground">
                              {member.user?.email}
                            </p>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline">{member.role}</Badge>
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {formatDate(member.joined_at)}
                      </TableCell>
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="text-red-500 hover:text-red-600"
                          onClick={() =>
                            handleRemove(
                              member.user_id,
                              member.user?.name ?? member.user?.email ?? "this user",
                            )
                          }
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* ── Cross-Repo Observatory ───────────────────────────────────────── */}
      <OrgCrossRepoLinksCard orgId={id} />
      <CrossRepoGraph orgId={id} />
    </>
  );
}

// ── Org-level Cross-Repo Links ──────────────────────────────────────────────

function OrgCrossRepoLinksCard({ orgId }: { orgId: string }) {
  const { data, isLoading } = useOrgCrossRepoLinks(orgId);
  const links = data?.links ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Link2 className="h-4 w-4" />
          Cross-Repo Links ({data?.total ?? 0})
        </CardTitle>
        <CardDescription>
          All repository links within this organization.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : links.length === 0 ? (
          <p className="py-4 text-center text-sm text-muted-foreground">
            No cross-repo links in this organization yet.
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Source → Target</TableHead>
                <TableHead>Share Profile</TableHead>
                <TableHead>Label</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {links.map((link) => (
                <TableRow key={link.id}>
                  <TableCell>
                    <div className="flex items-center gap-2 text-sm">
                      <span className="font-medium">
                        {link.source_repo_name}
                      </span>
                      <ArrowRight className="h-3 w-3 text-muted-foreground" />
                      <span className="font-medium">
                        {link.target_repo_name}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      <Shield className="mr-1 h-3 w-3" />
                      {link.share_profile}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {link.label || "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
