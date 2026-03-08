// ─── Review Diff View ────────────────────────────────────────────────────────
// Groups review comments by file and renders them in a diff-like side-by-side
// view with line numbers, severity indicators, code suggestions, and inline
// comment annotations — similar to GitHub's PR review interface.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import React, { useMemo, useState } from "react";
import {
  FileCode2,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  AlertOctagon,
  Info,
  Lightbulb,
  Star,
  Copy,
  Check,
  Filter,
  List,
  Code2,
  MessageSquare,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { ReviewComment, Severity } from "@/types/api";

// ═══════════════════════════════════════════════════════════════════════════
// Types
// ═══════════════════════════════════════════════════════════════════════════

interface ReviewDiffViewProps {
  comments: ReviewComment[];
}

type ViewMode = "files" | "flat";

interface FileGroup {
  filePath: string;
  extension: string;
  comments: ReviewComment[];
  severityCounts: Record<string, number>;
  maxSeverity: Severity;
}

// ═══════════════════════════════════════════════════════════════════════════
// Severity Helpers
// ═══════════════════════════════════════════════════════════════════════════

const severityOrder: Record<string, number> = {
  critical: 0,
  warning: 1,
  suggestion: 2,
  info: 3,
  praise: 4,
};

const severityConfig: Record<
  string,
  {
    icon: React.ElementType;
    color: string;
    bg: string;
    border: string;
    badgeVariant: "destructive" | "warning" | "secondary" | "outline" | "success";
    label: string;
  }
> = {
  critical: {
    icon: AlertOctagon,
    color: "text-red-400",
    bg: "bg-red-500/10",
    border: "border-red-500/30",
    badgeVariant: "destructive",
    label: "Critical",
  },
  warning: {
    icon: AlertTriangle,
    color: "text-amber-400",
    bg: "bg-amber-500/10",
    border: "border-amber-500/30",
    badgeVariant: "warning",
    label: "Warning",
  },
  suggestion: {
    icon: Lightbulb,
    color: "text-blue-400",
    bg: "bg-blue-500/10",
    border: "border-blue-500/30",
    badgeVariant: "secondary",
    label: "Suggestion",
  },
  info: {
    icon: Info,
    color: "text-zinc-400",
    bg: "bg-zinc-500/10",
    border: "border-zinc-500/30",
    badgeVariant: "outline",
    label: "Info",
  },
  praise: {
    icon: Star,
    color: "text-emerald-400",
    bg: "bg-emerald-500/10",
    border: "border-emerald-500/30",
    badgeVariant: "success",
    label: "Praise",
  },
};

function getSeverityConfig(severity: string) {
  return severityConfig[severity] ?? severityConfig.info;
}

// ═══════════════════════════════════════════════════════════════════════════
// Language Detection (for syntax highlighting labels)
// ═══════════════════════════════════════════════════════════════════════════

const extToLang: Record<string, string> = {
  ts: "TypeScript",
  tsx: "TypeScript (React)",
  js: "JavaScript",
  jsx: "JavaScript (React)",
  py: "Python",
  rs: "Rust",
  go: "Go",
  cpp: "C++",
  cc: "C++",
  cxx: "C++",
  c: "C",
  h: "C/C++ Header",
  hpp: "C++ Header",
  java: "Java",
  kt: "Kotlin",
  rb: "Ruby",
  sh: "Bash",
  yml: "YAML",
  yaml: "YAML",
  json: "JSON",
  md: "Markdown",
  sql: "SQL",
  css: "CSS",
  scss: "SCSS",
  html: "HTML",
  xml: "XML",
  proto: "Protocol Buffers",
  cmake: "CMake",
  toml: "TOML",
  dockerfile: "Dockerfile",
};

function getLanguage(filePath: string): string {
  const ext = filePath.split(".").pop()?.toLowerCase() ?? "";
  const basename = filePath.split("/").pop()?.toLowerCase() ?? "";
  if (basename === "dockerfile") return "Dockerfile";
  if (basename === "makefile") return "Makefile";
  if (basename === "cmakelists.txt") return "CMake";
  return extToLang[ext] ?? ext.toUpperCase();
}

// ═══════════════════════════════════════════════════════════════════════════
// Main Component
// ═══════════════════════════════════════════════════════════════════════════

export function ReviewDiffView({ comments }: ReviewDiffViewProps) {
  const [viewMode, setViewMode] = useState<ViewMode>("files");
  const [filterSeverity, setFilterSeverity] = useState<string | null>(null);
  const [expandedFiles, setExpandedFiles] = useState<Set<string>>(
    () => new Set(),
  );
  const [allExpanded, setAllExpanded] = useState(true);

  // Group comments by file.
  const fileGroups = useMemo(() => {
    const groups = new Map<string, ReviewComment[]>();
    for (const comment of comments) {
      const key = comment.file_path || "(no file)";
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key)!.push(comment);
    }

    const result: FileGroup[] = [];
    for (const [filePath, fileComments] of groups) {
      // Sort comments by line number within each file.
      fileComments.sort((a, b) => (a.line_start ?? 0) - (b.line_start ?? 0));

      const severityCounts: Record<string, number> = {};
      let maxSeverity: Severity = "info";
      for (const c of fileComments) {
        severityCounts[c.severity] =
          (severityCounts[c.severity] ?? 0) + 1;
        if (
          (severityOrder[c.severity] ?? 99) <
          (severityOrder[maxSeverity] ?? 99)
        ) {
          maxSeverity = c.severity;
        }
      }

      result.push({
        filePath,
        extension: filePath.split(".").pop() ?? "",
        comments: fileComments,
        severityCounts,
        maxSeverity,
      });
    }

    // Sort files: highest severity first, then alphabetically.
    result.sort((a, b) => {
      const sA = severityOrder[a.maxSeverity] ?? 99;
      const sB = severityOrder[b.maxSeverity] ?? 99;
      if (sA !== sB) return sA - sB;
      return a.filePath.localeCompare(b.filePath);
    });

    return result;
  }, [comments]);

  // Filtered comments.
  const filteredGroups = useMemo(() => {
    if (!filterSeverity) return fileGroups;
    return fileGroups
      .map((g) => ({
        ...g,
        comments: g.comments.filter((c) => c.severity === filterSeverity),
      }))
      .filter((g) => g.comments.length > 0);
  }, [fileGroups, filterSeverity]);

  // Severity summary.
  const severitySummary = useMemo(() => {
    const summary: Record<string, number> = {};
    for (const c of comments) {
      summary[c.severity] = (summary[c.severity] ?? 0) + 1;
    }
    return summary;
  }, [comments]);

  // Initial expand — expand all files on first render.
  useMemo(() => {
    if (allExpanded) {
      setExpandedFiles(new Set(fileGroups.map((g) => g.filePath)));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fileGroups.length]);

  const toggleFile = (filePath: string) => {
    setExpandedFiles((prev) => {
      const next = new Set(prev);
      if (next.has(filePath)) {
        next.delete(filePath);
      } else {
        next.add(filePath);
      }
      return next;
    });
  };

  const toggleAll = () => {
    if (allExpanded) {
      setExpandedFiles(new Set());
      setAllExpanded(false);
    } else {
      setExpandedFiles(new Set(filteredGroups.map((g) => g.filePath)));
      setAllExpanded(true);
    }
  };

  return (
    <div className="space-y-3">
      {/* ── Toolbar ──────────────────────────────────────────────────────── */}
      <div className="flex items-center justify-between gap-4 flex-wrap">
        {/* Severity filter pills */}
        <div className="flex items-center gap-2 flex-wrap">
          <Filter className="h-3.5 w-3.5 text-zinc-500" />
          <button
            onClick={() => setFilterSeverity(null)}
            className={cn(
              "px-2.5 py-1 rounded-full text-[11px] font-medium transition-colors",
              !filterSeverity
                ? "bg-zinc-700 text-zinc-100"
                : "text-zinc-500 hover:text-zinc-300",
            )}
          >
            All ({comments.length})
          </button>
          {Object.entries(severitySummary)
            .sort(
              ([a], [b]) =>
                (severityOrder[a] ?? 99) - (severityOrder[b] ?? 99),
            )
            .map(([sev, count]) => {
              const cfg = getSeverityConfig(sev);
              return (
                <button
                  key={sev}
                  onClick={() =>
                    setFilterSeverity(filterSeverity === sev ? null : sev)
                  }
                  className={cn(
                    "px-2.5 py-1 rounded-full text-[11px] font-medium transition-colors flex items-center gap-1",
                    filterSeverity === sev
                      ? `${cfg.bg} ${cfg.color} ${cfg.border} border`
                      : "text-zinc-500 hover:text-zinc-300",
                  )}
                >
                  <cfg.icon className="h-3 w-3" />
                  {cfg.label} ({count})
                </button>
              );
            })}
        </div>

        {/* View controls */}
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs gap-1"
            onClick={toggleAll}
          >
            {allExpanded ? (
              <ChevronDown className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
            {allExpanded ? "Collapse" : "Expand"} all
          </Button>

          <div className="flex items-center rounded-md border border-zinc-700 overflow-hidden">
            <button
              onClick={() => setViewMode("files")}
              className={cn(
                "px-2 py-1 text-[11px] transition-colors",
                viewMode === "files"
                  ? "bg-zinc-700 text-zinc-100"
                  : "text-zinc-500 hover:text-zinc-300",
              )}
            >
              <Code2 className="h-3 w-3" />
            </button>
            <button
              onClick={() => setViewMode("flat")}
              className={cn(
                "px-2 py-1 text-[11px] transition-colors",
                viewMode === "flat"
                  ? "bg-zinc-700 text-zinc-100"
                  : "text-zinc-500 hover:text-zinc-300",
              )}
            >
              <List className="h-3 w-3" />
            </button>
          </div>
        </div>
      </div>

      {/* ── File Tree Summary ────────────────────────────────────────────── */}
      <div className="rounded-lg border border-zinc-800 bg-zinc-950 overflow-hidden">
        <div className="px-4 py-2.5 bg-zinc-900/50 border-b border-zinc-800 flex items-center justify-between">
          <span className="text-xs text-zinc-400 font-medium">
            {filteredGroups.length} file{filteredGroups.length !== 1 ? "s" : ""}{" "}
            reviewed
          </span>
          <span className="text-xs text-zinc-500">
            {filteredGroups.reduce((acc, g) => acc + g.comments.length, 0)}{" "}
            comments
          </span>
        </div>

        {/* File list */}
        <div className="divide-y divide-zinc-800/50">
          {filteredGroups.map((group) => (
            <FileSection
              key={group.filePath}
              group={group}
              expanded={expandedFiles.has(group.filePath)}
              onToggle={() => toggleFile(group.filePath)}
              viewMode={viewMode}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// File Section (collapsible file header + comment list)
// ═══════════════════════════════════════════════════════════════════════════

function FileSection({
  group,
  expanded,
  onToggle,
  viewMode,
}: {
  group: FileGroup;
  expanded: boolean;
  onToggle: () => void;
  viewMode: ViewMode;
}) {
  const cfg = getSeverityConfig(group.maxSeverity);
  const language = getLanguage(group.filePath);
  const fileName = group.filePath.split("/").pop() ?? group.filePath;
  const dirPath = group.filePath.includes("/")
    ? group.filePath.slice(0, group.filePath.lastIndexOf("/"))
    : "";

  return (
    <div>
      {/* File header */}
      <button
        onClick={onToggle}
        className={cn(
          "w-full flex items-center gap-3 px-4 py-2.5 text-left transition-colors",
          "hover:bg-zinc-900/30",
          expanded && "bg-zinc-900/20",
        )}
      >
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 text-zinc-500 shrink-0" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 text-zinc-500 shrink-0" />
        )}

        <FileCode2 className={cn("h-4 w-4 shrink-0", cfg.color)} />

        <div className="flex items-center gap-1.5 min-w-0 flex-1">
          {dirPath && (
            <span className="text-xs text-zinc-500 truncate">{dirPath}/</span>
          )}
          <span className="text-sm text-zinc-200 font-mono font-medium">
            {fileName}
          </span>
        </div>

        <span className="text-[10px] text-zinc-600 font-mono shrink-0">
          {language}
        </span>

        {/* Severity badges */}
        <div className="flex items-center gap-1.5 shrink-0">
          <TooltipProvider delayDuration={0}>
            {Object.entries(group.severityCounts)
              .sort(
                ([a], [b]) =>
                  (severityOrder[a] ?? 99) - (severityOrder[b] ?? 99),
              )
              .map(([sev, count]) => {
                const scfg = getSeverityConfig(sev);
                return (
                  <Tooltip key={sev}>
                    <TooltipTrigger asChild>
                      <div
                        className={cn(
                          "flex items-center gap-0.5 px-1.5 py-0.5 rounded text-[10px] font-medium",
                          scfg.bg,
                          scfg.color,
                        )}
                      >
                        <scfg.icon className="h-2.5 w-2.5" />
                        {count}
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      {count} {scfg.label.toLowerCase()}
                      {count !== 1 ? "s" : ""}
                    </TooltipContent>
                  </Tooltip>
                );
              })}
          </TooltipProvider>
        </div>
      </button>

      {/* Comments */}
      {expanded && (
        <div className="border-t border-zinc-800/50">
          {viewMode === "files" ? (
            <DiffStyleComments comments={group.comments} />
          ) : (
            <FlatComments comments={group.comments} />
          )}
        </div>
      )}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Diff-Style Comments (side-by-side code context + comments)
// ═══════════════════════════════════════════════════════════════════════════

function DiffStyleComments({ comments }: { comments: ReviewComment[] }) {
  return (
    <div className="divide-y divide-zinc-800/30">
      {comments.map((comment) => (
        <DiffCommentBlock key={comment.id} comment={comment} />
      ))}
    </div>
  );
}

function DiffCommentBlock({ comment }: { comment: ReviewComment }) {
  const cfg = getSeverityConfig(comment.severity);
  const SevIcon = cfg.icon;
  const hasLineRange =
    comment.line_start > 0 ||
    (comment.line_end != null && comment.line_end > 0);
  const lineRange =
    comment.line_start && comment.line_end && comment.line_end !== comment.line_start
      ? `L${comment.line_start}–L${comment.line_end}`
      : comment.line_start
        ? `L${comment.line_start}`
        : "";

  return (
    <div className="group">
      {/* ── Code context header (line range indicator) ──── */}
      {hasLineRange && (
        <div className="flex items-center gap-2 px-4 py-1.5 bg-zinc-900/40 border-b border-zinc-800/30">
          <div className="flex items-center gap-1.5">
            <span className="font-mono text-[11px] text-blue-400">
              @@ {lineRange} @@
            </span>
          </div>
        </div>
      )}

      {/* ── Comment body (looks like an inline review comment) ──── */}
      <div
        className={cn(
          "flex gap-0 border-l-2",
          cfg.border,
        )}
      >
        {/* Severity gutter */}
        <div
          className={cn(
            "w-10 shrink-0 flex flex-col items-center pt-3",
            cfg.bg,
          )}
        >
          <SevIcon className={cn("h-4 w-4", cfg.color)} />
          {hasLineRange && (
            <span className={cn("text-[9px] mt-1 font-mono", cfg.color)}>
              {comment.line_start || ""}
            </span>
          )}
        </div>

        {/* Content */}
        <div className="flex-1 min-w-0 px-4 py-3 space-y-2.5">
          {/* Header row */}
          <div className="flex items-center gap-2 flex-wrap">
            <Badge variant={cfg.badgeVariant} className="text-[10px] h-5">
              {cfg.label}
            </Badge>
            {comment.category && (
              <Badge
                variant="outline"
                className="text-[10px] h-5 text-zinc-400"
              >
                {comment.category}
              </Badge>
            )}
            {hasLineRange && (
              <span className="text-[10px] text-zinc-500 font-mono">
                {lineRange}
              </span>
            )}
          </div>

          {/* Title */}
          {comment.title && (
            <p className="text-sm font-medium text-zinc-200">
              {comment.title}
            </p>
          )}

          {/* Body */}
          <div className="text-sm text-zinc-300 leading-relaxed whitespace-pre-wrap">
            {comment.body}
          </div>

          {/* Suggestion (rendered as a diff-style code block) */}
          {comment.suggestion && (
            <SuggestionBlock suggestion={comment.suggestion} />
          )}
        </div>
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Suggestion Block (diff-style code change suggestion)
// ═══════════════════════════════════════════════════════════════════════════

function SuggestionBlock({ suggestion }: { suggestion: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(suggestion);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // Parse suggestion lines — treat lines starting with + as additions,
  // - as deletions, and everything else as context.
  const lines = suggestion.split("\n");
  const hasActualDiff = lines.some(
    (l) => l.startsWith("+") || l.startsWith("-"),
  );

  return (
    <div className="rounded-lg overflow-hidden border border-zinc-700 bg-zinc-950">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-1.5 bg-zinc-900 border-b border-zinc-700">
        <div className="flex items-center gap-1.5 text-[10px] text-zinc-400">
          <Lightbulb className="h-3 w-3 text-blue-400" />
          <span className="font-medium">Suggested change</span>
        </div>
        <button
          onClick={handleCopy}
          className="text-zinc-500 hover:text-zinc-300 transition-colors"
        >
          {copied ? (
            <Check className="h-3.5 w-3.5 text-emerald-400" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </button>
      </div>

      {/* Code */}
      <pre className="overflow-x-auto text-xs font-mono leading-relaxed">
        {hasActualDiff
          ? lines.map((line, i) => (
              <DiffLine key={i} line={line} />
            ))
          : // If no diff markers, show as a plain suggested code block (green).
            lines.map((line, i) => (
              <div
                key={i}
                className="px-3 py-0 bg-emerald-500/5 text-emerald-300"
              >
                <span className="select-none text-emerald-500/50 mr-3">
                  +
                </span>
                {line}
              </div>
            ))}
      </pre>
    </div>
  );
}

function DiffLine({ line }: { line: string }) {
  if (line.startsWith("+")) {
    return (
      <div className="px-3 py-0 bg-emerald-500/8 text-emerald-300">
        <span className="select-none text-emerald-500/50 mr-3">+</span>
        {line.slice(1)}
      </div>
    );
  }
  if (line.startsWith("-")) {
    return (
      <div className="px-3 py-0 bg-red-500/8 text-red-300">
        <span className="select-none text-red-500/50 mr-3">−</span>
        {line.slice(1)}
      </div>
    );
  }
  return (
    <div className="px-3 py-0 text-zinc-400">
      <span className="select-none text-zinc-600 mr-3">&nbsp;</span>
      {line}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Flat Comments (list view — original style, improved)
// ═══════════════════════════════════════════════════════════════════════════

function FlatComments({ comments }: { comments: ReviewComment[] }) {
  return (
    <div className="divide-y divide-zinc-800/30">
      {comments.map((comment) => {
        const cfg = getSeverityConfig(comment.severity);
        const SevIcon = cfg.icon;
        return (
          <div key={comment.id} className="px-4 py-3 space-y-2">
            <div className="flex items-center gap-2 flex-wrap">
              <SevIcon className={cn("h-3.5 w-3.5", cfg.color)} />
              <Badge variant={cfg.badgeVariant} className="text-[10px] h-5">
                {cfg.label}
              </Badge>
              {comment.category && (
                <Badge
                  variant="outline"
                  className="text-[10px] h-5 text-zinc-400"
                >
                  {comment.category}
                </Badge>
              )}
              {comment.line_start > 0 && (
                <span className="text-[10px] text-zinc-500 font-mono">
                  L{comment.line_start}
                  {comment.line_end &&
                    comment.line_end !== comment.line_start &&
                    `–${comment.line_end}`}
                </span>
              )}
            </div>
            {comment.title && (
              <p className="text-sm font-medium text-zinc-200">
                {comment.title}
              </p>
            )}
            <p className="text-sm text-zinc-300 whitespace-pre-wrap">
              {comment.body}
            </p>
            {comment.suggestion && (
              <SuggestionBlock suggestion={comment.suggestion} />
            )}
          </div>
        );
      })}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// File Summary Bar (for the page header area)
// ═══════════════════════════════════════════════════════════════════════════

export function ReviewFileSummary({
  comments,
}: {
  comments: ReviewComment[];
}) {
  const fileCount = useMemo(() => {
    const files = new Set(comments.map((c) => c.file_path));
    return files.size;
  }, [comments]);

  const sevCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const c of comments) {
      counts[c.severity] = (counts[c.severity] ?? 0) + 1;
    }
    return counts;
  }, [comments]);

  return (
    <div className="flex items-center gap-3 text-xs text-zinc-500">
      <span className="flex items-center gap-1">
        <FileCode2 className="h-3.5 w-3.5" />
        {fileCount} file{fileCount !== 1 ? "s" : ""}
      </span>
      <span className="flex items-center gap-1">
        <MessageSquare className="h-3.5 w-3.5" />
        {comments.length} comment{comments.length !== 1 ? "s" : ""}
      </span>
      {Object.entries(sevCounts)
        .sort(
          ([a], [b]) => (severityOrder[a] ?? 99) - (severityOrder[b] ?? 99),
        )
        .map(([sev, count]) => {
          const cfg = getSeverityConfig(sev);
          return (
            <span key={sev} className={cn("flex items-center gap-0.5", cfg.color)}>
              <cfg.icon className="h-3 w-3" />
              {count}
            </span>
          );
        })}
    </div>
  );
}
