// Money formatting for the opt-in display currency.
//
// The governing rule of this whole feature lives in formatMoney below: while the
// user is on USD, output must be byte-identical to what the app rendered before
// display currency existed. Not "equivalent" — identical. Every call site this
// replaces used `$${n.toFixed(2)}`, so the USD branch is literally that, and
// deliberately does NOT go through Intl.NumberFormat, which would introduce
// thousands separators ($1,234.57 where the app has always shown $1234.57).

export const DEFAULT_CURRENCY = "USD";

/** The curated shortlist. Must stay in step with models.SupportedCurrencies and
 *  the user_settings.display_currency CHECK constraint. */
export const SUPPORTED_CURRENCIES = [
  "USD",
  "INR",
  "EUR",
  "GBP",
  "JPY",
  "AUD",
  "CAD",
  "SGD",
  "AED",
  "CHF",
] as const;

export type Currency = (typeof SUPPORTED_CURRENCIES)[number];

/** USD-based rates, as served by GET /fx/rates. */
export type Rates = Record<string, number>;

export const CURRENCY_LABELS: Record<Currency, string> = {
  USD: "US dollar",
  INR: "Indian rupee",
  EUR: "Euro",
  GBP: "Pound sterling",
  JPY: "Japanese yen",
  AUD: "Australian dollar",
  CAD: "Canadian dollar",
  SGD: "Singapore dollar",
  AED: "UAE dirham",
  CHF: "Swiss franc",
};

// Prefixes for call sites that render their own glyph next to a bare number
// (the usage page does this so figures stay in a fixed-width mono column).
// AED and CHF have no widely-recognised single glyph, so they use their code —
// "AED 1.50" is the conventional rendering, not a fallback.
export const CURRENCY_SYMBOLS: Record<Currency, string> = {
  USD: "$",
  INR: "₹",
  EUR: "€",
  GBP: "£",
  JPY: "¥",
  AUD: "A$",
  CAD: "C$",
  SGD: "S$",
  AED: "AED ",
  CHF: "CHF ",
};

/**
 * How many decimal places a currency conventionally shows: 2 for USD/EUR,
 * 0 for JPY, 3 for the Gulf dinars.
 *
 * Call sites that format bare numbers themselves need this, or they render
 * ¥12,355.69 for a currency with no subunit while the Intl-formatted figures
 * elsewhere on the same page correctly show ¥12,356.
 */
export function currencyFractionDigits(currency: string): number {
  try {
    return (
      new Intl.NumberFormat("en", {
        style: "currency",
        currency,
      }).resolvedOptions().maximumFractionDigits ?? 2
    );
  } catch {
    return 2;
  }
}

export function isSupportedCurrency(code: string): code is Currency {
  return (SUPPORTED_CURRENCIES as readonly string[]).includes(code);
}

export const isDefaultCurrency = (code: string): boolean =>
  code === DEFAULT_CURRENCY;

/** Exactly what every call site produced before this feature. */
const formatUSD = (usd: number): string => `$${usd.toFixed(2)}`;

/**
 * Convert a USD amount into `currency`. Returns null when the conversion cannot
 * be made honestly — no rate table yet, or no rate for that code — so callers
 * fall back to USD rather than rendering a number from an unknown rate.
 */
export function convert(
  usd: number,
  currency: string,
  rates: Rates | null,
): number | null {
  if (isDefaultCurrency(currency)) return usd;
  if (!rates) return null;
  const rate = rates[currency];
  if (typeof rate !== "number" || !Number.isFinite(rate) || rate <= 0)
    return null;
  return usd * rate;
}

/**
 * Render a USD amount in the user's display currency.
 *
 * USD short-circuits before touching `rates` at all, which is what lets the
 * default path work with no rate table and no network request.
 *
 * Non-USD goes through Intl.NumberFormat so minor units are correct per
 * currency — JPY has none, and hardcoding two decimals would render ¥1,234.00.
 */
export function formatMoney(
  usd: number,
  currency: string = DEFAULT_CURRENCY,
  rates: Rates | null = null,
): string {
  if (isDefaultCurrency(currency)) return formatUSD(usd);

  // Checked against our own allowlist rather than trusting Intl to reject a bad
  // code. Intl.NumberFormat does NOT throw on a well-formed-but-unknown ISO
  // code — `new Intl.NumberFormat("en",{style:"currency",currency:"XYZ"})`
  // happily renders "XYZ 25.00". Only codes whose formatting we have actually
  // verified are allowed through; anything else falls back to USD.
  if (!isSupportedCurrency(currency)) return formatUSD(usd);

  const converted = convert(usd, currency, rates);
  if (converted === null) return formatUSD(usd);

  try {
    return new Intl.NumberFormat("en", {
      style: "currency",
      currency,
    }).format(converted);
  } catch {
    // Belt and braces for a malformed code that slipped past the allowlist.
    return formatUSD(usd);
  }
}

/**
 * Credits rendered as credits. Used only in non-USD mode, where the fiat figure
 * becomes a labelled estimate beside this — see CURRENCY_PLAN.md §3 on why the
 * balance is not presented as a fiat amount.
 */
export const formatCredits = (usd: number): string =>
  `${usd.toFixed(2)} credits`;

/**
 * A credit balance, for the inline/compact places that need one string.
 *
 * USD is unchanged — `$12.50`, exactly as before. In another currency the
 * credits lead and the fiat figure trails as an estimate, because the terms say
 * credits are not currency and the refund policy says they have no monetary
 * value (CURRENCY_PLAN.md §3). If the rate is unavailable this degrades all the
 * way back to plain USD rather than showing "12.50 credits (≈ $12.50)", which
 * would be noise.
 */
export function formatBalance(
  usd: number,
  currency: string = DEFAULT_CURRENCY,
  rates: Rates | null = null,
): string {
  if (isDefaultCurrency(currency)) return formatUSD(usd);
  if (convert(usd, currency, rates) === null) return formatUSD(usd);
  return `${formatCredits(usd)} (≈ ${formatMoney(usd, currency, rates)})`;
}
