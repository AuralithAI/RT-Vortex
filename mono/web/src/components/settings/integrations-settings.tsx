"use client";

import { useState } from "react";
import {
  CheckCircle,
  XCircle,
  Loader2,
  Trash2,
  ExternalLink,
  Plug,
  Shield,
  Clock,
  Activity,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  ArrowRight,
  KeyRound,
  Zap,
} from "lucide-react";
import { useIntegrations, useIntegrationProviders, useIntegrationCallLog, useIntegrationOAuthStatus } from "@/lib/api/queries";
import { useConnectIntegration, useDisconnectIntegration, useTestIntegration } from "@/lib/api/mutations";
import { integrations as integrationsApi } from "@/lib/api/client";
import type { MCPConnection, MCPProviderInfo, MCPCallLogEntry } from "@/types/api";
import { getMCPIcon } from "@/components/icons/brand-icons";
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

// ── Provider metadata ───────────────────────────────────────────────────────

const providerMeta: Record<string, {
  label: string;
  color: string;
  gradient: string;
  docsUrl: string;
  description: string;
  actions: string[];
}> = {
  slack: {
    label: "Slack",
    color: "border-l-[#4A154B]",
    gradient: "from-[#4A154B]/10 to-transparent",
    docsUrl: "https://api.slack.com/authentication/token-types",
    description: "Send messages, read channels, manage notifications",
    actions: ["Send Messages", "Read Channels", "List Users", "Create Channels"],
  },
  ms365: {
    label: "Microsoft 365",
    color: "border-l-[#0078D4]",
    gradient: "from-[#0078D4]/10 to-transparent",
    docsUrl: "https://learn.microsoft.com/en-us/graph/auth/",
    description: "Email, calendar, files, and Teams integration",
    actions: ["Read Mail", "Send Mail", "Calendar Events", "Read Files", "Teams Messages"],
  },
  gmail: {
    label: "Gmail",
    color: "border-l-[#EA4335]",
    gradient: "from-[#EA4335]/10 to-transparent",
    docsUrl: "https://developers.google.com/gmail/api/auth/about-auth",
    description: "Read emails, send messages, manage labels",
    actions: ["Read Emails", "Send Emails", "Manage Labels", "Search Mail"],
  },
  discord: {
    label: "Discord",
    color: "border-l-[#5865F2]",
    gradient: "from-[#5865F2]/10 to-transparent",
    docsUrl: "https://discord.com/developers/docs/getting-started",
    description: "Send messages, manage channels, bot interactions",
    actions: ["Send Messages", "Read Channels", "Manage Roles", "Server Info"],
  },
};

function getStatusBadge(status: string) {
  switch (status) {
    case "active":
      return <Badge variant="default" className="bg-emerald-600 hover:bg-emerald-600"><CheckCircle className="mr-1 h-3 w-3" /> Active</Badge>;
    case "expired":
      return <Badge variant="secondary" className="bg-amber-600 text-white hover:bg-amber-600"><Clock className="mr-1 h-3 w-3" /> Expired</Badge>;
    case "error":
      return <Badge variant="destructive"><XCircle className="mr-1 h-3 w-3" /> Error</Badge>;
    case "revoked":
      return <Badge variant="outline"><XCircle className="mr-1 h-3 w-3" /> Revoked</Badge>;
    default:
      return <Badge variant="outline">{status}</Badge>;
  }
}

// ── Manual Token Form ───────────────────────────────────────────────────────

function ManualConnectForm({ provider, onClose }: { provider: MCPProviderInfo; onClose: () => void }) {
  const [token, setToken] = useState("");
  const [refreshToken, setRefreshToken] = useState("");
  const [scopes, setScopes] = useState("");
  const [isOrgLevel, setIsOrgLevel] = useState(false);
  const connectMutation = useConnectIntegration();

  const meta = providerMeta[provider.name];

  const handleSubmit = () => {
    if (!token.trim()) return;
    connectMutation.mutate(
      {
        provider: provider.name,
        access_token: token.trim(),
        refresh_token: refreshToken.trim() || undefined,
        scopes: scopes ? scopes.split(",").map((s) => s.trim()).filter(Boolean) : undefined,
        is_org_level: isOrgLevel,
      },
      { onSuccess: () => onClose() },
    );
  };

  return (
    <Card className={`border-l-4 ${meta?.color ?? ""}`}>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          <KeyRound className="h-4 w-4" />
          Manual Token — {meta?.label ?? provider.name}
        </CardTitle>
        <CardDescription>
          Paste an API token or OAuth access token directly.
          {meta?.docsUrl && (
            <a href={meta.docsUrl} target="_blank" rel="noopener noreferrer" className="ml-1 inline-flex items-center gap-0.5 text-blue-500 hover:underline">
              Docs <ExternalLink className="h-3 w-3" />
            </a>
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div>
          <Label htmlFor={`token-${provider.name}`}>Access Token</Label>
          <Input
            id={`token-${provider.name}`}
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="xoxb-... or Bearer token"
          />
        </div>
        <div>
          <Label htmlFor={`refresh-${provider.name}`}>Refresh Token (optional)</Label>
          <Input
            id={`refresh-${provider.name}`}
            type="password"
            value={refreshToken}
            onChange={(e) => setRefreshToken(e.target.value)}
            placeholder="Refresh token for auto-renewal"
          />
        </div>
        <div>
          <Label htmlFor={`scopes-${provider.name}`}>Scopes (comma-separated, optional)</Label>
          <Input
            id={`scopes-${provider.name}`}
            value={scopes}
            onChange={(e) => setScopes(e.target.value)}
            placeholder="channels:read,chat:write"
          />
        </div>
        <div className="flex items-center gap-2">
          <input
            type="checkbox"
            id={`org-${provider.name}`}
            checked={isOrgLevel}
            onChange={(e) => setIsOrgLevel(e.target.checked)}
            className="h-4 w-4 rounded border-gray-300"
          />
          <Label htmlFor={`org-${provider.name}`} className="text-sm">
            <Shield className="mr-1 inline h-3 w-3" /> Organization-level connection
          </Label>
        </div>
        <div className="flex gap-2 pt-2">
          <Button onClick={handleSubmit} disabled={!token.trim() || connectMutation.isPending} size="sm">
            {connectMutation.isPending ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <Plug className="mr-1 h-4 w-4" />}
            Connect
          </Button>
          <Button variant="outline" onClick={onClose} size="sm">Cancel</Button>
        </div>
        {connectMutation.isError && (
          <p className="text-sm text-red-500">
            <AlertTriangle className="mr-1 inline h-3 w-3" />
            {(connectMutation.error as Error)?.message ?? "Connection failed."}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

// ── Connection Card ─────────────────────────────────────────────────────────

function ConnectionCard({ connection }: { connection: MCPConnection }) {
  const [showLog, setShowLog] = useState(false);
  const disconnectMutation = useDisconnectIntegration();
  const testMutation = useTestIntegration();
  const meta = providerMeta[connection.provider];
  const Icon = getMCPIcon(connection.provider);

  return (
    <Card className={`border-l-4 ${meta?.color ?? ""} relative overflow-hidden`}>
      <div className={`absolute inset-0 bg-gradient-to-r ${meta?.gradient ?? ""} pointer-events-none`} />
      <CardHeader className="relative pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2.5 text-base">
            <span className="flex h-7 w-7 shrink-0 items-center justify-center">
              {Icon ? <Icon size={24} /> : <Plug className="h-5 w-5 text-muted-foreground" />}
            </span>
            {meta?.label ?? connection.provider}
            {connection.is_org_level && (
              <Badge variant="outline" className="text-xs"><Shield className="mr-0.5 h-3 w-3" /> Org</Badge>
            )}
          </CardTitle>
          {getStatusBadge(connection.status)}
        </div>
        <CardDescription className="text-xs pl-9">
          Connected {new Date(connection.connected_at).toLocaleDateString()}
          {connection.last_used_at && <> · Last used {new Date(connection.last_used_at).toLocaleDateString()}</>}
          {connection.expires_at && <> · Expires {new Date(connection.expires_at).toLocaleDateString()}</>}
        </CardDescription>
      </CardHeader>
      <CardContent className="relative space-y-2 pl-9">
        {connection.scopes?.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {connection.scopes.map((scope) => (
              <Badge key={scope} variant="outline" className="text-xs font-mono">{scope}</Badge>
            ))}
          </div>
        )}
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => testMutation.mutate(connection.id)}
            disabled={testMutation.isPending}
          >
            {testMutation.isPending ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : <Activity className="mr-1 h-3 w-3" />}
            Test
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowLog(!showLog)}
          >
            {showLog ? <ChevronDown className="mr-1 h-3 w-3" /> : <ChevronRight className="mr-1 h-3 w-3" />}
            Logs
          </Button>
          <Button
            variant="destructive"
            size="sm"
            onClick={() => disconnectMutation.mutate(connection.id)}
            disabled={disconnectMutation.isPending}
          >
            {disconnectMutation.isPending ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : <Trash2 className="mr-1 h-3 w-3" />}
            Disconnect
          </Button>
        </div>
        {testMutation.isSuccess && (
          <div className={`text-sm ${testMutation.data.success ? "text-emerald-600" : "text-red-500"}`}>
            {testMutation.data.success ? <CheckCircle className="mr-1 inline h-3 w-3" /> : <XCircle className="mr-1 inline h-3 w-3" />}
            {testMutation.data.success ? "Connection verified" : testMutation.data.error ?? "Test failed"}
          </div>
        )}
        {showLog && <CallLogSection connectionId={connection.id} />}
      </CardContent>
    </Card>
  );
}

// ── Call Log ────────────────────────────────────────────────────────────────

function CallLogSection({ connectionId }: { connectionId: string }) {
  const { data: entries, isLoading } = useIntegrationCallLog(connectionId);

  if (isLoading) return <Skeleton className="h-20 w-full" />;
  if (!entries?.length) return <p className="text-xs text-muted-foreground">No call history yet.</p>;

  return (
    <div className="max-h-48 overflow-y-auto rounded border">
      <table className="w-full text-xs">
        <thead className="sticky top-0 bg-muted">
          <tr>
            <th className="px-2 py-1 text-left">Action</th>
            <th className="px-2 py-1 text-left">Status</th>
            <th className="px-2 py-1 text-right">Latency</th>
            <th className="px-2 py-1 text-right">Time</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((entry: MCPCallLogEntry) => (
            <tr key={entry.id} className="border-t">
              <td className="px-2 py-1 font-mono">{entry.action}</td>
              <td className="px-2 py-1">
                <Badge
                  variant={entry.status === "ok" ? "default" : "destructive"}
                  className="text-xs"
                >
                  {entry.status}
                </Badge>
              </td>
              <td className="px-2 py-1 text-right">{entry.latency_ms}ms</td>
              <td className="px-2 py-1 text-right text-muted-foreground">
                {new Date(entry.created_at).toLocaleTimeString()}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ── Provider Card (available, not connected) ────────────────────────────────

function ProviderCard({
  provider,
  hasOAuth,
  onManualConnect,
}: {
  provider: MCPProviderInfo;
  hasOAuth: boolean;
  onManualConnect: () => void;
}) {
  const [showActions, setShowActions] = useState(false);
  const meta = providerMeta[provider.name];
  const Icon = getMCPIcon(provider.name);

  const handleOAuthConnect = () => {
    // Navigate to the OAuth authorize endpoint — the server will redirect to the provider.
    window.location.href = integrationsApi.oauthUrl(provider.name);
  };

  return (
    <Card className={`border-l-4 ${meta?.color ?? ""} relative overflow-hidden transition-all hover:shadow-md`}>
      <div className={`absolute inset-0 bg-gradient-to-r ${meta?.gradient ?? ""} pointer-events-none`} />
      <CardHeader className="relative pb-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-background shadow-sm border">
              {Icon ? <Icon size={20} /> : <Plug className="h-5 w-5 text-muted-foreground" />}
            </span>
            <div>
              <CardTitle className="text-base">{meta?.label ?? provider.name}</CardTitle>
              <CardDescription className="text-xs">{meta?.description ?? ""}</CardDescription>
            </div>
          </div>
          <Badge variant="secondary" className="text-xs">
            {provider.actions.length} actions
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="relative space-y-3">
        {/* Expandable actions list */}
        <button
          type="button"
          onClick={() => setShowActions(!showActions)}
          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          {showActions ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
          <Zap className="h-3 w-3" />
          Supported actions
        </button>
        {showActions && (
          <div className="flex flex-wrap gap-1.5 pl-4">
            {(meta?.actions ?? provider.actions.map((a) => a)).map((action) => (
              <Badge key={action} variant="outline" className="text-[10px] font-normal">
                {action}
              </Badge>
            ))}
          </div>
        )}

        {/* Connect buttons */}
        <div className="flex items-center gap-2 pt-1">
          {hasOAuth ? (
            <>
              <Button size="sm" onClick={handleOAuthConnect} className="gap-1.5">
                <ArrowRight className="h-3.5 w-3.5" />
                Connect with {meta?.label ?? provider.name}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={onManualConnect}
                className="text-xs text-muted-foreground"
              >
                <KeyRound className="mr-1 h-3 w-3" />
                Use Token
              </Button>
            </>
          ) : (
            <Button size="sm" onClick={onManualConnect} className="gap-1.5">
              <KeyRound className="h-3.5 w-3.5" />
              Connect with Token
            </Button>
          )}
          {meta?.docsUrl && (
            <a
              href={meta.docsUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="ml-auto inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              Docs <ExternalLink className="h-3 w-3" />
            </a>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

// ── Main Component ──────────────────────────────────────────────────────────

export function IntegrationsSettings() {
  const { data: connections, isLoading: connectionsLoading } = useIntegrations();
  const { data: providers, isLoading: providersLoading } = useIntegrationProviders();
  const { data: oauthStatusData } = useIntegrationOAuthStatus();
  const [manualConnectProvider, setManualConnectProvider] = useState<string | null>(null);

  const isLoading = connectionsLoading || providersLoading;
  const oauthEnabled = oauthStatusData?.oauth_enabled ?? {};

  const connectedProviders = new Set((connections as MCPConnection[] | undefined)?.map((c) => c.provider) ?? []);
  const availableProviders = (providers as MCPProviderInfo[] | undefined)?.filter((p) => !connectedProviders.has(p.name)) ?? [];

  if (isLoading) {
    return (
      <Card className="max-w-3xl">
        <CardContent className="space-y-4 py-6">
          <Skeleton className="h-32 w-full" />
          <Skeleton className="h-32 w-full" />
          <Skeleton className="h-32 w-full" />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="max-w-3xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Plug className="h-5 w-5" />
          MCP Integrations
        </CardTitle>
        <CardDescription>
          Connect external services to enable swarm agents to read and write on your behalf.
          All credentials are encrypted at rest with AES-256-GCM and stored in an isolated per-user vault.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Connected services */}
        {connections && connections.length > 0 && (
          <div className="space-y-3">
            <h4 className="text-sm font-medium flex items-center gap-1.5">
              <CheckCircle className="h-3.5 w-3.5 text-emerald-500" />
              Active Connections
            </h4>
            {(connections as MCPConnection[]).map((conn) => (
              <ConnectionCard key={conn.id} connection={conn} />
            ))}
          </div>
        )}

        {/* Manual token form */}
        {manualConnectProvider && (() => {
          const prov = (providers as MCPProviderInfo[] | undefined)?.find((p) => p.name === manualConnectProvider);
          return prov ? (
            <ManualConnectForm provider={prov} onClose={() => setManualConnectProvider(null)} />
          ) : null;
        })()}

        {/* Available providers */}
        {availableProviders.length > 0 && !manualConnectProvider && (
          <div className="space-y-3">
            <h4 className="text-sm font-medium text-muted-foreground">
              {connections && connections.length > 0 ? "Add More Services" : "Available Services"}
            </h4>
            <div className="space-y-3">
              {availableProviders.map((p: MCPProviderInfo) => (
                <ProviderCard
                  key={p.name}
                  provider={p}
                  hasOAuth={!!oauthEnabled[p.name]}
                  onManualConnect={() => setManualConnectProvider(p.name)}
                />
              ))}
            </div>
          </div>
        )}

        {/* Empty state */}
        {connections?.length === 0 && availableProviders.length === 0 && !manualConnectProvider && (
          <div className="flex flex-col items-center justify-center py-8 text-center">
            <Plug className="mb-2 h-8 w-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No MCP providers available.</p>
            <p className="text-xs text-muted-foreground">Check server configuration to enable Slack, Microsoft 365, Gmail, or Discord.</p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
