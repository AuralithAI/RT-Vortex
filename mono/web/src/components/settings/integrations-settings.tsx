"use client";

import { useState, useMemo, Fragment } from "react";
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
  KeyRound,
  X,
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
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

// ── Provider metadata ───────────────────────────────────────────────────────

interface ProviderMeta {
  label: string;
  brandColor: string;        // raw hex for icon tinting & accents
  borderColor: string;       // Tailwind border-l color class
  gradient: string;          // Tailwind gradient class
  docsUrl: string;
  description: string;
  actions: string[];
  category: string;          // used if server doesn't supply one
}

const providerMeta: Record<string, ProviderMeta> = {
  gmail: {
    label: "Gmail",
    brandColor: "#EA4335",
    borderColor: "border-l-[#EA4335]",
    gradient: "from-[#EA4335]/10 to-transparent",
    docsUrl: "https://developers.google.com/gmail/api/auth/about-auth",
    description: "Read emails, send messages, manage labels",
    actions: ["Read Emails", "Send Emails", "Manage Labels", "Search Mail"],
    category: "google",
  },
  google_calendar: {
    label: "Google Calendar",
    brandColor: "#4285F4",
    borderColor: "border-l-[#4285F4]",
    gradient: "from-[#4285F4]/10 to-transparent",
    docsUrl: "https://developers.google.com/calendar",
    description: "Create events, check availability, manage attendees",
    actions: ["List Events", "Create Event", "Update Event", "Delete Event", "List Calendars", "Free/Busy Query"],
    category: "google",
  },
  google_drive: {
    label: "Google Drive",
    brandColor: "#0066DA",
    borderColor: "border-l-[#0066DA]",
    gradient: "from-[#0066DA]/10 to-transparent",
    docsUrl: "https://developers.google.com/drive",
    description: "List, search, read, and share files in Google Drive",
    actions: ["List Files", "Get File", "Read Content", "Search Files", "Create File", "Share File", "Delete File"],
    category: "google",
  },
  ms365: {
    label: "Microsoft 365",
    brandColor: "#0078D4",
    borderColor: "border-l-[#0078D4]",
    gradient: "from-[#0078D4]/10 to-transparent",
    docsUrl: "https://learn.microsoft.com/en-us/graph/auth/",
    description: "Email, calendar, files, and Teams integration",
    actions: ["Read Mail", "Send Mail", "Calendar Events", "Read Files", "Teams Messages"],
    category: "microsoft",
  },
  github: {
    label: "GitHub",
    brandColor: "#24292F",
    borderColor: "border-l-[#24292F]",
    gradient: "from-[#24292F]/10 to-transparent",
    docsUrl: "https://docs.github.com/en/rest",
    description: "Issues, pull requests, repos, actions, code search",
    actions: ["List Repos", "Issues", "Pull Requests", "Search Code", "Workflow Runs", "Notifications"],
    category: "devops",
  },
  jira: {
    label: "Jira",
    brandColor: "#2684FF",
    borderColor: "border-l-[#2684FF]",
    gradient: "from-[#2684FF]/10 to-transparent",
    docsUrl: "https://developer.atlassian.com/cloud/jira/platform/rest/v3/",
    description: "Issues, sprints, boards, transitions, project management",
    actions: ["Search Issues", "Create Issue", "Add Comment", "Transition Issue", "List Projects", "Board Sprints"],
    category: "devops",
  },
  slack: {
    label: "Slack",
    brandColor: "#4A154B",
    borderColor: "border-l-[#4A154B]",
    gradient: "from-[#4A154B]/10 to-transparent",
    docsUrl: "https://api.slack.com/authentication/token-types",
    description: "Send messages, read channels, manage notifications",
    actions: ["Send Messages", "Read Channels", "List Users", "Create Channels"],
    category: "communication",
  },
  discord: {
    label: "Discord",
    brandColor: "#5865F2",
    borderColor: "border-l-[#5865F2]",
    gradient: "from-[#5865F2]/10 to-transparent",
    docsUrl: "https://discord.com/developers/docs/getting-started",
    description: "Send messages, manage channels, bot interactions",
    actions: ["Send Messages", "Read Channels", "Manage Roles", "Server Info"],
    category: "communication",
  },
  notion: {
    label: "Notion",
    brandColor: "#000000",
    borderColor: "border-l-[#000000]",
    gradient: "from-[#000000]/8 to-transparent",
    docsUrl: "https://developers.notion.com/",
    description: "Pages, databases, blocks, comments, full-text search",
    actions: ["Search", "Get Page", "Create Page", "Query Database", "Append Blocks", "List Comments"],
    category: "productivity",
  },
};

// Stable ordering for categories
const categoryOrder: { key: string; label: string }[] = [
  { key: "google", label: "Google Workspace" },
  { key: "microsoft", label: "Microsoft 365" },
  { key: "devops", label: "DevOps & Engineering" },
  { key: "communication", label: "Communication" },
  { key: "productivity", label: "Productivity" },
];

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
    <Card className={`border-l-4 ${meta?.borderColor ?? ""}`}>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-base">
            <KeyRound className="h-4 w-4" />
            Manual Token — {meta?.label ?? provider.name}
          </CardTitle>
          <Button variant="ghost" size="icon" onClick={onClose} className="h-7 w-7">
            <X className="h-4 w-4" />
          </Button>
        </div>
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

// ── Provider Icon Tile (icon grid) ──────────────────────────────────────────

function ProviderTile({
  provider,
  isConnected,
  hasOAuth,
  connection,
  onManualConnect,
}: {
  provider: MCPProviderInfo;
  isConnected: boolean;
  hasOAuth: boolean;
  connection?: MCPConnection;
  onManualConnect: () => void;
}) {
  const meta = providerMeta[provider.name];
  const Icon = getMCPIcon(provider.name);
  const label = meta?.label ?? provider.name;
  const description = meta?.description ?? provider.description ?? "";
  const actions = meta?.actions ?? provider.actions;

  const handleClick = () => {
    if (isConnected) return; // already connected — no action on click
    if (hasOAuth) {
      window.location.href = integrationsApi.oauthUrl(provider.name);
    } else {
      onManualConnect();
    }
  };

  return (
    <TooltipProvider delayDuration={200}>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={handleClick}
            className={`
              group relative flex flex-col items-center justify-center gap-2
              w-[88px] h-[88px] rounded-xl border transition-all duration-200
              focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring
              ${isConnected
                ? "border-emerald-500/40 bg-background shadow-sm hover:shadow-md cursor-default"
                : "border-border bg-muted/40 hover:bg-muted hover:shadow-md hover:border-foreground/20 cursor-pointer"
              }
            `}
          >
            {/* Connected badge */}
            {isConnected && (
              <span className="absolute -top-1.5 -right-1.5 flex h-5 w-5 items-center justify-center rounded-full bg-emerald-500 shadow-sm">
                <CheckCircle className="h-3 w-3 text-white" />
              </span>
            )}

            {/* Icon — greyed when disconnected, brand-colored when connected */}
            <span
              className={`flex h-9 w-9 items-center justify-center transition-all duration-200 ${
                isConnected
                  ? ""
                  : "grayscale opacity-40 group-hover:grayscale-0 group-hover:opacity-100"
              }`}
            >
              {Icon ? <Icon size={28} /> : <Plug className="h-7 w-7 text-muted-foreground" />}
            </span>

            {/* Label */}
            <span className={`text-[11px] font-medium leading-tight text-center px-1 truncate w-full ${
              isConnected ? "text-foreground" : "text-muted-foreground group-hover:text-foreground"
            }`}>
              {label}
            </span>
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom" className="max-w-[240px] space-y-2 p-3">
          <p className="font-semibold text-sm">{label}</p>
          <p className="text-xs text-muted-foreground">{description}</p>
          <div className="flex flex-wrap gap-1">
            {actions.map((a) => (
              <Badge key={a} variant="secondary" className="text-[10px] font-normal px-1.5 py-0">
                {a}
              </Badge>
            ))}
          </div>
          {isConnected && connection && (
            <p className="text-[10px] text-emerald-600">
              <CheckCircle className="mr-0.5 inline h-3 w-3" />
              Connected {new Date(connection.connected_at).toLocaleDateString()}
            </p>
          )}
          {!isConnected && (
            <p className="text-[10px] text-muted-foreground italic">
              {hasOAuth ? "Click to connect via OAuth" : "Click to connect with token"}
            </p>
          )}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

// ── Connected Drawer (expanded details for a connected provider) ────────────

function ConnectedDrawer({
  connection,
  onClose,
}: {
  connection: MCPConnection;
  onClose: () => void;
}) {
  const [showLog, setShowLog] = useState(false);
  const disconnectMutation = useDisconnectIntegration();
  const testMutation = useTestIntegration();
  const meta = providerMeta[connection.provider];
  const Icon = getMCPIcon(connection.provider);

  return (
    <Card className={`border-l-4 ${meta?.borderColor ?? ""} relative overflow-hidden`}>
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
          <div className="flex items-center gap-2">
            {getStatusBadge(connection.status)}
            <Button variant="ghost" size="icon" onClick={onClose} className="h-7 w-7">
              <X className="h-4 w-4" />
            </Button>
          </div>
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

// ── Main Component ──────────────────────────────────────────────────────────

export function IntegrationsSettings() {
  const { data: connections, isLoading: connectionsLoading } = useIntegrations();
  const { data: providers, isLoading: providersLoading } = useIntegrationProviders();
  const { data: oauthStatusData } = useIntegrationOAuthStatus();
  const [manualConnectProvider, setManualConnectProvider] = useState<string | null>(null);
  const [selectedConnection, setSelectedConnection] = useState<string | null>(null);

  const isLoading = connectionsLoading || providersLoading;
  const oauthEnabled = oauthStatusData?.oauth_enabled ?? {};

  // Build a lookup of provider→connection
  const connectionMap = useMemo(() => {
    const map: Record<string, MCPConnection> = {};
    (connections as MCPConnection[] | undefined)?.forEach((c) => {
      map[c.provider] = c;
    });
    return map;
  }, [connections]);

  // Group providers by category
  const groupedProviders = useMemo(() => {
    const allProviders = (providers as MCPProviderInfo[] | undefined) ?? [];
    const groups: Record<string, MCPProviderInfo[]> = {};
    for (const p of allProviders) {
      const cat = p.category || providerMeta[p.name]?.category || "other";
      if (!groups[cat]) groups[cat] = [];
      groups[cat].push(p);
    }
    return groups;
  }, [providers]);

  // Count connected
  const connectedCount = Object.keys(connectionMap).length;
  const totalCount = (providers as MCPProviderInfo[] | undefined)?.length ?? 0;

  if (isLoading) {
    return (
      <Card className="max-w-4xl">
        <CardContent className="py-8">
          <div className="grid grid-cols-6 gap-4">
            {Array.from({ length: 9 }).map((_, i) => (
              <Skeleton key={i} className="h-[88px] w-[88px] rounded-xl" />
            ))}
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="max-w-4xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Plug className="h-5 w-5" />
          MCP Integrations
        </CardTitle>
        <CardDescription>
          Connect external services to enable swarm agents to act on your behalf.
          {connectedCount > 0 && (
            <span className="ml-2 inline-flex items-center gap-1">
              <CheckCircle className="h-3 w-3 text-emerald-500" />
              {connectedCount}/{totalCount} connected
            </span>
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* ─── Icon grid grouped by category ─── */}
        {categoryOrder.map(({ key: cat, label: catLabel }) => {
          const group = groupedProviders[cat];
          if (!group?.length) return null;
          return (
            <Fragment key={cat}>
              <div>
                <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
                  {catLabel}
                </h4>
                <div className="flex flex-wrap gap-3">
                  {group.map((p) => (
                    <div key={p.name} onClick={() => {
                      if (connectionMap[p.name]) {
                        setSelectedConnection(selectedConnection === p.name ? null : p.name);
                        setManualConnectProvider(null);
                      }
                    }}>
                      <ProviderTile
                        provider={p}
                        isConnected={!!connectionMap[p.name]}
                        hasOAuth={!!oauthEnabled[p.name]}
                        connection={connectionMap[p.name]}
                        onManualConnect={() => {
                          setManualConnectProvider(p.name);
                          setSelectedConnection(null);
                        }}
                      />
                    </div>
                  ))}
                </div>
              </div>
            </Fragment>
          );
        })}

        {/* Show providers that don't fit known categories */}
        {(() => {
          const knownCats = new Set(categoryOrder.map((c) => c.key));
          const otherProviders = Object.entries(groupedProviders)
            .filter(([cat]) => !knownCats.has(cat))
            .flatMap(([, pList]) => pList);
          if (!otherProviders.length) return null;
          return (
            <div>
              <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
                Other
              </h4>
              <div className="flex flex-wrap gap-3">
                {otherProviders.map((p) => (
                  <div key={p.name} onClick={() => {
                    if (connectionMap[p.name]) {
                      setSelectedConnection(selectedConnection === p.name ? null : p.name);
                      setManualConnectProvider(null);
                    }
                  }}>
                    <ProviderTile
                      provider={p}
                      isConnected={!!connectionMap[p.name]}
                      hasOAuth={!!oauthEnabled[p.name]}
                      connection={connectionMap[p.name]}
                      onManualConnect={() => {
                        setManualConnectProvider(p.name);
                        setSelectedConnection(null);
                      }}
                    />
                  </div>
                ))}
              </div>
            </div>
          );
        })()}

        {/* ─── Expanded detail panel for connected provider ─── */}
        {selectedConnection && connectionMap[selectedConnection] && (
          <ConnectedDrawer
            connection={connectionMap[selectedConnection]}
            onClose={() => setSelectedConnection(null)}
          />
        )}

        {/* ─── Manual token form ─── */}
        {manualConnectProvider && (() => {
          const prov = (providers as MCPProviderInfo[] | undefined)?.find((p) => p.name === manualConnectProvider);
          return prov ? (
            <ManualConnectForm provider={prov} onClose={() => setManualConnectProvider(null)} />
          ) : null;
        })()}

        {/* ─── Empty state ─── */}
        {totalCount === 0 && (
          <div className="flex flex-col items-center justify-center py-10 text-center">
            <Plug className="mb-3 h-10 w-10 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No MCP providers available.</p>
            <p className="text-xs text-muted-foreground mt-1">Check server configuration to enable integrations.</p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
