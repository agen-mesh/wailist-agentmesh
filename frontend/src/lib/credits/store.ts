"use client";
import { useSyncExternalStore } from "react";
import type { PaymentMethod } from "@/components/checkout/types";
import type { AutoRecharge, CreditsState, Purchase } from "@/lib/credits/types";
import { creditsForTopup } from "@/lib/credits/fx";
import { credits as creditsApi } from "@/lib/api";

// Credit wallet state, shared across routes via useSyncExternalStore.
//
// balanceUSD is NOT local state: it is fetched from the backend
// (users.credit_balance_usd_micros via GET /credits/balance), the same row the
// engine reserves against and debits on every paid x402 call. It is
// deliberately never read from or written to localStorage — a cached copy
// silently goes stale the instant a run spends money, which previously showed
// a healthy balance for an account the backend considered empty.
//
// purchases/autoRecharge remain browser-local for now (top-up history and
// auto-recharge preferences have no backend yet).

const STORAGE_KEY = "agentmesh_credits_v1";

const DEFAULT_AUTO_RECHARGE: AutoRecharge = {
  enabled: false,
  thresholdUSD: 5,
  amountINR: 1000,
  monthlyCapINR: null,
};

const DEFAULT_STATE: CreditsState = {
  balanceUSD: 0,
  purchases: [],
  autoRecharge: DEFAULT_AUTO_RECHARGE,
};

let state: CreditsState = DEFAULT_STATE;
let loaded = false;
const listeners = new Set<() => void>();

// Read persisted state, tolerating missing/corrupt data (best-effort mock).
function loadState(): CreditsState {
  if (typeof window === "undefined") return DEFAULT_STATE;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULT_STATE;
    const parsed = JSON.parse(raw) as Partial<CreditsState>;
    return {
      // Deliberately not restored from storage — see the module comment.
      balanceUSD: 0,
      purchases: Array.isArray(parsed.purchases) ? parsed.purchases : [],
      autoRecharge: {
        ...DEFAULT_AUTO_RECHARGE,
        ...(parsed.autoRecharge ?? {}),
      },
    };
  } catch {
    return DEFAULT_STATE;
  }
}

// Hydrate lazily on the first client snapshot so the server render (defaults)
// and the initial client render match, then React re-renders with real values.
function ensureLoaded(): void {
  if (loaded || typeof window === "undefined") return;
  state = loadState();
  loaded = true;
}

function persist(): void {
  if (typeof window === "undefined") return;
  try {
    // balanceUSD is intentionally excluded: persisting it would recreate the
    // stale-cache bug the moment a run debits credits in another tab/session.
    const { balanceUSD: _omit, ...persistable } = state;
    void _omit;
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(persistable));
  } catch {
    /* ignore storage quota/availability errors in this mock */
  }
}

function commit(next: CreditsState): void {
  state = next;
  persist();
  listeners.forEach((l) => l());
}

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange);
  return () => {
    listeners.delete(onChange);
  };
}

function getSnapshot(): CreditsState {
  ensureLoaded();
  return state;
}

function getServerSnapshot(): CreditsState {
  return DEFAULT_STATE;
}

function newId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `txn_${Date.now()}_${Math.floor(Math.random() * 1e6)}`;
}

// Record a successful top-up: grants credits (base + bonus) and prepends the
// purchase to history. Returns the created record.
//
// `creditsUSDOverride` lets a caller that already knows the real, authoritative
// credited amount (e.g. a backend-verified payment) supply it directly instead
// of falling through to the local mock-FX estimate. Providers without a real
// backend yet (e.g. the NOWPayments stub) omit it and get the mock amount.
export function addPurchase(input: {
  amountINR: number;
  method: PaymentMethod;
  creditsUSDOverride?: number;
}): Purchase {
  const creditsUSD =
    input.creditsUSDOverride ?? creditsForTopup(input.amountINR);
  const purchase: Purchase = {
    id: newId(),
    createdAt: new Date().toISOString(),
    amountINR: input.amountINR,
    creditsUSD,
    method: input.method,
    status: "paid",
  };
  commit({ ...state, purchases: [purchase, ...state.purchases] });
  // The backend is what actually credited the account; re-read it rather than
  // adding creditsUSD here, so the displayed balance can never disagree with
  // what the engine will let the user spend.
  void refreshBalance();
  return purchase;
}

// balanceKnown distinguishes "the server says $0" from "we have not asked
// yet" — without it a real empty balance and an unloaded one look identical,
// and the UI would flash $0.00 on every page load.
let balanceKnown = false;

// refreshBalance re-reads the authoritative balance. Call it on mount and
// after anything that moves money (a completed run, a verified top-up).
export async function refreshBalance(): Promise<void> {
  try {
    const balanceUSD = await creditsApi.balance();
    balanceKnown = true;
    commit({ ...state, balanceUSD });
  } catch {
    // Leave the last known value in place; a failed poll is not evidence the
    // balance changed, and blanking it would look like funds vanished.
  }
}

export function setAutoRecharge(cfg: AutoRecharge): void {
  commit({ ...state, autoRecharge: cfg });
}

// Mirrors the server-side low balance threshold
// (user_settings.low_balance_usd_micros) into the store the UI already reads.
//
// The threshold used to be a browser-local default that no screen could
// change, duplicated as a second hardcoded constant on the credits page. The
// settings page now owns it and the server stores it; this keeps
// LowBalanceBanner and the canvas indicator — both of which read
// autoRecharge.thresholdUSD — in step without either having to learn about
// /settings.
//
// This is a cache of a server value, not the source of truth: SettingsPage
// calls it after every load and save, and a browser that never opens settings
// simply keeps the default until it does.
export function setLowBalanceThresholdUSD(thresholdUSD: number): void {
  ensureLoaded();
  if (state.autoRecharge.thresholdUSD === thresholdUSD) return;
  commit({
    ...state,
    autoRecharge: { ...state.autoRecharge, thresholdUSD },
  });
}

export interface CreditsSnapshot extends CreditsState {
  hydrated: boolean;
  balanceKnown: boolean;
  refreshBalance: typeof refreshBalance;
  lastPurchase: Purchase | undefined;
  addPurchase: typeof addPurchase;
  setAutoRecharge: typeof setAutoRecharge;
}

export function useCredits(): CreditsSnapshot {
  const snapshot = useSyncExternalStore(
    subscribe,
    getSnapshot,
    getServerSnapshot,
  );
  const hydrated = useSyncExternalStore(
    subscribe,
    () => loaded,
    () => false,
  );
  const known = useSyncExternalStore(
    subscribe,
    () => balanceKnown,
    () => false,
  );
  return {
    ...snapshot,
    hydrated,
    balanceKnown: known,
    refreshBalance,
    lastPurchase: snapshot.purchases[0],
    addPurchase,
    setAutoRecharge,
  };
}
