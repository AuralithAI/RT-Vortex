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
} from "lucide-react";
import { useIntegrations, useIntegrationProviders, useIntegrationCallLog } from "@/lib/api/queries";
import { useConnectIntegration, useDisconnectIntegration, useTestIntegration } from "@/lib/api/mutations";
import type { MCPConnection, MCPProviderInfo, MCPCallLogEntry } from "@/types/api";
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

const providerMeta: Record<string, { label: string; color: string; icon: string; docsUrl: string }> = {
  slack: {
    label: "Slack",
    color: "border-l-purple-500",
    icon: "🔮",
    docsUrl: "https://api.slack.com/authentication/token-types",
  },
  ms365: {
    label: "Microsoft 365",
    color: "border-l-blue-500",
    icon: "🔷",
    docsUrl: "https://learn.microsoft.com/en-us/graph/auth/",
  },
  gmail: {
    label: "Gmail",
    color: "border-l-red-500",
    icon: "📧",
    docsUrl: "https://developers.google.com/gmail/api/auth/about-auth",
  },
  discord: {
    label: "Discord",
    color: "border-l-indigo-500",
    icon: "🎮",
    docsUrl: "https://discord.com/developers/docs/getting-started",
  },
};

function getProviderLabel(name: string) {
  return providerMeta[name]?.label ?? name;
}

function getStatusBadge(status: string) {
  switch (status) {
    case "active":
      return <Badge variant="default" className="bg-green-600"><CheckCircle className="mr-1 h-3 w-3" /> Active</Badge>;
    case "expired":
      return <Badge variant="secondary" className="bg-amber-600 text-white"><Clock className="mr-1 h-3 w-3" /> Expired</Badge>;
    case "error":
      return <Badge variant="destructive"><XCircle className="mr-1 h-3 w-3" /> Error</Badge>;
    case "revoked":
      return <Badge variant="outline"><XCircle className="mr-1 h-3 w-3" /> Revoked</Badge>;
    default:
      return <Badge variant="outline">{status}</Badge>;
  }
}

function ConnectForm({ provider, onClose }: { provider: MCPProviderInfo; onClose: () => void }) {
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
          <span>{meta?.icon}</span>
          Connect {meta?.label ?? provider.name}
        </CardTitle>
        <CardDescription>
          Provide an API token or OAuth access token.
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

function ConnectionCard({ connection }: { connection: MCPConnection }) {
  const [showLog, setShowLog] = useState(false);
  const disconnectMutation = useDisconnectIntegration();
  const testMutation = useTestIntegration();
  const meta = providerMeta[connection.provider];

  return (
    <Card className={`border-l-4 ${meta?.color ?? ""}`}>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-base">
            <span>{meta?.icon}</span>
            {meta?.label ?? connection.provider}
            {connection.is_org_level && (
              <Badge variant="outline" className="text-xs"><Shield className="mr-0.5 h-3 w-3" /> Org</Badge>
            )}
          </CardTitle>
          {getStatusBadge(connection.status)}
        </div>
        <CardDescription className="text-xs">
          Connected {new Date(connection.connected_at).toLocaleDateString()}
          {connection.last_used_at && <> · Last used {new Date(connection.last_used_at).toLocaleDateString()}</>}
          {connection.expires_at && <> · Expires {new Date(connection.expires_at).toLocaleDateString()}</>}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        {connection.scopes?.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {connection.scopes.map((scope) => (
              <Badge key={scope} variant="outline" className="text-xs">{scope}</Badge>
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
          <div className={`text-sm ${testMutation.data.success ? "text-green-600" : "text-red-500"}`}>
            {testMutation.data.success ? <CheckCircle className="mr-1 inline h-3 w-3" /> : <XCircle className="mr-1 inline h-3 w-3" />}
            {testMutation.data.success ? "Connection verified" : testMutation.data.error ?? "Test failed"}
          </div>
        )}
        {showLog && <CallLogSection connectionId={connection.id} />}
      </CardContent>
    </Card>
  );
}

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

export function IntegrationsSettings() {
  const { data: connections, isLoading: connectionsLoading } = useIntegrations();
  const { data: providers, isLoading: providersLoading } = useIntegrationProviders();
  const [connectingProvider, setConnectingProvider] = useState<string | null>(null);

  const isLoading = connectionsLoading || providersLoading;

  const connectedProviders = new Set((connections as MCPConnection[] | undefined)?.map((c) => c.provider) ?? []);
  const availableProviders = (providers as MCPProviderInfo[] | undefined)?.filter((p) => !connectedProviders.has(p.name)) ?? [];

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-lg font-medium">Connected Apps</h3>
        <p className="text-sm text-muted-foreground">
          Connect external services so swarm agents can read and write on your behalf.
          Tokens are encrypted at rest with AES-256-GCM.
        </p>
      </div>

      {connections && connections.length > 0 && (
        <div className="space-y-3">
          {(connections as MCPConnection[]).map((conn) => (
            <ConnectionCard key={conn.id} connection={conn} />
          ))}
        </div>
      )}

      {connections?.length === 0 && !connectingProvider && (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-8 text-center">
            <Plug className="mb-2 h-8 w-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No integrations connected yet.</p>
            <p className="text-xs text-muted-foreground">Connect Slack, Teams, Gmail, or Discord below.</p>
          </CardContent>
        </Card>
      )}

      {connectingProvider && (() => {
        const prov = (providers as MCPProviderInfo[] | undefined)?.find((p) => p.name === connectingProvider);
        return prov ? (
          <ConnectForm provider={prov} onClose={() => setConnectingProvider(null)} />
        ) : null;
      })()}

      {availableProviders.length > 0 && !connectingProvider && (
        <div>
          <h4 className="mb-2 text-sm font-medium text-muted-foreground">Available Integrations</h4>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {availableProviders.map((p: MCPProviderInfo) => {
              const meta = providerMeta[p.name];
              return (
                <Button
                  key={p.name}
                  variant="outline"
                  className="h-auto flex-col gap-1 py-3"
                  onClick={() => setConnectingProvider(p.name)}
                >
                  <span className="text-lg">{meta?.icon ?? "🔗"}</span>
                  <span className="text-xs">{meta?.label ?? p.name}</span>
                  <span className="text-[10px] text-muted-foreground">{p.actions.length} actions</span>
                </Button>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
