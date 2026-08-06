"use client";
import { useSyncExternalStore } from "react";
import type { PaymentMethod } from "@/components/checkout/types";
import type { AutoRecharge, CreditsState, Purchase } from "@/lib/credits/types";
import { creditsForTopup } from "@/lib/credits/fx";
import { credits as creditsApi, hasBackend } from "@/lib/api";

// Credit wallet shared across routes via useSyncExternalStore.
//
// balanceUSD is NOT local state: it is fetched from the backend
// (users.credit_balance_usd_micros via GET /credits/balance), the same row the
// engine reserves against and debits on every paid x402 call. It is
// deliberately never persisted to localStorage — a cached copy silently goes
// stale the instant a run spends money, which previously showed a healthy
// balance for an account the backend considered empty.
//
// Real mode (a backend is configured): purchase history is DB-backed too, via
// GET /credits/history; only auto-recharge — a client-side preference with no
// backend — stays in localStorage.
//
// Mock mode (no backend): purchases and auto-recharge are localStorage-backed,
// per browser. The balance still comes from the (stubbed) API rather than
// storage, so the "never trust a cached balance" rule holds in both modes.

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
let loaded = false; // ensureLoaded has run
let ready = false; // data is populated — drives `hydrated` for consumers
let fetching = false; // a backend refresh is in flight

// balanceKnown distinguishes "the server says $0" from "we have not asked
// yet" — without it a real empty balance and an unloaded one look identical,
// and the UI would flash $0.00 on every page load.
let balanceKnown = false;

// loadError is the other half of that distinction: it separates "we asked and
// the request failed" from both of the above, so the billing UI can say so
// instead of rendering a confident, wrong $0.
let loadError: string | null = null;

const listeners = new Set<() => void>();

function notify(): void {
  listeners.forEach((l) => l());
}

// Read persisted state, tolerating missing/corrupt data (best-effort).
function loadPersisted(): CreditsState {
  if (typeof window === "undefined") return DEFAULT_STATE;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULT_STATE;
    const parsed = JSON.parse(raw) as Partial<CreditsState>;
    return {
      // Deliberately not restored from storage — see the module comment. persist()
      // already refuses to write it, but storage from an older build can still
      // carry the key, and adopting it would resurrect the stale-cache bug.
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

// Pull the authoritative balance — and, in real mode, the DB-backed purchase
// history — from the backend. `ready` flips true once this settles so consumers
// don't flash a stale/empty wallet.
async function refreshFromBackend(): Promise<void> {
  if (fetching) return;
  fetching = true;
  try {
    if (hasBackend) {
      const [balanceUSD, purchases] = await Promise.all([
        creditsApi.balance(),
        creditsApi.history(),
      ]);
      state = { ...state, balanceUSD, purchases };
    } else {
      // Mock mode: history is browser-local, so only the balance is re-read.
      const balanceUSD = await creditsApi.balance();
      state = { ...state, balanceUSD };
    }
    balanceKnown = true;
    loadError = null;
  } catch (e) {
    // Surface, don't swallow — a failed load must not masquerade as $0. The
    // last known values stay in place: a failed poll is not evidence the
    // balance changed, and blanking it would look like funds vanished.
    loadError = e instanceof Error ? e.message : "failed to load credits";
  } finally {
    fetching = false;
    ready = true;
    notify();
  }
}

// Hydrate lazily on the first client snapshot so the server render (defaults)
// and the initial client render match, then re-render with real values.
function ensureLoaded(): void {
  if (loaded || typeof window === "undefined") return;
  loaded = true;
  if (hasBackend) {
    // Real mode: seed the local preference, fetch the rest from the DB.
    state = { ...DEFAULT_STATE, autoRecharge: loadPersisted().autoRecharge };
  } else {
    // Mock mode: history/preferences are available synchronously, so consumers
    // can render immediately while the balance request is still in flight.
    state = loadPersisted();
    ready = true;
  }
  void refreshFromBackend();
}

function persist(): void {
  if (typeof window === "undefined") return;
  try {
    // balanceUSD is intentionally excluded: persisting it would recreate the
    // stale-cache bug the moment a run debits credits in another tab/session.
    // In real mode history is DB-backed too, leaving auto-recharge as the only
    // genuinely local preference.
    const { balanceUSD: _omit, ...persistable } = state;
    void _omit;
    const toStore = hasBackend
      ? { autoRecharge: state.autoRecharge }
      : persistable;
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(toStore));
  } catch {
    /* ignore storage quota/availability errors */
  }
}

function commit(next: CreditsState): void {
  state = next;
  persist();
  notify();
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

// Record a successful top-up. Returns the created record so the checkout
// success screen can show the credited amount immediately.
//
// `creditsUSDOverride` lets a caller that already knows the real, authoritative
// credited amount (e.g. a backend-verified payment) supply it directly instead
// of falling through to the local mock-FX estimate. Providers without a real
// backend yet (e.g. the NOWPayments stub) omit it and get the mock amount.
//
// Real mode: the DB was already credited server-side by payment verification
// and history is DB-backed, so this re-reads from the backend rather than
// prepending locally. Mock mode: prepends to the browser-local history.
//
// Either way the balance is re-read rather than incremented here, so the
// displayed figure can never disagree with what the engine will let the user
// spend.
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
  if (!hasBackend) {
    commit({ ...state, purchases: [purchase, ...state.purchases] });
  }
  void refreshFromBackend();
  return purchase;
}

// refreshBalance re-reads the authoritative balance (and, in real mode, the
// purchase history alongside it). Call it on mount and after anything that
// moves money (a completed run, a verified top-up).
export async function refreshBalance(): Promise<void> {
  await refreshFromBackend();
}

export function setAutoRecharge(cfg: AutoRecharge): void {
  commit({ ...state, autoRecharge: cfg });
}

// Fire-and-forget variant for callers that just want a re-read (e.g. a retry
// button) without awaiting it.
export function refreshCredits(): void {
  void refreshFromBackend();
}

export interface CreditsSnapshot extends CreditsState {
  hydrated: boolean;
  balanceKnown: boolean;
  loadError: string | null;
  lastPurchase: Purchase | undefined;
  addPurchase: typeof addPurchase;
  setAutoRecharge: typeof setAutoRecharge;
  refreshBalance: typeof refreshBalance;
  refresh: typeof refreshCredits;
}

export function useCredits(): CreditsSnapshot {
  const snapshot = useSyncExternalStore(
    subscribe,
    getSnapshot,
    getServerSnapshot,
  );
  const hydrated = useSyncExternalStore(
    subscribe,
    () => ready,
    () => false,
  );
  const known = useSyncExternalStore(
    subscribe,
    () => balanceKnown,
    () => false,
  );
  const err = useSyncExternalStore(
    subscribe,
    () => loadError,
    () => null,
  );
  return {
    ...snapshot,
    hydrated,
    balanceKnown: known,
    loadError: err,
    lastPurchase: snapshot.purchases[0],
    addPurchase,
    setAutoRecharge,
    refreshBalance,
    refresh: refreshCredits,
  };
}
