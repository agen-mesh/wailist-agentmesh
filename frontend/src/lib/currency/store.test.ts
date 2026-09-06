import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Only fx.rates matters here; useAuth is stubbed because the store imports it
// for the hook path, which these tests do not exercise.
const ratesMock = vi.fn();
vi.mock("@/lib/api", () => ({ fx: { rates: () => ratesMock() } }));
vi.mock("@/hooks/useAuth", () => ({ useAuth: () => ({ user: null }) }));

const { applyDisplayCurrency, __resetCurrencyStoreForTest } =
  await import("@/lib/currency/store");

const flush = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  ratesMock.mockReset();
  __resetCurrencyStoreForTest();
  localStorage.clear();
});

afterEach(() => {
  __resetCurrencyStoreForTest();
});

describe("rate fetching", () => {
  it("never fetches for the default currency", async () => {
    // The §0 invariant at the network layer: a USD user must gain no request.
    applyDisplayCurrency("USD");
    await flush();
    expect(ratesMock).not.toHaveBeenCalled();
  });

  it("fetches once when a non-default currency is chosen", async () => {
    ratesMock.mockResolvedValue({ base: "USD", rates: { EUR: 0.86 } });
    applyDisplayCurrency("EUR");
    await flush();
    expect(ratesMock).toHaveBeenCalledTimes(1);
  });

  it("does not refetch once rates are loaded", async () => {
    ratesMock.mockResolvedValue({
      base: "USD",
      rates: { EUR: 0.86, JPY: 157 },
    });
    applyDisplayCurrency("EUR");
    await flush();
    applyDisplayCurrency("JPY");
    await flush();
    expect(ratesMock).toHaveBeenCalledTimes(1);
  });

  // Without a retry path, one transient outage strands the whole session on
  // USD until a full page reload — the user could switch currency repeatedly
  // and nothing would ever happen.
  it("retries after a failure when the user picks a currency again", async () => {
    ratesMock.mockRejectedValueOnce(new Error("fx down"));
    applyDisplayCurrency("EUR");
    await flush();
    expect(ratesMock).toHaveBeenCalledTimes(1);

    ratesMock.mockResolvedValueOnce({ base: "USD", rates: { JPY: 157 } });
    applyDisplayCurrency("JPY");
    await flush();
    expect(ratesMock).toHaveBeenCalledTimes(2);
  });

  it("ignores a currency outside the shortlist", async () => {
    applyDisplayCurrency("XYZ");
    await flush();
    expect(ratesMock).not.toHaveBeenCalled();
    expect(localStorage.getItem("agentmesh_currency_v1")).toBeNull();
  });

  it("mirrors the chosen currency for the next first paint", async () => {
    ratesMock.mockResolvedValue({ base: "USD", rates: { EUR: 0.86 } });
    applyDisplayCurrency("EUR");
    await flush();
    expect(localStorage.getItem("agentmesh_currency_v1")).toBe("EUR");

    // Switching back must overwrite it, or a returning user briefly sees the
    // currency they just left.
    applyDisplayCurrency("USD");
    await flush();
    expect(localStorage.getItem("agentmesh_currency_v1")).toBe("USD");
  });
});
