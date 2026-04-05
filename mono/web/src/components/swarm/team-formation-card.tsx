// ─── Team Formation Card ─────────────────────────────────────────────────────
// Displays the dynamic team formation recommendation for a swarm task.
// Fetches from GET /api/v1/swarm/tasks/{id}/team-formation and shows the
// complexity analysis, recommended roles with ELO tiers, and reasoning.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Brain,
  Users,
  Shield,
  Code2,
  Bug,
  BookOpen,
  Palette,
  Wrench,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  Loader2,
  TrendingUp,
  TrendingDown,
  Minus,
  Zap,
  FileCode,
  Layers,
  Globe,
  Database,
  Lock,
  Monitor,
  FlaskConical,
  GitBranch,
} from "lucide-react";
import type {
  TeamFormationData,
  ComplexityLabel,
  RoleELOTier,
} from "@/types/swarm";

// ── Props ───────────────────────────────────────────────────────────────────

interface TeamFormationCardProps {
  taskId: string;
  /** Compact mode — just shows complexity badge and team size. */
  compact?: boolean;
  /** Auto-refresh interval in ms (0 = no auto-refresh). */
  refreshInterval?: number;
}

// ── Complexity → visual mapping ─────────────────────────────────────────────

function complexityConfig(label: ComplexityLabel): {
  displayLabel: string;
  color: string;
  bgColor: string;
  borderColor: string;
  icon: React.ReactNode;
} {
  switch (label) {
    case "trivial":
      return {
        displayLabel: "Trivial",
        color: "text-slate-500 dark:text-slate-400",
        bgColor: "bg-slate-100 dark:bg-slate-800/50",
        borderColor: "border-slate-300 dark:border-slate-600",
        icon: <Minus className="h-3.5 w-3.5" />,
      };
    case "small":
      return {
        displayLabel: "Small",
        color: "text-green-600 dark:text-green-400",
        bgColor: "bg-green-100 dark:bg-green-900/40",
        borderColor: "border-green-300 dark:border-green-700",
        icon: <Code2 className="h-3.5 w-3.5" />,
      };
    case "medium":
      return {
        displayLabel: "Medium",
        color: "text-amber-600 dark:text-amber-400",
        bgColor: "bg-amber-100 dark:bg-amber-900/40",
        borderColor: "border-amber-300 dark:border-amber-700",
        icon: <Layers className="h-3.5 w-3.5" />,
      };
    case "large":
      return {
        displayLabel: "Large",
        color: "text-orange-600 dark:text-orange-400",
        bgColor: "bg-orange-100 dark:bg-orange-900/40",
        borderColor: "border-orange-300 dark:border-orange-700",
        icon: <Brain className="h-3.5 w-3.5" />,
      };
    case "critical":
      return {
        displayLabel: "Critical",
        color: "text-red-600 dark:text-red-400",
        bgColor: "bg-red-100 dark:bg-red-900/40",
        borderColor: "border-red-300 dark:border-red-700",
        icon: <Zap className="h-3.5 w-3.5" />,
      };
  }
}

// ── Role → icon mapping ─────────────────────────────────────────────────────

function roleIcon(role: string): React.ReactNode {
  switch (role) {
    case "architect":
      return <Brain className="h-3.5 w-3.5" />;
    case "senior_dev":
      return <Code2 className="h-3.5 w-3.5" />;
    case "junior_dev":
      return <Code2 className="h-3.5 w-3.5 opacity-60" />;
    case "qa":
      return <Bug className="h-3.5 w-3.5" />;
    case "security":
      return <Shield className="h-3.5 w-3.5" />;
    case "docs":
      return <BookOpen className="h-3.5 w-3.5" />;
    case "ui_ux":
      return <Palette className="h-3.5 w-3.5" />;
    case "ops":
    case "devops":
      return <Wrench className="h-3.5 w-3.5" />;
    default:
      return <Users className="h-3.5 w-3.5" />;
  }
}

function roleLabel(role: string): string {
  const map: Record<string, string> = {
    architect: "Architect",
    senior_dev: "Senior Dev",
    junior_dev: "Junior Dev",
    qa: "QA",
    security: "Security",
    docs: "Docs",
    ui_ux: "UI/UX",
    ops: "DevOps",
    devops: "DevOps",
  };
  return map[role] ?? role;
}

// ── Tier → visual mapping ───────────────────────────────────────────────────

function tierConfig(tier: RoleELOTier): {
  label: string;
  color: string;
  bgColor: string;
  icon: React.ReactNode;
} {
  switch (tier) {
    case "expert":
      return {
        label: "Expert",
        color: "text-emerald-600 dark:text-emerald-400",
        bgColor: "bg-emerald-100 dark:bg-emerald-900/40",
        icon: <TrendingUp className="h-3 w-3" />,
      };
    case "standard":
      return {
        label: "Standard",
        color: "text-blue-600 dark:text-blue-400",
        bgColor: "bg-blue-100 dark:bg-blue-900/40",
        icon: <Minus className="h-3 w-3" />,
      };
    case "restricted":
      return {
        label: "Restricted",
        color: "text-red-600 dark:text-red-400",
        bgColor: "bg-red-100 dark:bg-red-900/40",
        icon: <TrendingDown className="h-3 w-3" />,
      };
  }
}

// ── Component ───────────────────────────────────────────────────────────────

export function TeamFormationCard({
  taskId,
  compact = false,
  refreshInterval = 0,
}: TeamFormationCardProps) {
  const [formation, setFormation] = useState<TeamFormationData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);

  const fetchFormation = useCallback(async () => {
    try {
      const res = await fetch(`/api/v1/swarm/tasks/${taskId}/team-formation`);
      if (!res.ok) {
        if (res.status === 404) {
          setFormation(null);
          setError(null);
          return;
        }
        throw new Error(`HTTP ${res.status}`);
      }
      const data = await res.json();
      setFormation(data.team_formation ?? null);
      setError(null);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  }, [taskId]);

  useEffect(() => {
    fetchFormation();
    if (refreshInterval > 0) {
      const iv = setInterval(fetchFormation, refreshInterval);
      return () => clearInterval(iv);
    }
  }, [fetchFormation, refreshInterval]);

  // ── Loading / Empty states ────────────────────────────────────────────
  if (loading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground py-2">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span>Loading team formation…</span>
      </div>
    );
  }

  if (!formation) {
    return null; // No formation data yet — nothing to show.
  }

  const cx = complexityConfig(formation.complexity_label);
  const signals = formation.input_signals;

  // ── Compact mode ──────────────────────────────────────────────────────
  if (compact) {
    return (
      <div className="flex items-center gap-2">
        <span
          className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${cx.bgColor} ${cx.color}`}
        >
          {cx.icon}
          {cx.displayLabel}
        </span>
        <span className="text-xs text-muted-foreground">
          {formation.team_size} agents · {formation.recommended_roles.length} roles
        </span>
      </div>
    );
  }

  // ── Full mode ─────────────────────────────────────────────────────────
  return (
    <div className={`rounded-lg border ${cx.borderColor} ${cx.bgColor}/30 p-4`}>
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className={`flex items-center gap-1.5 ${cx.color}`}>
            {cx.icon}
            <span className="font-semibold text-sm">{cx.displayLabel} Complexity</span>
          </div>
          <span className="text-xs text-muted-foreground">
            Score: {(formation.complexity_score * 100).toFixed(1)}%
          </span>
          <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium bg-indigo-100 dark:bg-indigo-900/40 text-indigo-600 dark:text-indigo-400`}>
            <Users className="h-3 w-3" />
            {formation.team_size} agents
          </span>
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={() => { setLoading(true); fetchFormation(); }}
            className="p-1 hover:bg-white/50 dark:hover:bg-white/10 rounded"
            title="Refresh"
          >
            <RefreshCw className="h-3.5 w-3.5 text-muted-foreground" />
          </button>
          <button
            onClick={() => setExpanded(!expanded)}
            className="p-1 hover:bg-white/50 dark:hover:bg-white/10 rounded"
          >
            {expanded ? (
              <ChevronUp className="h-4 w-4 text-muted-foreground" />
            ) : (
              <ChevronDown className="h-4 w-4 text-muted-foreground" />
            )}
          </button>
        </div>
      </div>

      {/* Recommended Roles */}
      <div className="mt-3 flex flex-wrap gap-2">
        {formation.recommended_roles.map((role, idx) => {
          const eloInfo = formation.role_elos?.[role];
          const tc = eloInfo ? tierConfig(eloInfo.tier) : null;
          return (
            <div
              key={`${role}-${idx}`}
              className="inline-flex items-center gap-1.5 rounded-md border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 px-2.5 py-1 text-xs"
            >
              {roleIcon(role)}
              <span className="font-medium">{roleLabel(role)}</span>
              {tc && (
                <span
                  className={`inline-flex items-center gap-0.5 rounded-full px-1.5 py-0 text-[10px] font-medium ${tc.bgColor} ${tc.color}`}
                  title={`ELO: ${eloInfo!.elo.toFixed(0)} — ${tc.label}`}
                >
                  {tc.icon}
                  {eloInfo!.elo.toFixed(0)}
                </span>
              )}
            </div>
          );
        })}
      </div>

      {/* Reasoning (always visible) */}
      <p className="mt-2 text-xs text-muted-foreground leading-relaxed">
        {formation.reasoning}
      </p>

      {/* Expanded: Input Signals */}
      {expanded && (
        <div className="mt-4 space-y-3 border-t pt-3 border-gray-200 dark:border-gray-700">
          <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
            Input Signals
          </h4>
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
            <SignalPill icon={<FileCode className="h-3.5 w-3.5" />} label="Files" value={signals.file_count} />
            <SignalPill icon={<Layers className="h-3.5 w-3.5" />} label="Steps" value={signals.step_count} />
            <SignalPill icon={<Globe className="h-3.5 w-3.5" />} label="Languages" value={signals.language_count} />
            <SignalPill icon={<FlaskConical className="h-3.5 w-3.5" />} label="Test files" value={signals.test_files} />
            {signals.has_migrations && (
              <SignalFlag icon={<Database className="h-3.5 w-3.5" />} label="Migrations" />
            )}
            {signals.cross_package && (
              <SignalFlag icon={<GitBranch className="h-3.5 w-3.5" />} label="Cross-pkg" />
            )}
            {signals.has_api_changes && (
              <SignalFlag icon={<Globe className="h-3.5 w-3.5" />} label="API changes" />
            )}
            {signals.has_security_impact && (
              <SignalFlag icon={<Lock className="h-3.5 w-3.5" />} label="Security" />
            )}
            {signals.has_ui_changes && (
              <SignalFlag icon={<Monitor className="h-3.5 w-3.5" />} label="UI changes" />
            )}
          </div>

          {signals.languages.length > 0 && (
            <div className="flex flex-wrap gap-1 mt-1">
              {signals.languages.map((lang) => (
                <span
                  key={lang}
                  className="rounded-full bg-gray-100 dark:bg-gray-800 px-2 py-0.5 text-[10px] font-mono text-muted-foreground"
                >
                  {lang}
                </span>
              ))}
            </div>
          )}

          <div className="flex items-center gap-3 text-[10px] text-muted-foreground pt-1">
            <span>Strategy: <strong>{formation.strategy}</strong></span>
            <span>Created: {new Date(formation.created_at).toLocaleTimeString()}</span>
          </div>
        </div>
      )}

      {/* Error */}
      {error && (
        <p className="mt-2 text-xs text-red-500">Error: {error}</p>
      )}
    </div>
  );
}

// ── Sub-components ──────────────────────────────────────────────────────────

function SignalPill({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: number;
}) {
  return (
    <div className="flex items-center gap-1.5 rounded-md bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 px-2 py-1 text-xs">
      <span className="text-muted-foreground">{icon}</span>
      <span className="text-muted-foreground">{label}:</span>
      <span className="font-semibold">{value}</span>
    </div>
  );
}

function SignalFlag({
  icon,
  label,
}: {
  icon: React.ReactNode;
  label: string;
}) {
  return (
    <div className="flex items-center gap-1.5 rounded-md bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-700 px-2 py-1 text-xs text-amber-700 dark:text-amber-400">
      {icon}
      <span className="font-medium">{label}</span>
    </div>
  );
}
