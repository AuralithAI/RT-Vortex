// ─── Diff File Tree ──────────────────────────────────────────────────────────
// Sidebar listing all files in a diff set with change-type badges and
// selection state.  Used alongside the main DiffViewer.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useMemo } from "react";
import {
  File,
  FilePlus,
  FileMinus,
  FileEdit,
  FileSymlink,
} from "lucide-react";
import type { ChangeType, DiffStatus } from "@/types/swarm";

// ── Types ────────────────────────────────────────────────────────────────────

interface FileEntry {
  file_path: string;
  change_type: ChangeType;
  status: DiffStatus;
  diff_id: string;
}

interface DiffFileTreeProps {
  files: FileEntry[];
  selectedId?: string;
  onSelect: (diffId: string) => void;
  className?: string;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

const changeIcon: Record<ChangeType, typeof File> = {
  modified: FileEdit,
  added: FilePlus,
  deleted: FileMinus,
  renamed: FileSymlink,
};

const changeBadge: Record<ChangeType, { label: string; className: string }> = {
  modified: { label: "M", className: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300" },
  added: { label: "A", className: "bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300" },
  deleted: { label: "D", className: "bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300" },
  renamed: { label: "R", className: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300" },
};

const statusDot: Record<DiffStatus, string> = {
  pending: "bg-gray-400",
  approved: "bg-emerald-500",
  rejected: "bg-red-500",
};

function fileName(path: string): string {
  return path.split("/").pop() ?? path;
}

function dirName(path: string): string {
  const parts = path.split("/");
  return parts.length > 1 ? parts.slice(0, -1).join("/") : "";
}

// ── Component ────────────────────────────────────────────────────────────────

export function DiffFileTree({ files, selectedId, onSelect, className }: DiffFileTreeProps) {
  // Group by directory for better readability.
  const grouped = useMemo(() => {
    const map = new Map<string, FileEntry[]>();
    for (const f of files) {
      const dir = dirName(f.file_path);
      const list = map.get(dir) ?? [];
      list.push(f);
      map.set(dir, list);
    }
    // Sort directories.
    return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [files]);

  return (
    <nav className={`flex flex-col gap-0.5 text-sm ${className ?? ""}`}>
      <div className="flex items-center gap-2 px-2 py-1.5 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
        <File className="h-3.5 w-3.5" />
        Files ({files.length})
      </div>

      {grouped.map(([dir, entries]) => (
        <div key={dir}>
          {dir && (
            <div className="px-2 py-1 text-[11px] text-muted-foreground truncate">
              {dir}/
            </div>
          )}
          {entries.map((entry) => {
            const Icon = changeIcon[entry.change_type] ?? File;
            const badge = changeBadge[entry.change_type];
            const isSelected = entry.diff_id === selectedId;

            return (
              <button
                key={entry.diff_id}
                onClick={() => onSelect(entry.diff_id)}
                className={`w-full flex items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors
                  ${isSelected
                    ? "bg-primary/10 text-primary font-medium"
                    : "hover:bg-muted text-foreground"
                  }`}
              >
                <Icon className="h-3.5 w-3.5 flex-shrink-0 text-muted-foreground" />
                <span className="truncate flex-1">{fileName(entry.file_path)}</span>
                <span className={`flex-shrink-0 h-2 w-2 rounded-full ${statusDot[entry.status]}`} />
                <span className={`flex-shrink-0 rounded px-1 py-0.5 text-[10px] font-bold ${badge.className}`}>
                  {badge.label}
                </span>
              </button>
            );
          })}
        </div>
      ))}

      {files.length === 0 && (
        <p className="px-2 py-4 text-xs text-muted-foreground italic text-center">
          No files
        </p>
      )}
    </nav>
  );
}
