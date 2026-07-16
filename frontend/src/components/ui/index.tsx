"use client";
import React, { CSSProperties } from "react";

// ── Logo ──────────────────────────────────────────────────────────────────
export function Logo({ size = 18 }: { size?: number }) {
  return (
    <div style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
      <svg width={size} height={size} viewBox="0 0 24 24" fill="none">
        <circle cx="5"  cy="6"  r="2.4" fill="var(--accent)" />
        <circle cx="19" cy="6"  r="2.4" fill="currentColor" />
        <circle cx="12" cy="18" r="2.4" fill="currentColor" />
        <path d="M5 6 L19 6 M5 6 L12 18 M19 6 L12 18" stroke="currentColor" strokeWidth="1.2" opacity="0.6" />
      </svg>
      <span style={{
        fontFamily: "var(--font-sans)",
        fontWeight: 600,
        fontSize: size * 0.85,
        letterSpacing: "-0.02em",
        color: "var(--fg)",
      }}>
        AgentMesh
      </span>
    </div>
  );
}

// ── Pill ─────────────────────────────────────────────────────────────────
type PillTone = "default" | "accent" | "warm" | "danger" | "ok";
export function Pill({
  children,
  tone = "default",
  mono = false,
  dot = false,
}: {
  children: React.ReactNode;
  tone?: PillTone;
  mono?: boolean;
  dot?: boolean;
}) {
  const tones: Record<PillTone, { bg: string; fg: string; border: string }> = {
    default: { bg: "var(--bg-elev-2)",   fg: "var(--fg-muted)", border: "var(--border)" },
    accent:  { bg: "var(--accent-soft)", fg: "var(--accent)",   border: "var(--accent-line)" },
    warm:    { bg: "var(--warm-soft)",   fg: "var(--warm)",     border: "rgba(255,181,71,0.35)" },
    danger:  { bg: "rgba(255,92,92,0.10)", fg: "var(--danger)", border: "rgba(255,92,92,0.35)" },
    ok:      { bg: "rgba(167,140,250,0.10)", fg: "var(--accent)", border: "var(--accent-line)" },
  };
  const s = tones[tone];
  return (
    <span style={{
      display: "inline-flex", alignItems: "center", gap: 6,
      height: 22, padding: "0 8px",
      borderRadius: 999,
      border: `1px solid ${s.border}`,
      background: s.bg, color: s.fg,
      fontSize: 11, fontWeight: 500,
      fontFamily: mono ? "var(--font-mono)" : "var(--font-sans)",
      letterSpacing: mono ? "0.02em" : "-0.01em",
    }}>
      {dot && <span style={{ width: 6, height: 6, borderRadius: 999, background: s.fg, display: "inline-block" }} />}
      {children}
    </span>
  );
}

// ── Card ─────────────────────────────────────────────────────────────────
// The standard elevated panel (bg-elev-1 / border / r-3 / 16px padding) used
// across the workflows and usage pages. Override via style (e.g. padding: 0)
// and attach handlers as needed — extra props spread onto the div.
export function Card({ style, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      {...rest}
      style={{ background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-3)", padding: 16, ...style }}
    />
  );
}

// ── Tag ──────────────────────────────────────────────────────────────────
export function Tag({ children }: { children: React.ReactNode }) {
  return (
    <span style={{
      display: "inline-flex", alignItems: "baseline", gap: 6,
      fontFamily: "var(--font-mono)", fontSize: 11,
      color: "var(--fg-muted)", letterSpacing: "0.04em",
      textTransform: "uppercase",
    }}>
      <span style={{ width: 4, height: 4, background: "var(--accent)", borderRadius: 999, display: "inline-block", alignSelf: "center" }} />
      {children}
    </span>
  );
}

// ── Hairline ─────────────────────────────────────────────────────────────
export function Hairline({ vertical = false, length = "100%" }: { vertical?: boolean; length?: string | number }) {
  return (
    <div style={{
      background: "var(--border)",
      width: vertical ? 1 : length,
      height: vertical ? length : 1,
      flexShrink: 0,
    }} />
  );
}

// ── StatusDot ────────────────────────────────────────────────────────────
export function StatusDot({ tone = "ok", size = 8 }: { tone?: "ok" | "warn" | "err" | "default"; size?: number }) {
  const c = tone === "ok" ? "var(--accent)" : tone === "warn" ? "var(--warm)" : tone === "err" ? "var(--danger)" : "var(--fg-dim)";
  return (
    <span style={{
      display: "inline-block", width: size, height: size, borderRadius: 999, background: c,
      boxShadow: tone === "ok" ? `0 0 8px ${c}` : "none",
    }} />
  );
}

// ── Icons ─────────────────────────────────────────────────────────────────
export const IconArrow = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none">
    <path d="M3 8 L13 8 M9 4 L13 8 L9 12" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
  </svg>
);

export const IconPlay = ({ size = 12 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 12 12" fill="currentColor">
    <path d="M3 2 L10 6 L3 10 Z" />
  </svg>
);

export const IconStop = ({ size = 10 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 10 10" fill="currentColor">
    <rect x="2" y="2" width="6" height="6" rx="1" />
  </svg>
);

export const IconSearch = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none">
    <circle cx="7" cy="7" r="4.5" stroke="currentColor" strokeWidth="1.4"/>
    <path d="M11 11 L14 14" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"/>
  </svg>
);

export const IconClose = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none">
    <path d="M3 3 L13 13 M13 3 L3 13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"/>
  </svg>
);

export const IconGrid = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.3">
    <rect x="2" y="2" width="5" height="5"/><rect x="9" y="2" width="5" height="5"/>
    <rect x="2" y="9" width="5" height="5"/><rect x="9" y="9" width="5" height="5"/>
  </svg>
);

export const IconWallet = ({ size = 14 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none">
    <rect x="2" y="4" width="12" height="9" rx="1.5" stroke="currentColor" strokeWidth="1.3"/>
    <path d="M2 7 H14" stroke="currentColor" strokeWidth="1.3"/>
    <circle cx="11" cy="10" r="1" fill="currentColor"/>
  </svg>
);

// ── Toast ─────────────────────────────────────────────────────────────────
export function Toast({ message }: { message: string }) {
  return (
    <div style={{
      position: "fixed", bottom: 24, left: "50%", transform: "translateX(-50%)",
      zIndex: 9999,
      background: "var(--bg-elev-3)",
      border: "1px solid var(--accent-line)",
      color: "var(--fg)",
      padding: "10px 16px", borderRadius: "var(--r-2)",
      fontFamily: "var(--font-mono)", fontSize: 12,
      boxShadow: "0 10px 32px rgba(0,0,0,0.5)",
      display: "flex", alignItems: "center", gap: 10,
      animation: "fade-up 0.25s var(--ease)",
    }}>
      <StatusDot tone="ok" /> {message}
    </div>
  );
}
