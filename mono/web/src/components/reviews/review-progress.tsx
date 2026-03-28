// ─── Review Progress Component ───────────────────────────────────────────────
// Shows real-time progress of an ongoing review via WebSocket.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { Check, Loader2, AlertCircle, Clock } from "lucide-react";
import { Progress } from "@/components/ui/progress";
import { cn } from "@/lib/utils";
import type { ReviewProgressEvent } from "@/types/api";

interface ReviewProgressProps {
  events: ReviewProgressEvent[];
  connected: boolean;
}

const statusIcon = {
  pending: Clock,
  running: Loader2,
  completed: Check,
  failed: AlertCircle,
};

export function ReviewProgress({ events, connected }: ReviewProgressProps) {
  const latestEvent = events[events.length - 1];
  const progressPercent = latestEvent
    ? Math.round((latestEvent.step / latestEvent.total_steps) * 100)
    : 0;

  return (
    <div className="space-y-4 rounded-lg border p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">Review Progress</h3>
        <div className="flex items-center gap-1.5">
          <div
            className={cn(
              "h-2 w-2 rounded-full",
              connected ? "bg-green-500" : "bg-gray-400",
            )}
          />
          <span className="text-xs text-muted-foreground">
            {connected ? "Live" : "Disconnected"}
          </span>
        </div>
      </div>

      <Progress value={progressPercent} className="h-2" />
      <p className="text-xs text-muted-foreground">
        {progressPercent}% complete
      </p>

      <div className="space-y-2">
        {events.map((event, idx) => {
          const Icon = statusIcon[event.status] ?? Clock;
          return (
            <div
              key={idx}
              className="flex items-center gap-2 text-sm"
            >
              <Icon
                className={cn(
                  "h-4 w-4 shrink-0",
                  event.status === "running" && "animate-spin text-blue-500",
                  event.status === "completed" && "text-green-500",
                  event.status === "failed" && "text-red-500",
                  event.status === "pending" && "text-muted-foreground",
                )}
              />
              <span
                className={cn(
                  event.status === "completed" && "text-muted-foreground line-through",
                  event.status === "failed" && "text-red-600",
                )}
              >
                {event.label}
              </span>
              {event.detail && (
                <span className="text-xs text-muted-foreground">
                  — {event.detail}
                </span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
