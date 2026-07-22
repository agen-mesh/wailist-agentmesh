// Mock currency conversion for the frontend-only credits wallet. There is no
// live FX and no backend: ₹83 ≈ $1 is a fixed placeholder. Swap for a real rate
// (and server-side computation) when the billing backend lands.
export const USD_PER_INR = 1 / 83;

// Base USD credits for an INR top-up (excludes any bonus).
export function inrToCreditsUSD(amountINR: number): number {
  return amountINR * USD_PER_INR;
}

// Illustrative loyalty bonus: +5% credits on top-ups of ₹1000 or more.
export function bonusUSD(amountINR: number): number {
  return amountINR >= 1000 ? inrToCreditsUSD(amountINR) * 0.05 : 0;
}

// Total credits granted (base + bonus) for a given INR top-up.
export function creditsForTopup(amountINR: number): number {
  return inrToCreditsUSD(amountINR) + bonusUSD(amountINR);
}
