"use client";

import { useState, useEffect, useCallback, useMemo } from "react";
import {
  Hammer,
  CheckCircle2,
  XCircle,
  Clock,
  Loader2,
  RefreshCw,
  ChevronDown,
  ChevronUp,
  Terminal,
  Shield,
  Container,
  Timer,
  AlertTriangle,
  Layers,
  Copy,
  Check,
  Search,
  KeyRound,
  CircleDot,
} from "lucide-react";
import type {
  SandboxBuild,
  BuildsSummary,
  BuildsResponse,
  BuildLogResponse,
  BuildStatus,
} from "@/types/swarm";
import type { SwarmWsEvent } from "@/hooks/use-swarm-events";

interface BuildValidationCardProps {
  taskId: string;
  refreshInterval?: number;
  events?: SwarmWsEvent[];
}

function statusConfig(status: BuildStatus): {
  label: string;
  color: string;
  bgColor: string;
  ringColor: string;
  icon: React.ReactNode;
} {
  switch (status) {
    case "success":
      return {
        label: "Passed",
        color: "text-emerald-600 dark:text-emerald-400",
        bgColor: "bg-emerald-100 dark:bg-emerald-900/40",
        ringColor: "ring-emerald-500/20",
        icon: <CheckCircle2 className="h-4 w-4" />,
      };
    case "failed":
      return {
        label: "Failed",
        color: "text-red-600 dark:text-red-400",
        bgColor: "bg-red-100 dark:bg-red-900/40",
        ringColor: "ring-red-500/20",
        icon: <XCircle className="h-4 w-4" />,
      };
    case "running":
      return {
        label: "Running",
        color: "text-blue-600 dark:text-blue-400",
        bgColor: "bg-blue-100 dark:bg-blue-900/40",
        ringColor: "ring-blue-500/20",
        icon: <Loader2 className="h-4 w-4 animate-spin" />,
      };
    case "blocked":
      return {
        label: "Blocked",
        color: "text-orange-600 dark:text-orange-400",
        bgColor: "bg-orange-100 dark:bg-orange-900/40",
        ringColor: "ring-orange-500/20",
        icon: <AlertTriangle className="h-4 w-4" />,
      };
    default:
      return {
        label: "Pending",
        color: "text-slate-500 dark:text-slate-400",
        bgColor: "bg-slate-100 dark:bg-slate-800/40",
        ringColor: "ring-slate-500/20",
        icon: <Clock className="h-4 w-4" />,
      };
  }
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const secs = ms / 1000;
  if (secs < 60) return `${secs.toFixed(1)}s`;
  const mins = Math.floor(secs / 60);
  const remainSecs = Math.floor(secs % 60);
  return `${mins}m ${remainSecs}s`;
}

function buildSystemLabel(sys: string): string {
  const map: Record<string, string> = {
    maven: "Maven",
    gradle: "Gradle",
    npm: "npm",
    yarn: "Yarn",
    pnpm: "pnpm",
    cargo: "Cargo",
    go: "Go",
    make: "Make",
    cmake: "CMake",
    pip: "pip",
    poetry: "Poetry",
    bazel: "Bazel",
    dotnet: ".NET",
    unknown: "Unknown",
  };
  return map[sys] ?? sys;
}

export function BuildValidationCard({
  taskId,
  refreshInterval = 0,
  events = [],
}: BuildValidationCardProps) {
  const [builds, setBuilds] = useState<SandboxBuild[]>([]);
  const [summary, setSummary] = useState<BuildsSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expandedBuild, setExpandedBuild] = useState<string | null>(null);
  const [logs, setLogs] = useState<Record<string, BuildLogResponse>>({});
  const [loadingLogs, setLoadingLogs] = useState<Record<string, boolean>>({});
  const [copied, setCopied] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const probeEvent = useMemo(() => {
    for (const evt of events) {
      if (
        evt.type === "swarm_agent" &&
        evt.event === "build_plan" &&
        evt.data
      ) {
        return evt;
      }
    }
    return null;
  }, [events]);

  const fetchBuilds = useCallback(async () => {
    try {
      const res = await fetch(`/api/v1/swarm/tasks/${taskId}/builds`);
      if (!res.ok) {
        if (res.status === 404) {
          setBuilds([]);
          setSummary(null);
          setError(null);
          return;
        }
        throw new Error(`HTTP ${res.status}`);
      }
      const data: BuildsResponse = await res.json();
      setBuilds(data.builds ?? []);
      setSummary(data.summary ?? null);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load builds");
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [taskId]);

  useEffect(() => {
    fetchBuilds();
  }, [fetchBuilds]);

  useEffect(() => {
    if (!refreshInterval || refreshInterval <= 0) return;
    const timer = setInterval(fetchBuilds, refreshInterval);
    return () => clearInterval(timer);
  }, [fetchBuilds, refreshInterval]);

  const fetchLog = useCallback(
    async (buildId: string) => {
      if (logs[buildId]) return;
      setLoadingLogs((prev) => ({ ...prev, [buildId]: true }));
      try {
        const res = await fetch(
          `/api/v1/swarm/tasks/${taskId}/builds/${buildId}/logs`,
        );
        if (res.ok) {
          const data: BuildLogResponse = await res.json();
          setLogs((prev) => ({ ...prev, [buildId]: data }));
        }
      } finally {
        setLoadingLogs((prev) => ({ ...prev, [buildId]: false }));
      }
    },
    [taskId, logs],
  );

  const toggleExpand = useCallback(
    (buildId: string) => {
      if (expandedBuild === buildId) {
        setExpandedBuild(null);
      } else {
        setExpandedBuild(buildId);
        fetchLog(buildId);
      }
    },
    [expandedBuild, fetchLog],
  );

  const handleCopy = useCallback((text: string, buildId: string) => {
    navigator.clipboard.writeText(text);
    setCopied(buildId);
    setTimeout(() => setCopied(null), 2000);
  }, []);

  const handleRefresh = useCallback(() => {
    setRefreshing(true);
    fetchBuilds();
  }, [fetchBuilds]);

  if (loading) {
    return (
      <div className="rounded-xl border bg-card">
        <div className="flex items-center gap-3 border-b px-5 py-4">
          <div className="h-5 w-5 animate-pulse rounded bg-muted" />
          <div className="h-4 w-32 animate-pulse rounded bg-muted" />
        </div>
        <div className="space-y-3 p-5">
          <div className="h-16 animate-pulse rounded-lg bg-muted" />
          <div className="h-16 animate-pulse rounded-lg bg-muted" />
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-xl border border-red-200 bg-red-50 p-5 dark:border-red-800 dark:bg-red-950/30">
        <div className="flex items-center gap-2 text-sm text-red-600 dark:text-red-400">
          <XCircle className="h-4 w-4" />
          <span>Failed to load build data: {error}</span>
        </div>
      </div>
    );
  }

  if (builds.length === 0) {
    if (probeEvent) {
      const meta = (probeEvent.data?.metadata ?? {}) as Record<string, unknown>;
      const content = (probeEvent.data?.content as string) ?? "";
      const buildSystem = (meta.build_system as string) ?? "unknown";
      const ready = meta.ready === true;
      const missingSecrets = (meta.missing_secrets as string[]) ?? [];
      const matchedSecrets = (meta.matched_secrets as string[]) ?? [];

      return (
        <div className="rounded-xl border bg-card shadow-sm">
          <div className="flex items-center gap-3 border-b px-5 py-4">
            <div
              className={`flex h-8 w-8 items-center justify-center rounded-lg ${
                ready
                  ? "bg-emerald-100 text-emerald-600 dark:bg-emerald-900/40 dark:text-emerald-400"
                  : missingSecrets.length > 0
                    ? "bg-amber-100 text-amber-600 dark:bg-amber-900/40 dark:text-amber-400"
                    : "bg-blue-100 text-blue-600 dark:bg-blue-900/40 dark:text-blue-400"
              }`}
            >
              <Search className="h-4 w-4" />
            </div>
            <div>
              <h3 className="text-sm font-semibold">Build Probe Complete</h3>
              <p className="text-xs text-muted-foreground">
                {ready
                  ? "Build environment is ready"
                  : missingSecrets.length > 0
                    ? `${missingSecrets.length} missing secret${missingSecrets.length !== 1 ? "s" : ""} detected`
                    : "Build system detected"}
              </p>
            </div>
          </div>

          <div className="p-5 space-y-4">
            {/* Build system info */}
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
              <div className="rounded-lg border bg-background p-2.5">
                <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  Build System
                </p>
                <p className="mt-0.5 text-sm font-semibold">
                  {buildSystemLabel(buildSystem)}
                </p>
              </div>
              <div className="rounded-lg border bg-background p-2.5">
                <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  Status
                </p>
                <p className={`mt-0.5 text-sm font-semibold ${
                  ready
                    ? "text-emerald-600 dark:text-emerald-400"
                    : "text-amber-600 dark:text-amber-400"
                }`}>
                  {ready ? "Ready" : "Not Ready"}
                </p>
              </div>
              <div className="rounded-lg border bg-background p-2.5">
                <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  Docker Build
                </p>
                <p className="mt-0.5 text-sm font-semibold text-muted-foreground">
                  {ready ? "Pending approval" : "Blocked"}
                </p>
              </div>
            </div>

            {/* Missing secrets */}
            {missingSecrets.length > 0 && (
              <div className="rounded-lg border border-amber-200 bg-amber-50/50 p-3 dark:border-amber-800/50 dark:bg-amber-950/20">
                <p className="mb-2 flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-wider text-amber-700 dark:text-amber-400">
                  <KeyRound className="h-3 w-3" />
                  Missing Secrets
                </p>
                <div className="flex flex-wrap gap-1.5">
                  {missingSecrets.map((name) => (
                    <span
                      key={name}
                      className="flex items-center gap-1 rounded-md border border-amber-200 bg-white px-2 py-0.5 font-mono text-[11px] text-amber-800 dark:border-amber-700 dark:bg-amber-950/40 dark:text-amber-300"
                    >
                      <AlertTriangle className="h-2.5 w-2.5" />
                      {name}
                    </span>
                  ))}
                </div>
                <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
                  Add these secrets in the Build Secrets settings to enable the Docker build.
                </p>
              </div>
            )}

            {/* Matched secrets */}
            {matchedSecrets.length > 0 && (
              <div className="rounded-lg border border-emerald-200 bg-emerald-50/50 p-3 dark:border-emerald-800/50 dark:bg-emerald-950/20">
                <p className="mb-2 flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-wider text-emerald-700 dark:text-emerald-400">
                  <CheckCircle2 className="h-3 w-3" />
                  Available Secrets
                </p>
                <div className="flex flex-wrap gap-1.5">
                  {matchedSecrets.map((name) => (
                    <span
                      key={name}
                      className="flex items-center gap-1 rounded-md border border-emerald-200 bg-white px-2 py-0.5 font-mono text-[11px] text-emerald-800 dark:border-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300"
                    >
                      <Layers className="h-2.5 w-2.5" />
                      {name}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {/* Full probe summary (markdown-like content from the agent) */}
            {content && (
              <div className="rounded-lg border bg-muted/30 p-3">
                <p className="mb-1.5 flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                  <CircleDot className="h-3 w-3" />
                  Builder Agent Analysis
                </p>
                <pre className="whitespace-pre-wrap font-mono text-xs leading-relaxed text-foreground/80">
                  {content}
                </pre>
              </div>
            )}
          </div>
        </div>
      );
    }

    return (
      <div className="rounded-xl border border-dashed bg-card">
        <div className="flex flex-col items-center justify-center py-12">
          <Hammer className="mb-3 h-10 w-10 text-muted-foreground/30" />
          <p className="text-sm font-medium text-muted-foreground">
            No builds yet
          </p>
          <p className="mt-1 text-xs text-muted-foreground/70">
            Build validation results will appear here when the agent runs builds
          </p>
        </div>
      </div>
    );
  }

  const overallStatus: BuildStatus =
    summary && summary.failed > 0
      ? "failed"
      : summary && summary.running > 0
        ? "running"
        : summary && summary.pending > 0
          ? "pending"
          : "success";

  const overallCfg = statusConfig(overallStatus);

  return (
    <div className="rounded-xl border bg-card shadow-sm">
      <div className="flex items-center justify-between border-b px-5 py-4">
        <div className="flex items-center gap-3">
          <div
            className={`flex h-8 w-8 items-center justify-center rounded-lg ${overallCfg.bgColor} ${overallCfg.color}`}
          >
            <Hammer className="h-4 w-4" />
          </div>
          <div>
            <h3 className="text-sm font-semibold">Build Validation</h3>
            <p className="text-xs text-muted-foreground">
              {summary?.total ?? 0} build{(summary?.total ?? 0) !== 1 ? "s" : ""}
              {summary && summary.total > 0 && (
                <span className="ml-1.5">
                  &middot;{" "}
                  {summary.total_duration_ms > 0
                    ? formatDuration(summary.total_duration_ms)
                    : "in progress"}
                </span>
              )}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          {summary && summary.total > 0 && (
            <div className="flex items-center gap-1.5">
              {summary.passed > 0 && (
                <span className="flex items-center gap-1 rounded-full bg-emerald-100 px-2 py-0.5 text-[10px] font-semibold text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
                  <CheckCircle2 className="h-3 w-3" />
                  {summary.passed}
                </span>
              )}
              {summary.failed > 0 && (
                <span className="flex items-center gap-1 rounded-full bg-red-100 px-2 py-0.5 text-[10px] font-semibold text-red-700 dark:bg-red-900/40 dark:text-red-300">
                  <XCircle className="h-3 w-3" />
                  {summary.failed}
                </span>
              )}
              {summary.running > 0 && (
                <span className="flex items-center gap-1 rounded-full bg-blue-100 px-2 py-0.5 text-[10px] font-semibold text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  {summary.running}
                </span>
              )}
            </div>
          )}
          <button
            onClick={handleRefresh}
            disabled={refreshing}
            className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
          >
            <RefreshCw
              className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`}
            />
          </button>
        </div>
      </div>

      <div className="divide-y">
        {builds.map((build) => {
          const cfg = statusConfig(build.status);
          const isExpanded = expandedBuild === build.id;
          const logData = logs[build.id];
          const isLoadingLog = loadingLogs[build.id];

          return (
            <div key={build.id} className="group">
              <button
                onClick={() => toggleExpand(build.id)}
                className="flex w-full items-center gap-3 px-5 py-3.5 text-left transition-colors hover:bg-muted/50"
              >
                <div
                  className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-lg ring-1 ${cfg.bgColor} ${cfg.color} ${cfg.ringColor}`}
                >
                  {cfg.icon}
                </div>

                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium">
                      {buildSystemLabel(build.build_system)}
                    </span>
                    <span
                      className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${cfg.bgColor} ${cfg.color}`}
                    >
                      {cfg.label}
                    </span>
                    {build.sandbox_mode && (
                      <span className="flex items-center gap-0.5 rounded-full bg-violet-100 px-1.5 py-0.5 text-[10px] font-semibold text-violet-700 dark:bg-violet-900/40 dark:text-violet-300">
                        <Shield className="h-2.5 w-2.5" />
                        Sandbox
                      </span>
                    )}
                    {build.retry_count > 0 && (
                      <span className="rounded-full bg-amber-100 px-1.5 py-0.5 text-[10px] font-semibold text-amber-700 dark:bg-amber-900/40 dark:text-amber-300">
                        Retry #{build.retry_count}
                      </span>
                    )}
                  </div>
                  <div className="mt-0.5 flex items-center gap-3 text-xs text-muted-foreground">
                    {build.base_image && (
                      <span className="flex items-center gap-1">
                        <Container className="h-3 w-3" />
                        {build.base_image.split(":")[0].split("/").pop()}
                      </span>
                    )}
                    {build.duration_ms != null && (
                      <span className="flex items-center gap-1">
                        <Timer className="h-3 w-3" />
                        {formatDuration(build.duration_ms)}
                      </span>
                    )}
                    {build.exit_code != null && (
                      <span
                        className={
                          build.exit_code === 0
                            ? "text-emerald-600 dark:text-emerald-400"
                            : "text-red-600 dark:text-red-400"
                        }
                      >
                        exit {build.exit_code}
                      </span>
                    )}
                    <span>
                      {new Date(build.created_at).toLocaleTimeString([], {
                        hour: "2-digit",
                        minute: "2-digit",
                      })}
                    </span>
                  </div>
                </div>

                <div className="shrink-0 text-muted-foreground/50 transition-transform group-hover:text-muted-foreground">
                  {isExpanded ? (
                    <ChevronUp className="h-4 w-4" />
                  ) : (
                    <ChevronDown className="h-4 w-4" />
                  )}
                </div>
              </button>

              {isExpanded && (
                <div className="border-t bg-muted/20 px-5 py-4">
                  <div className="mb-3 grid grid-cols-2 gap-3 sm:grid-cols-4">
                    <div className="rounded-lg border bg-background p-2.5">
                      <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                        System
                      </p>
                      <p className="mt-0.5 text-sm font-semibold">
                        {buildSystemLabel(build.build_system)}
                      </p>
                    </div>
                    <div className="rounded-lg border bg-background p-2.5">
                      <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                        Image
                      </p>
                      <p className="mt-0.5 truncate text-sm font-semibold">
                        {build.base_image || "—"}
                      </p>
                    </div>
                    <div className="rounded-lg border bg-background p-2.5">
                      <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                        Duration
                      </p>
                      <p className="mt-0.5 text-sm font-semibold">
                        {build.duration_ms != null
                          ? formatDuration(build.duration_ms)
                          : "—"}
                      </p>
                    </div>
                    <div className="rounded-lg border bg-background p-2.5">
                      <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                        Secrets
                      </p>
                      <p className="mt-0.5 text-sm font-semibold">
                        {build.secret_names?.length ?? 0}
                      </p>
                    </div>
                  </div>

                  {build.command && (
                    <div className="mb-3">
                      <div className="flex items-center justify-between">
                        <p className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                          Command
                        </p>
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            handleCopy(build.command, `cmd-${build.id}`);
                          }}
                          className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                        >
                          {copied === `cmd-${build.id}` ? (
                            <Check className="h-3 w-3 text-emerald-500" />
                          ) : (
                            <Copy className="h-3 w-3" />
                          )}
                        </button>
                      </div>
                      <pre className="mt-1 overflow-x-auto rounded-lg border bg-background p-3 font-mono text-xs">
                        {build.command}
                      </pre>
                    </div>
                  )}

                  {build.secret_names && build.secret_names.length > 0 && (
                    <div className="mb-3">
                      <p className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                        Injected Secrets
                      </p>
                      <div className="flex flex-wrap gap-1">
                        {build.secret_names.map((name) => (
                          <span
                            key={name}
                            className="flex items-center gap-1 rounded-md border bg-background px-2 py-0.5 font-mono text-[11px]"
                          >
                            <Layers className="h-2.5 w-2.5 text-muted-foreground" />
                            {name}
                          </span>
                        ))}
                      </div>
                    </div>
                  )}

                  <div>
                    <div className="flex items-center justify-between">
                      <p className="flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                        <Terminal className="h-3 w-3" />
                        Build Output
                      </p>
                      {logData && (
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            handleCopy(logData.log, `log-${build.id}`);
                          }}
                          className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                        >
                          {copied === `log-${build.id}` ? (
                            <Check className="h-3 w-3 text-emerald-500" />
                          ) : (
                            <Copy className="h-3 w-3" />
                          )}
                        </button>
                      )}
                    </div>
                    {isLoadingLog ? (
                      <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
                        <Loader2 className="h-3 w-3 animate-spin" />
                        Loading logs…
                      </div>
                    ) : logData ? (
                      <pre className="mt-1 max-h-64 overflow-auto rounded-lg border bg-zinc-950 p-3 font-mono text-xs leading-relaxed text-zinc-200 dark:bg-zinc-900">
                        {logData.log || "No output captured."}
                      </pre>
                    ) : build.log_summary ? (
                      <pre className="mt-1 max-h-64 overflow-auto rounded-lg border bg-zinc-950 p-3 font-mono text-xs leading-relaxed text-zinc-200 dark:bg-zinc-900">
                        {build.log_summary}
                      </pre>
                    ) : (
                      <p className="mt-2 text-xs text-muted-foreground">
                        No log output available.
                      </p>
                    )}
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
