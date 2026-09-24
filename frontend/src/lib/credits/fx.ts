// Currency and tax arithmetic for the credits wallet.
//
// The rate belongs to the SERVER. `GET /payments/providers` returns
// `usd_per_inr` from a live source, and a Cashfree order pins a freshly
// fetched rate into its ledger row at creation time
// (backend/internal/db/store.go: credit_usd_micros = paise/100 * fxRate * 1e6).
// Nothing after that re-reads it, so whatever this file quotes is what the
// payer compares against their balance afterwards.
//
// That is why creditsForTopup takes the rate as an argument rather than
// reading a constant: a caller that has not fetched the live rate must be
// unable to quote a number by accident.

// Last-resort fallback, used ONLY when the live rate has not arrived.
//
// It is a placeholder and has been wrong in the payer's favour: at 1/83 it
// quoted roughly 15% more credit than the backend granted at the real rate.
// Prefer showing nothing to showing a figure derived from this.
export const FALLBACK_USD_PER_INR = 1 / 83;

// Ceiling on a single top-up, in USD so the limit means the same thing in
// credits wherever the rate moves.
export const MAX_TOPUP_USD = 1000;

// The USD ceiling in whole rupees at a given rate, rounded down so converting
// back can never exceed MAX_TOPUP_USD.
export function maxTopupINR(usdPerINR: number): number {
  const rate = usdPerINR > 0 ? usdPerINR : FALLBACK_USD_PER_INR;
  return Math.floor(MAX_TOPUP_USD / rate);
}

// Credits granted for an INR top-up.
//
// There is no bonus. A "+5% on top-ups over ₹1000" was advertised here and on
// the checkout for a long time and was never paid: no multiplier exists
// anywhere in the backend, and the ledger credits the plain conversion. The
// promise is gone rather than made true, because making it true is a pricing
// decision and not this file's to take.
export function creditsForTopup(amountINR: number, usdPerINR: number): number {
  return amountINR * usdPerINR;
}

// Balance at or below which the UI warns the user to top up.
export const LOW_BALANCE_THRESHOLD_USD = 5;

// GST for Indian payments. Prices are tax-inclusive, so this splits a total
// into its base and tax components for display.
export const GST_RATE = 0.18;

export function gstBreakdown(totalInclusive: number): {
  base: number;
  gst: number;
} {
  const base = totalInclusive / (1 + GST_RATE);
  return { base, gst: totalInclusive - base };
}
