import { beforeEach, describe, expect, it, vi } from "vitest";
import { creditsForTopup } from "./fx";

// `store.ts` keeps its wallet state in a module-level variable (mirroring a
// real singleton store), so each test needs a fresh module instance --
// otherwise state written by one test would leak into the next. Reset both
// the module registry and localStorage before every test, then re-import.
const STORAGE_KEY = "agentmesh_credits_v1";

beforeEach(() => {
  window.localStorage.clear();
  vi.resetModules();
});

async function freshStore() {
  return await import("./store");
}

describe("addPurchase", () => {
  it("credits the mock-FX amount and prepends to purchase history", async () => {
    const { addPurchase } = await freshStore();

    const first = addPurchase({ amountINR: 500, method: "cashfree" });
    expect(first.creditsUSD).toBeCloseTo(creditsForTopup(500), 10);
    expect(first.status).toBe("paid");

    const second = addPurchase({ amountINR: 2000, method: "cashfree" });

    // Newest first.
    const raw = window.localStorage.getItem(STORAGE_KEY);
    expect(raw).not.toBeNull();
    const persisted = JSON.parse(raw!);
    expect(persisted.purchases.map((p: { id: string }) => p.id)).toEqual([
      second.id,
      first.id,
    ]);
  });

  // balanceUSD is no longer locally accumulated by addPurchase, and is never
  // persisted to localStorage at all (see store.ts's module comment) --
  // it is fetched from the backend by refreshBalance, the only source of
  // truth for what a run can actually spend. A cached local balance
  // previously went stale the instant a run spent credits in another tab.
  it("never persists balanceUSD to localStorage", async () => {
    const { addPurchase } = await freshStore();
    addPurchase({ amountINR: 500, method: "cashfree" });
    addPurchase({ amountINR: 1000, method: "cashfree" });

    const raw = JSON.parse(window.localStorage.getItem(STORAGE_KEY)!);
    expect(raw.balanceUSD).toBeUndefined();
  });

  it("persists to localStorage under the expected key", async () => {
    const { addPurchase } = await freshStore();
    addPurchase({ amountINR: 100, method: "nowpayments" });
    expect(window.localStorage.getItem(STORAGE_KEY)).not.toBeNull();
  });

  // Regression: a real, backend-verified payment must record the real credited
  // amount, not the local mock-FX estimate derived from amountINR.
  it("uses creditsUSDOverride instead of the mock-FX amount when provided", async () => {
    const { addPurchase } = await freshStore();

    const amountINR = 1000;
    const mockEstimate = creditsForTopup(amountINR);
    const realBackendCredited = mockEstimate + 3.7; // deliberately different

    const purchase = addPurchase({
      amountINR,
      method: "cashfree",
      creditsUSDOverride: realBackendCredited,
    });

    expect(purchase.creditsUSD).toBe(realBackendCredited);
    expect(purchase.creditsUSD).not.toBeCloseTo(mockEstimate, 5);
  });

  it("falls back to the mock-FX amount when no override is given", async () => {
    const { addPurchase } = await freshStore();
    const purchase = addPurchase({ amountINR: 1500, method: "nowpayments" });
    expect(purchase.creditsUSD).toBeCloseTo(creditsForTopup(1500), 10);
  });
});

describe("setAutoRecharge", () => {
  it("replaces the autoRecharge config", async () => {
    const { setAutoRecharge } = await freshStore();
    setAutoRecharge({
      enabled: true,
      thresholdUSD: 2,
      amountINR: 500,
      monthlyCapINR: 5000,
    });

    const raw = JSON.parse(window.localStorage.getItem(STORAGE_KEY)!);
    expect(raw.autoRecharge).toEqual({
      enabled: true,
      thresholdUSD: 2,
      amountINR: 500,
      monthlyCapINR: 5000,
    });
  });
});
