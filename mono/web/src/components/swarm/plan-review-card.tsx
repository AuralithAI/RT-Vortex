// ─── Plan Review Card ────────────────────────────────────────────────────────
// Enhanced plan display with approve/reject, comment support, and step-by-step
// progress tracking.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useMemo, useState } from "react";
import {
  CheckCircle,
  XCircle,
  FileCode,
  MessageSquare,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  Clock,
  Send,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { LLMMarkdown } from "@/components/ui/llm-markdown";
import type { PlanDocument, PlanStep } from "@/types/swarm";

// ── Helpers ──────────────────────────────────────────────────────────────────

/**
 * Strip conversational preamble that some LLMs (especially Grok) add.
 * e.g. "Ok, I am the orchestrator agent and here is my plan..."
 *
 * Only scans the first few leading lines for preamble patterns.
 */
function stripPreamble(text: string): string {
  if (!text || typeof text !== "string") return "";
  const lines = text.split("\n");
  const result: string[] = [];
  let pastPreamble = false;
  let scanned = 0;

  for (const line of lines) {
    if (!pastPreamble) {
      const lower = line.trim().toLowerCase();
      if (!lower) continue; // skip leading blanks
      if (++scanned > 5) {
        pastPreamble = true;
        result.push(line);
        continue;
      }
      if (
        /^(ok[,.]?\s+)?(i am|i'm|as)\s+(the\s+)?(an?\s+)?orchestrator/.test(lower) ||
        /^(ok[,.]?\s+)?(let me|i'll|i will)\s+(start|begin|create|produce|analy[sz]e)/.test(lower) ||
        /^(sure|alright|certainly|understood)[,!.]/.test(lower)
      ) {
        continue;
      }
      pastPreamble = true;
    }
    result.push(line);
  }
  return result.length > 0 ? result.join("\n").trim() : text;
}

/**
 * When the backend fallback stuffs the entire LLM response into a single
 * step description, try to split it into structured steps on the client side.
 *
 * Detects:
 *  - Numbered lists  ("1. …", "2. …")
 *  - Bulleted lists  ("- …", "* …")
 *  - Markdown headings as separators
 */
function splitOverstuffedStep(desc: string): PlanStep[] {
  // Only trigger when a single description is very large (> 500 chars)
  // and contains list-like structure.
  if (desc.length < 500) return [{ description: desc }];

  // Try numbered list.
  const numberedPattern = /^\d+[.)]\s+/m;
  if (numberedPattern.test(desc)) {
    const items = desc.split(/\n(?=\d+[.)]\s+)/);
    const steps: PlanStep[] = [];
    for (const item of items) {
      const cleaned = item.replace(/^\d+[.)]\s+/, "").trim();
      if (cleaned) {
        steps.push({ description: cleaned.slice(0, 1000) });
      }
    }
    if (steps.length >= 2) return steps;
  }

  // Try bulleted list.
  const bulletPattern = /^[-*]\s+/m;
  if (bulletPattern.test(desc)) {
    const items = desc.split(/\n(?=[-*]\s+)/);
    const steps: PlanStep[] = [];
    for (const item of items) {
      const cleaned = item.replace(/^[-*]\s+/, "").trim();
      if (cleaned) {
        steps.push({ description: cleaned.slice(0, 1000) });
      }
    }
    if (steps.length >= 2) return steps;
  }

  // Try splitting on markdown headings.
  const headingPattern = /^#{2,4}\s+/m;
  if (headingPattern.test(desc)) {
    const sections = desc.split(/\n(?=#{2,4}\s+)/);
    const steps: PlanStep[] = [];
    for (const section of sections) {
      const cleaned = section.trim();
      if (cleaned) {
        const heading = cleaned.match(/^#{2,4}\s+(.+)/);
        if (heading) {
          const body = cleaned.slice(heading[0].length).trim();
          steps.push({
            description: body
              ? `**${heading[1]}**: ${body.slice(0, 800)}`
              : heading[1].slice(0, 500),
          });
        } else {
          steps.push({ description: cleaned.slice(0, 1000) });
        }
      }
    }
    if (steps.length >= 2) return steps;
  }

  // Couldn't split meaningfully — return as-is but truncated.
  return [{ description: desc.slice(0, 2000) }];
}

/**
 * Normalise a plan for display. Handles edge cases where the backend
 * fallback produced a single step containing the entire LLM response.
 * Also strips preamble from the summary and adds defensive null guards.
 */
function normalisePlan(plan: PlanDocument): PlanDocument {
  // ── Defensive defaults — backend may send partial / malformed data ────
  let summary = typeof plan.summary === "string" ? plan.summary : "";
  let steps: PlanStep[] = Array.isArray(plan.steps)
    ? plan.steps.filter(
        (s): s is PlanStep =>
          typeof s === "object" && s !== null && typeof s.description === "string",
      )
    : [];
  const affected_files: string[] = Array.isArray(plan.affected_files)
    ? plan.affected_files.filter((f): f is string => typeof f === "string")
    : [];
  const estimated_complexity = plan.estimated_complexity ?? "medium";
  const agents_needed = plan.agents_needed ?? [];

  // Strip preamble from summary.
  summary = stripPreamble(summary);

  // If there are no steps, nothing to normalise.
  if (steps.length === 0) {
    return { summary, steps: [{ description: summary || "No steps provided." }], affected_files, estimated_complexity, agents_needed };
  }

  // If there's only 1 step and it's excessively long, the backend fallback
  // likely stuffed the entire response into it. Try to extract structure.
  if (steps.length === 1 && steps[0].description.length > 500) {
    const rawDesc = steps[0].description;

    // Try to extract a JSON plan from the step description.
    const jsonMatch = rawDesc.match(/```(?:json)?\s*(\{[\s\S]*?\})\s*```/);
    if (jsonMatch) {
      try {
        const candidate = JSON.parse(jsonMatch[1]);
        if (candidate.summary || candidate.steps) {
          return {
            summary: stripPreamble(candidate.summary || summary),
            steps: Array.isArray(candidate.steps)
              ? candidate.steps.map((s: unknown) => {
                  if (typeof s === "string") return { description: s };
                  if (typeof s === "object" && s !== null) {
                    const obj = s as Record<string, unknown>;
                    return {
                      description:
                        (obj.description as string) ||
                        (obj.title as string) ||
                        (obj.action as string) ||
                        JSON.stringify(s),
                      files: Array.isArray(obj.files) ? obj.files : undefined,
                    };
                  }
                  return { description: String(s) };
                })
              : steps,
            affected_files: candidate.affected_files || affected_files,
            estimated_complexity: candidate.estimated_complexity || estimated_complexity,
            agents_needed: candidate.agents_needed || agents_needed,
          };
        }
      } catch {
        // JSON parse failed — continue to other strategies.
      }
    }

    // Try to split the overstuffed step.
    steps = splitOverstuffedStep(rawDesc);

    // If the summary is also the entire response (or very close), try to
    // extract just the first meaningful paragraph as the summary.
    if (summary.length > 300) {
      const paragraphs = summary.split(/\n{2,}/);
      if (paragraphs.length > 1) {
        const firstUseful = paragraphs.find(
          (p) => p.trim() && !p.trim().startsWith("#") && !p.trim().startsWith("```")
        );
        summary = firstUseful?.trim().slice(0, 500) || paragraphs[0].trim().slice(0, 500);
      }
    }
  }

  // Strip preamble from each step description too.
  steps = steps.map((step) => ({
    ...step,
    description: stripPreamble(step.description),
  }));

  return { summary, steps, affected_files, estimated_complexity, agents_needed };
}

interface PlanReviewCardProps {
  plan: PlanDocument;
  taskId: string;
  status: string;
  onApprove?: () => void;
  onReject?: () => void;
  onComment?: (comment: string) => void;
}

function complexityBadge(complexity: string) {
  switch (complexity) {
    case "small":
      return (
        <span className="rounded-full bg-green-100 px-2.5 py-0.5 text-xs font-medium text-green-800 dark:bg-green-900 dark:text-green-200">
          Small
        </span>
      );
    case "large":
      return (
        <span className="rounded-full bg-red-100 px-2.5 py-0.5 text-xs font-medium text-red-800 dark:bg-red-900 dark:text-red-200">
          Large
        </span>
      );
    default:
      return (
        <span className="rounded-full bg-yellow-100 px-2.5 py-0.5 text-xs font-medium text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200">
          Medium
        </span>
      );
  }
}

export function PlanReviewCard({
  plan,
  taskId,
  status,
  onApprove,
  onReject,
  onComment,
}: PlanReviewCardProps) {
  // Normalise plan: split overstuffed single-step plans, strip preamble, etc.
  const normalised = useMemo(() => normalisePlan(plan), [plan]);

  const [showComment, setShowComment] = useState(false);
  const [comment, setComment] = useState("");
  const [expandedSteps, setExpandedSteps] = useState<Set<number>>(() =>
    new Set(normalised.steps.map((_, i) => i))
  );

  // Re-sync expanded indices when the normalised step count changes
  // (e.g. a WebSocket update delivers a new plan).
  const stepCount = normalised.steps.length;
  useEffect(() => {
    setExpandedSteps(new Set(Array.from({ length: stepCount }, (_, i) => i)));
  }, [stepCount]);

  const toggleStep = (index: number) => {
    setExpandedSteps((prev) => {
      const next = new Set(prev);
      if (next.has(index)) {
        next.delete(index);
      } else {
        next.add(index);
      }
      return next;
    });
  };

  const handleSubmitComment = () => {
    if (comment.trim() && onComment) {
      onComment(comment.trim());
      setComment("");
      setShowComment(false);
    }
  };

  const canReview = status === "plan_review";

  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      {/* Header */}
      <div className="flex items-center justify-between border-b bg-muted/30 px-6 py-4">
        <div className="space-y-1">
          <h3 className="text-lg font-semibold">Implementation Plan</h3>
          <div className="flex items-center gap-3 text-sm text-muted-foreground">
            {complexityBadge(normalised.estimated_complexity)}
            <span>{normalised.steps.length} steps</span>
            <span>•</span>
            <span>{normalised.affected_files.length} files</span>
            {(() => {
              const needed = normalised.agents_needed;
              const count = Array.isArray(needed) ? needed.length : (typeof needed === "number" ? needed : 0);
              return count > 0 ? (
                <>
                  <span>•</span>
                  <span>{count} agent{count !== 1 ? "s" : ""}</span>
                </>
              ) : null;
            })()}
          </div>
        </div>
        {canReview && (
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => setShowComment(!showComment)}
            >
              <MessageSquare className="mr-2 h-4 w-4" />
              Comment
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={onReject}
            >
              <XCircle className="mr-2 h-4 w-4" />
              Reject
            </Button>
            <Button size="sm" onClick={onApprove}>
              <CheckCircle className="mr-2 h-4 w-4" />
              Approve
            </Button>
          </div>
        )}
      </div>

      {/* Comment Input */}
      {showComment && (
        <div className="border-b bg-muted/10 px-6 py-3">
          <div className="flex gap-2">
            <textarea
              className="flex-1 rounded-md border bg-background px-3 py-2 text-sm placeholder:text-muted-foreground"
              rows={2}
              placeholder="Add a comment or request changes…"
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                  handleSubmitComment();
                }
              }}
            />
            <Button
              size="sm"
              className="self-end"
              onClick={handleSubmitComment}
              disabled={!comment.trim()}
            >
              <Send className="h-4 w-4" />
            </Button>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            Press ⌘+Enter to submit
          </p>
        </div>
      )}

      {/* Summary */}
      <div className="border-b px-6 py-4">
        <h4 className="mb-2 text-sm font-medium text-muted-foreground">Summary</h4>
        <LLMMarkdown content={normalised.summary} variant="light" className="text-sm" />
      </div>

      {/* Steps */}
      <div className="border-b px-6 py-4">
        <h4 className="mb-3 text-sm font-medium text-muted-foreground">Steps</h4>
        <div className="space-y-2">
          {normalised.steps.map((step, i) => (
            <div key={i} className="rounded-md border">
              <button
                className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm hover:bg-muted/30"
                onClick={() => toggleStep(i)}
              >
                <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">
                  {i + 1}
                </span>
                {expandedSteps.has(i) ? (
                  <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                ) : (
                  <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                )}
                <span className="flex-1 font-medium line-clamp-2">
                  {step.description.split("\n")[0].slice(0, 200)}
                </span>
                {step.files && step.files.length > 0 && (
                  <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium text-muted-foreground">
                    {step.files.length} file{step.files.length !== 1 ? "s" : ""}
                  </span>
                )}
              </button>
              {expandedSteps.has(i) && (
                <div className="border-t bg-muted/10 px-4 py-3 space-y-2">
                  {/* Render the step description as rich markdown when expanded.
                      Constrain height to prevent a single long step from dominating the page. */}
                  <div className="max-h-[400px] overflow-y-auto">
                    <LLMMarkdown content={step.description} variant="light" className="text-xs" />
                  </div>
                  {step.files && step.files.length > 0 && (
                    <div className="pt-2 border-t border-dashed">
                      <p className="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                        Affected Files
                      </p>
                      <ul className="space-y-1">
                        {step.files.map((f, j) => (
                          <li
                            key={j}
                            className="flex items-center gap-2 font-mono text-xs text-muted-foreground"
                          >
                            <FileCode className="h-3 w-3 shrink-0" />
                            {f}
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      </div>

      {/* Affected Files */}
      <div className="px-6 py-4">
        <h4 className="mb-3 text-sm font-medium text-muted-foreground">
          All Affected Files ({normalised.affected_files.length})
        </h4>
        <div className="grid gap-1.5 sm:grid-cols-2">
          {normalised.affected_files.map((f, i) => (
            <div
              key={i}
              className="flex items-center gap-2 rounded-md border bg-muted/30 px-3 py-1.5 font-mono text-xs transition-colors hover:bg-muted/50"
            >
              <FileCode className="h-3.5 w-3.5 shrink-0 text-primary/60" />
              <span className="truncate">{f}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
