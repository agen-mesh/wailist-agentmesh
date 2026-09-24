import { describe, expect, it } from "vitest";
import {
  creditsForTopup,
  FALLBACK_USD_PER_INR,
  GST_RATE,
  gstBreakdown,
  maxTopupINR,
  MAX_TOPUP_USD,
} from "./fx";

// The rate the backend reported live on 2026-09-20. Used here so the numbers
// in this file are the ones a real payer would have seen.
const LIVE = 0.010423;

describe("creditsForTopup", () => {
  it("is the plain conversion at the rate it is given", () => {
    expect(creditsForTopup(5000, LIVE)).toBeCloseTo(52.115, 6);
    expect(creditsForTopup(1000, LIVE)).toBeCloseTo(10.423, 6);
    expect(creditsForTopup(0, LIVE)).toBe(0);
  });

  // The regression this file exists for. The old arithmetic was
  // amountINR / 83 * 1.05, which quoted $63.25 for ₹5,000 while the ledger
  // credited about $52.12 -- a figure shown directly above a Pay button.
  it("no longer adds the 5% bonus the backend never paid", () => {
    expect(creditsForTopup(5000, LIVE)).not.toBeCloseTo(63.25, 2);
    // A bonus would make the result non-linear across the old ₹1000 threshold.
    const belowPerRupee = creditsForTopup(999, LIVE) / 999;
    const abovePerRupee = creditsForTopup(1000, LIVE) / 1000;
    expect(abovePerRupee).toBeCloseTo(belowPerRupee, 12);
  });

  it("stays linear at every amount, so there is no threshold anywhere", () => {
    for (const inr of [1, 500, 999, 1000, 1001, 20000, 500000]) {
      expect(creditsForTopup(inr, LIVE)).toBeCloseTo(inr * LIVE, 9);
    }
  });
});

describe("maxTopupINR", () => {
  it("derives the rupee ceiling from the rate it is given", () => {
    expect(maxTopupINR(LIVE)).toBe(Math.floor(MAX_TOPUP_USD / LIVE));
    // Converting the ceiling back must never exceed the USD cap.
    expect(creditsForTopup(maxTopupINR(LIVE), LIVE)).toBeLessThanOrEqual(
      MAX_TOPUP_USD,
    );
  });

  it("falls back only when there is no usable rate", () => {
    expect(maxTopupINR(0)).toBe(
      Math.floor(MAX_TOPUP_USD / FALLBACK_USD_PER_INR),
    );
    expect(maxTopupINR(-1)).toBe(
      Math.floor(MAX_TOPUP_USD / FALLBACK_USD_PER_INR),
    );
  });
});

describe("gstBreakdown", () => {
  it("splits a tax-inclusive total into base and GST", () => {
    const { base, gst } = gstBreakdown(5000);
    expect(base + gst).toBeCloseTo(5000, 9);
    expect(gst).toBeCloseTo(5000 - 5000 / (1 + GST_RATE), 9);
  });

  it("is zero for zero", () => {
    expect(gstBreakdown(0)).toEqual({ base: 0, gst: 0 });
  });
});
