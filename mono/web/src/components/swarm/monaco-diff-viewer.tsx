// ─── Monaco Side-by-Side Diff Viewer ─────────────────────────────────────────
// Full-featured diff viewer using Monaco Editor with side-by-side "before/after"
// display, inline comments, and agent annotation support.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import {
  SplitSquareVertical,
  AlignLeft,
  MessageSquare,
  Send,
  Bot,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import type { SwarmDiff, SwarmDiffMeta } from "@/types/swarm";

interface MonacoDiffViewerProps {
  diff: SwarmDiff | SwarmDiffMeta;
  originalContent?: string;
  modifiedContent?: string;
  language?: string;
  onComment?: (line: number, text: string) => void;
  onAskSwarm?: (selection: string, line: number) => void;
  readOnly?: boolean;
  height?: string;
}

interface InlineComment {
  line: number;
  text: string;
  author: string;
  timestamp: string;
}

export function MonacoDiffViewer({
  diff,
  originalContent,
  modifiedContent,
  language,
  onComment,
  onAskSwarm,
  readOnly = false,
  height = "600px",
}: MonacoDiffViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<any>(null);
  const [isSideBySide, setIsSideBySide] = useState(true);
  const [comments, setComments] = useState<InlineComment[]>([]);
  const [commentInput, setCommentInput] = useState("");
  const [commentLine, setCommentLine] = useState<number | null>(null);
  const [monacoLoaded, setMonacoLoaded] = useState(false);

  // Detect language from file extension
  const detectLanguage = useCallback((filePath: string): string => {
    if (language) return language;
    const ext = filePath.split(".").pop()?.toLowerCase() || "";
    const langMap: Record<string, string> = {
      ts: "typescript",
      tsx: "typescript",
      js: "javascript",
      jsx: "javascript",
      py: "python",
      go: "go",
      rs: "rust",
      java: "java",
      cpp: "cpp",
      c: "c",
      h: "c",
      hpp: "cpp",
      cs: "csharp",
      rb: "ruby",
      php: "php",
      swift: "swift",
      kt: "kotlin",
      yaml: "yaml",
      yml: "yaml",
      json: "json",
      md: "markdown",
      sql: "sql",
      sh: "shell",
      dockerfile: "dockerfile",
    };
    return langMap[ext] || "plaintext";
  }, [language]);

  // Parse the unified diff to extract original and modified content
  const parseUnifiedDiff = useCallback((unified: string): { original: string; modified: string } => {
    if (originalContent && modifiedContent) {
      return { original: originalContent, modified: modifiedContent };
    }

    const lines = unified.split("\n");
    const origLines: string[] = [];
    const modLines: string[] = [];

    for (const line of lines) {
      if (line.startsWith("---") || line.startsWith("+++") || line.startsWith("@@")) {
        continue;
      }
      if (line.startsWith("-")) {
        origLines.push(line.substring(1));
      } else if (line.startsWith("+")) {
        modLines.push(line.substring(1));
      } else if (line.startsWith(" ")) {
        origLines.push(line.substring(1));
        modLines.push(line.substring(1));
      }
    }

    return { original: origLines.join("\n"), modified: modLines.join("\n") };
  }, [originalContent, modifiedContent]);

  // Load Monaco Editor dynamically
  useEffect(() => {
    let mounted = true;

    const loadMonaco = async () => {
      // Monaco is expected to be available via the Next.js bundle
      // or loaded from CDN. Check if it's already on window.
      if ((window as any).monaco) {
        if (mounted) setMonacoLoaded(true);
        return;
      }

      // Dynamic import as fallback
      try {
        const monaco = await import("monaco-editor");
        (window as any).monaco = monaco;
        if (mounted) setMonacoLoaded(true);
      } catch {
        // Monaco not available — fall back to text display
        if (mounted) setMonacoLoaded(false);
      }
    };

    loadMonaco();
    return () => { mounted = false; };
  }, []);

  // Create/update the diff editor
  useEffect(() => {
    if (!monacoLoaded || !containerRef.current) return;

    const monaco = (window as any).monaco;
    if (!monaco) return;

    const filePath = "file_path" in diff ? diff.file_path : "";
    const lang = detectLanguage(filePath);
    const diffContent = "content" in diff ? String(diff.content ?? "") : "";
    const { original, modified } = parseUnifiedDiff(diffContent);

    const originalModel = monaco.editor.createModel(original, lang);
    const modifiedModel = monaco.editor.createModel(modified, lang);

    if (editorRef.current) {
      editorRef.current.dispose();
    }

    const editor = monaco.editor.createDiffEditor(containerRef.current, {
      automaticLayout: true,
      readOnly: true,
      renderSideBySide: isSideBySide,
      enableSplitViewResizing: true,
      minimap: { enabled: false },
      lineNumbers: "on",
      scrollBeyondLastLine: false,
      fontSize: 13,
      theme: document.documentElement.classList.contains("dark")
        ? "vs-dark"
        : "vs",
      glyphMargin: true,
    });

    editor.setModel({ original: originalModel, modified: modifiedModel });
    editorRef.current = editor;

    // Add right-click context menu for comments and "Ask Swarm"
    if (!readOnly) {
      const modifiedEditor = editor.getModifiedEditor();
      modifiedEditor.addAction({
        id: "add-comment",
        label: "Add Comment",
        contextMenuGroupId: "navigation",
        contextMenuOrder: 1,
        run: (ed: any) => {
          const pos = ed.getPosition();
          if (pos) setCommentLine(pos.lineNumber);
        },
      });

      if (onAskSwarm) {
        modifiedEditor.addAction({
          id: "ask-swarm",
          label: "🤖 Ask Swarm to Explain",
          contextMenuGroupId: "navigation",
          contextMenuOrder: 2,
          run: (ed: any) => {
            const selection = ed.getModel().getValueInRange(ed.getSelection());
            const pos = ed.getPosition();
            if (selection && pos) {
              onAskSwarm(selection, pos.lineNumber);
            }
          },
        });
      }
    }

    return () => {
      originalModel.dispose();
      modifiedModel.dispose();
    };
  }, [monacoLoaded, diff, isSideBySide, detectLanguage, parseUnifiedDiff, readOnly, onAskSwarm]);

  const handleAddComment = () => {
    if (!commentInput.trim() || commentLine === null) return;
    const newComment: InlineComment = {
      line: commentLine,
      text: commentInput.trim(),
      author: "You",
      timestamp: new Date().toISOString(),
    };
    setComments((prev) => [...prev, newComment]);
    onComment?.(commentLine, commentInput.trim());
    setCommentInput("");
    setCommentLine(null);
  };

  const filePath = "file_path" in diff ? diff.file_path : "";

  // Fallback when Monaco is not available
  if (!monacoLoaded) {
    const diffContent = "content" in diff ? String(diff.content ?? "") : "";
    return (
      <div className="border rounded-lg overflow-hidden">
        <div className="flex items-center justify-between px-3 py-2 bg-muted/50 border-b">
          <span className="text-sm font-mono">{filePath}</span>
          <span className="text-xs text-muted-foreground">Monaco not available — text fallback</span>
        </div>
        <pre className="p-4 text-sm overflow-auto whitespace-pre font-mono" style={{ maxHeight: height }}>
          {diffContent}
        </pre>
      </div>
    );
  }

  return (
    <div className="border rounded-lg overflow-hidden">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-3 py-2 bg-muted/50 border-b">
        <span className="text-sm font-mono truncate">{filePath}</span>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setIsSideBySide(!isSideBySide)}
            title={isSideBySide ? "Switch to inline" : "Switch to side-by-side"}
          >
            {isSideBySide ? <AlignLeft className="h-4 w-4" /> : <SplitSquareVertical className="h-4 w-4" />}
          </Button>
          {comments.length > 0 && (
            <span className="text-xs text-muted-foreground flex items-center gap-1">
              <MessageSquare className="h-3 w-3" /> {comments.length}
            </span>
          )}
        </div>
      </div>

      {/* Monaco diff editor container */}
      <div ref={containerRef} style={{ height }} />

      {/* Inline comment input */}
      {commentLine !== null && (
        <div className="flex items-center gap-2 px-3 py-2 border-t bg-muted/30">
          <MessageSquare className="h-4 w-4 text-muted-foreground" />
          <span className="text-xs text-muted-foreground">Line {commentLine}:</span>
          <input
            type="text"
            className="flex-1 text-sm bg-transparent border-none outline-none"
            placeholder="Add a comment..."
            value={commentInput}
            onChange={(e) => setCommentInput(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && handleAddComment()}
            autoFocus
          />
          <Button size="sm" variant="ghost" onClick={handleAddComment}>
            <Send className="h-3 w-3" />
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setCommentLine(null)}>
            <X className="h-3 w-3" />
          </Button>
        </div>
      )}

      {/* Comment list */}
      {comments.length > 0 && (
        <div className="border-t divide-y max-h-48 overflow-auto">
          {comments.map((c, i) => (
            <div key={i} className="px-3 py-2 text-sm flex gap-2">
              <span className="text-muted-foreground font-mono text-xs">L{c.line}</span>
              <span className="font-medium">{c.author}:</span>
              <span>{c.text}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── Ask Swarm Button ────────────────────────────────────────────────────────

interface AskSwarmButtonProps {
  taskId: string;
  context?: string;
  onResponse?: (response: string) => void;
}

export function AskSwarmButton({ taskId, context, onResponse }: AskSwarmButtonProps) {
  const [loading, setLoading] = useState(false);
  const [response, setResponse] = useState<string | null>(null);
  const [streaming, setStreaming] = useState(false);
  const responseRef = useRef<HTMLDivElement>(null);

  const handleAsk = async () => {
    setLoading(true);
    setResponse("");
    setStreaming(true);

    try {
      const res = await fetch(`/api/v1/swarm/tasks/${taskId}/explain`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ context: context || "" }),
      });

      if (!res.ok) {
        setResponse("Failed to get explanation from swarm.");
        return;
      }

      // Stream the response via SSE-style chunked transfer
      const reader = res.body?.getReader();
      const decoder = new TextDecoder();
      let accumulated = "";

      if (reader) {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          accumulated += decoder.decode(value, { stream: true });
          setResponse(accumulated);
          if (responseRef.current) {
            responseRef.current.scrollTop = responseRef.current.scrollHeight;
          }
        }
      }

      onResponse?.(accumulated);
    } catch {
      setResponse("Error communicating with swarm.");
    } finally {
      setLoading(false);
      setStreaming(false);
    }
  };

  return (
    <div className="space-y-2">
      <Button
        onClick={handleAsk}
        disabled={loading}
        variant="outline"
        size="sm"
        className="gap-2"
      >
        <Bot className="h-4 w-4" />
        {loading ? "Asking swarm..." : "Ask swarm to explain"}
      </Button>

      {response !== null && (
        <div
          ref={responseRef}
          className="rounded-lg border p-3 bg-muted/30 text-sm max-h-64 overflow-auto whitespace-pre-wrap"
        >
          {streaming && (
            <span className="inline-block w-2 h-4 bg-primary animate-pulse ml-0.5" />
          )}
          {response || "Waiting for response..."}
        </div>
      )}
    </div>
  );
}
