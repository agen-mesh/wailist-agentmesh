import type { UsageCategory } from "@/lib/types";

// Figures and colours shared by the desktop Usage page and the phone screen,
// so the two read spend the same way.

// x402 = accent, LLM = info, action = the orange already used in LogDrawer.
export const CAT_COLOR: Record<UsageCategory, string> = {
  x402: "var(--accent)",
  llm: "var(--info)",
  action: "#FB923C",
};
// Endpoint type pill keeps the x402 magenta used elsewhere (tx links / tool402).
export const TYPE_PILL: Record<UsageCategory, string> = {
  x402: "#E879F9",
  llm: "#6EA8FF", // hex (matches --info) so the `${c}55`/`${c}1A` alpha suffixes stay valid
  action: "#FB923C",
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
