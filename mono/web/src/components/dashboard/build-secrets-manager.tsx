// ─── Build Secrets Manager ───────────────────────────────────────────────────
// Repo-scoped secret management for sandbox build validation.
// Users add key=value secrets here (e.g. JAVA_HOME, DATABASE_URL).
// Values are E2E encrypted in the user's keychain, tagged with the repo ID.
// Only secret *names* are ever shown — values are never exposed in the UI.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useCallback } from "react";
import {
  Plus,
  Trash2,
  Loader2,
  KeyRound,
  Shield,
  Eye,
  EyeOff,
  AlertTriangle,
  Clock,
  Lock,
} from "lucide-react";
import { useBuildSecrets } from "@/lib/api/queries";
import { usePutBuildSecret, useDeleteBuildSecret } from "@/lib/api/mutations";
import type { BuildSecretEntry } from "@/types/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function timeAgo(dateStr: string): string {
  try {
    const diff = Date.now() - new Date(dateStr).getTime();
    const secs = Math.floor(diff / 1000);
    if (secs < 60) return "just now";
    const mins = Math.floor(secs / 60);
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    const days = Math.floor(hrs / 24);
    return `${days}d ago`;
  } catch {
    return "";
  }
}

/** Well-known env var patterns with descriptions. */
const COMMON_ENV_VARS: { name: string; hint: string }[] = [
  { name: "JAVA_HOME", hint: "JDK installation path" },
  { name: "GRADLE_HOME", hint: "Gradle installation path" },
  { name: "MAVEN_HOME", hint: "Maven installation path" },
  { name: "NODE_PATH", hint: "Node.js module resolution path" },
  { name: "PYTHONPATH", hint: "Python module search path" },
  { name: "GOPATH", hint: "Go workspace path" },
  { name: "CMAKE_PREFIX_PATH", hint: "CMake install prefixes" },
  { name: "DATABASE_URL", hint: "Database connection string" },
  { name: "AWS_ACCESS_KEY_ID", hint: "AWS access key" },
  { name: "AWS_SECRET_ACCESS_KEY", hint: "AWS secret key" },
  { name: "AWS_REGION", hint: "AWS region" },
];

// ─────────────────────────────────────────────────────────────────────────────
// Main Component
// ─────────────────────────────────────────────────────────────────────────────

interface BuildSecretsManagerProps {
  repoId: string;
}

export function BuildSecretsManager({ repoId }: BuildSecretsManagerProps) {
  const { data: secrets, isLoading } = useBuildSecrets(repoId);
  const putSecret = usePutBuildSecret();
  const deleteSecret = useDeleteBuildSecret();

  // Add-secret dialog state
  const [showAddDialog, setShowAddDialog] = useState(false);
  const [newName, setNewName] = useState("");
  const [newValue, setNewValue] = useState("");
  const [showValue, setShowValue] = useState(false);

  // Delete confirmation
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  // ── Handlers ────────────────────────────────────────────────────────────

  const handleAdd = useCallback(() => {
    if (!newName.trim() || !newValue.trim()) return;

    putSecret.mutate(
      { repoId, data: { name: newName.trim().toUpperCase(), value: newValue } },
      {
        onSuccess: () => {
          setNewName("");
          setNewValue("");
          setShowValue(false);
          setShowAddDialog(false);
        },
      },
    );
  }, [putSecret, repoId, newName, newValue]);

  const handleDelete = useCallback(
    (name: string) => {
      deleteSecret.mutate(
        { repoId, name },
        {
          onSuccess: () => setDeleteTarget(null),
        },
      );
    },
    [deleteSecret, repoId],
  );

  const handleQuickAdd = useCallback(
    (name: string) => {
      setNewName(name);
      setShowAddDialog(true);
    },
    [],
  );

  // ── Loading state ───────────────────────────────────────────────────────

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <Skeleton className="h-5 w-40" />
          <Skeleton className="mt-1 h-4 w-64" />
        </CardHeader>
        <CardContent>
          <Skeleton className="h-24" />
        </CardContent>
      </Card>
    );
  }

  const secretList: BuildSecretEntry[] = secrets ?? [];

  // Which common env vars haven't been added yet?
  const existingNames = new Set(secretList.map((s) => s.name));
  const suggestions = COMMON_ENV_VARS.filter((v) => !existingNames.has(v.name));

  return (
    <>
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="flex items-center gap-2 text-base">
                <KeyRound className="h-4 w-4" />
                Build Secrets
              </CardTitle>
              <CardDescription className="mt-1">
                Environment variables injected into sandbox builds for this
                repository. Values are end-to-end encrypted in your keychain.
              </CardDescription>
            </div>
            <Button size="sm" onClick={() => setShowAddDialog(true)}>
              <Plus className="mr-1 h-4 w-4" />
              Add Secret
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {secretList.length === 0 ? (
            <div className="rounded-lg border border-dashed p-6 text-center">
              <Lock className="mx-auto mb-2 h-8 w-8 text-muted-foreground/40" />
              <p className="text-sm font-medium text-muted-foreground">
                No build secrets configured
              </p>
              <p className="mt-1 text-xs text-muted-foreground/70">
                Add environment variables that your project needs to build
                (e.g. JAVA_HOME, DATABASE_URL, API keys).
              </p>

              {/* Quick-add suggestions */}
              {suggestions.length > 0 && (
                <div className="mt-4">
                  <p className="mb-2 text-xs font-medium text-muted-foreground">
                    Common variables:
                  </p>
                  <div className="flex flex-wrap justify-center gap-1.5">
                    {suggestions.slice(0, 6).map((v) => (
                      <button
                        key={v.name}
                        onClick={() => handleQuickAdd(v.name)}
                        className="rounded-md border bg-background px-2 py-1 font-mono text-[11px] transition-colors hover:bg-muted"
                        title={v.hint}
                      >
                        {v.name}
                      </button>
                    ))}
                  </div>
                </div>
              )}
            </div>
          ) : (
            <div className="space-y-2">
              {secretList.map((secret) => (
                <div
                  key={secret.name}
                  className="flex items-center justify-between rounded-lg border bg-background px-4 py-3"
                >
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-amber-100 dark:bg-amber-900/30">
                      <KeyRound className="h-4 w-4 text-amber-600 dark:text-amber-400" />
                    </div>
                    <div className="min-w-0">
                      <p className="truncate font-mono text-sm font-medium">
                        {secret.name}
                      </p>
                      <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <Shield className="h-3 w-3" />
                        <span>Encrypted</span>
                        <span>·</span>
                        <span>v{secret.version}</span>
                        {secret.updated_at && (
                          <>
                            <span>·</span>
                            <Clock className="h-3 w-3" />
                            <span>{timeAgo(secret.updated_at)}</span>
                          </>
                        )}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge
                      variant="outline"
                      className="text-[10px] font-mono"
                    >
                      ••••••••
                    </Badge>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground hover:text-red-600"
                      onClick={() => setDeleteTarget(secret.name)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              ))}

              {/* Quick-add suggestions below the list */}
              {suggestions.length > 0 && (
                <div className="mt-3 rounded-lg bg-muted/50 p-3">
                  <p className="mb-2 text-xs font-medium text-muted-foreground">
                    Suggestions:
                  </p>
                  <div className="flex flex-wrap gap-1.5">
                    {suggestions.slice(0, 5).map((v) => (
                      <button
                        key={v.name}
                        onClick={() => handleQuickAdd(v.name)}
                        className="rounded-md border bg-background px-2 py-0.5 font-mono text-[10px] transition-colors hover:bg-muted"
                        title={v.hint}
                      >
                        + {v.name}
                      </button>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Security notice */}
          <div className="mt-4 flex items-start gap-2 rounded-md bg-blue-50 p-3 dark:bg-blue-950/30">
            <Shield className="mt-0.5 h-4 w-4 shrink-0 text-blue-600 dark:text-blue-400" />
            <div className="text-xs text-blue-700 dark:text-blue-300">
              <p className="font-medium">How build secrets work</p>
              <p className="mt-0.5 text-blue-600/80 dark:text-blue-300/70">
                Secrets are encrypted with your personal keychain key (AES-256-GCM)
                and stored per-repo. During sandbox builds, values are injected as
                container environment variables and destroyed when the container exits.
                Secret values are never logged or exposed in the UI.
              </p>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* ── Add Secret Dialog ──────────────────────────────────────────────── */}
      <Dialog open={showAddDialog} onOpenChange={setShowAddDialog}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <KeyRound className="h-5 w-5" />
              Add Build Secret
            </DialogTitle>
            <DialogDescription>
              Add an environment variable for sandbox builds. The value is
              encrypted and stored in your keychain.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="secret-name">Name</Label>
              <Input
                id="secret-name"
                placeholder="e.g. JAVA_HOME"
                value={newName}
                onChange={(e) => setNewName(e.target.value.toUpperCase())}
                className="font-mono"
                autoFocus
              />
              <p className="text-xs text-muted-foreground">
                Use SCREAMING_SNAKE_CASE (e.g. DATABASE_URL, AWS_REGION)
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="secret-value">Value</Label>
              <div className="relative">
                <Input
                  id="secret-value"
                  type={showValue ? "text" : "password"}
                  placeholder="Enter secret value..."
                  value={newValue}
                  onChange={(e) => setNewValue(e.target.value)}
                  className="pr-10 font-mono"
                />
                <button
                  type="button"
                  onClick={() => setShowValue(!showValue)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                >
                  {showValue ? (
                    <EyeOff className="h-4 w-4" />
                  ) : (
                    <Eye className="h-4 w-4" />
                  )}
                </button>
              </div>
            </div>

            {/* Existing secret warning */}
            {existingNames.has(newName.trim()) && (
              <div className="flex items-center gap-2 rounded-md bg-amber-50 p-2 text-xs text-amber-700 dark:bg-amber-950/30 dark:text-amber-300">
                <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                A secret with this name already exists. Saving will overwrite it.
              </div>
            )}
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setShowAddDialog(false);
                setNewName("");
                setNewValue("");
                setShowValue(false);
              }}
            >
              Cancel
            </Button>
            <Button
              onClick={handleAdd}
              disabled={
                !newName.trim() || !newValue.trim() || putSecret.isPending
              }
            >
              {putSecret.isPending ? (
                <Loader2 className="mr-1 h-4 w-4 animate-spin" />
              ) : (
                <Shield className="mr-1 h-4 w-4" />
              )}
              Encrypt &amp; Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ── Delete Confirmation Dialog ─────────────────────────────────────── */}
      <Dialog
        open={!!deleteTarget}
        onOpenChange={(open: boolean) => !open && setDeleteTarget(null)}
      >
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <AlertTriangle className="h-5 w-5 text-red-500" />
              Delete Build Secret
            </DialogTitle>
            <DialogDescription>
              Are you sure you want to delete{" "}
              <code className="rounded bg-muted px-1 py-0.5 font-mono text-sm font-semibold">
                {deleteTarget}
              </code>
              ? Builds that depend on this variable may fail.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => deleteTarget && handleDelete(deleteTarget)}
              disabled={deleteSecret.isPending}
            >
              {deleteSecret.isPending ? (
                <Loader2 className="mr-1 h-4 w-4 animate-spin" />
              ) : (
                <Trash2 className="mr-1 h-4 w-4" />
              )}
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
