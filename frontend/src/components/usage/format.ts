import type { UsageCategory } from "@/lib/types";
import { TYPE } from "@/lib/tokens";

// Figures and colours shared by the desktop Usage page and the phone screen,
// so the two read spend the same way.

// x402 = accent, LLM = info, action = the orange the console log also uses.
export const CAT_COLOR: Record<UsageCategory, string> = {
  x402: "var(--accent)",
  llm: "var(--info)",
  action: "var(--type-action)",
};
// Raw hex, not var(), and that must not be "tidied": UsagePage builds the
// pill's border and fill by concatenating an alpha pair (`${c}55`), which is
// only a colour if `c` is a literal. Sourced from lib/tokens.ts.
export const TYPE_PILL: Record<UsageCategory, string> = {
  x402: TYPE.x402,
  llm: TYPE.llm,
  action: TYPE.action,
};
export const CAT_LABEL: Record<UsageCategory, string> = {
  x402: "x402",
  llm: "LLM",
  action: "Actions",
};

// Spend figures arrive from /usage/* already denominated in USD — they come
// from debit_ledger.amount_usd_micros, the same units credits are sold and
// billed in. The multiplier is retained (as 1) rather than deleted so the call
// sites stay honest about doing no conversion; applying the old 0.17 ALGO rate
// to USD figures would under-report every number on this page by ~6x.
export const ALGO_USD = 1;
export function usd(algoAmount: number, dp = 2) {
  return (algoAmount * ALGO_USD).toLocaleString("en", {
    minimumFractionDigits: dp,
    maximumFractionDigits: dp,
  });
}
// A share of total spend, for display. The table used to render the raw
// payload value: lib/data.ts's fixtures pre-round, so only the real API
// response showed it -- "38.95454763146232%" in a 40px cell. The component
// cannot know which source it has, so it rounds here.
export function pct(value: number, dp = 1) {
  if (!Number.isFinite(value)) return "0";
  return value.toFixed(dp);
}

// How to label a row in the endpoints table.
//
// Only x402 rows have an endpoint. An LLM step is billed against its agent
// node, which has no endpoint/url, so /usage/by-endpoint falls back to the
// node id for BOTH `endpoint` and `host` -- the cell printed the same uuid
// twice. The node's name is the readable answer; the id stays in the title.
export function endpointLabel(row: {
  endpoint: string;
  host: string;
  provider: string;
}): { primary: string; secondary: string } {
  const isUrl = row.host !== row.endpoint;
  if (isUrl) return { primary: row.endpoint, secondary: row.host };
  return { primary: row.provider || row.endpoint, secondary: "" };
}

export function relTime(iso: string) {
  const diff = Date.now() - new Date(iso).getTime();
  const m = Math.floor(diff / 60000);
  if (m < 1) return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}
// Compact USD for the credit balance -- keeps large figures small (100K, 50, 2.3M).
export function compactUsd(algoAmount: number) {
  return Intl.NumberFormat("en", {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(algoAmount * ALGO_USD);
}
// The figure in the middle of a ring has a fixed hole to fit in. Exact below a
// thousand, compact above ($12.3K, $1.2M): the exact amount is on the screen
// beside it, so the ring only has to say how big.
export function centreFigure(amount: number) {
  return amount < 1000 ? `$${usd(amount)}` : `$${compactUsd(amount)}`;
}
