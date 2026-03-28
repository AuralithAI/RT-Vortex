// ─── Comment Thread Component ────────────────────────────────────────────────
// Renders a single review comment with severity badge and file location.
// ─────────────────────────────────────────────────────────────────────────────

import { Badge } from "@/components/ui/badge";
import type { ReviewComment } from "@/types/api";

const severityVariant: Record<string, "default" | "secondary" | "destructive" | "success" | "warning" | "outline"> = {
  critical: "destructive",
  warning: "warning",
  suggestion: "secondary",
  info: "outline",
};

interface CommentThreadProps {
  comment: ReviewComment;
}

export function CommentThread({ comment }: CommentThreadProps) {
  return (
    <div className="rounded-lg border p-4 space-y-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Badge variant={severityVariant[comment.severity] ?? "outline"}>
            {comment.severity}
          </Badge>
          {comment.file_path && (
            <code className="text-xs text-muted-foreground">
              {comment.file_path}
              {comment.line_start ? `:${comment.line_start}` : ""}
              {comment.line_end && comment.line_end !== comment.line_start
                ? `-${comment.line_end}`
                : ""}
            </code>
          )}
        </div>
      </div>
      <p className="text-sm whitespace-pre-wrap">{comment.body}</p>
      {comment.suggestion && (
        <div className="rounded-md border bg-muted/50 p-3">
          <p className="mb-1 text-xs font-medium text-muted-foreground">
            Suggested change:
          </p>
          <pre className="text-xs overflow-x-auto whitespace-pre-wrap font-mono">
            {comment.suggestion}
          </pre>
        </div>
      )}
    </div>
  );
}
