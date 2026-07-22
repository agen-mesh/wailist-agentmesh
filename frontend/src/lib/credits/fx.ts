// Mock currency conversion for the frontend-only credits wallet. There is no
// live FX and no backend: ₹83 ≈ $1 is a fixed placeholder. Swap for a real rate
// (and server-side computation) when the billing backend lands.
export const USD_PER_INR = 1 / 83;

// Illustrative loyalty bonus: +5% credits on top-ups of ₹1000 or more.
export const BONUS_THRESHOLD_INR = 1000;
export const BONUS_RATE = 0.05;

// Base USD credits for an INR top-up (excludes any bonus).
export function inrToCreditsUSD(amountINR: number): number {
  return amountINR * USD_PER_INR;
}

// Bonus rate that applies to a given INR top-up (0 below the threshold).
export function bonusRate(amountINR: number): number {
  return amountINR >= BONUS_THRESHOLD_INR ? BONUS_RATE : 0;
}

// Bonus USD credits granted on top of the base for an INR top-up.
export function bonusUSD(amountINR: number): number {
  return inrToCreditsUSD(amountINR) * bonusRate(amountINR);
}

// Total credits granted (base + bonus) for a given INR top-up.
export function creditsForTopup(amountINR: number): number {
  return inrToCreditsUSD(amountINR) + bonusUSD(amountINR);
}

// Illustrative GST for Indian payments. Prices are treated as tax-inclusive, so
// this splits a total into its base and GST components (display only, mock).
export const GST_RATE = 0.18;

export function gstBreakdown(totalInclusive: number): {
  base: number;
  gst: number;
} {
  const base = totalInclusive / (1 + GST_RATE);
  return { base, gst: totalInclusive - base };
}
