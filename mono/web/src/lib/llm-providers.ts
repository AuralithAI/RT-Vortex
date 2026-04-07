// ─── LLM Provider Metadata ───────────────────────────────────────────────────
// Display names, colors, and styling for each LLM provider used across the
// multi-LLM discussion panels, consensus cards, and judge verdict displays.
// ─────────────────────────────────────────────────────────────────────────────

import type { LLMProviderMeta } from "@/types/swarm";

const providers: Record<string, LLMProviderMeta> = {
  grok: {
    name: "grok",
    displayName: "Grok",
    color: "text-orange-600 dark:text-orange-400",
    bgColor: "bg-orange-50 dark:bg-orange-950/30",
    borderColor: "border-orange-200 dark:border-orange-800",
    accentHex: "#ea580c",
    dotColor: "bg-orange-500",
  },
  anthropic: {
    name: "anthropic",
    displayName: "Claude",
    color: "text-amber-700 dark:text-amber-400",
    bgColor: "bg-amber-50 dark:bg-amber-950/30",
    borderColor: "border-amber-200 dark:border-amber-800",
    accentHex: "#d97706",
    dotColor: "bg-amber-500",
  },
  openai: {
    name: "openai",
    displayName: "GPT-4o",
    color: "text-emerald-600 dark:text-emerald-400",
    bgColor: "bg-emerald-50 dark:bg-emerald-950/30",
    borderColor: "border-emerald-200 dark:border-emerald-800",
    accentHex: "#10b981",
    dotColor: "bg-emerald-500",
  },
  gemini: {
    name: "gemini",
    displayName: "Gemini",
    color: "text-blue-600 dark:text-blue-400",
    bgColor: "bg-blue-50 dark:bg-blue-950/30",
    borderColor: "border-blue-200 dark:border-blue-800",
    accentHex: "#2563eb",
    dotColor: "bg-blue-500",
  },
  google: {
    name: "google",
    displayName: "Gemini",
    color: "text-blue-600 dark:text-blue-400",
    bgColor: "bg-blue-50 dark:bg-blue-950/30",
    borderColor: "border-blue-200 dark:border-blue-800",
    accentHex: "#2563eb",
    dotColor: "bg-blue-500",
  },
  ollama: {
    name: "ollama",
    displayName: "Ollama",
    color: "text-slate-600 dark:text-slate-400",
    bgColor: "bg-slate-50 dark:bg-slate-950/30",
    borderColor: "border-slate-200 dark:border-slate-800",
    accentHex: "#64748b",
    dotColor: "bg-slate-500",
  },
  consensus: {
    name: "consensus",
    displayName: "Consensus",
    color: "text-violet-600 dark:text-violet-400",
    bgColor: "bg-violet-50 dark:bg-violet-950/30",
    borderColor: "border-violet-200 dark:border-violet-800",
    accentHex: "#7c3aed",
    dotColor: "bg-violet-500",
  },
};

const fallback: LLMProviderMeta = {
  name: "unknown",
  displayName: "Unknown",
  color: "text-gray-600 dark:text-gray-400",
  bgColor: "bg-gray-50 dark:bg-gray-950/30",
  borderColor: "border-gray-200 dark:border-gray-800",
  accentHex: "#6b7280",
  dotColor: "bg-gray-500",
};

/**
 * Get display metadata for an LLM provider name.
 * Falls back to a neutral gray style for unrecognised providers.
 */
export function getProviderMeta(providerName: string): LLMProviderMeta {
  const key = providerName.toLowerCase().replace(/[^a-z]/g, "");
  return providers[key] ?? { ...fallback, name: providerName, displayName: providerName };
}

/** All known provider keys in priority order (primary 4 first). */
export const PROVIDER_ORDER = ["openai", "grok", "anthropic", "gemini", "ollama"] as const;

/** Get all known provider meta objects. */
export function getAllProviderMetas(): LLMProviderMeta[] {
  return PROVIDER_ORDER.map((k) => providers[k]).filter(Boolean);
}
