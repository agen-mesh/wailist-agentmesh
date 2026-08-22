"use client";
import { useCallback, useEffect, useSyncExternalStore } from "react";
import { useAuth } from "@/hooks/useAuth";
import { fx } from "@/lib/api";
import {
  DEFAULT_CURRENCY,
  convert,
  formatBalance,
  formatMoney,
  isDefaultCurrency,
  isSupportedCurrency,
  type Rates,
} from "@/lib/currency/format";

// Rate table shared across every component on the page, fetched at most once.
//
// The whole module is inert while the user is on USD: `ensureRates` is never
// called, so there is no request, no listener churn, and no new failure mode for
// the default experience. That is the §0 invariant in CURRENCY_PLAN.md.

type Status = "idle" | "loading" | "ready" | "failed";
type Snapshot = {
  rates: Rates | null;
  status: Status;
  override: string | null;
  cached: string | null;
};

let rates: Rates | null = null;
let status: Status = "idle";
// Set when the user changes currency in settings. useAuth caches the user it
// fetched at mount and has no refresh, so without this the new currency would
// not appear until a full page reload.
let override: string | null = null;
// Last-known currency, read from localStorage on the first *client* snapshot
// and never during render. React uses getServerSnapshot for the hydration
// render, so the first client paint matches the server and only then
// re-renders with this. Reading storage in render instead makes the server
// emit "$12.50" where the client hydrates "€10.82" — a mismatch on every
// screen that shows money.
let cached: string | null = null;
let cacheLoaded = false;
const listeners = new Set<() => void>();

// One stable object per state change: useSyncExternalStore compares snapshots by
// identity, so returning a fresh literal each call would loop forever.
let snapshot: Snapshot = { rates, status, override, cached };
const SERVER_SNAPSHOT: Snapshot = {
  rates: null,
  status: "idle",
  override: null,
  cached: null,
};

function commit(next: Snapshot): void {
  rates = next.rates;
  status = next.status;
  override = next.override;
  cached = next.cached;
  snapshot = next;
  listeners.forEach((l) => l());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

// Mirrors ensureLoaded in lib/credits/store.ts: hydrate once, on the client,
// outside render. Deliberately does not notify listeners — this runs during
// React's own read, and the new snapshot identity is what triggers the
// post-hydration re-render.
function ensureCachedCurrency(): void {
  if (cacheLoaded || typeof window === "undefined") return;
  cacheLoaded = true;
  const stored = readCachedCurrency();
  if (stored !== null) {
    cached = stored;
    snapshot = { ...snapshot, cached: stored };
  }
}

const getSnapshot = () => {
  ensureCachedCurrency();
  return snapshot;
};
const getServerSnapshot = () => SERVER_SNAPSHOT;

// `allowRetry` re-attempts after a previous failure. Callers pass it only from
// places that fire on a deliberate change (mount, or the user picking a
// currency), never from render — without it a transient FX outage would strand
// the session on USD until a full page reload.
function ensureRates(allowRetry = false): void {
  if (status === "loading" || status === "ready") return;
  if (status === "failed" && !allowRetry) return;
  commit({ ...snapshot, status: "loading" });
  fx.rates()
    .then((table) =>
      commit({ ...snapshot, rates: table.rates, status: "ready" }),
    )
    // Leaves rates null, which makes formatMoney fall back to USD. Showing a
    // figure derived from a rate we could not fetch would be worse than showing
    // the underlying one.
    .catch(() => commit({ ...snapshot, rates: null, status: "failed" }));
}

/**
 * Apply a currency the user just chose, without waiting for a reload.
 *
 * Call only after PATCH /settings has succeeded — this is a reflection of
 * persisted state, not a substitute for persisting it.
 */
export function applyDisplayCurrency(code: string): void {
  if (!isSupportedCurrency(code)) return;
  cacheCurrency(code);
  commit({ ...snapshot, override: code });
  // Retries a previous failure: picking a currency is a deliberate act, and is
  // the natural moment to try the rate table again.
  if (!isDefaultCurrency(code)) ensureRates(true);
}

// Last-known currency, mirrored so a returning non-USD user's first paint is
// already correct instead of flashing USD while /auth/me resolves. Same
// technique lib/credits/store.ts uses for its own state.
const CURRENCY_KEY = "agentmesh_currency_v1";

function readCachedCurrency(): string | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(CURRENCY_KEY);
    return raw && isSupportedCurrency(raw) ? raw : null;
  } catch {
    return null;
  }
}

function cacheCurrency(code: string): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(CURRENCY_KEY, code);
  } catch {
    /* private mode / quota — the authoritative value still comes from the API */
  }
}

export interface CurrencyView {
  /** The active display currency. `USD` means render exactly as before. */
  currency: string;
  /** True while the user is on the default and nothing should change. */
  isDefault: boolean;
  /** Rates could not be fetched; amounts are falling back to USD. */
  ratesFailed: boolean;
  /** Format a USD amount for display. USD input renders byte-identically. */
  format: (usd: number) => string;
  /** Format a credit balance — credits lead in non-USD mode. See §3. */
  formatBalance: (usd: number) => string;
  /**
   * Convert a USD amount to the active currency, or null when that cannot be
   * done honestly (no rate table, or no rate for this code).
   *
   * For call sites that render their own glyph and precision — the usage page
   * keeps bare numbers in a fixed-width mono column with the currency as a
   * separate label, which Intl's currency style cannot reproduce. Returns the
   * input unchanged for USD, so those call sites stay byte-identical.
   */
  convertAmount: (usd: number) => number | null;
}

export function useCurrency(): CurrencyView {
  const { user } = useAuth();
  const snap = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);

  // /auth/me is authoritative; the cached value only covers the gap before it
  // lands. Both fall back to USD, so an unauthenticated or still-loading render
  // is the default experience rather than a guess.
  const currency =
    snap.override ?? user?.displayCurrency ?? snap.cached ?? DEFAULT_CURRENCY;

  useEffect(() => {
    // Cached unconditionally, including USD: switching back to the default has
    // to clear a previously cached non-USD code, or the next first paint would
    // briefly render the currency the user just left.
    cacheCurrency(currency);
    if (isDefaultCurrency(currency)) return;
    // Only fires when the currency actually changes (or on mount), so passing
    // the retry flag here cannot loop on a persistent outage.
    ensureRates(true);
  }, [currency]);

  const format = useCallback(
    (usd: number) => formatMoney(usd, currency, snap.rates),
    [currency, snap.rates],
  );

  const balance = useCallback(
    (usd: number) => formatBalance(usd, currency, snap.rates),
    [currency, snap.rates],
  );

  const convertAmount = useCallback(
    (usd: number) => convert(usd, currency, snap.rates),
    [currency, snap.rates],
  );

  return {
    currency,
    isDefault: isDefaultCurrency(currency),
    ratesFailed: !isDefaultCurrency(currency) && snap.status === "failed",
    format,
    formatBalance: balance,
    convertAmount,
  };
}

/** Test-only: drop the module-level rate cache between cases. */
export function __resetCurrencyStoreForTest(): void {
  cacheLoaded = false;
  commit({ rates: null, status: "idle", override: null, cached: null });
}
