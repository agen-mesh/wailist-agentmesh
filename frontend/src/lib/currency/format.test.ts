import { describe, expect, it } from "vitest";
import {
  CURRENCY_LABELS,
  DEFAULT_CURRENCY,
  SUPPORTED_CURRENCIES,
  convert,
  currencyFractionDigits,
  formatCredits,
  formatMoney,
  isSupportedCurrency,
} from "@/lib/currency/format";

const RATES = { USD: 1, EUR: 0.8655, INR: 95.25, JPY: 157.88 };

describe("the USD no-op invariant", () => {
  // The whole feature is opt-in, and this is what makes that true. Every call
  // site being replaced rendered `$${n.toFixed(2)}`; if this drifts, users who
  // never touched the setting see their UI change.
  const AMOUNTS = [
    0, 0.004, 0.005, 0.01, 1, 1.005, 5, 12.5, 99.999, 1234.5678, 1_000_000,
    -3.5,
  ];

  it.each(AMOUNTS)("renders %d byte-identically to the old code", (usd) => {
    expect(formatMoney(usd, "USD", RATES)).toBe(`$${usd.toFixed(2)}`);
  });

  it("does not group thousands, matching the previous output", () => {
    // Intl.NumberFormat would produce "$1,234.57" here. The app has always
    // shown "$1234.57", so the USD branch must not go through Intl.
    expect(formatMoney(1234.5678, "USD", RATES)).toBe("$1234.57");
    expect(formatMoney(1234.5678, "USD", RATES)).not.toContain(",");
  });

  it("ignores the rate table entirely when on USD", () => {
    // A poisoned table must not change the default rendering — proof that USD
    // short-circuits before any lookup.
    expect(formatMoney(10, "USD", { USD: 999 })).toBe("$10.00");
    expect(formatMoney(10, "USD", null)).toBe("$10.00");
  });

  it("is the default when no currency is supplied", () => {
    expect(formatMoney(10)).toBe("$10.00");
    expect(DEFAULT_CURRENCY).toBe("USD");
  });
});

describe("conversion", () => {
  it("converts using the USD-based rate", () => {
    expect(convert(10, "EUR", RATES)).toBeCloseTo(8.655, 6);
  });

  it("returns null rather than guessing when a rate is unusable", () => {
    expect(convert(10, "EUR", null)).toBeNull();
    expect(convert(10, "GBP", RATES)).toBeNull(); // absent from the table
    expect(convert(10, "EUR", { EUR: 0 })).toBeNull();
    expect(convert(10, "EUR", { EUR: Number.NaN })).toBeNull();
    expect(convert(10, "EUR", { EUR: Number.POSITIVE_INFINITY })).toBeNull();
  });

  it("treats USD as an identity so no rate is needed", () => {
    expect(convert(10, "USD", null)).toBe(10);
  });
});

describe("non-USD formatting", () => {
  it("renders a currency symbol and converted amount", () => {
    const out = formatMoney(10, "EUR", RATES);
    expect(out).toContain("8.6");
    expect(out).not.toBe("$10.00");
  });

  it("gives JPY no minor units", () => {
    // 10 USD * 157.88 = 1578.8 yen. Yen has no subunit, so two decimals would
    // be wrong — this is why formatting goes through Intl rather than toFixed.
    const out = formatMoney(10, "JPY", RATES);
    expect(out).not.toMatch(/\.\d/);
    expect(out).toContain("1,579");
  });

  it("falls back to USD when the rate table is missing", () => {
    // An FX outage must show the real underlying figure, never a number derived
    // from a rate we could not fetch.
    expect(formatMoney(12.5, "EUR", null)).toBe("$12.50");
  });

  it("falls back to USD for a currency absent from the table", () => {
    expect(formatMoney(12.5, "GBP", RATES)).toBe("$12.50");
  });

  it("falls back for a code outside the allowlist, even with a rate", () => {
    // Intl does NOT reject a well-formed unknown code — it would render
    // "XYZ 25.00". The allowlist is what stops an unverified currency
    // reaching a user, so this must not depend on Intl throwing.
    expect(formatMoney(12.5, "XYZ", { XYZ: 2 })).toBe("$12.50");
    expect(formatMoney(12.5, "ZWL", { ZWL: 322 })).toBe("$12.50");
  });
});

describe("the supported shortlist", () => {
  it("includes the default and rejects anything else", () => {
    expect(isSupportedCurrency(DEFAULT_CURRENCY)).toBe(true);
    expect(isSupportedCurrency("XYZ")).toBe(false);
    expect(isSupportedCurrency("eur")).toBe(false); // codes are upper-case
  });

  it("labels every supported currency", () => {
    // A missing label would render an empty option in the settings selector.
    for (const code of SUPPORTED_CURRENCIES) {
      expect(CURRENCY_LABELS[code]).toBeTruthy();
    }
  });

  it("formats every supported currency without throwing", () => {
    const table = Object.fromEntries(SUPPORTED_CURRENCIES.map((c) => [c, 2]));
    for (const code of SUPPORTED_CURRENCIES) {
      expect(() => formatMoney(9.99, code, table)).not.toThrow();
      expect(formatMoney(9.99, code, table)).toBeTruthy();
    }
  });
});

describe("currencyFractionDigits", () => {
  // The usage page formats bare numbers in a mono column and adds its own
  // glyph, so it needs the precision separately. Getting this wrong renders
  // ¥12,355.69 next to an Intl-formatted ¥12,356 on the same screen.
  it("matches each currency's minor units", () => {
    expect(currencyFractionDigits("USD")).toBe(2);
    expect(currencyFractionDigits("EUR")).toBe(2);
    expect(currencyFractionDigits("JPY")).toBe(0);
  });

  it("falls back to 2 for an unknown code rather than throwing", () => {
    expect(currencyFractionDigits("")).toBe(2);
    expect(currencyFractionDigits("nonsense")).toBe(2);
  });

  it("agrees with what formatMoney actually renders", () => {
    // The two paths must not disagree — that inconsistency is the whole reason
    // this helper exists.
    for (const code of SUPPORTED_CURRENCIES) {
      const rendered = formatMoney(1000, code, { [code]: 1 });
      const decimals = rendered.split(".")[1]?.replace(/\D/g, "").length ?? 0;
      expect(decimals).toBe(currencyFractionDigits(code));
    }
  });
});

describe("credits", () => {
  it("renders credits as a count, not a currency", () => {
    // Per CURRENCY_PLAN.md §3: the terms say credits are not currency, so in
    // non-USD mode they are shown as credits with fiat only as an estimate.
    expect(formatCredits(12.5)).toBe("12.50 credits");
    expect(formatCredits(12.5)).not.toContain("$");
  });
});
