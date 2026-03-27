// ─── VCS Settings ────────────────────────────────────────────────────────────
// Per-user version control system platform configuration.
// Secrets (tokens, passwords) → encrypted file vault (AES-256-GCM).
// Non-secret config (URLs, usernames) → application database.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import {
  CheckCircle,
  XCircle,
  Loader2,
  Eye,
  EyeOff,
  Trash2,
  GitBranch,
  ExternalLink,
  ShieldCheck,
  Globe,
  KeyRound,
  Info,
  AlertTriangle,
  Copy,
  ChevronDown,
  ChevronRight,
} from "lucide-react";
import { useVCSPlatforms, useVCSTokenCapabilities } from "@/lib/api/queries";
import {
  useConfigureVCS,
  useDeleteVCS,
  useTestVCS,
} from "@/lib/api/mutations";
import type { VCSPlatformInfo, VCSFieldInfo, VCSTestResult, VCSTokenCapability } from "@/types/api";
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
import { Skeleton } from "@/components/ui/skeleton";
import { getVCSIcon } from "@/components/icons/brand-icons";

// Platform brand colors and docs links
const platformMeta: Record<
  string,
  { color: string; docsUrl: string }
> = {
  github: {
    color: "border-l-zinc-800 dark:border-l-zinc-200",
    docsUrl: "https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/creating-a-personal-access-token",
  },
  gitlab: {
    color: "border-l-orange-500",
    docsUrl: "https://docs.gitlab.com/ee/user/profile/personal_access_tokens.html",
  },
  bitbucket: {
    color: "border-l-blue-500",
    docsUrl: "https://support.atlassian.com/bitbucket-cloud/docs/create-an-app-password/",
  },
  azure_devops: {
    color: "border-l-sky-500",
    docsUrl: "https://learn.microsoft.com/en-us/azure/devops/organizations/accounts/use-personal-access-tokens-to-authenticate",
  },
};

export function VCSSettings() {
  const { data: platforms, isLoading } = useVCSPlatforms();
  const { data: tokenCaps } = useVCSTokenCapabilities();
  const configureVCS = useConfigureVCS();
  const deleteVCS = useDeleteVCS();
  const testVCS = useTestVCS();

  const [fieldValues, setFieldValues] = useState<Record<string, Record<string, string>>>({});
  const [showSecrets, setShowSecrets] = useState<Record<string, Record<string, boolean>>>({});
  const [testResults, setTestResults] = useState<Record<string, VCSTestResult>>({});

  const setFieldValue = (platform: string, key: string, value: string) => {
    setFieldValues((prev) => ({
      ...prev,
      [platform]: { ...prev[platform], [key]: value },
    }));
  };

  const toggleSecret = (platform: string, key: string) => {
    setShowSecrets((prev) => ({
      ...prev,
      [platform]: {
        ...prev[platform],
        [key]: !prev[platform]?.[key],
      },
    }));
  };

  const handleSave = (platformName: string) => {
    const fields = fieldValues[platformName];
    if (!fields || Object.keys(fields).length === 0) return;
    configureVCS.mutate({ platform: platformName, fields });
  };

  const handleTest = (platformName: string) => {
    testVCS.mutate(platformName, {
      onSuccess: (result) => {
        setTestResults((prev) => ({ ...prev, [platformName]: result }));
      },
    });
  };

  const handleDelete = (platformName: string) => {
    if (!confirm(`Remove all ${platformName} credentials and configuration? This cannot be undone.`)) return;
    deleteVCS.mutate(platformName, {
      onSuccess: () => {
        setFieldValues((prev) => {
          const next = { ...prev };
          delete next[platformName];
          return next;
        });
        setTestResults((prev) => {
          const next = { ...prev };
          delete next[platformName];
          return next;
        });
      },
    });
  };

  return (
    <Card className="max-w-3xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <GitBranch className="h-5 w-5" />
          Version Control Systems
        </CardTitle>
        <CardDescription>
          Connect your VCS platforms to enable automatic PR review. Your tokens are
          encrypted in the vault — URLs and config are stored separately in the database.
        </CardDescription>
        <div className="flex items-center gap-1.5 pt-1 text-xs text-emerald-600 dark:text-emerald-400">
          <ShieldCheck className="h-3.5 w-3.5" />
          AES-256-GCM encrypted secrets • Per-user vault isolation • Config in application database
        </div>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-48 w-full" />
            ))}
          </div>
        ) : !platforms?.length ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            No VCS platforms available. Check server configuration.
          </p>
        ) : (
          <div className="space-y-4">
            {platforms.map((platform) => (
              <PlatformCard
                key={platform.name}
                platform={platform}
                meta={platformMeta[platform.name]}
                tokenCapabilities={tokenCaps?.[platform.name] ?? []}
                fieldValues={fieldValues[platform.name] ?? {}}
                showSecrets={showSecrets[platform.name] ?? {}}
                testResult={testResults[platform.name]}
                isSaving={
                  configureVCS.isPending &&
                  configureVCS.variables?.platform === platform.name
                }
                isTesting={
                  testVCS.isPending &&
                  testVCS.variables === platform.name
                }
                isDeleting={
                  deleteVCS.isPending &&
                  deleteVCS.variables === platform.name
                }
                onFieldChange={(key, val) => setFieldValue(platform.name, key, val)}
                onToggleSecret={(key) => toggleSecret(platform.name, key)}
                onSave={() => handleSave(platform.name)}
                onTest={() => handleTest(platform.name)}
                onDelete={() => handleDelete(platform.name)}
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ── Platform Card ───────────────────────────────────────────────────────────

interface PlatformCardProps {
  platform: VCSPlatformInfo;
  meta?: { color: string; docsUrl: string };
  tokenCapabilities: VCSTokenCapability[];
  fieldValues: Record<string, string>;
  showSecrets: Record<string, boolean>;
  testResult?: VCSTestResult;
  isSaving: boolean;
  isTesting: boolean;
  isDeleting: boolean;
  onFieldChange: (key: string, value: string) => void;
  onToggleSecret: (key: string) => void;
  onSave: () => void;
  onTest: () => void;
  onDelete: () => void;
}

function PlatformCard({
  platform,
  meta,
  tokenCapabilities,
  fieldValues,
  showSecrets,
  testResult,
  isSaving,
  isTesting,
  isDeleting,
  onFieldChange,
  onToggleSecret,
  onSave,
  onTest,
  onDelete,
}: PlatformCardProps) {
  const hasChanges = Object.keys(fieldValues).some((k) => fieldValues[k] !== "");
  const [showCapabilities, setShowCapabilities] = useState(false);
  const BrandIcon = getVCSIcon(platform.name);

  // Separate secret fields from config fields for visual grouping
  const secretFields = platform.fields.filter((f) => f.secret);
  const configFields = platform.fields.filter((f) => !f.secret);

  return (
    <div
      className={`rounded-lg border border-l-4 p-4 space-y-3 ${
        meta?.color ?? "border-l-zinc-400"
      }`}
    >
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2.5">
          <span className="flex h-6 w-6 shrink-0 items-center justify-center">
            {BrandIcon ? <BrandIcon size={22} /> : <GitBranch className="h-5 w-5 text-muted-foreground" />}
          </span>
          <p className="text-sm font-semibold">{platform.display_name}</p>
          {platform.configured ? (
            <Badge variant="success" className="text-xs">
              Configured
            </Badge>
          ) : (
            <Badge variant="secondary" className="text-xs">
              Not Configured
            </Badge>
          )}
        </div>
        <div className="flex items-center gap-2">
          {meta?.docsUrl && (
            <a
              href={meta.docsUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              Docs <ExternalLink className="h-3 w-3" />
            </a>
          )}
          {platform.configured && (
            <Button
              variant="ghost"
              size="sm"
              onClick={onDelete}
              disabled={isDeleting}
              className="text-destructive hover:text-destructive"
            >
              {isDeleting ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Trash2 className="h-4 w-4" />
              )}
            </Button>
          )}
        </div>
      </div>

      {/* Token Capability Info (collapsible) */}
      {tokenCapabilities.length > 0 && (
        <div className="rounded-md border bg-muted/30 overflow-hidden">
          <button
            type="button"
            onClick={() => setShowCapabilities(!showCapabilities)}
            className="flex items-center gap-2 w-full px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
          >
            {showCapabilities ? (
              <ChevronDown className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
            <Info className="h-3 w-3" />
            Token types &amp; permissions guide
          </button>
          {showCapabilities && (
            <div className="px-3 pb-3 space-y-2">
              {tokenCapabilities.map((cap) => (
                <div
                  key={cap.token_type}
                  className="rounded border bg-background p-2.5 space-y-1.5"
                >
                  <div className="flex items-center justify-between">
                    <p className="text-xs font-medium">{cap.label}</p>
                    <div className="flex gap-1">
                      {cap.can_clone && (
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                          Clone
                        </Badge>
                      )}
                      {cap.can_review && (
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                          Review
                        </Badge>
                      )}
                      {cap.can_webhook && (
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                          Webhooks
                        </Badge>
                      )}
                      {cap.can_read_pr && (
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                          PRs
                        </Badge>
                      )}
                    </div>
                  </div>
                  <div className="flex flex-wrap gap-1">
                    {cap.scopes.map((scope) => (
                      <code
                        key={scope}
                        className="rounded bg-muted px-1 py-0.5 text-[10px] font-mono text-muted-foreground"
                      >
                        {scope}
                      </code>
                    ))}
                  </div>
                  <p className="text-[10px] text-muted-foreground/80 leading-relaxed">
                    {cap.setup_guide}
                  </p>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Secret Fields (vault-stored) */}
      {secretFields.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground font-medium">
            <KeyRound className="h-3 w-3" />
            Credentials (encrypted in vault)
          </div>
          {secretFields.map((field) => (
            <FieldInput
              key={field.key}
              field={field}
              value={fieldValues[field.key] ?? ""}
              showSecret={showSecrets[field.key]}
              onChange={(val) => onFieldChange(field.key, val)}
              onToggleSecret={() => onToggleSecret(field.key)}
            />
          ))}
        </div>
      )}

      {/* Config Fields (DB-stored) */}
      {configFields.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground font-medium">
            <Globe className="h-3 w-3" />
            Configuration
          </div>
          {configFields.map((field) => (
            <FieldInput
              key={field.key}
              field={field}
              value={fieldValues[field.key] ?? ""}
              onChange={(val) => onFieldChange(field.key, val)}
            />
          ))}
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center gap-2 pt-1">
        <Button
          size="sm"
          onClick={onSave}
          disabled={!hasChanges || isSaving}
        >
          {isSaving ? (
            <>
              <Loader2 className="mr-1 h-4 w-4 animate-spin" />
              Saving…
            </>
          ) : (
            "Save"
          )}
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={onTest}
          disabled={isTesting}
        >
          {isTesting ? (
            <>
              <Loader2 className="mr-1 h-4 w-4 animate-spin" />
              Testing…
            </>
          ) : (
            "Test Connection"
          )}
        </Button>

        {/* Test result */}
        {testResult && (
          <span
            className={`flex items-center gap-1 text-xs ${
              testResult.success
                ? "text-emerald-600 dark:text-emerald-400"
                : "text-red-600 dark:text-red-400"
            }`}
          >
            {testResult.success ? (
              <CheckCircle className="h-3.5 w-3.5" />
            ) : (
              <XCircle className="h-3.5 w-3.5" />
            )}
            {testResult.message || testResult.error || "Unknown"}
          </span>
        )}
      </div>
    </div>
  );
}

// ── Field Input ─────────────────────────────────────────────────────────────

interface FieldInputProps {
  field: VCSFieldInfo;
  value: string;
  showSecret?: boolean;
  onChange: (value: string) => void;
  onToggleSecret?: () => void;
}

function FieldInput({ field, value, showSecret, onChange, onToggleSecret }: FieldInputProps) {
  // For non-secret fields with a default, show the default as placeholder
  const placeholder = field.has_value
    ? field.value
    : field.default_value
      ? field.default_value
      : `Enter ${field.label.toLowerCase()}…`;

  return (
    <div className="space-y-1">
      <div className="flex items-center gap-2">
        <Label className="text-xs text-muted-foreground">
          {field.label}
        </Label>
        {field.hint && (
          <span className="text-[10px] text-muted-foreground/60 italic">
            {field.hint}
          </span>
        )}
      </div>
      <div className="relative">
        <Input
          type={field.secret && !showSecret ? "password" : "text"}
          placeholder={placeholder}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="pr-10 text-sm"
        />
        {field.secret && onToggleSecret && (
          <button
            type="button"
            onClick={onToggleSecret}
            className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          >
            {showSecret ? (
              <EyeOff className="h-4 w-4" />
            ) : (
              <Eye className="h-4 w-4" />
            )}
          </button>
        )}
      </div>
    </div>
  );
}
