// ─── Animated Agent Avatars ──────────────────────────────────────────────────
// Fun pixel-art inspired animated avatars for each agent role.
// Inspired by retro game characters — each role has a distinct personality.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import React from "react";
import type { AgentRole } from "@/types/swarm";

interface AgentAvatarProps {
  role: AgentRole | string;
  size?: "sm" | "md" | "lg";
  busy?: boolean;
  className?: string;
}

// ── Pixel-art SVG characters per role ────────────────────────────────────────

function OrchestratorAvatar({ size, busy }: { size: number; busy: boolean }) {
  // Wizard / brain boss — purple robe, star staff
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} className={busy ? "animate-orchestrator" : ""}>
      {/* Hat */}
      <polygon points="16,2 10,14 22,14" fill="#7c3aed" />
      <rect x="12" y="12" width="8" height="2" fill="#6d28d9" rx="1" />
      {/* Star on hat */}
      <polygon points="16,5 17,8 20,8 17.5,10 18.5,13 16,11 13.5,13 14.5,10 12,8 15,8" fill="#fbbf24" className="animate-twinkle" />
      {/* Face */}
      <rect x="12" y="14" width="8" height="7" fill="#fde68a" rx="2" />
      {/* Eyes */}
      <circle cx="14" cy="17" r="1" fill="#1e1b4b" className="animate-blink" />
      <circle cx="18" cy="17" r="1" fill="#1e1b4b" className="animate-blink" />
      {/* Smile */}
      <path d="M14 19 Q16 21 18 19" fill="none" stroke="#92400e" strokeWidth="0.7" />
      {/* Body / robe */}
      <path d="M10 21 L12 21 L12 28 L10 30 Z" fill="#7c3aed" />
      <path d="M20 21 L22 21 L22 30 L20 28 Z" fill="#7c3aed" />
      <rect x="12" y="21" width="8" height="7" fill="#8b5cf6" rx="1" />
      {/* Staff */}
      <rect x="23" y="8" width="1.5" height="20" fill="#92400e" rx="0.5" />
      <circle cx="23.75" cy="7" r="2.5" fill="#fbbf24" className="animate-glow" />
    </svg>
  );
}

function SeniorDevAvatar({ size, busy }: { size: number; busy: boolean }) {
  // Mario-style plumber / coder with hard hat and laptop
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} className={busy ? "animate-bounce-gentle" : ""}>
      {/* Hard hat */}
      <ellipse cx="16" cy="11" rx="7" ry="3" fill="#2563eb" />
      <rect x="10" y="8" width="12" height="4" fill="#3b82f6" rx="2" />
      {/* Hat brim light */}
      <rect x="11" y="8" width="4" height="1.5" fill="#60a5fa" rx="0.5" />
      {/* Face */}
      <rect x="11" y="12" width="10" height="8" fill="#fde68a" rx="3" />
      {/* Glasses */}
      <rect x="12" y="14" width="3" height="2.5" fill="none" stroke="#1e3a5f" strokeWidth="0.7" rx="0.5" />
      <rect x="17" y="14" width="3" height="2.5" fill="none" stroke="#1e3a5f" strokeWidth="0.7" rx="0.5" />
      <line x1="15" y1="15.2" x2="17" y2="15.2" stroke="#1e3a5f" strokeWidth="0.5" />
      {/* Eyes behind glasses */}
      <circle cx="13.5" cy="15.2" r="0.7" fill="#1e3a5f" className="animate-blink" />
      <circle cx="18.5" cy="15.2" r="0.7" fill="#1e3a5f" className="animate-blink" />
      {/* Grin */}
      <path d="M14 18 Q16 20 18 18" fill="none" stroke="#92400e" strokeWidth="0.8" />
      {/* Body */}
      <rect x="11" y="20" width="10" height="8" fill="#2563eb" rx="2" />
      {/* Laptop */}
      <rect x="7" y="23" width="6" height="4" fill="#374151" rx="1" />
      <rect x="7.5" y="23.5" width="5" height="2.5" fill="#60a5fa" rx="0.5" className="animate-screen-flicker" />
      {/* Typing hand */}
      <rect x="9" y="27" width="3" height="1" fill="#fde68a" rx="0.5" />
    </svg>
  );
}

function JuniorDevAvatar({ size, busy }: { size: number; busy: boolean }) {
  // Energetic bird — Angry Birds inspired, small & eager
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} className={busy ? "animate-wobble" : ""}>
      {/* Body — round like angry bird */}
      <ellipse cx="16" cy="18" rx="10" ry="9" fill="#f59e0b" />
      <ellipse cx="16" cy="18" rx="10" ry="9" fill="url(#jr-belly)" />
      {/* Belly gradient */}
      <defs>
        <radialGradient id="jr-belly" cx="50%" cy="60%" r="50%">
          <stop offset="0%" stopColor="#fef3c7" />
          <stop offset="100%" stopColor="#f59e0b" stopOpacity="0" />
        </radialGradient>
      </defs>
      {/* Hair tuft */}
      <path d="M15 9 L14 5 L16 8 L17 4 L17 9" fill="#d97706" />
      {/* Eyes — big and eager */}
      <ellipse cx="13" cy="16" rx="2.5" ry="3" fill="white" />
      <ellipse cx="19" cy="16" rx="2.5" ry="3" fill="white" />
      <circle cx="13.5" cy="16.5" r="1.5" fill="#1c1917" className="animate-look-around" />
      <circle cx="19.5" cy="16.5" r="1.5" fill="#1c1917" className="animate-look-around" />
      {/* Sparkle in eyes */}
      <circle cx="14.2" cy="15.5" r="0.5" fill="white" />
      <circle cx="20.2" cy="15.5" r="0.5" fill="white" />
      {/* Beak */}
      <polygon points="15,20 17,20 16,23" fill="#ea580c" />
      {/* Tiny wings */}
      <ellipse cx="7" cy="20" rx="3" ry="2" fill="#d97706" className={busy ? "animate-flap" : ""} />
      <ellipse cx="25" cy="20" rx="3" ry="2" fill="#d97706" className={busy ? "animate-flap-r" : ""} />
      {/* Feet */}
      <path d="M12 26 L10 30 L14 30 Z" fill="#ea580c" />
      <path d="M20 26 L18 30 L22 30 Z" fill="#ea580c" />
    </svg>
  );
}

function ArchitectAvatar({ size, busy }: { size: number; busy: boolean }) {
  // Wise owl with blueprint
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} className={busy ? "animate-float" : ""}>
      {/* Ear tufts */}
      <polygon points="8,10 10,4 12,10" fill="#0891b2" />
      <polygon points="20,10 22,4 24,10" fill="#0891b2" />
      {/* Head */}
      <ellipse cx="16" cy="14" rx="8" ry="7" fill="#06b6d4" />
      {/* Face disk */}
      <ellipse cx="16" cy="15" rx="6" ry="5.5" fill="#cffafe" />
      {/* Eyes — wise */}
      <ellipse cx="13" cy="14" rx="2.5" ry="2.5" fill="white" stroke="#164e63" strokeWidth="0.5" />
      <ellipse cx="19" cy="14" rx="2.5" ry="2.5" fill="white" stroke="#164e63" strokeWidth="0.5" />
      <circle cx="13" cy="14" r="1.5" fill="#164e63" className="animate-blink" />
      <circle cx="19" cy="14" r="1.5" fill="#164e63" className="animate-blink" />
      {/* Beak */}
      <polygon points="15,17 17,17 16,19.5" fill="#ea580c" />
      {/* Body */}
      <ellipse cx="16" cy="24" rx="7" ry="6" fill="#0891b2" />
      {/* Blueprint */}
      <rect x="4" y="22" width="7" height="5" fill="#dbeafe" rx="0.5" stroke="#3b82f6" strokeWidth="0.5" />
      <line x1="5" y1="24" x2="10" y2="24" stroke="#3b82f6" strokeWidth="0.3" />
      <line x1="5" y1="25.5" x2="9" y2="25.5" stroke="#3b82f6" strokeWidth="0.3" />
      {/* Wing hand holding blueprint */}
      <ellipse cx="10" cy="24" rx="2" ry="1.5" fill="#06b6d4" />
    </svg>
  );
}

function QAAvatar({ size, busy }: { size: number; busy: boolean }) {
  // Mushroom inspector — Super Mario toad with magnifying glass
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} className={busy ? "animate-bounce-gentle" : ""}>
      {/* Mushroom cap */}
      <ellipse cx="16" cy="10" rx="10" ry="7" fill="#16a34a" />
      {/* Cap spots */}
      <circle cx="12" cy="8" r="2.5" fill="white" />
      <circle cx="20" cy="8" r="2.5" fill="white" />
      <circle cx="16" cy="5" r="2" fill="white" />
      {/* Face */}
      <rect x="11" y="13" width="10" height="8" fill="#fef3c7" rx="3" />
      {/* Eyes */}
      <circle cx="14" cy="16" r="1.2" fill="#14532d" className="animate-blink" />
      <circle cx="18" cy="16" r="1.2" fill="#14532d" className="animate-blink" />
      {/* Serious mouth */}
      <line x1="14" y1="19" x2="18" y2="19" stroke="#92400e" strokeWidth="0.7" strokeLinecap="round" />
      {/* Body */}
      <rect x="12" y="21" width="8" height="7" fill="#dcfce7" rx="2" />
      {/* Magnifying glass */}
      <circle cx="25" cy="16" r="3" fill="none" stroke="#374151" strokeWidth="1.2" />
      <circle cx="25" cy="16" r="2" fill="#bfdbfe" opacity="0.5" />
      <line x1="23" y1="18.5" x2="21" y2="21" stroke="#374151" strokeWidth="1.2" strokeLinecap="round" />
      {/* Checkmark inside glass when busy */}
      {busy && <path d="M24 16 L25 17.5 L27 14.5" fill="none" stroke="#16a34a" strokeWidth="0.8" className="animate-check-draw" />}
    </svg>
  );
}

function SecurityAvatar({ size, busy }: { size: number; busy: boolean }) {
  // Shield knight — red armor, vigilant
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} className={busy ? "animate-vigilant" : ""}>
      {/* Helmet */}
      <path d="M10 14 L10 8 Q16 2 22 8 L22 14 Z" fill="#991b1b" />
      <rect x="10" y="12" width="12" height="3" fill="#b91c1c" rx="1" />
      {/* Visor slit */}
      <rect x="12" y="13" width="8" height="1.5" fill="#1c1917" rx="0.5" />
      {/* Eyes through visor */}
      <circle cx="14" cy="13.7" r="0.6" fill="#fbbf24" className="animate-scan" />
      <circle cx="18" cy="13.7" r="0.6" fill="#fbbf24" className="animate-scan" />
      {/* Face */}
      <rect x="12" y="15" width="8" height="5" fill="#fde68a" rx="2" />
      {/* Stern expression */}
      <line x1="14" y1="19" x2="18" y2="19" stroke="#92400e" strokeWidth="0.7" strokeLinecap="round" />
      {/* Body armor */}
      <rect x="10" y="20" width="12" height="8" fill="#dc2626" rx="2" />
      {/* Shield emblem on chest */}
      <path d="M14 22 L16 21 L18 22 L18 25 Q16 27 14 25 Z" fill="#fbbf24" />
      <path d="M15 23 L16 22.5 L17 23 L17 24.5 Q16 25.5 15 24.5 Z" fill="#dc2626" />
      {/* Sword */}
      <rect x="23" y="14" width="1" height="12" fill="#9ca3af" rx="0.3" />
      <rect x="21.5" y="14" width="4" height="1.5" fill="#d4a017" rx="0.5" />
      <polygon points="23,14 24,14 23.5,11" fill="#d1d5db" />
    </svg>
  );
}

function DocsAvatar({ size, busy }: { size: number; busy: boolean }) {
  // Friendly bookworm with quill
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} className={busy ? "animate-write" : ""}>
      {/* Body — green caterpillar / worm */}
      <ellipse cx="16" cy="22" rx="6" ry="5" fill="#14b8a6" />
      <ellipse cx="16" cy="22" rx="4" ry="3.5" fill="#2dd4bf" />
      {/* Head — big round */}
      <circle cx="16" cy="14" r="7" fill="#14b8a6" />
      <circle cx="16" cy="14" r="5.5" fill="#2dd4bf" />
      {/* Glasses */}
      <circle cx="13.5" cy="13.5" r="2.5" fill="none" stroke="#374151" strokeWidth="0.7" />
      <circle cx="18.5" cy="13.5" r="2.5" fill="none" stroke="#374151" strokeWidth="0.7" />
      <line x1="16" y1="13.5" x2="16" y2="13.5" stroke="#374151" strokeWidth="0.7" />
      {/* Eyes behind glasses */}
      <circle cx="13.5" cy="13.5" r="1" fill="#134e4a" className="animate-blink" />
      <circle cx="18.5" cy="13.5" r="1" fill="#134e4a" className="animate-blink" />
      {/* Smile */}
      <path d="M14 17 Q16 19 18 17" fill="none" stroke="#134e4a" strokeWidth="0.6" />
      {/* Book */}
      <rect x="4" y="20" width="7" height="8" fill="#fef3c7" rx="1" stroke="#d97706" strokeWidth="0.5" />
      <line x1="7.5" y1="20" x2="7.5" y2="28" stroke="#d97706" strokeWidth="0.5" />
      <line x1="5" y1="22" x2="7" y2="22" stroke="#d97706" strokeWidth="0.3" />
      <line x1="5" y1="24" x2="7" y2="24" stroke="#d97706" strokeWidth="0.3" />
      {/* Quill */}
      <path d="M24 10 Q26 8 28 6" fill="none" stroke="#7c3aed" strokeWidth="0.8" />
      <path d="M28 6 L27 5 L29 5 Z" fill="#7c3aed" />
      <line x1="24" y1="10" x2="22" y2="18" stroke="#92400e" strokeWidth="0.5" />
    </svg>
  );
}

function OpsAvatar({ size, busy }: { size: number; busy: boolean }) {
  // Robot / wrench mechanic — gears & tools
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} className={busy ? "animate-bounce-gentle" : ""}>
      {/* Antenna */}
      <line x1="16" y1="6" x2="16" y2="2" stroke="#9ca3af" strokeWidth="1" />
      <circle cx="16" cy="2" r="1.5" fill="#f97316" className={busy ? "animate-glow" : ""} />
      {/* Head */}
      <rect x="10" y="6" width="12" height="10" fill="#9ca3af" rx="3" />
      <rect x="11" y="7" width="10" height="8" fill="#d1d5db" rx="2" />
      {/* Eyes — LED style */}
      <rect x="12.5" y="9" width="2.5" height="2.5" fill="#22c55e" rx="0.5" className="animate-led" />
      <rect x="17" y="9" width="2.5" height="2.5" fill="#22c55e" rx="0.5" className="animate-led" />
      {/* Mouth — grid */}
      <rect x="13" y="13" width="6" height="2" fill="#6b7280" rx="0.5" />
      <line x1="15" y1="13" x2="15" y2="15" stroke="#9ca3af" strokeWidth="0.3" />
      <line x1="17" y1="13" x2="17" y2="15" stroke="#9ca3af" strokeWidth="0.3" />
      {/* Body */}
      <rect x="11" y="17" width="10" height="9" fill="#f97316" rx="2" />
      {/* Gear emblem */}
      <circle cx="16" cy="21.5" r="2.5" fill="none" stroke="#fff" strokeWidth="0.7" className={busy ? "animate-spin-slow" : ""} />
      <circle cx="16" cy="21.5" r="1" fill="#fff" />
      {/* Arms */}
      <rect x="6" y="18" width="5" height="2" fill="#9ca3af" rx="1" />
      <rect x="21" y="18" width="5" height="2" fill="#9ca3af" rx="1" />
      {/* Wrench in hand */}
      <rect x="3" y="17" width="4" height="1.2" fill="#6b7280" rx="0.3" />
      <circle cx="3" cy="17.6" r="1.5" fill="none" stroke="#6b7280" strokeWidth="0.8" />
    </svg>
  );
}

function UIUXAvatar({ size, busy }: { size: number; busy: boolean }) {
  // Creative designer — painter with beret and palette
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} className={busy ? "animate-float" : ""}>
      {/* Beret */}
      <ellipse cx="16" cy="9" rx="8" ry="3.5" fill="#ec4899" />
      <circle cx="16" cy="6" r="2" fill="#ec4899" />
      <circle cx="16" cy="5.5" r="1" fill="#f472b6" />
      {/* Face */}
      <rect x="11" y="11" width="10" height="8" fill="#fde68a" rx="3" />
      {/* Eyes — creative sparkle */}
      <circle cx="14" cy="14.5" r="1" fill="#831843" className="animate-blink" />
      <circle cx="18" cy="14.5" r="1" fill="#831843" className="animate-blink" />
      {/* Sparkle near eye */}
      <polygon points="20,12 20.5,13 21.5,13 20.7,13.6 21,14.5 20,14 19,14.5 19.3,13.6 18.5,13 19.5,13" fill="#fbbf24" className="animate-twinkle" />
      {/* Happy smile */}
      <path d="M14 17 Q16 19 18 17" fill="none" stroke="#92400e" strokeWidth="0.7" />
      {/* Body — colorful smock */}
      <rect x="11" y="19" width="10" height="9" fill="#f472b6" rx="2" />
      {/* Paint splotches on smock */}
      <circle cx="13" cy="22" r="1.2" fill="#3b82f6" />
      <circle cx="17" cy="24" r="1" fill="#22c55e" />
      <circle cx="15" cy="21" r="0.8" fill="#f59e0b" />
      {/* Palette in left hand */}
      <ellipse cx="6" cy="22" rx="4" ry="3" fill="#fef3c7" stroke="#d97706" strokeWidth="0.4" />
      <circle cx="5" cy="21" r="0.8" fill="#ef4444" />
      <circle cx="7" cy="21" r="0.8" fill="#3b82f6" />
      <circle cx="6" cy="23" r="0.8" fill="#22c55e" />
      <circle cx="4.5" cy="23" r="0.8" fill="#f59e0b" />
      {/* Paintbrush in right hand */}
      <line x1="24" y1="14" x2="26" y2="22" stroke="#92400e" strokeWidth="0.8" strokeLinecap="round" />
      <ellipse cx="24" cy="13.5" rx="1" ry="2" fill="#ec4899" className={busy ? "animate-write" : ""} />
      {/* Floating color dots when busy */}
      {busy && (
        <>
          <circle cx="27" cy="10" r="1" fill="#ef4444" className="animate-float" opacity="0.8" />
          <circle cx="25" cy="8" r="0.8" fill="#3b82f6" className="animate-twinkle" opacity="0.8" />
          <circle cx="29" cy="12" r="0.7" fill="#22c55e" className="animate-glow" opacity="0.8" />
        </>
      )}
    </svg>
  );
}

function DefaultAgentAvatar({ size, busy }: { size: number; busy: boolean }) {
  // Generic bot
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} className={busy ? "animate-bounce-gentle" : ""}>
      <rect x="9" y="6" width="14" height="12" fill="#6b7280" rx="4" />
      <circle cx="13" cy="11" r="1.5" fill="#e5e7eb" className="animate-blink" />
      <circle cx="19" cy="11" r="1.5" fill="#e5e7eb" className="animate-blink" />
      <rect x="13" y="15" width="6" height="1.5" fill="#e5e7eb" rx="0.5" />
      <rect x="11" y="19" width="10" height="9" fill="#4b5563" rx="2" />
      <line x1="16" y1="3" x2="16" y2="6" stroke="#9ca3af" strokeWidth="1" />
      <circle cx="16" cy="3" r="1" fill="#60a5fa" />
    </svg>
  );
}

// ── Size map ─────────────────────────────────────────────────────────────────

const sizeMap = { sm: 28, md: 40, lg: 56 };

// ── Avatar dispatch ──────────────────────────────────────────────────────────

const avatarMap: Record<string, (props: { size: number; busy: boolean }) => React.JSX.Element> = {
  orchestrator: OrchestratorAvatar,
  senior_dev: SeniorDevAvatar,
  junior_dev: JuniorDevAvatar,
  architect: ArchitectAvatar,
  qa: QAAvatar,
  security: SecurityAvatar,
  docs: DocsAvatar,
  ops: OpsAvatar,
  ui_ux: UIUXAvatar,
};

export function AgentAvatar({ role, size = "md", busy = false, className }: AgentAvatarProps) {
  const px = sizeMap[size];
  const Renderer = avatarMap[role] ?? DefaultAgentAvatar;

  return (
    <div
      className={`inline-flex items-center justify-center ${className ?? ""}`}
      title={role.replace(/_/g, " ")}
    >
      <Renderer size={px} busy={busy} />
    </div>
  );
}
