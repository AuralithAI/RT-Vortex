// ─── Diff Viewer ─────────────────────────────────────────────────────────────
// Side-by-side / inline diff viewer for agent-generated code changes.
// Uses a pre-formatted unified diff display with syntax-highlighted lines.
// Phase 1: pure HTML/CSS diff rendering. Phase 2: Monaco editor integration.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import {
  FileCode,
  CheckCircle,
  XCircle,
  ChevronDown,
  ChevronRight,
  Plus,
  Minus,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import type { SwarmDiff, DiffStatus } from "@/types/swarm";

interface DiffViewerProps {
  diff: SwarmDiff;
  onApprove?: (diffId: string) => void;
  onReject?: (diffId: string) => void;
  readOnly?: boolean;
}

interface ParsedLine {
  type: "added" | "removed" | "context" | "header" | "hunk";
  content: string;
  oldLineNumber?: number;
  newLineNumber?: number;
}

function parseDiff(unified: string): ParsedLine[] {
  const lines = unified.split("\n");
  const parsed: ParsedLine[] = [];
  let oldLine = 0;
  let newLine = 0;

  for (const line of lines) {
    if (line.startsWith("---") || line.startsWith("+++")) {
      parsed.push({ type: "header", content: line });
    } else if (line.startsWith("@@")) {
      // Parse hunk header: @@ -start,count +start,count @@
      const match = line.match(/@@ -(\d+),?\d* \+(\d+),?\d* @@/);
      if (match) {
        oldLine = parseInt(match[1], 10);
        newLine = parseInt(match[2], 10);
      }
      parsed.push({ type: "hunk", content: line });
    } else if (line.startsWith("+")) {
      parsed.push({
        type: "added",
        content: line.substring(1),
        newLineNumber: newLine++,
      });
    } else if (line.startsWith("-")) {
      parsed.push({
        type: "removed",
        content: line.substring(1),
        oldLineNumber: oldLine++,
      });
    } else if (line.startsWith(" ")) {
      parsed.push({
        type: "context",
        content: line.substring(1),
        oldLineNumber: oldLine++,
        newLineNumber: newLine++,
      });
    }
  }

  return parsed;
}

function changeTypeLabel(type: string) {
  switch (type) {
    case "added":
      return "A";
    case "deleted":
      return "D";
    case "renamed":
      return "R";
    default:
      return "M";
  }
}

function changeTypeColor(type: string) {
  switch (type) {
    case "added":
      return "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200";
    case "deleted":
      return "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200";
    case "renamed":
      return "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200";
    default:
      return "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200";
  }
}

function statusBadge(status: DiffStatus) {
  switch (status) {
    case "approved":
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-800 dark:bg-green-900 dark:text-green-200">
          <CheckCircle className="h-3 w-3" />
          Approved
        </span>
      );
    case "rejected":
      return (
        <span className="inline-flex items-center gap-1 rounded-full bg-red-100 px-2 py-0.5 text-xs font-medium text-red-800 dark:bg-red-900 dark:text-red-200">
          <XCircle className="h-3 w-3" />
          Rejected
        </span>
      );
    default:
      return (
        <span className="rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
          Pending
        </span>
      );
  }
}

export function DiffViewer({
  diff,
  onApprove,
  onReject,
  readOnly = false,
}: DiffViewerProps) {
  const [expanded, setExpanded] = useState(true);
  const parsed = parseDiff(diff.unified_diff || "");

  const additions = parsed.filter((l) => l.type === "added").length;
  const deletions = parsed.filter((l) => l.type === "removed").length;

  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      {/* File Header */}
      <div className="flex items-center justify-between border-b bg-muted/30 px-4 py-2">
        <button
          className="flex items-center gap-2"
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? (
            <ChevronDown className="h-4 w-4" />
          ) : (
            <ChevronRight className="h-4 w-4" />
          )}
          <FileCode className="h-4 w-4 text-muted-foreground" />
          <span className="font-mono text-sm">{diff.file_path}</span>
          <span
            className={`ml-2 rounded px-1.5 py-0.5 text-xs font-medium ${changeTypeColor(diff.change_type)}`}
          >
            {changeTypeLabel(diff.change_type)}
          </span>
          <span className="ml-2 text-xs text-muted-foreground">
            <span className="text-green-600 dark:text-green-400">+{additions}</span>
            {" / "}
            <span className="text-red-600 dark:text-red-400">-{deletions}</span>
          </span>
        </button>
        <div className="flex items-center gap-2">
          {statusBadge(diff.status)}
          {!readOnly && diff.status === "pending" && (
            <>
              <Button
                size="sm"
                variant="ghost"
                className="h-7 text-green-600 hover:text-green-700"
                onClick={() => onApprove?.(diff.id)}
              >
                <CheckCircle className="mr-1 h-3.5 w-3.5" />
                Approve
              </Button>
              <Button
                size="sm"
                variant="ghost"
                className="h-7 text-red-600 hover:text-red-700"
                onClick={() => onReject?.(diff.id)}
              >
                <XCircle className="mr-1 h-3.5 w-3.5" />
                Reject
              </Button>
            </>
          )}
        </div>
      </div>

      {/* Diff Content */}
      {expanded && (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse font-mono text-xs">
            <tbody>
              {parsed.map((line, i) => {
                if (line.type === "header") {
                  return (
                    <tr key={i} className="bg-muted/20">
                      <td
                        colSpan={3}
                        className="px-4 py-0.5 text-muted-foreground"
                      >
                        {line.content}
                      </td>
                    </tr>
                  );
                }
                if (line.type === "hunk") {
                  return (
                    <tr key={i} className="bg-blue-50 dark:bg-blue-950/30">
                      <td
                        colSpan={3}
                        className="px-4 py-1 text-blue-600 dark:text-blue-400"
                      >
                        {line.content}
                      </td>
                    </tr>
                  );
                }

                const bgClass =
                  line.type === "added"
                    ? "bg-green-50 dark:bg-green-950/20"
                    : line.type === "removed"
                      ? "bg-red-50 dark:bg-red-950/20"
                      : "";

                const lineNumClass = "w-12 select-none px-2 py-0 text-right text-muted-foreground/60";

                return (
                  <tr key={i} className={bgClass}>
                    <td className={lineNumClass}>
                      {line.oldLineNumber ?? ""}
                    </td>
                    <td className={lineNumClass}>
                      {line.newLineNumber ?? ""}
                    </td>
                    <td className="whitespace-pre px-4 py-0">
                      {line.type === "added" && (
                        <span className="mr-1 text-green-600">+</span>
                      )}
                      {line.type === "removed" && (
                        <span className="mr-1 text-red-600">-</span>
                      )}
                      {line.type === "context" && (
                        <span className="mr-1 text-muted-foreground/40">
                          {" "}
                        </span>
                      )}
                      {line.content}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
