// ─── Keychain Vault Settings ─────────────────────────────────────────────────
// AES-256-GCM + HKDF-SHA256 • BIP39 recovery phrase • Per-secret DEK wrapping
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  CheckCircle,
  XCircle,
  Loader2,
  Eye,
  EyeOff,
  Trash2,
  Plus,
  Shield,
  ShieldCheck,
  KeyRound,
  RefreshCw,
  Copy,
  Lock,
  Unlock,
  AlertTriangle,
  Clock,
  Fingerprint,
  HardDrive,
} from "lucide-react";
import { useKeychainStatus, useKeychainSecrets, useKeychainAuditLog, queryKeys } from "@/lib/api/queries";
import {
  useInitKeychain,
  usePutKeychainSecret,
  useDeleteKeychainSecret,
  useRotateKeychainKeys,
  useRecoverKeychain,
} from "@/lib/api/mutations";
import type { KeychainSecretListEntry, KeychainAuditLogEntry } from "@/types/api";
import api from "@/lib/api/client";
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
// Main Component
// ─────────────────────────────────────────────────────────────────────────────

export function KeychainSettings() {
  const { data: status, isLoading: statusLoading } = useKeychainStatus();
  const { data: secrets, isLoading: secretsLoading } = useKeychainSecrets();
  const { data: auditLog, isLoading: auditLoading } = useKeychainAuditLog(50);

  const initKeychain = useInitKeychain();
  const rotateKeys = useRotateKeychainKeys();
  const qc = useQueryClient();

  const [recoveryPhrase, setRecoveryPhrase] = useState<string | null>(null);
  const [showRecoveryDialog, setShowRecoveryDialog] = useState(false);
  const [showRecoverInput, setShowRecoverInput] = useState(false);
  const [showAddSecret, setShowAddSecret] = useState(false);
  const [showRotateConfirm, setShowRotateConfirm] = useState(false);
  const [showAuditLog, setShowAuditLog] = useState(false);
  const [copied, setCopied] = useState(false);

  const handleInit = useCallback(() => {
    initKeychain.mutate(undefined, {
      onSuccess: (data: { recovery_phrase: string }) => {
        setRecoveryPhrase(data.recovery_phrase);
        setShowRecoveryDialog(true);
      },
    });
  }, [initKeychain]);

  const handleCloseRecoveryDialog = useCallback(() => {
    setShowRecoveryDialog(false);
    setRecoveryPhrase(null);
    // Ensure status is fresh after the user has acknowledged the phrase.
    qc.invalidateQueries({ queryKey: queryKeys.keychainStatus });
  }, [qc]);

  const handleCopyPhrase = useCallback(() => {
    if (!recoveryPhrase) return;
    navigator.clipboard.writeText(recoveryPhrase).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [recoveryPhrase]);

  const handleRotate = useCallback(() => {
    rotateKeys.mutate(undefined, {
      onSuccess: () => setShowRotateConfirm(false),
    });
  }, [rotateKeys]);

  // ── Determine which content to render based on state ──────────────────

  const isInitialized = status?.initialized ?? false;

  // ── Single Return ─────────────────────────────────────────────────────
  // All dialogs live outside the conditional content so they are never
  // unmounted by a status transition. This eliminates race conditions
  // between query invalidation and dialog visibility.

  return (
    <>
      {/* ── Conditional Main Content ─────────────────────────────────── */}
      {statusLoading ? (
        <Card className="max-w-3xl">
          <CardHeader>
            <Skeleton className="h-6 w-48" />
            <Skeleton className="h-4 w-96" />
          </CardHeader>
          <CardContent className="space-y-4">
            <Skeleton className="h-40 w-full" />
          </CardContent>
        </Card>
      ) : !isInitialized ? (
        <Card className="max-w-3xl">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Shield className="h-5 w-5" />
              Secure Vault
            </CardTitle>
            <CardDescription>
              End-to-end encrypted secret storage. Your secrets are encrypted with
              AES-256-GCM using per-secret keys wrapped by your master encryption
              key — the server never stores plaintext.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-6">
            {/* Architecture overview */}
            <div className="rounded-lg border bg-muted/30 p-4 space-y-3">
              <p className="text-sm font-medium">How it works</p>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <FeatureCard
                  icon={<Fingerprint className="h-4 w-4 text-violet-500" />}
                  title="BIP39 Recovery"
                  description="12-word seed phrase you control — back it up safely"
                />
                <FeatureCard
                  icon={<Lock className="h-4 w-4 text-blue-500" />}
                  title="AES-256-GCM"
                  description="Per-secret encryption keys wrapped by your master key"
                />
                <FeatureCard
                  icon={<HardDrive className="h-4 w-4 text-emerald-500" />}
                  title="Zero-Knowledge"
                  description="Server stores ciphertext only — no plaintext access"
                />
              </div>
            </div>

            {/* Init or Recover */}
            <div className="flex flex-col sm:flex-row gap-3">
              <Button
                onClick={handleInit}
                disabled={initKeychain.isPending}
                className="flex-1"
              >
                {initKeychain.isPending ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    Creating Vault…
                  </>
                ) : (
                  <>
                    <Shield className="mr-2 h-4 w-4" />
                    Create New Vault
                  </>
                )}
              </Button>
              <Button
                variant="outline"
                onClick={() => setShowRecoverInput(true)}
                className="flex-1"
              >
                <Unlock className="mr-2 h-4 w-4" />
                Recover with Phrase
              </Button>
            </div>

            {initKeychain.isError && (
              <ErrorBanner message={(initKeychain.error as Error).message ?? "Failed to create vault"} />
            )}
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-4 max-w-3xl">
          {/* ── Security Dashboard Card ──────────────────────────────── */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <ShieldCheck className="h-5 w-5 text-emerald-500" />
                Secure Vault
              </CardTitle>
              <CardDescription>
                Your encrypted vault is active. Secrets are protected with AES-256-GCM
                envelope encryption — each secret has its own data encryption key.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                <SecurityStatCard
                  icon={<ShieldCheck className="h-4 w-4 text-emerald-500" />}
                  label="Status"
                  value="Active"
                  valueClassName="text-emerald-600 dark:text-emerald-400"
                />
                <SecurityStatCard
                  icon={<KeyRound className="h-4 w-4 text-blue-500" />}
                  label="Secrets"
                  value={String(status.secret_count)}
                />
                <SecurityStatCard
                  icon={<RefreshCw className="h-4 w-4 text-violet-500" />}
                  label="Key Version"
                  value={`v${status.key_version}`}
                />
                <SecurityStatCard
                  icon={<Lock className="h-4 w-4 text-amber-500" />}
                  label="Encryption"
                  value="AES-256"
                />
              </div>
            </CardContent>
          </Card>

          {/* ── Secrets Card ─────────────────────────────────────────── */}
          <Card>
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <CardTitle className="text-base">Stored Secrets</CardTitle>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowAddSecret(true)}
                >
                  <Plus className="mr-1 h-3.5 w-3.5" />
                  Add Secret
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              {secretsLoading ? (
                <div className="space-y-2">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} className="h-14 w-full" />
                  ))}
                </div>
              ) : !secrets?.length ? (
                <div className="rounded-lg border border-dashed p-8 text-center">
                  <Lock className="mx-auto h-8 w-8 text-muted-foreground/40" />
                  <p className="mt-2 text-sm text-muted-foreground">
                    No secrets stored yet. Add API keys, tokens, or passwords to
                    keep them encrypted in your vault.
                  </p>
                </div>
              ) : (
                <div className="space-y-2">
                  {secrets.map((secret: KeychainSecretListEntry) => (
                    <SecretRow key={secret.name} secret={secret} />
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          {/* ── Vault Management Card ────────────────────────────────── */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base">Vault Management</CardTitle>
              <CardDescription>
                Rotate encryption keys, recover your vault, or view the access audit log.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowRotateConfirm(true)}
                  disabled={rotateKeys.isPending}
                >
                  {rotateKeys.isPending ? (
                    <>
                      <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />
                      Rotating…
                    </>
                  ) : (
                    <>
                      <RefreshCw className="mr-1 h-3.5 w-3.5" />
                      Rotate Keys
                    </>
                  )}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowRecoverInput(true)}
                >
                  <Unlock className="mr-1 h-3.5 w-3.5" />
                  Recover from Phrase
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowAuditLog(!showAuditLog)}
                >
                  <Clock className="mr-1 h-3.5 w-3.5" />
                  {showAuditLog ? "Hide" : "View"} Audit Log
                </Button>
              </div>
              {rotateKeys.isSuccess && (
                <p className="flex items-center gap-1.5 text-xs text-emerald-600 dark:text-emerald-400">
                  <CheckCircle className="h-3.5 w-3.5" />
                  Keys rotated successfully — all secrets re-encrypted
                </p>
              )}
              {rotateKeys.isError && (
                <ErrorBanner message="Key rotation failed. Please try again." />
              )}

              {/* ── Inline Audit Log ─────────────────────────────────── */}
              {showAuditLog && (
                <AuditLogPanel entries={auditLog} isLoading={auditLoading} />
              )}
            </CardContent>
          </Card>

          {/* ── Encryption Details Card ──────────────────────────────── */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-base flex items-center gap-2">
                <Fingerprint className="h-4 w-4" />
                Encryption Details
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <FeatureCard
                  icon={<Fingerprint className="h-4 w-4 text-violet-500" />}
                  title="BIP39 Recovery"
                  description="12-word seed phrase you control — back it up safely"
                />
                <FeatureCard
                  icon={<Lock className="h-4 w-4 text-blue-500" />}
                  title="Envelope Encryption"
                  description="Per-secret DEK wrapped by master key via HKDF-SHA256"
                />
                <FeatureCard
                  icon={<HardDrive className="h-4 w-4 text-emerald-500" />}
                  title="Zero-Knowledge Server"
                  description="Server stores ciphertext only — never sees plaintext"
                />
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      {/* ── Dialogs (always mounted — never torn down by state changes) ── */}

      <RecoveryPhraseDialog
        open={showRecoveryDialog}
        phrase={recoveryPhrase}
        copied={copied}
        onCopy={handleCopyPhrase}
        onClose={handleCloseRecoveryDialog}
      />

      <RecoverFromPhraseDialog
        open={showRecoverInput}
        onClose={() => setShowRecoverInput(false)}
      />

      <AddSecretDialog
        open={showAddSecret}
        onClose={() => setShowAddSecret(false)}
      />

      <RotateKeysDialog
        open={showRotateConfirm}
        isPending={rotateKeys.isPending}
        onConfirm={handleRotate}
        onClose={() => setShowRotateConfirm(false)}
      />
    </>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-components
// ─────────────────────────────────────────────────────────────────────────────

function SecurityStatCard({
  icon,
  label,
  value,
  valueClassName,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  valueClassName?: string;
}) {
  return (
    <div className="rounded-lg border bg-background p-3 space-y-1.5">
      <div className="flex items-center gap-1.5">
        {icon}
        <p className="text-[11px] text-muted-foreground">{label}</p>
      </div>
      <p className={`text-sm font-semibold ${valueClassName ?? ""}`}>{value}</p>
    </div>
  );
}

// ── Audit Log Panel ─────────────────────────────────────────────────────────

const AUDIT_ACTION_LABELS: Record<string, { label: string; color: string }> = {
  keychain_init:  { label: "Vault Created",   color: "text-emerald-600 dark:text-emerald-400" },
  secret_put:     { label: "Secret Stored",   color: "text-blue-600 dark:text-blue-400" },
  secret_get:     { label: "Secret Read",     color: "text-muted-foreground" },
  secret_delete:  { label: "Secret Deleted",  color: "text-red-600 dark:text-red-400" },
  sync:           { label: "Sync",            color: "text-violet-600 dark:text-violet-400" },
  key_rotate:     { label: "Keys Rotated",    color: "text-amber-600 dark:text-amber-400" },
  recovery:       { label: "Vault Recovered", color: "text-orange-600 dark:text-orange-400" },
};

function AuditLogPanel({
  entries,
  isLoading,
}: {
  entries?: KeychainAuditLogEntry[];
  isLoading: boolean;
}) {
  if (isLoading) {
    return (
      <div className="space-y-2 pt-2">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-8 w-full" />
        ))}
      </div>
    );
  }

  if (!entries?.length) {
    return (
      <div className="rounded-lg border border-dashed p-6 text-center mt-2">
        <Clock className="mx-auto h-6 w-6 text-muted-foreground/40" />
        <p className="mt-1.5 text-xs text-muted-foreground">
          No audit events recorded yet
        </p>
      </div>
    );
  }

  return (
    <div className="mt-2 rounded-lg border overflow-hidden">
      <div className="max-h-64 overflow-y-auto">
        <table className="w-full text-xs">
          <thead className="bg-muted/50 sticky top-0">
            <tr>
              <th className="text-left px-3 py-2 font-medium text-muted-foreground">Action</th>
              <th className="text-left px-3 py-2 font-medium text-muted-foreground">Secret</th>
              <th className="text-left px-3 py-2 font-medium text-muted-foreground hidden sm:table-cell">IP</th>
              <th className="text-right px-3 py-2 font-medium text-muted-foreground">Time</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {entries.map((entry) => {
              const meta = AUDIT_ACTION_LABELS[entry.action] ?? {
                label: entry.action,
                color: "text-muted-foreground",
              };
              return (
                <tr key={entry.id} className="hover:bg-muted/30 transition-colors">
                  <td className={`px-3 py-2 font-medium ${meta.color}`}>
                    {meta.label}
                  </td>
                  <td className="px-3 py-2 font-mono text-muted-foreground truncate max-w-[120px]">
                    {entry.secret_name || "—"}
                  </td>
                  <td className="px-3 py-2 text-muted-foreground hidden sm:table-cell">
                    {entry.ip_addr || "—"}
                  </td>
                  <td className="px-3 py-2 text-right text-muted-foreground whitespace-nowrap">
                    {formatRelativeTime(entry.created_at)}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function FeatureCard({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
}) {
  return (
    <div className="rounded-md border bg-background p-3 space-y-1">
      <div className="flex items-center gap-2">
        {icon}
        <p className="text-xs font-medium">{title}</p>
      </div>
      <p className="text-[11px] text-muted-foreground leading-relaxed">
        {description}
      </p>
    </div>
  );
}

// ── Secret Row ──────────────────────────────────────────────────────────────

function SecretRow({ secret }: { secret: KeychainSecretListEntry }) {
  const [revealed, setRevealed] = useState(false);
  const [value, setValue] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [copied, setCopied] = useState(false);
  const deleteSecret = useDeleteKeychainSecret();

  const handleReveal = useCallback(async () => {
    if (revealed) {
      setRevealed(false);
      setValue(null);
      return;
    }
    setLoading(true);
    try {
      const res = await api.keychain.getSecret(secret.name);
      setValue(res.value);
      setRevealed(true);
    } catch {
      /* ignore */
    } finally {
      setLoading(false);
    }
  }, [revealed, secret.name]);

  const handleCopy = useCallback(async () => {
    let text = value;
    if (!text) {
      try {
        const res = await api.keychain.getSecret(secret.name);
        text = res.value;
      } catch {
        return;
      }
    }
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [value, secret.name]);

  const handleDelete = useCallback(() => {
    if (!confirm(`Delete secret "${secret.name}"? This cannot be undone.`)) return;
    deleteSecret.mutate(secret.name);
  }, [deleteSecret, secret.name]);

  const timeAgo = formatRelativeTime(secret.updated_at);

  return (
    <div className="flex items-center justify-between rounded-lg border p-3 hover:bg-muted/30 transition-colors">
      <div className="flex items-center gap-3 min-w-0 flex-1">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted">
          <KeyRound className="h-4 w-4 text-muted-foreground" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium truncate">{secret.name}</p>
            {secret.category && (
              <Badge variant="secondary" className="text-[10px] shrink-0">
                {secret.category}
              </Badge>
            )}
          </div>
          {revealed && value ? (
            <code className="block truncate text-xs font-mono text-muted-foreground mt-0.5 max-w-sm">
              {value}
            </code>
          ) : (
            <p className="flex items-center gap-1 text-[11px] text-muted-foreground mt-0.5">
              <Clock className="h-3 w-3" />
              {timeAgo} • v{secret.version}
            </p>
          )}
        </div>
      </div>

      <div className="flex items-center gap-1 shrink-0 ml-2">
        <Button
          variant="ghost"
          size="sm"
          onClick={handleReveal}
          disabled={loading}
          className="h-8 w-8 p-0"
        >
          {loading ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : revealed ? (
            <EyeOff className="h-3.5 w-3.5" />
          ) : (
            <Eye className="h-3.5 w-3.5" />
          )}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={handleCopy}
          className="h-8 w-8 p-0"
        >
          {copied ? (
            <CheckCircle className="h-3.5 w-3.5 text-emerald-500" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={handleDelete}
          disabled={deleteSecret.isPending}
          className="h-8 w-8 p-0 text-destructive hover:text-destructive"
        >
          {deleteSecret.isPending ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Trash2 className="h-3.5 w-3.5" />
          )}
        </Button>
      </div>
    </div>
  );
}

// ── Recovery Phrase Dialog ───────────────────────────────────────────────────

function RecoveryPhraseDialog({
  open,
  phrase,
  copied,
  onCopy,
  onClose,
}: {
  open: boolean;
  phrase: string | null;
  copied: boolean;
  onCopy: () => void;
  onClose: () => void;
}) {
  const [acknowledged, setAcknowledged] = useState(false);
  const words = phrase?.split(" ") ?? [];

  return (
    <Dialog open={open} onOpenChange={(o: boolean) => !o && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="h-5 w-5 text-amber-500" />
            Recovery Phrase
          </DialogTitle>
          <DialogDescription>
            Write down these 12 words in order. This is the <strong>only way</strong> to
            recover your vault if you lose access. It will <strong>never</strong> be
            shown again.
          </DialogDescription>
        </DialogHeader>

        {/* Word grid */}
        <div className="grid grid-cols-3 gap-2 rounded-lg border bg-muted/30 p-4">
          {words.map((word, i) => (
            <div
              key={i}
              className="flex items-center gap-2 rounded-md bg-background px-2.5 py-1.5 border"
            >
              <span className="text-[10px] font-mono text-muted-foreground w-4 text-right">
                {i + 1}
              </span>
              <span className="text-sm font-mono font-medium">{word}</span>
            </div>
          ))}
        </div>

        {/* Copy button */}
        <Button variant="outline" size="sm" onClick={onCopy} className="w-full">
          {copied ? (
            <>
              <CheckCircle className="mr-2 h-4 w-4 text-emerald-500" />
              Copied
            </>
          ) : (
            <>
              <Copy className="mr-2 h-4 w-4" />
              Copy to Clipboard
            </>
          )}
        </Button>

        {/* Warning */}
        <div className="rounded-md border border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-950 p-3">
          <p className="text-xs text-amber-700 dark:text-amber-300 leading-relaxed">
            <strong>⚠ Important:</strong> Store this phrase in a safe, offline location
            (e.g. written on paper, password manager). Anyone with this phrase can
            recover your vault.
          </p>
        </div>

        <DialogFooter>
          <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
            <input
              type="checkbox"
              checked={acknowledged}
              onChange={(e) => setAcknowledged(e.target.checked)}
              className="rounded border-muted-foreground"
            />
            I have written down my recovery phrase
          </label>
          <Button disabled={!acknowledged} onClick={onClose}>
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ── Recover from Phrase Dialog ──────────────────────────────────────────────

function RecoverFromPhraseDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [phrase, setPhrase] = useState("");
  const recover = useRecoverKeychain();

  const handleRecover = useCallback(() => {
    const trimmed = phrase.trim().toLowerCase();
    if (!trimmed) return;
    recover.mutate(
      { recovery_phrase: trimmed },
      {
        onSuccess: () => {
          setPhrase("");
          onClose();
        },
      },
    );
  }, [phrase, recover, onClose]);

  const wordCount = phrase.trim().split(/\s+/).filter(Boolean).length;

  return (
    <Dialog
      open={open}
      onOpenChange={(o: boolean) => {
        if (!o) {
          setPhrase("");
          onClose();
        }
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Unlock className="h-5 w-5" />
            Recover Vault
          </DialogTitle>
          <DialogDescription>
            Enter your 12-word BIP39 recovery phrase to restore access to your
            encrypted vault.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <Label htmlFor="recovery-phrase">Recovery Phrase</Label>
          <textarea
            id="recovery-phrase"
            rows={3}
            value={phrase}
            onChange={(e) => setPhrase(e.target.value)}
            placeholder="word1 word2 word3 word4 word5 word6 word7 word8 word9 word10 word11 word12"
            className="flex min-h-[80px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          />
          <p className="text-xs text-muted-foreground">
            {wordCount}/12 words entered
          </p>
        </div>

        {recover.isError && (
          <ErrorBanner message="Recovery failed — check that the phrase is correct." />
        )}

        {recover.isSuccess && (
          <p className="flex items-center gap-1.5 text-xs text-emerald-600 dark:text-emerald-400">
            <CheckCircle className="h-3.5 w-3.5" />
            Vault recovered successfully
          </p>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            onClick={handleRecover}
            disabled={wordCount !== 12 || recover.isPending}
          >
            {recover.isPending ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Recovering…
              </>
            ) : (
              "Recover Vault"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ── Add Secret Dialog ───────────────────────────────────────────────────────

function AddSecretDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [category, setCategory] = useState("");
  const [showValue, setShowValue] = useState(false);
  const putSecret = usePutKeychainSecret();

  const handleSave = useCallback(() => {
    if (!name.trim() || !value.trim()) return;
    putSecret.mutate(
      {
        name: name.trim(),
        value: value.trim(),
        category: category.trim() || undefined,
      },
      {
        onSuccess: () => {
          setName("");
          setValue("");
          setCategory("");
          setShowValue(false);
          onClose();
        },
      },
    );
  }, [name, value, category, putSecret, onClose]);

  return (
    <Dialog
      open={open}
      onOpenChange={(o: boolean) => {
        if (!o) {
          setName("");
          setValue("");
          setCategory("");
          setShowValue(false);
          onClose();
        }
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Plus className="h-5 w-5" />
            Add Secret
          </DialogTitle>
          <DialogDescription>
            Store a new encrypted secret in your vault. The value will be
            encrypted with a unique data encryption key (AES-256-GCM).
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="secret-name">Name</Label>
            <Input
              id="secret-name"
              placeholder="e.g. github-token, openai-api-key"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="secret-value">Value</Label>
            <div className="relative">
              <Input
                id="secret-value"
                type={showValue ? "text" : "password"}
                placeholder="Enter secret value…"
                value={value}
                onChange={(e) => setValue(e.target.value)}
                className="pr-10"
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

          <div className="space-y-2">
            <Label htmlFor="secret-category">
              Category{" "}
              <span className="text-muted-foreground font-normal">(optional)</span>
            </Label>
            <Input
              id="secret-category"
              placeholder="e.g. api-key, token, password, custom"
              value={category}
              onChange={(e) => setCategory(e.target.value)}
            />
          </div>
        </div>

        {putSecret.isError && (
          <ErrorBanner message="Failed to store secret. Please try again." />
        )}

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            onClick={handleSave}
            disabled={!name.trim() || !value.trim() || putSecret.isPending}
          >
            {putSecret.isPending ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Encrypting…
              </>
            ) : (
              <>
                <Lock className="mr-2 h-4 w-4" />
                Store Secret
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ── Rotate Keys Confirmation Dialog ─────────────────────────────────────────

function RotateKeysDialog({
  open,
  isPending,
  onConfirm,
  onClose,
}: {
  open: boolean;
  isPending: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(o: boolean) => !o && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <RefreshCw className="h-5 w-5" />
            Rotate Encryption Keys
          </DialogTitle>
          <DialogDescription>
            This will generate new encryption keys and re-encrypt all your
            secrets. Your recovery phrase remains the same.
          </DialogDescription>
        </DialogHeader>

        <div className="rounded-md border border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-950 p-3">
          <p className="text-xs text-amber-700 dark:text-amber-300 leading-relaxed">
            Key rotation is recommended periodically or after any suspected
            compromise. All secrets will be re-encrypted with new keys.
          </p>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={onConfirm} disabled={isPending}>
            {isPending ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Rotating…
              </>
            ) : (
              "Rotate Keys"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ── Helpers ─────────────────────────────────────────────────────────────────

function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-red-200 bg-red-50 dark:border-red-800 dark:bg-red-950 p-3">
      <XCircle className="h-4 w-4 text-red-500 shrink-0 mt-0.5" />
      <p className="text-xs text-red-700 dark:text-red-300">{message}</p>
    </div>
  );
}

function formatRelativeTime(iso: string): string {
  try {
    const date = new Date(iso);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMin = Math.floor(diffMs / 60_000);
    if (diffMin < 1) return "just now";
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    const diffDay = Math.floor(diffHr / 24);
    if (diffDay < 30) return `${diffDay}d ago`;
    return date.toLocaleDateString();
  } catch {
    return iso;
  }
}
