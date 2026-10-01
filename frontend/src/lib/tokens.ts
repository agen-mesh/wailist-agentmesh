// Raw hex mirrors of the status/category custom properties in globals.css.
//
// Everything that can say `var(--success)` should. This exists for the cases
// that cannot: alpha suffixes built by string concatenation (`${c}55`), and
// APIs that paint outside CSS (xterm's canvas, the theme-color meta tag).
//
// tokens.test.ts asserts these still match globals.css, so the pair cannot
// drift silently. Change a colour in both, never one alone.

export const STATUS = {
  success: "#34d399",
  warning: "#ffb547",
  danger: "#ff5c5c",
  pending: "#a5a2b8",
} as const;

export const SURFACE = {
  bg: "#08070c",
  elev1: "#0f0e18",
} as const;

export const TYPE = {
  agent: "#a78bfa",
  x402: "#e879f9",
  x402Fg: "#1a0a1a",
  llm: "#6ea8ff",
  action: "#fb923c",
} as const;
