import { beforeEach, describe, expect, it, vi } from "vitest";

// `store.ts` keeps its wallet state in a module-level variable (mirroring a
// real singleton store), so each test needs a fresh module instance --
// otherwise state written by one test would leak into the next. Reset both
// the module registry and localStorage before every test, then re-import.
const STORAGE_KEY = "agentmesh_credits_v1";

const purchasesMock = vi.fn();
const balanceMock = vi.fn();

vi.mock("@/lib/api", () => ({
  credits: {
    purchases: (...args: unknown[]) => purchasesMock(...args),
    balance: (...args: unknown[]) => balanceMock(...args),
  },
}));

beforeEach(() => {
  window.localStorage.clear();
  vi.resetModules();
  purchasesMock.mockReset();
  balanceMock.mockReset();
  balanceMock.mockResolvedValue(0);
});

async function freshStore() {
  return await import("./store");
}

// One credit_ledger row as GET /credits/purchases returns it.
function ledgerRow(over: Record<string, unknown> = {}) {
  return {
    id: "row-1",
    provider: "cashfree",
    providerOrderId: "order-1",
    status: "completed",
    amountInrPaise: 50_000, // ₹500.00
    fxRateUsdPerInr: 0.012,
    creditUsdMicros: 6_000_000, // $6.00
    createdAt: "2026-09-01T10:00:00Z",
    ...over,
  };
}

describe("refreshPurchases", () => {
  it("maps stored units to display units", async () => {
    purchasesMock.mockResolvedValue([ledgerRow()]);
    const { refreshPurchases, readCredits } = await freshStore();
    await refreshPurchases();

    const { purchases } = readCredits();
    expect(purchases).toHaveLength(1);
    expect(purchases[0].amountINR).toBe(500);
    expect(purchases[0].creditsUSD).toBe(6);
    expect(purchases[0].status).toBe("completed");
  });

  // The whole point of the change: history is server-owned, so nothing about
  // it may survive in a browser that a different account later signs in to.
  // The whole point of the change: this store writes nothing to the browser,
  // so nothing about one account's money can survive for the next one.
  it("writes nothing to localStorage", async () => {
    purchasesMock.mockResolvedValue([ledgerRow(), ledgerRow({ id: "row-2" })]);
    const { refreshPurchases, refreshBalance } = await freshStore();
    await refreshPurchases();
    await refreshBalance();

    expect(window.localStorage.getItem(STORAGE_KEY)).toBeNull();
    expect(window.localStorage.length).toBe(0);
  });

  // A blob written by the previous localStorage-backed build can still be
  // sitting in a returning user's browser. Reading any of it back would show
  // one account's receipts (and balance) to whoever signs in next on that
  // machine, so the store must ignore the key entirely.
  it("ignores state left behind by the older localStorage build", async () => {
    window.localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        balanceUSD: 42,
        purchases: [{ id: "stale", amountINR: 999, creditsUSD: 12 }],
        autoRecharge: { enabled: true, thresholdUSD: 2 },
      }),
    );

    const snap = (await freshStore()).readCredits();
    expect(snap.purchases).toEqual([]);
    expect(snap.balanceUSD).toBe(0);
  });

  it("keeps the last known list when the fetch fails", async () => {
    purchasesMock.mockResolvedValueOnce([ledgerRow()]);
    const { refreshPurchases, readCredits } = await freshStore();
    await refreshPurchases();
    expect(readCredits().purchases).toHaveLength(1);

    purchasesMock.mockRejectedValueOnce(new Error("offline"));
    await refreshPurchases();
    expect(readCredits().purchases).toHaveLength(1);
  });

  it("maps a crypto row to USD with no INR amount", async () => {
    purchasesMock.mockResolvedValue([
      ledgerRow({
        provider: "nowpayments",
        amountInrPaise: undefined,
        fxRateUsdPerInr: undefined,
        amountUsdCents: 1_500, // $15.00
      }),
    ]);
    const { refreshPurchases, readCredits } = await freshStore();
    await refreshPurchases();

    const p = readCredits().purchases[0];
    expect(p.amountINR).toBeUndefined();
    expect(p.amountUSD).toBe(15);
    expect(p.method).toBe("nowpayments");
  });

  // A gateway added backend-first must not render as "undefined" in the label
  // maps the UI keys by provider.
  it("falls back to a known method for an unrecognised provider", async () => {
    purchasesMock.mockResolvedValue([ledgerRow({ provider: "some-new-psp" })]);
    const { refreshPurchases, readCredits } = await freshStore();
    await refreshPurchases();
    expect(readCredits().purchases[0].method).toBe("cashfree");
  });

  it("preserves a non-completed status rather than assuming paid", async () => {
    purchasesMock.mockResolvedValue([ledgerRow({ status: "failed" })]);
    const { refreshPurchases, readCredits } = await freshStore();
    await refreshPurchases();
    expect(readCredits().purchases[0].status).toBe("failed");
  });
});

describe("legacy localStorage migration", () => {
  // The old build's keys are dead but not harmless: they hold one account's
  // purchase history and settlement rows in a browser someone else may sign
  // in to. Reading is already prevented; this clears them out for good.
  it("clears the old credits and settlements keys on first read", async () => {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ balanceUSD: 9 }));
    window.localStorage.setItem("agentmesh_settlements_u1", "[]");
    window.localStorage.setItem("agentmesh_settlements_u2", "[]");
    window.localStorage.setItem("unrelated_key", "keep me");

    (await freshStore()).readCredits();

    expect(window.localStorage.getItem(STORAGE_KEY)).toBeNull();
    expect(window.localStorage.getItem("agentmesh_settlements_u1")).toBeNull();
    expect(window.localStorage.getItem("agentmesh_settlements_u2")).toBeNull();
    // Only this store's own keys, never someone else's.
    expect(window.localStorage.getItem("unrelated_key")).toBe("keep me");
  });
});

describe("lastPurchase", () => {
  // Regression: rows now carry every credit_ledger status, so the newest row
  // is not necessarily a payment that landed. Offering "Repeat last top-up"
  // for a failed attempt labels it with money the user never spent.
  it("skips a failed row and picks the last completed one", async () => {
    purchasesMock.mockResolvedValue([
      ledgerRow({ id: "newest-failed", status: "failed", amountInrPaise: 200000 }),
      ledgerRow({ id: "older-ok", status: "completed", amountInrPaise: 50000 }),
    ]);
    const { refreshPurchases, readCredits } = await freshStore();
    await refreshPurchases();

    const rows = readCredits().purchases;
    const last = rows.find(
      (p) => p.status === "completed" && p.amountINR !== undefined,
    );
    expect(last?.id).toBe("older-ok");
    expect(last?.amountINR).toBe(500);
  });

  it("skips a completed crypto row, which has no INR amount to repeat", async () => {
    purchasesMock.mockResolvedValue([
      ledgerRow({
        id: "crypto",
        provider: "nowpayments",
        amountInrPaise: undefined,
        amountUsdCents: 2000,
      }),
      ledgerRow({ id: "inr", amountInrPaise: 70000 }),
    ]);
    const { refreshPurchases, readCredits } = await freshStore();
    await refreshPurchases();

    const last = readCredits().purchases.find(
      (p) => p.status === "completed" && p.amountINR !== undefined,
    );
    expect(last?.id).toBe("inr");
  });
});
