// ─── Cross-Repo Links Panel ──────────────────────────────────────────────────
// CRUD management for cross-repo links within a single repository.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import {
  Link2,
  Plus,
  Trash2,
  ArrowRight,
  Shield,
  Clock,
  Pencil,
} from "lucide-react";
import { useCrossRepoLinks } from "@/lib/api/queries";
import {
  useCreateCrossRepoLink,
  useUpdateCrossRepoLink,
  useDeleteCrossRepoLink,
} from "@/lib/api/mutations";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { useUIStore } from "@/lib/stores/ui";
import { timeAgo } from "@/lib/utils";
import type { ShareProfile } from "@/types/api";

const shareProfileVariant: Record<
  ShareProfile,
  "default" | "secondary" | "success" | "destructive" | "outline" | "warning"
> = {
  full: "success",
  symbols: "default",
  metadata: "secondary",
  none: "destructive",
};

const shareProfileDesc: Record<ShareProfile, string> = {
  full: "All data — search, symbols, file content",
  symbols: "Exported symbols, signatures, types",
  metadata: "Manifest — language, build, deps",
  none: "Paused — no sharing",
};

interface CrossRepoLinksProps {
  repoId: string;
}

export function CrossRepoLinks({ repoId }: CrossRepoLinksProps) {
  const { data, isLoading } = useCrossRepoLinks(repoId);
  const createLink = useCreateCrossRepoLink();
  const updateLink = useUpdateCrossRepoLink();
  const deleteLink = useDeleteCrossRepoLink();
  const { showConfirm, addToast } = useUIStore();

  // Create dialog state
  const [createOpen, setCreateOpen] = useState(false);
  const [targetRepoId, setTargetRepoId] = useState("");
  const [shareProfile, setShareProfile] = useState<ShareProfile>("metadata");
  const [label, setLabel] = useState("");

  // Edit dialog state
  const [editOpen, setEditOpen] = useState(false);
  const [editLinkId, setEditLinkId] = useState("");
  const [editProfile, setEditProfile] = useState<ShareProfile>("metadata");
  const [editLabel, setEditLabel] = useState("");

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!targetRepoId.trim()) return;
    try {
      await createLink.mutateAsync({
        repoId,
        data: {
          target_repo_id: targetRepoId.trim(),
          share_profile: shareProfile,
          label: label.trim() || undefined,
        },
      });
      addToast({ title: "Cross-repo link created", variant: "success" });
      setCreateOpen(false);
      setTargetRepoId("");
      setShareProfile("metadata");
      setLabel("");
    } catch (err) {
      addToast({
        title: "Failed to create link",
        description: err instanceof Error ? err.message : "Unknown error",
        variant: "error",
      });
    }
  };

  const handleUpdate = async () => {
    try {
      await updateLink.mutateAsync({
        repoId,
        linkId: editLinkId,
        data: {
          share_profile: editProfile,
          label: editLabel.trim() || undefined,
        },
      });
      addToast({ title: "Link updated", variant: "success" });
      setEditOpen(false);
    } catch (err) {
      addToast({
        title: "Failed to update link",
        description: err instanceof Error ? err.message : "Unknown error",
        variant: "error",
      });
    }
  };

  const handleDelete = (linkId: string, label: string) => {
    showConfirm(
      "Delete Cross-Repo Link",
      `Remove the link "${label || linkId}"? Federated search and dependency graph data for this link will stop.`,
      async () => {
        try {
          await deleteLink.mutateAsync({ repoId, linkId });
          addToast({ title: "Link deleted", variant: "success" });
        } catch {
          addToast({ title: "Failed to delete link", variant: "error" });
        }
      },
    );
  };

  const links = data?.links ?? [];

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-base">
            <Link2 className="h-4 w-4" />
            Cross-Repo Links ({data?.total ?? 0})
          </CardTitle>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="mr-1 h-4 w-4" />
            New Link
          </Button>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : links.length === 0 ? (
            <div className="flex flex-col items-center gap-2 py-8 text-center">
              <Link2 className="h-8 w-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">
                No cross-repo links yet. Link this repo to another to enable
                federated search and dependency analysis.
              </p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Source → Target</TableHead>
                  <TableHead>Share Profile</TableHead>
                  <TableHead>Label</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-[80px]" />
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
                      <Badge
                        variant={
                          shareProfileVariant[
                            link.share_profile as ShareProfile
                          ] ?? "secondary"
                        }
                      >
                        <Shield className="mr-1 h-3 w-3" />
                        {link.share_profile}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {link.label || "—"}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      <div className="flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {timeAgo(link.created_at)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7"
                          onClick={() => {
                            setEditLinkId(link.id);
                            setEditProfile(
                              link.share_profile as ShareProfile,
                            );
                            setEditLabel(link.label || "");
                            setEditOpen(true);
                          }}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7 text-red-500 hover:text-red-600"
                          onClick={() =>
                            handleDelete(link.id, link.label || link.target_repo_name)
                          }
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* ── Create Link Dialog ───────────────────────────────────────────── */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create Cross-Repo Link</DialogTitle>
            <DialogDescription>
              Link this repository to another within the same organization to
              enable federated search and dependency analysis.
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={handleCreate} className="space-y-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">
                Target Repository ID
              </label>
              <Input
                placeholder="UUID of the target repo"
                value={targetRepoId}
                onChange={(e) => setTargetRepoId(e.target.value)}
                required
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">Share Profile</label>
              <Select
                value={shareProfile}
                onValueChange={(v) => setShareProfile(v as ShareProfile)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(
                    ["full", "symbols", "metadata", "none"] as ShareProfile[]
                  ).map((p) => (
                    <SelectItem key={p} value={p}>
                      <div>
                        <span className="font-medium">{p}</span>
                        <span className="ml-2 text-xs text-muted-foreground">
                          — {shareProfileDesc[p]}
                        </span>
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">Label (optional)</label>
              <Input
                placeholder="e.g. shared-utils, backend-api"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
              />
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                type="button"
                onClick={() => setCreateOpen(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={createLink.isPending}>
                {createLink.isPending ? "Creating…" : "Create Link"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* ── Edit Link Dialog ─────────────────────────────────────────────── */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit Cross-Repo Link</DialogTitle>
            <DialogDescription>
              Update the share profile or label for this link.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">Share Profile</label>
              <Select
                value={editProfile}
                onValueChange={(v) => setEditProfile(v as ShareProfile)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(
                    ["full", "symbols", "metadata", "none"] as ShareProfile[]
                  ).map((p) => (
                    <SelectItem key={p} value={p}>
                      <div>
                        <span className="font-medium">{p}</span>
                        <span className="ml-2 text-xs text-muted-foreground">
                          — {shareProfileDesc[p]}
                        </span>
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">Label</label>
              <Input
                placeholder="e.g. shared-utils"
                value={editLabel}
                onChange={(e) => setEditLabel(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleUpdate} disabled={updateLink.isPending}>
              {updateLink.isPending ? "Saving…" : "Save Changes"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
