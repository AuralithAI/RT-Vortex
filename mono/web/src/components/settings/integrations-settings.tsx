"use client";

import { useState, useMemo, Fragment, useCallback } from "react";
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
  X,
  Plus,
  Zap,
  ArrowRight,
  ArrowLeft,
  Globe,
  Lock,
  Code2,
  Play,
  RefreshCw,
  Settings2,
  Link2,
  Unlink,
} from "lucide-react";
import { useIntegrations, useIntegrationProviders, useIntegrationCallLog, useIntegrationOAuthStatus, useCustomTemplates } from "@/lib/api/queries";
import { useDisconnectIntegration, useTestIntegration, useCreateCustomTemplate, useDeleteCustomTemplate, useValidateCustomTemplate, useSimulateCustomConnection } from "@/lib/api/mutations";
import { integrations as integrationsApi } from "@/lib/api/client";
import type { MCPConnection, MCPProviderInfo, MCPCallLogEntry, CustomMCPTemplate, CustomMCPActionDef, MCPValidationError } from "@/types/api";
import { getMCPIcon, GoogleIcon, MicrosoftIcon } from "@/components/icons/brand-icons";
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
  brandColor: string;
  borderColor: string;
  gradient: string;
  docsUrl: string;
  description: string;
  actions: string[];
  category: string;
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
    category: "atlassian",
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
  gitlab: {
    label: "GitLab",
    brandColor: "#FC6D26",
    borderColor: "border-l-[#FC6D26]",
    gradient: "from-[#FC6D26]/10 to-transparent",
    docsUrl: "https://docs.gitlab.com/ee/api/rest/",
    description: "Merge requests, issues, pipelines, projects, CI/CD",
    actions: ["List Projects", "Merge Requests", "Issues", "Pipelines", "Create MR", "MR Comments"],
    category: "devops",
  },
  confluence: {
    label: "Confluence",
    brandColor: "#1868DB",
    borderColor: "border-l-[#1868DB]",
    gradient: "from-[#1868DB]/10 to-transparent",
    docsUrl: "https://developer.atlassian.com/cloud/confluence/rest/v2/",
    description: "Pages, spaces, search, comments, content management",
    actions: ["List Spaces", "Get Page", "Search Content", "Create Page", "Update Page", "List Comments", "Get Space", "Delete Page"],
    category: "atlassian",
  },
  linear: {
    label: "Linear",
    brandColor: "#5E6AD2",
    borderColor: "border-l-[#5E6AD2]",
    gradient: "from-[#5E6AD2]/10 to-transparent",
    docsUrl: "https://developers.linear.app/docs/graphql/working-with-the-graphql-api",
    description: "Issues, projects, cycles, teams, workflow management",
    actions: ["List Issues", "Create Issue", "Update Issue", "List Projects", "List Teams", "List Cycles", "Add Comment", "Search Issues"],
    category: "project_management",
  },
  asana: {
    label: "Asana",
    brandColor: "#F06A6A",
    borderColor: "border-l-[#F06A6A]",
    gradient: "from-[#F06A6A]/10 to-transparent",
    docsUrl: "https://developers.asana.com/docs/getting-started",
    description: "Tasks, projects, sections, workspaces, team management",
    actions: ["List Tasks", "Create Task", "Update Task", "List Projects", "List Workspaces", "Search Tasks", "Add Comment", "List Sections"],
    category: "project_management",
  },
  trello: {
    label: "Trello",
    brandColor: "#0079BF",
    borderColor: "border-l-[#0079BF]",
    gradient: "from-[#0079BF]/10 to-transparent",
    docsUrl: "https://developer.atlassian.com/cloud/trello/rest/",
    description: "Boards, lists, cards, checklists, members",
    actions: ["List Boards", "List Cards", "Create Card", "Update Card", "Move Card", "Add Comment", "List Members", "Search"],
    category: "project_management",
  },
  figma: {
    label: "Figma",
    brandColor: "#A259FF",
    borderColor: "border-l-[#A259FF]",
    gradient: "from-[#A259FF]/10 to-transparent",
    docsUrl: "https://www.figma.com/developers/api",
    description: "Design files, components, styles, comments, images",
    actions: ["Get File", "List Projects", "Get Comments", "List Components", "Get Images", "Get Styles", "Post Comment", "Get Team Projects"],
    category: "design",
  },
  zendesk: {
    label: "Zendesk",
    brandColor: "#03363D",
    borderColor: "border-l-[#03363D]",
    gradient: "from-[#03363D]/10 to-transparent",
    docsUrl: "https://developer.zendesk.com/api-reference/",
    description: "Tickets, users, organizations, search, support management",
    actions: ["List Tickets", "Create Ticket", "Update Ticket", "Search", "List Users", "Get Ticket", "Add Comment", "List Organizations"],
    category: "support",
  },
  pagerduty: {
    label: "PagerDuty",
    brandColor: "#06AC38",
    borderColor: "border-l-[#06AC38]",
    gradient: "from-[#06AC38]/10 to-transparent",
    docsUrl: "https://developer.pagerduty.com/api-reference/",
    description: "Incidents, services, on-call schedules, alerts",
    actions: ["List Incidents", "Create Incident", "Get Incident", "List Services", "On-Call", "Acknowledge", "Resolve", "List Alerts"],
    category: "monitoring",
  },
  datadog: {
    label: "Datadog",
    brandColor: "#632CA6",
    borderColor: "border-l-[#632CA6]",
    gradient: "from-[#632CA6]/10 to-transparent",
    docsUrl: "https://docs.datadoghq.com/api/latest/",
    description: "Metrics, monitors, dashboards, events, logs",
    actions: ["Query Metrics", "List Monitors", "Create Monitor", "List Dashboards", "Search Logs", "Post Event", "Get Monitor", "Mute Monitor"],
    category: "monitoring",
  },
  stripe: {
    label: "Stripe",
    brandColor: "#635BFF",
    borderColor: "border-l-[#635BFF]",
    gradient: "from-[#635BFF]/10 to-transparent",
    docsUrl: "https://stripe.com/docs/api",
    description: "Payments, customers, invoices, subscriptions, balances",
    actions: ["List Charges", "List Customers", "Create Customer", "List Invoices", "Get Balance", "List Subscriptions", "Get Charge", "List Payouts"],
    category: "finance",
  },
  hubspot: {
    label: "HubSpot",
    brandColor: "#FF7A59",
    borderColor: "border-l-[#FF7A59]",
    gradient: "from-[#FF7A59]/10 to-transparent",
    docsUrl: "https://developers.hubspot.com/docs/api/overview",
    description: "Contacts, deals, companies, tickets, CRM management",
    actions: ["List Contacts", "Create Contact", "List Deals", "Create Deal", "List Companies", "Search CRM", "Get Contact", "List Tickets"],
    category: "crm",
  },
  salesforce: {
    label: "Salesforce",
    brandColor: "#00A1E0",
    borderColor: "border-l-[#00A1E0]",
    gradient: "from-[#00A1E0]/10 to-transparent",
    docsUrl: "https://developer.salesforce.com/docs/atlas.en-us.api_rest.meta/api_rest/",
    description: "Leads, opportunities, accounts, SOQL queries, reports",
    actions: ["SOQL Query", "List Objects", "Get Record", "Create Record", "Update Record", "Search (SOSL)", "Describe Object", "List Reports"],
    category: "crm",
  },
  twilio: {
    label: "Twilio",
    brandColor: "#F22F46",
    borderColor: "border-l-[#F22F46]",
    gradient: "from-[#F22F46]/10 to-transparent",
    docsUrl: "https://www.twilio.com/docs/usage/api",
    description: "SMS, voice, messaging, phone numbers, call management",
    actions: ["Send SMS", "List Messages", "Get Message", "List Calls", "Make Call", "List Numbers", "Get Call", "List Recordings"],
    category: "messaging",
  },
};

// ── Platform group definitions ──────────────────────────────────────────────

interface PlatformGroupDef {
  key: string;
  label: string;
  description: string;
  brandColor: string;
  borderColor: string;
  gradient: string;
  icon: React.ComponentType<{ size?: number }>;
  /** Provider keys that belong to this platform */
  services: string[];
}

const AtlassianIcon = ({ size = 28 }: { size?: number }) => (
  <svg viewBox="0 0 24 24" fill="none" width={size} height={size}>
    <path d="M7.12 11.53a.69.69 0 0 0-1.18.14L1.26 21.39a.69.69 0 0 0 .62 1h6.88a.69.69 0 0 0 .62-.39 13.28 13.28 0 0 0-2.26-10.47Z" fill="#2684FF" />
    <path d="M11.37 2.41a16.3 16.3 0 0 0-.85 16.15.69.69 0 0 0 .62.39h6.88a.69.69 0 0 0 .62-1L12.56 2.55a.69.69 0 0 0-1.19-.14Z" fill="#2684FF" />
  </svg>
);

const platformGroups: PlatformGroupDef[] = [
  {
    key: "google",
    label: "Google Workspace",
    description: "Gmail, Calendar, Drive — unified under a single Google OAuth credential",
    brandColor: "#4285F4",
    borderColor: "border-l-[#4285F4]",
    gradient: "from-[#4285F4]/8 via-[#EA4335]/5 to-transparent",
    icon: GoogleIcon,
    services: ["gmail", "google_calendar", "google_drive"],
  },
  {
    key: "microsoft",
    label: "Microsoft 365",
    description: "Outlook, OneDrive, Teams, Calendar — unified under a single Microsoft OAuth credential",
    brandColor: "#0078D4",
    borderColor: "border-l-[#0078D4]",
    gradient: "from-[#0078D4]/8 via-[#00A4EF]/5 to-transparent",
    icon: MicrosoftIcon,
    services: ["ms365"],
  },
  {
    key: "atlassian",
    label: "Atlassian",
    description: "Jira, Confluence — unified under a single Atlassian OAuth credential",
    brandColor: "#2684FF",
    borderColor: "border-l-[#2684FF]",
    gradient: "from-[#2684FF]/8 via-[#1868DB]/5 to-transparent",
    icon: AtlassianIcon,
    services: ["jira", "confluence"],
  },
];

// Standalone categories for non-platform providers
const standaloneCategoryOrder: { key: string; label: string }[] = [
  { key: "devops", label: "DevOps & Engineering" },
  { key: "communication", label: "Communication" },
  { key: "productivity", label: "Productivity" },
  { key: "project_management", label: "Project Management" },
  { key: "design", label: "Design" },
  { key: "support", label: "Support" },
  { key: "monitoring", label: "Monitoring & Observability" },
  { key: "finance", label: "Finance & Payments" },
  { key: "crm", label: "CRM" },
  { key: "messaging", label: "Messaging" },
];

// Category order used in the custom wizard dropdown
const categoryOrder: { key: string; label: string }[] = [
  { key: "google", label: "Google Workspace" },
  { key: "microsoft", label: "Microsoft 365" },
  { key: "atlassian", label: "Atlassian" },
  ...standaloneCategoryOrder,
  { key: "custom", label: "Custom Integrations" },
];

// ── Helpers ─────────────────────────────────────────────────────────────────

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

function formatDate(dateStr: string) {
  return new Date(dateStr).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

/** Set of all provider keys that belong to any platform group. */
const platformProviderKeys = new Set(platformGroups.flatMap((g) => g.services));

// ── OAuth Not Configured Notice ─────────────────────────────────────────────

function OAuthNotConfiguredNotice({ provider, onClose }: { provider: MCPProviderInfo; onClose: () => void }) {
  const meta = providerMeta[provider.name];

  return (
    <Card className={`border-l-4 ${meta?.borderColor ?? ""}`}>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-base">
            <AlertTriangle className="h-4 w-4 text-amber-500" />
            OAuth Not Configured — {meta?.label ?? provider.name}
          </CardTitle>
          <Button variant="ghost" size="icon" onClick={onClose} className="h-7 w-7">
            <X className="h-4 w-4" />
          </Button>
        </div>
        <CardDescription>
          This integration requires OAuth credentials to be configured on the server.
          Ask your administrator to set the <code className="text-xs bg-muted rounded px-1">{provider.name.toUpperCase()}_CLIENT_ID</code> and <code className="text-xs bg-muted rounded px-1">{provider.name.toUpperCase()}_CLIENT_SECRET</code> environment variables.
          {meta?.docsUrl && (
            <a href={meta.docsUrl} target="_blank" rel="noopener noreferrer" className="ml-1 inline-flex items-center gap-0.5 text-blue-500 hover:underline">
              Docs <ExternalLink className="h-3 w-3" />
            </a>
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="rounded-lg border border-amber-200 bg-amber-50 dark:bg-amber-950 dark:border-amber-900 p-3 text-sm text-amber-800 dark:text-amber-200">
          <p className="flex items-center gap-1.5 font-medium">
            <Shield className="h-4 w-4" />
            All integrations connect via OAuth
          </p>
          <p className="mt-1 text-xs text-amber-700 dark:text-amber-300">
            When you click Connect, you&apos;ll be redirected to {meta?.label ?? provider.name}&apos;s login page to authorize access.
            No tokens or API keys need to be managed manually — we handle token storage, refresh, and expiry automatically.
          </p>
        </div>
        <div className="flex gap-2 pt-3">
          <Button variant="outline" onClick={onClose} size="sm">Close</Button>
        </div>
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

// ── Service Row (individual service within a platform card) ─────────────────

function ServiceRow({
  providerName,
  isConnected,
  hasOAuth,
  connection,
  onConnect,
  onNotConfigured,
}: {
  providerName: string;
  isConnected: boolean;
  hasOAuth: boolean;
  connection?: MCPConnection;
  onConnect: () => void;
  onDisconnect: (id: string) => void;
  onNotConfigured: () => void;
}) {
  const meta = providerMeta[providerName];
  const Icon = getMCPIcon(providerName);
  const label = meta?.label ?? providerName;
  const description = meta?.description ?? "";
  const disconnectMutation = useDisconnectIntegration();
  const testMutation = useTestIntegration();
  const [showLog, setShowLog] = useState(false);

  return (
    <div className="group">
      <div className="flex items-center gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-muted/50">
        {/* Icon */}
        <span
          className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg transition-all ${
            isConnected ? "bg-background shadow-sm" : "bg-muted/60 grayscale opacity-50 group-hover:grayscale-0 group-hover:opacity-100"
          }`}
        >
          {Icon ? <Icon size={20} /> : <Plug className="h-4 w-4 text-muted-foreground" />}
        </span>

        {/* Info */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium">{label}</span>
            {isConnected && (
              <Badge variant="default" className="bg-emerald-600 hover:bg-emerald-600 h-5 text-[10px] px-1.5">
                <CheckCircle className="mr-0.5 h-2.5 w-2.5" /> Connected
              </Badge>
            )}
          </div>
          <p className="text-xs text-muted-foreground truncate">{description}</p>
          {isConnected && connection && (
            <p className="text-[10px] text-muted-foreground mt-0.5">
              Since {formatDate(connection.connected_at)}
              {connection.last_used_at && <> · Last used {formatDate(connection.last_used_at)}</>}
            </p>
          )}
        </div>

        {/* Actions */}
        <div className="flex items-center gap-1.5 shrink-0">
          {isConnected && connection ? (
            <>
              <TooltipProvider delayDuration={200}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7"
                      onClick={() => testMutation.mutate(connection.id)}
                      disabled={testMutation.isPending}
                    >
                      {testMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Activity className="h-3.5 w-3.5" />}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>Test connection</TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <TooltipProvider delayDuration={200}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7"
                      onClick={() => setShowLog(!showLog)}
                    >
                      {showLog ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>View logs</TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <TooltipProvider delayDuration={200}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-muted-foreground hover:text-red-500"
                      onClick={() => disconnectMutation.mutate(connection.id)}
                      disabled={disconnectMutation.isPending}
                    >
                      {disconnectMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Unlink className="h-3.5 w-3.5" />}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>Disconnect</TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </>
          ) : (
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs"
              onClick={() => {
                if (hasOAuth) {
                  onConnect();
                } else {
                  onNotConfigured();
                }
              }}
            >
              <Link2 className="mr-1 h-3 w-3" /> Connect
            </Button>
          )}
        </div>
      </div>

      {/* Test result */}
      {testMutation.isSuccess && (
        <div className={`px-14 pb-1 text-xs ${testMutation.data.success ? "text-emerald-600" : "text-red-500"}`}>
          {testMutation.data.success ? <CheckCircle className="mr-1 inline h-3 w-3" /> : <XCircle className="mr-1 inline h-3 w-3" />}
          {testMutation.data.success ? "Connection verified" : testMutation.data.error ?? "Test failed"}
        </div>
      )}

      {/* Inline call log */}
      {showLog && connection && (
        <div className="px-14 pb-2">
          <CallLogSection connectionId={connection.id} />
        </div>
      )}
    </div>
  );
}

// ── Platform Card (expandable card for a platform group) ────────────────────

function PlatformCard({
  platform,
  providers,
  connectionMap,
  oauthEnabled,
  onNotConfigured,
}: {
  platform: PlatformGroupDef;
  providers: MCPProviderInfo[];
  connectionMap: Record<string, MCPConnection>;
  oauthEnabled: Record<string, boolean>;
  onNotConfigured: (providerName: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const PlatformIcon = platform.icon;

  // Calculate connection stats
  const connectedServices = platform.services.filter((s) => connectionMap[s]);
  const totalServices = platform.services.length;
  const isAnyConnected = connectedServices.length > 0;
  const isAllConnected = connectedServices.length === totalServices;

  // Get earliest connection date
  const earliestConnection = connectedServices.length > 0
    ? connectedServices.reduce((earliest, s) => {
        const date = connectionMap[s]?.connected_at;
        return date && (!earliest || date < earliest) ? date : earliest;
      }, "" as string)
    : null;

  // Check if any service has OAuth configured
  const hasAnyOAuth = platform.services.some((s) => oauthEnabled[s]);

  const handleConnectAll = () => {
    // Find the first service that's not connected and has OAuth
    const firstDisconnected = platform.services.find((s) => !connectionMap[s] && oauthEnabled[s]);
    if (firstDisconnected) {
      window.location.href = integrationsApi.oauthUrl(firstDisconnected);
    } else if (!hasAnyOAuth) {
      onNotConfigured(platform.services[0]);
    }
  };

  const handleReconnect = () => {
    const firstService = platform.services[0];
    if (oauthEnabled[firstService]) {
      window.location.href = integrationsApi.oauthUrl(firstService);
    }
  };

  return (
    <Card
      className={`relative overflow-hidden transition-all duration-200 ${
        isAnyConnected ? `border-l-4 ${platform.borderColor}` : "border"
      } ${expanded ? "shadow-md" : "hover:shadow-sm"}`}
    >
      {/* Background gradient for connected state */}
      {isAnyConnected && (
        <div className={`absolute inset-0 bg-gradient-to-r ${platform.gradient} pointer-events-none`} />
      )}

      {/* Header */}
      <div className="relative">
        <div
          className="flex items-center gap-4 px-5 py-4 cursor-pointer select-none"
          onClick={() => setExpanded(!expanded)}
        >
          {/* Platform icon */}
          <span
            className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-xl transition-all ${
              isAnyConnected
                ? "bg-background shadow-sm"
                : "bg-muted/60 grayscale opacity-50"
            }`}
          >
            <PlatformIcon size={28} />
          </span>

          {/* Platform info */}
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2.5">
              <h3 className="text-sm font-semibold">{platform.label}</h3>
              {isAllConnected ? (
                <Badge variant="default" className="bg-emerald-600 hover:bg-emerald-600 h-5 text-[10px] px-1.5">
                  <CheckCircle className="mr-0.5 h-2.5 w-2.5" /> All Connected
                </Badge>
              ) : isAnyConnected ? (
                <Badge variant="secondary" className="h-5 text-[10px] px-1.5">
                  {connectedServices.length}/{totalServices} Connected
                </Badge>
              ) : null}
            </div>
            <p className="text-xs text-muted-foreground mt-0.5">{platform.description}</p>
            {earliestConnection && (
              <p className="text-[10px] text-muted-foreground mt-0.5">
                Connected since {formatDate(earliestConnection)}
              </p>
            )}
          </div>

          {/* Service icons preview (when collapsed) */}
          {!expanded && (
            <div className="hidden sm:flex items-center gap-1">
              {platform.services.map((s) => {
                const SIcon = getMCPIcon(s);
                const connected = !!connectionMap[s];
                return (
                  <TooltipProvider key={s} delayDuration={200}>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span
                          className={`relative flex h-7 w-7 items-center justify-center rounded-md border transition-all ${
                            connected
                              ? "bg-background border-emerald-200 dark:border-emerald-800"
                              : "bg-muted/40 border-transparent opacity-40"
                          }`}
                        >
                          {SIcon ? <SIcon size={16} /> : <Plug className="h-3.5 w-3.5 text-muted-foreground" />}
                          {connected && (
                            <span className="absolute -top-1 -right-1 flex h-3.5 w-3.5 items-center justify-center rounded-full bg-emerald-500">
                              <CheckCircle className="h-2 w-2 text-white" />
                            </span>
                          )}
                        </span>
                      </TooltipTrigger>
                      <TooltipContent side="bottom" className="text-xs">
                        {providerMeta[s]?.label ?? s} — {connected ? "Connected" : "Not connected"}
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                );
              })}
            </div>
          )}

          {/* Quick actions & expand toggle */}
          <div className="flex items-center gap-2 shrink-0">
            {!isAnyConnected && !expanded && (
              <Button
                variant="default"
                size="sm"
                className="h-8 text-xs"
                onClick={(e) => {
                  e.stopPropagation();
                  handleConnectAll();
                }}
              >
                <Link2 className="mr-1.5 h-3 w-3" /> Connect
              </Button>
            )}
            {isAnyConnected && !expanded && (
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={(e) => {
                  e.stopPropagation();
                  setExpanded(true);
                }}
              >
                <Settings2 className="mr-1.5 h-3 w-3" /> Manage
              </Button>
            )}
            <ChevronDown
              className={`h-4 w-4 text-muted-foreground transition-transform duration-200 ${
                expanded ? "rotate-180" : ""
              }`}
            />
          </div>
        </div>
      </div>

      {/* Expanded services list */}
      {expanded && (
        <div className="relative border-t">
          {/* Platform-level actions bar */}
          {isAnyConnected && (
            <div className="flex items-center gap-2 px-5 py-2.5 bg-muted/30 border-b">
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-xs"
                onClick={handleReconnect}
              >
                <RefreshCw className="mr-1 h-3 w-3" /> Reconnect
              </Button>
              {!isAllConnected && (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs"
                  onClick={handleConnectAll}
                >
                  <Link2 className="mr-1 h-3 w-3" /> Connect Remaining
                </Button>
              )}
              <div className="flex-1" />
              <span className="text-[10px] text-muted-foreground uppercase tracking-wider font-medium">
                {connectedServices.length} of {totalServices} services active
              </span>
            </div>
          )}

          {/* Service rows */}
          <div className="divide-y">
            {platform.services.map((serviceName) => {
              const provider = providers.find((p) => p.name === serviceName);
              if (!provider) return null;
              return (
                <ServiceRow
                  key={serviceName}
                  providerName={serviceName}
                  isConnected={!!connectionMap[serviceName]}
                  hasOAuth={!!oauthEnabled[serviceName]}
                  connection={connectionMap[serviceName]}
                  onConnect={() => {
                    window.location.href = integrationsApi.oauthUrl(serviceName);
                  }}
                  onDisconnect={() => {}}
                  onNotConfigured={() => onNotConfigured(serviceName)}
                />
              );
            })}
          </div>
        </div>
      )}
    </Card>
  );
}

// ── Standalone Provider Card (for non-platform providers) ───────────────────

function StandaloneProviderCard({
  provider,
  isConnected,
  hasOAuth,
  connection,
  onNotConfigured,
  onSelect,
  isSelected,
}: {
  provider: MCPProviderInfo;
  isConnected: boolean;
  hasOAuth: boolean;
  connection?: MCPConnection;
  onNotConfigured: () => void;
  onSelect: () => void;
  isSelected: boolean;
}) {
  const meta = providerMeta[provider.name];
  const Icon = getMCPIcon(provider.name);
  const label = meta?.label ?? provider.name;
  const description = meta?.description ?? provider.description ?? "";

  const handleConnect = () => {
    if (hasOAuth) {
      window.location.href = integrationsApi.oauthUrl(provider.name);
    } else {
      onNotConfigured();
    }
  };

  return (
    <Card
      className={`relative overflow-hidden transition-all duration-200 cursor-pointer ${
        isConnected ? `border-l-4 ${meta?.borderColor ?? ""}` : "border hover:border-foreground/20"
      } ${isSelected ? "ring-2 ring-ring shadow-md" : "hover:shadow-sm"}`}
      onClick={() => {
        if (isConnected) onSelect();
      }}
    >
      {isConnected && (
        <div className={`absolute inset-0 bg-gradient-to-r ${meta?.gradient ?? ""} pointer-events-none`} />
      )}
      <div className="relative flex items-center gap-3 px-4 py-3">
        {/* Icon */}
        <span
          className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-xl transition-all ${
            isConnected ? "bg-background shadow-sm" : "bg-muted/60 grayscale opacity-50"
          }`}
        >
          {Icon ? <Icon size={24} /> : <Plug className="h-5 w-5 text-muted-foreground" />}
        </span>

        {/* Info */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium">{label}</span>
            {isConnected && (
              <Badge variant="default" className="bg-emerald-600 hover:bg-emerald-600 h-5 text-[10px] px-1.5">
                <CheckCircle className="mr-0.5 h-2.5 w-2.5" /> Connected
              </Badge>
            )}
          </div>
          <p className="text-xs text-muted-foreground truncate">{description}</p>
          {isConnected && connection && (
            <p className="text-[10px] text-muted-foreground mt-0.5">
              Since {formatDate(connection.connected_at)}
            </p>
          )}
        </div>

        {/* Action */}
        <div className="shrink-0">
          {isConnected ? (
            <Button
              variant="ghost"
              size="sm"
              className="h-8 text-xs"
              onClick={(e) => {
                e.stopPropagation();
                onSelect();
              }}
            >
              <Settings2 className="mr-1 h-3 w-3" /> Manage
            </Button>
          ) : (
            <Button
              variant="default"
              size="sm"
              className="h-8 text-xs"
              onClick={(e) => {
                e.stopPropagation();
                handleConnect();
              }}
            >
              <Link2 className="mr-1.5 h-3 w-3" /> Connect
            </Button>
          )}
        </div>
      </div>
    </Card>
  );
}

// ── Connected Detail Panel (expanded details for a selected standalone) ─────

function ConnectedDetailPanel({
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
          Connected {formatDate(connection.connected_at)}
          {connection.last_used_at && <> · Last used {formatDate(connection.last_used_at)}</>}
          {connection.expires_at && <> · Expires {formatDate(connection.expires_at)}</>}
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
            onClick={() => {
              window.location.href = integrationsApi.oauthUrl(connection.provider);
            }}
          >
            <RefreshCw className="mr-1 h-3 w-3" /> Reconnect
          </Button>
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

// ── Custom MCP Template Wizard ──────────────────────────────────────────────

const emptyAction: CustomMCPActionDef = {
  name: "",
  description: "",
  method: "GET",
  path: "",
  required_params: [],
  optional_params: [],
  body_template: "",
  consent_required: false,
};

type WizardStep = "basics" | "auth" | "actions" | "review";
const WIZARD_STEPS: { key: WizardStep; label: string; icon: React.ElementType }[] = [
  { key: "basics", label: "Basics", icon: Globe },
  { key: "auth", label: "Authentication", icon: Lock },
  { key: "actions", label: "Actions", icon: Code2 },
  { key: "review", label: "Review & Create", icon: Zap },
];

function CustomMCPWizard({ onClose }: { onClose: () => void }) {
  const [step, setStep] = useState<WizardStep>("basics");
  const [name, setName] = useState("");
  const [label, setLabel] = useState("");
  const [category, setCategory] = useState("custom");
  const [description, setDescription] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [authType, setAuthType] = useState<"bearer" | "basic" | "header" | "query">("bearer");
  const [authHeader, setAuthHeader] = useState("");
  const [actions, setActions] = useState<CustomMCPActionDef[]>([{ ...emptyAction }]);
  const [simToken, setSimToken] = useState("");

  const createMutation = useCreateCustomTemplate();
  const validateMutation = useValidateCustomTemplate();
  const simulateMutation = useSimulateCustomConnection();

  const [validationErrors, setValidationErrors] = useState<MCPValidationError[]>([]);

  const fieldError = useCallback(
    (field: string) => validationErrors.find((e) => e.field === field)?.message,
    [validationErrors],
  );

  const stepIdx = WIZARD_STEPS.findIndex((s) => s.key === step);

  const goPrev = () => {
    if (stepIdx > 0) setStep(WIZARD_STEPS[stepIdx - 1].key);
  };

  const goNext = () => {
    if (stepIdx < WIZARD_STEPS.length - 1) setStep(WIZARD_STEPS[stepIdx + 1].key);
  };

  const buildTemplate = () => ({
    name,
    label,
    category,
    description,
    base_url: baseUrl,
    auth_type: authType,
    auth_header: authHeader,
    actions: actions.filter((a) => a.name.trim() !== ""),
  });

  const handleValidate = () => {
    validateMutation.mutate(buildTemplate() as Parameters<typeof validateMutation.mutate>[0], {
      onSuccess: (data: { validation_errors?: MCPValidationError[] }) => {
        if (data.validation_errors?.length) {
          setValidationErrors(data.validation_errors);
        } else {
          setValidationErrors([]);
        }
      },
    });
  };

  const handleSimulate = () => {
    simulateMutation.mutate({ base_url: baseUrl, token: simToken, auth_type: authType, auth_header: authHeader });
  };

  const handleCreate = () => {
    createMutation.mutate(buildTemplate() as Parameters<typeof createMutation.mutate>[0], {
      onSuccess: (data: CustomMCPTemplate | { validation_errors: MCPValidationError[] }) => {
        if ("validation_errors" in data && data.validation_errors?.length) {
          setValidationErrors(data.validation_errors);
        } else {
          onClose();
        }
      },
    });
  };

  const updateAction = (idx: number, patch: Partial<CustomMCPActionDef>) => {
    setActions((prev) => prev.map((a, i) => (i === idx ? { ...a, ...patch } : a)));
  };

  const removeAction = (idx: number) => {
    setActions((prev) => prev.filter((_, i) => i !== idx));
  };

  const addAction = () => {
    setActions((prev) => [...prev, { ...emptyAction }]);
  };

  return (
    <Card className="border-l-4 border-l-violet-500">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2 text-base">
            <Zap className="h-4 w-4 text-violet-500" />
            Create Custom MCP Integration
          </CardTitle>
          <Button variant="ghost" size="icon" onClick={onClose} className="h-7 w-7">
            <X className="h-4 w-4" />
          </Button>
        </div>
        <CardDescription>
          Define a custom API integration that swarm agents can use as a tool.
        </CardDescription>

        {/* Step indicator */}
        <div className="flex items-center gap-1 pt-2">
          {WIZARD_STEPS.map(({ key, label: sLabel, icon: SIcon }, i) => (
            <Fragment key={key}>
              {i > 0 && <div className="h-px w-6 bg-border" />}
              <button
                type="button"
                onClick={() => setStep(key)}
                className={`flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium transition-colors ${
                  step === key
                    ? "bg-violet-100 text-violet-700 dark:bg-violet-900 dark:text-violet-300"
                    : i < stepIdx
                    ? "bg-emerald-50 text-emerald-700 dark:bg-emerald-900 dark:text-emerald-300"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <SIcon className="h-3 w-3" />
                {sLabel}
              </button>
            </Fragment>
          ))}
        </div>
      </CardHeader>

      <CardContent className="space-y-4">
        {/* ─── Step: Basics ─── */}
        {step === "basics" && (
          <div className="space-y-3">
            <div>
              <Label>Name <span className="text-muted-foreground text-xs">(snake_case, unique)</span></Label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my_custom_api" />
              {fieldError("name") && <p className="text-xs text-red-500 mt-1">{fieldError("name")}</p>}
            </div>
            <div>
              <Label>Display Label</Label>
              <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="My Custom API" />
              {fieldError("label") && <p className="text-xs text-red-500 mt-1">{fieldError("label")}</p>}
            </div>
            <div>
              <Label>Description</Label>
              <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Short description of what this integration does" />
            </div>
            <div>
              <Label>Category</Label>
              <select
                value={category}
                onChange={(e) => setCategory(e.target.value)}
                className="flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm transition-colors"
              >
                {categoryOrder.map((c) => (
                  <option key={c.key} value={c.key}>{c.label}</option>
                ))}
                <option value="other">Other</option>
              </select>
            </div>
            <div>
              <Label>Base URL</Label>
              <Input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://api.example.com/v1" />
              {fieldError("base_url") && <p className="text-xs text-red-500 mt-1">{fieldError("base_url")}</p>}
            </div>
          </div>
        )}

        {/* ─── Step: Auth ─── */}
        {step === "auth" && (
          <div className="space-y-3">
            <div>
              <Label>Authentication Type</Label>
              <div className="grid grid-cols-2 gap-2 mt-1">
                {(["bearer", "basic", "header", "query"] as const).map((at) => (
                  <button
                    key={at}
                    type="button"
                    onClick={() => setAuthType(at)}
                    className={`flex items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors ${
                      authType === at
                        ? "border-violet-500 bg-violet-50 text-violet-700 dark:bg-violet-900 dark:text-violet-300"
                        : "border-border hover:border-foreground/20"
                    }`}
                  >
                    <Lock className="h-3.5 w-3.5" />
                    <div className="text-left">
                      <div className="font-medium capitalize">{at}</div>
                      <div className="text-[10px] text-muted-foreground">
                        {at === "bearer" && "Authorization: Bearer <token>"}
                        {at === "basic" && "Authorization: Basic <token>"}
                        {at === "header" && "Custom header name"}
                        {at === "query" && "?api_key=<token>"}
                      </div>
                    </div>
                  </button>
                ))}
              </div>
              {fieldError("auth_type") && <p className="text-xs text-red-500 mt-1">{fieldError("auth_type")}</p>}
            </div>
            {authType === "header" && (
              <div>
                <Label>Custom Header Name</Label>
                <Input value={authHeader} onChange={(e) => setAuthHeader(e.target.value)} placeholder="X-API-Key" />
                {fieldError("auth_header") && <p className="text-xs text-red-500 mt-1">{fieldError("auth_header")}</p>}
              </div>
            )}

            {/* Simulation test */}
            <div className="rounded-lg border border-dashed p-3 space-y-2">
              <Label className="flex items-center gap-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                <Play className="h-3 w-3" /> Test Connection
              </Label>
              <Input
                type="password"
                value={simToken}
                onChange={(e) => setSimToken(e.target.value)}
                placeholder="Paste a test token/key..."
              />
              <Button
                variant="outline"
                size="sm"
                onClick={handleSimulate}
                disabled={!baseUrl || !simToken || simulateMutation.isPending}
              >
                {simulateMutation.isPending ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : <Activity className="mr-1 h-3 w-3" />}
                Simulate
              </Button>
              {simulateMutation.isSuccess && (
                <p className={`text-xs ${simulateMutation.data.success ? "text-emerald-600" : "text-red-500"}`}>
                  {simulateMutation.data.success
                    ? <><CheckCircle className="mr-1 inline h-3 w-3" /> Connection successful!</>
                    : <><XCircle className="mr-1 inline h-3 w-3" /> {simulateMutation.data.error}</>}
                </p>
              )}
            </div>
          </div>
        )}

        {/* ─── Step: Actions ─── */}
        {step === "actions" && (
          <div className="space-y-3">
            {fieldError("actions") && <p className="text-xs text-red-500">{fieldError("actions")}</p>}
            {actions.map((action, idx) => (
              <div key={idx} className="rounded-lg border p-3 space-y-2 relative">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-muted-foreground">Action {idx + 1}</span>
                  {actions.length > 1 && (
                    <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => removeAction(idx)}>
                      <Trash2 className="h-3 w-3" />
                    </Button>
                  )}
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <Label className="text-xs">Name</Label>
                    <Input value={action.name} onChange={(e) => updateAction(idx, { name: e.target.value })} placeholder="list_items" className="h-8 text-xs" />
                    {fieldError(`actions[${idx}].name`) && <p className="text-[10px] text-red-500">{fieldError(`actions[${idx}].name`)}</p>}
                  </div>
                  <div>
                    <Label className="text-xs">HTTP Method</Label>
                    <select
                      value={action.method}
                      onChange={(e) => updateAction(idx, { method: e.target.value as CustomMCPActionDef["method"] })}
                      className="flex h-8 w-full rounded-md border border-input bg-background px-2 text-xs shadow-sm"
                    >
                      {["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => (
                        <option key={m} value={m}>{m}</option>
                      ))}
                    </select>
                  </div>
                </div>
                <div>
                  <Label className="text-xs">Description</Label>
                  <Input value={action.description} onChange={(e) => updateAction(idx, { description: e.target.value })} placeholder="List all items from the API" className="h-8 text-xs" />
                  {fieldError(`actions[${idx}].description`) && <p className="text-[10px] text-red-500">{fieldError(`actions[${idx}].description`)}</p>}
                </div>
                <div>
                  <Label className="text-xs">Path <span className="text-muted-foreground">(use {"{param}"} for interpolation)</span></Label>
                  <Input value={action.path} onChange={(e) => updateAction(idx, { path: e.target.value })} placeholder="/items/{item_id}" className="h-8 text-xs font-mono" />
                  {fieldError(`actions[${idx}].path`) && <p className="text-[10px] text-red-500">{fieldError(`actions[${idx}].path`)}</p>}
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <Label className="text-xs">Required Params <span className="text-muted-foreground">(comma-sep)</span></Label>
                    <Input
                      value={action.required_params?.join(", ") ?? ""}
                      onChange={(e) => updateAction(idx, { required_params: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })}
                      placeholder="item_id, name"
                      className="h-8 text-xs"
                    />
                  </div>
                  <div>
                    <Label className="text-xs">Optional Params <span className="text-muted-foreground">(comma-sep)</span></Label>
                    <Input
                      value={action.optional_params?.join(", ") ?? ""}
                      onChange={(e) => updateAction(idx, { optional_params: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })}
                      placeholder="limit, offset"
                      className="h-8 text-xs"
                    />
                  </div>
                </div>
                {(action.method === "POST" || action.method === "PUT" || action.method === "PATCH") && (
                  <div>
                    <Label className="text-xs">Body Template <span className="text-muted-foreground">(JSON with {"{{param}}"} placeholders)</span></Label>
                    <textarea
                      value={action.body_template ?? ""}
                      onChange={(e) => updateAction(idx, { body_template: e.target.value })}
                      placeholder={'{"name": "{{name}}", "value": "{{value}}"}'}
                      rows={3}
                      className="flex w-full rounded-md border border-input bg-background px-3 py-2 text-xs font-mono shadow-sm resize-y"
                    />
                    {fieldError(`actions[${idx}].body_template`) && <p className="text-[10px] text-red-500">{fieldError(`actions[${idx}].body_template`)}</p>}
                  </div>
                )}
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={action.consent_required}
                    onChange={(e) => updateAction(idx, { consent_required: e.target.checked })}
                    className="h-3.5 w-3.5 rounded border-gray-300"
                  />
                  <Label className="text-xs">Require user consent before executing</Label>
                </div>
              </div>
            ))}
            <Button variant="outline" size="sm" onClick={addAction}>
              <Plus className="mr-1 h-3 w-3" /> Add Action
            </Button>
          </div>
        )}

        {/* ─── Step: Review ─── */}
        {step === "review" && (
          <div className="space-y-3">
            <div className="rounded-lg border p-3 space-y-2 text-sm">
              <div className="grid grid-cols-2 gap-x-4 gap-y-1">
                <span className="text-muted-foreground">Name:</span>
                <span className="font-mono">{name || "—"}</span>
                <span className="text-muted-foreground">Label:</span>
                <span>{label || "—"}</span>
                <span className="text-muted-foreground">Base URL:</span>
                <span className="font-mono text-xs break-all">{baseUrl || "—"}</span>
                <span className="text-muted-foreground">Auth:</span>
                <span className="capitalize">{authType}{authType === "header" && authHeader ? ` (${authHeader})` : ""}</span>
                <span className="text-muted-foreground">Actions:</span>
                <span>{actions.filter((a) => a.name).length}</span>
              </div>
              {actions.filter((a) => a.name).length > 0 && (
                <div className="flex flex-wrap gap-1 pt-1">
                  {actions.filter((a) => a.name).map((a) => (
                    <Badge key={a.name} variant="secondary" className="text-xs font-mono">
                      {a.method} {a.name}
                    </Badge>
                  ))}
                </div>
              )}
            </div>

            {/* Inline validation */}
            <Button variant="outline" size="sm" onClick={handleValidate} disabled={validateMutation.isPending}>
              {validateMutation.isPending ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : <CheckCircle className="mr-1 h-3 w-3" />}
              Validate
            </Button>

            {validationErrors.length > 0 && (
              <div className="rounded-lg border border-red-200 bg-red-50 dark:bg-red-950 dark:border-red-900 p-3 space-y-1">
                <p className="text-xs font-semibold text-red-700 dark:text-red-400 flex items-center gap-1">
                  <AlertTriangle className="h-3 w-3" /> Validation Errors
                </p>
                {validationErrors.map((e, i) => (
                  <p key={i} className="text-xs text-red-600 dark:text-red-400">
                    <span className="font-mono">{e.field}</span>: {e.message}
                  </p>
                ))}
              </div>
            )}

            {validateMutation.isSuccess && validationErrors.length === 0 && (
              <p className="text-xs text-emerald-600 flex items-center gap-1">
                <CheckCircle className="h-3 w-3" /> Template is valid!
              </p>
            )}
          </div>
        )}

        {/* ─── Navigation ─── */}
        <div className="flex items-center justify-between pt-2 border-t">
          <Button variant="ghost" size="sm" onClick={goPrev} disabled={stepIdx === 0}>
            <ArrowLeft className="mr-1 h-3 w-3" /> Back
          </Button>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={onClose}>Cancel</Button>
            {stepIdx < WIZARD_STEPS.length - 1 ? (
              <Button size="sm" onClick={goNext}>
                Next <ArrowRight className="ml-1 h-3 w-3" />
              </Button>
            ) : (
              <Button size="sm" onClick={handleCreate} disabled={createMutation.isPending}>
                {createMutation.isPending ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : <Zap className="mr-1 h-3 w-3" />}
                Create Integration
              </Button>
            )}
          </div>
        </div>

        {createMutation.isError && (
          <p className="text-sm text-red-500">
            <AlertTriangle className="mr-1 inline h-3 w-3" />
            {(createMutation.error as Error)?.message ?? "Failed to create template."}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

// ── Custom Template Card ────────────────────────────────────────────────────

function CustomTemplateCard({ template, onDelete }: { template: CustomMCPTemplate; onDelete: () => void }) {
  const deleteMutation = useDeleteCustomTemplate();

  return (
    <div className="flex items-center justify-between rounded-lg border px-3 py-2">
      <div className="flex items-center gap-3 min-w-0">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-violet-100 dark:bg-violet-900">
          <Zap className="h-4 w-4 text-violet-600 dark:text-violet-400" />
        </div>
        <div className="min-w-0">
          <p className="text-sm font-medium truncate">{template.label}</p>
          <p className="text-xs text-muted-foreground truncate">
            <span className="font-mono">{template.name}</span> · {template.actions.length} action{template.actions.length !== 1 ? "s" : ""} · {template.auth_type}
          </p>
        </div>
      </div>
      <Button
        variant="ghost"
        size="icon"
        className="h-7 w-7 shrink-0"
        onClick={() => deleteMutation.mutate(template.id, { onSuccess: onDelete })}
        disabled={deleteMutation.isPending}
      >
        {deleteMutation.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3 text-muted-foreground hover:text-red-500" />}
      </Button>
    </div>
  );
}

// ── Main Component ──────────────────────────────────────────────────────────

export function IntegrationsSettings() {
  const { data: connections, isLoading: connectionsLoading } = useIntegrations();
  const { data: providers, isLoading: providersLoading } = useIntegrationProviders();
  const { data: oauthStatusData } = useIntegrationOAuthStatus();
  const { data: customTemplates, refetch: refetchTemplates } = useCustomTemplates();
  const [notConfiguredProvider, setNotConfiguredProvider] = useState<string | null>(null);
  const [selectedConnection, setSelectedConnection] = useState<string | null>(null);
  const [showCustomWizard, setShowCustomWizard] = useState(false);

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

  // Group standalone providers by category (excluding platform-grouped ones)
  const standaloneGrouped = useMemo(() => {
    const allProviders = (providers as MCPProviderInfo[] | undefined) ?? [];
    const groups: Record<string, MCPProviderInfo[]> = {};
    for (const p of allProviders) {
      if (platformProviderKeys.has(p.name)) continue;
      const cat = p.category || providerMeta[p.name]?.category || "other";
      if (!groups[cat]) groups[cat] = [];
      groups[cat].push(p);
    }
    return groups;
  }, [providers]);

  // Count connected
  const connectedCount = Object.keys(connectionMap).length;
  const totalCount = (providers as MCPProviderInfo[] | undefined)?.length ?? 0;
  const allProviders = (providers as MCPProviderInfo[] | undefined) ?? [];

  if (isLoading) {
    return (
      <Card className="max-w-4xl">
        <CardContent className="py-8 space-y-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-20 w-full rounded-xl" />
          ))}
          <div className="grid grid-cols-2 gap-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full rounded-xl" />
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
          Connect external services via OAuth to enable swarm agents to act on your behalf.
          Tokens are managed automatically — no manual API keys needed.
          {connectedCount > 0 && (
            <span className="ml-2 inline-flex items-center gap-1">
              <CheckCircle className="h-3 w-3 text-emerald-500" />
              {connectedCount}/{totalCount} connected
            </span>
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-8">

        {/* ─── Platform Groups ─── */}
        <section className="space-y-3">
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-2">
            <Shield className="h-3.5 w-3.5" />
            Platform Suites
          </h3>
          <p className="text-xs text-muted-foreground -mt-1">
            Unified OAuth — one credential connects all services within a platform.
          </p>
          <div className="space-y-3">
            {platformGroups.map((platform) => (
              <PlatformCard
                key={platform.key}
                platform={platform}
                providers={allProviders}
                connectionMap={connectionMap}
                oauthEnabled={oauthEnabled}
                onNotConfigured={(providerName) => {
                  setNotConfiguredProvider(providerName);
                  setSelectedConnection(null);
                }}
              />
            ))}
          </div>
        </section>

        {/* ─── Standalone Providers by Category ─── */}
        {standaloneCategoryOrder.map(({ key: cat, label: catLabel }) => {
          const group = standaloneGrouped[cat];
          if (!group?.length) return null;
          return (
            <section key={cat} className="space-y-3">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                {catLabel}
              </h3>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                {group.map((p) => (
                  <StandaloneProviderCard
                    key={p.name}
                    provider={p}
                    isConnected={!!connectionMap[p.name]}
                    hasOAuth={!!oauthEnabled[p.name]}
                    connection={connectionMap[p.name]}
                    onNotConfigured={() => {
                      setNotConfiguredProvider(p.name);
                      setSelectedConnection(null);
                    }}
                    onSelect={() => {
                      setSelectedConnection(selectedConnection === p.name ? null : p.name);
                      setNotConfiguredProvider(null);
                    }}
                    isSelected={selectedConnection === p.name}
                  />
                ))}
              </div>
            </section>
          );
        })}

        {/* Show providers that don't fit known categories */}
        {(() => {
          const knownCats = new Set([
            ...platformGroups.map((g) => g.key),
            ...standaloneCategoryOrder.map((c) => c.key),
          ]);
          const otherProviders = Object.entries(standaloneGrouped)
            .filter(([cat]) => !knownCats.has(cat))
            .flatMap(([, pList]) => pList);
          if (!otherProviders.length) return null;
          return (
            <section className="space-y-3">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Other
              </h3>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                {otherProviders.map((p) => (
                  <StandaloneProviderCard
                    key={p.name}
                    provider={p}
                    isConnected={!!connectionMap[p.name]}
                    hasOAuth={!!oauthEnabled[p.name]}
                    connection={connectionMap[p.name]}
                    onNotConfigured={() => {
                      setNotConfiguredProvider(p.name);
                      setSelectedConnection(null);
                    }}
                    onSelect={() => {
                      setSelectedConnection(selectedConnection === p.name ? null : p.name);
                      setNotConfiguredProvider(null);
                    }}
                    isSelected={selectedConnection === p.name}
                  />
                ))}
              </div>
            </section>
          );
        })()}

        {/* ─── Expanded detail panel for selected standalone provider ─── */}
        {selectedConnection && connectionMap[selectedConnection] && (
          <ConnectedDetailPanel
            connection={connectionMap[selectedConnection]}
            onClose={() => setSelectedConnection(null)}
          />
        )}

        {/* ─── OAuth not configured notice ─── */}
        {notConfiguredProvider && (() => {
          const prov = allProviders.find((p) => p.name === notConfiguredProvider);
          return prov ? (
            <OAuthNotConfiguredNotice provider={prov} onClose={() => setNotConfiguredProvider(null)} />
          ) : null;
        })()}

        {/* ─── Custom MCP Templates ─── */}
        <section className="space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Custom Integrations
            </h3>
            <Button
              variant="outline"
              size="sm"
              onClick={() => { setShowCustomWizard(true); setNotConfiguredProvider(null); setSelectedConnection(null); }}
              className="h-7 text-xs"
            >
              <Plus className="mr-1 h-3 w-3" /> Create Custom
            </Button>
          </div>

          {showCustomWizard && (
            <CustomMCPWizard onClose={() => { setShowCustomWizard(false); refetchTemplates(); }} />
          )}

          {(customTemplates as CustomMCPTemplate[] | undefined)?.length ? (
            <div className="space-y-2">
              {(customTemplates as CustomMCPTemplate[]).map((t) => (
                <CustomTemplateCard key={t.id} template={t} onDelete={() => refetchTemplates()} />
              ))}
            </div>
          ) : !showCustomWizard ? (
            <p className="text-xs text-muted-foreground">
              No custom integrations yet. Create one to connect any REST API as an agent tool.
            </p>
          ) : null}
        </section>

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
