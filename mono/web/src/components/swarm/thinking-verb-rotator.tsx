// ─── Thinking Verb Rotator ───────────────────────────────────────────────────
// Claude-CLI-inspired animated "thinking" status indicator. Displays a
// rotating list of playful, developer-focused verbs with a spiral spinner
// and bouncing dots. Fully supports light and dark mode via Tailwind.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useRef, useState, useMemo } from "react";

// ── The canonical verb list ─────────────────────────────────────────────────
// Keep in sync with mono/server-go/internal/swarm/thinking_verbs.go

const THINKING_VERBS = [
  // Core reasoning
  "reasoning deeply",
  "synthesizing thoughts",
  "distilling insights",
  "crystallizing ideas",
  "pondering possibilities",
  "hypothesis testing",
  "chain-of-thought stepping",
  "self-reflecting",
  "emergent thinking",
  "insight harvesting",
  "thought compressing",
  "idea refining",

  // Pattern & structure
  "weaving patterns",
  "forging connections",
  "pattern matching",
  "semantic stitching",
  "context surfing",
  "knowledge retrieving",

  // ML / neural flavoured
  "chunking context",
  "munging tokens",
  "embedding vectors",
  "gradient descending",
  "attention focusing",
  "token dancing",
  "latent space navigating",
  "vector vortexing",
  "probabilistic dreaming",
  "memory consolidating",
  "orchestrating neurons",
  "parallel processing",

  // Code-review specific
  "scanning diff hunks",
  "tracing call graphs",
  "analysing dependencies",
  "mapping code paths",
  "cross-referencing modules",
  "evaluating complexity",
  "checking invariants",
  "verifying contracts",
  "linting logic",
  "profiling hot paths",

  // Creative / fun
  "quantum leaping",
  "creativity sparking",
  "hallucination checking",
  "coherence tuning",
  "wisdom distilling",
  "entropy reducing",
  "signal amplifying",
  "noise filtering",
  "context window juggling",
  "attention head aligning",
  "transformer layering",
  "weight adjusting",
  "beam searching",
  "temperature sampling",

  // RTVortex-flavoured
  "vortex spinning",
  "swarm coordinating",
  "consensus building",
  "agent syncing",
  "plan drafting",
  "diff assembling",
  "review composing",
  "insight merging",
  "feedback integrating",
  "strategy forming",
] as const;

// ── Accent colour palette (cycled randomly for each rotation) ───────────────

interface AccentTheme {
  /** Tailwind text class — dark mode */
  text: string;
  /** Tailwind text class — light mode */
  textLight: string;
  /** CSS color for glow shadow */
  glow: string;
}

const ACCENTS: AccentTheme[] = [
  { text: "text-blue-400",    textLight: "text-blue-600",    glow: "#60a5fa" },
  { text: "text-violet-400",  textLight: "text-violet-600",  glow: "#a78bfa" },
  { text: "text-cyan-400",    textLight: "text-cyan-600",    glow: "#22d3ee" },
  { text: "text-purple-400",  textLight: "text-purple-600",  glow: "#c084fc" },
  { text: "text-emerald-400", textLight: "text-emerald-600", glow: "#34d399" },
  { text: "text-pink-400",    textLight: "text-pink-600",    glow: "#f472b6" },
  { text: "text-amber-400",   textLight: "text-amber-600",   glow: "#fbbf24" },
];

// ── Component Props ─────────────────────────────────────────────────────────

export interface ThinkingVerbRotatorProps {
  /** Rotation interval in ms (default 1200) */
  intervalMs?: number;
  /**
   * Visual size variant.
   *  - `"sm"`:  compact inline badge (for card headers)
   *  - `"md"`:  standard indicator with spinner + dots
   *  - `"lg"`:  hero-sized statement with full spiral
   */
  size?: "sm" | "md" | "lg";
  /** Extra Tailwind classes */
  className?: string;
}

// ─── Component ──────────────────────────────────────────────────────────────

export function ThinkingVerbRotator({
  intervalMs = 1200,
  size = "md",
  className = "",
}: ThinkingVerbRotatorProps) {
  // Shuffle the verb list once per mount so different components don't sync.
  const verbs = useMemo(() => {
    const copy = [...THINKING_VERBS];
    for (let i = copy.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [copy[i], copy[j]] = [copy[j], copy[i]];
    }
    return copy;
  }, []);

  const [index, setIndex] = useState(0);
  const [visible, setVisible] = useState(true);
  const [accent, setAccent] = useState(() =>
    ACCENTS[Math.floor(Math.random() * ACCENTS.length)],
  );
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    timerRef.current = setInterval(() => {
      // Fade out
      setVisible(false);

      // After fade-out, swap text + accent and fade in
      setTimeout(() => {
        setIndex((prev) => (prev + 1) % verbs.length);
        setAccent(ACCENTS[Math.floor(Math.random() * ACCENTS.length)]);
        setVisible(true);
      }, 200);
    }, intervalMs);

    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [intervalMs, verbs.length]);

  const verb = verbs[index];

  // ── Size-specific rendering ─────────────────────────────────────────────

  if (size === "sm") {
    return (
      <span
        className={`inline-flex items-center gap-1.5 transition-all duration-200 ${className}`}
        style={{ opacity: visible ? 1 : 0, transform: visible ? "translateY(0)" : "translateY(-3px)" }}
      >
        <span className="relative inline-block h-3 w-3">
          <span className="absolute inset-0 rounded-full border-[1.5px] border-transparent border-t-current animate-spin dark:text-blue-400 text-blue-600" />
        </span>
        <span
          className={`text-[11px] font-medium dark:${accent.text} ${accent.textLight}`}
          style={{ textShadow: `0 0 12px ${accent.glow}40` }}
        >
          {verb}
        </span>
      </span>
    );
  }

  if (size === "lg") {
    return (
      <div className={`flex items-center gap-5 ${className}`}>
        <div className="thinking-spiral relative h-10 w-10">
          <span className="absolute inset-0 rounded-full border-[3px] border-transparent border-t-blue-400 border-r-violet-400 border-b-cyan-400 animate-[spin_1.2s_linear_infinite] dark:border-t-blue-400 dark:border-r-violet-400 dark:border-b-cyan-400" />
          <span className="absolute inset-[5px] rounded-full border-[3px] border-transparent border-t-purple-400 animate-[spin_1.8s_linear_infinite_reverse] dark:border-t-purple-400" />
        </div>

        <span
          className={`text-lg font-medium transition-all duration-200 min-w-[200px] dark:${accent.text} ${accent.textLight}`}
          style={{
            opacity: visible ? 1 : 0,
            transform: visible ? "translateY(0)" : "translateY(-4px)",
            textShadow: `0 0 20px ${accent.glow}60`,
          }}
        >
          {verb}
        </span>

        <BouncingDots />
      </div>
    );
  }

  // ── Default: "md" ───────────────────────────────────────────────────────

  return (
    <div className={`flex items-center gap-3 ${className}`}>
      {/* Spiral spinner (smaller) */}
      <div className="relative h-5 w-5 flex-shrink-0">
        <span className="absolute inset-0 rounded-full border-2 border-transparent border-t-blue-400 border-r-violet-400 animate-[spin_1.2s_linear_infinite] dark:border-t-blue-400 dark:border-r-violet-400 text-blue-600" />
        <span className="absolute inset-[3px] rounded-full border-2 border-transparent border-t-purple-400 animate-[spin_1.8s_linear_infinite_reverse] dark:border-t-purple-400 text-purple-600" />
      </div>

      <span
        className={`text-xs font-medium transition-all duration-200 dark:${accent.text} ${accent.textLight}`}
        style={{
          opacity: visible ? 1 : 0,
          transform: visible ? "translateY(0)" : "translateY(-3px)",
          textShadow: `0 0 14px ${accent.glow}40`,
        }}
      >
        {verb}
      </span>

      <BouncingDots small />
    </div>
  );
}

// ── Bouncing Dots Sub-component ─────────────────────────────────────────────

function BouncingDots({ small = false }: { small?: boolean }) {
  const dotSize = small ? "h-1.5 w-1.5" : "h-2 w-2";
  return (
    <span className="inline-flex items-center gap-1">
      <span
        className={`inline-block ${dotSize} rounded-full bg-blue-400 dark:bg-blue-400 animate-bounce`}
        style={{ animationDelay: "0ms" }}
      />
      <span
        className={`inline-block ${dotSize} rounded-full bg-violet-400 dark:bg-violet-400 animate-bounce`}
        style={{ animationDelay: "150ms" }}
      />
      <span
        className={`inline-block ${dotSize} rounded-full bg-cyan-400 dark:bg-cyan-400 animate-bounce`}
        style={{ animationDelay: "300ms" }}
      />
    </span>
  );
}

// ── Re-export the verb list for use by other components ─────────────────────
export { THINKING_VERBS };
