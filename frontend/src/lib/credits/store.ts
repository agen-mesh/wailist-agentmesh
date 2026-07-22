"use client";
import { useSyncExternalStore } from "react";
import type { PaymentMethod } from "@/components/checkout/types";
import type { AutoRecharge, CreditsState, Purchase } from "@/lib/credits/types";
import { creditsForTopup } from "@/lib/credits/fx";

// Frontend-only credit wallet, shared across routes via useSyncExternalStore —
// the idiomatic way to subscribe React to an external (localStorage) source
// without a provider or a setState-in-effect. Mock: per-browser, no backend.

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
      balanceUSD: typeof parsed.balanceUSD === "number" ? parsed.balanceUSD : 0,
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
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
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
export function addPurchase(input: {
  amountINR: number;
  method: PaymentMethod;
}): Purchase {
  const creditsUSD = creditsForTopup(input.amountINR);
  const purchase: Purchase = {
    id: newId(),
    createdAt: new Date().toISOString(),
    amountINR: input.amountINR,
    creditsUSD,
    method: input.method,
    status: "paid",
  };
  commit({
    ...state,
    balanceUSD: state.balanceUSD + creditsUSD,
    purchases: [purchase, ...state.purchases],
  });
  return purchase;
}

export function setAutoRecharge(cfg: AutoRecharge): void {
  commit({ ...state, autoRecharge: cfg });
}

export interface CreditsSnapshot extends CreditsState {
  hydrated: boolean;
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
  return {
    ...snapshot,
    hydrated,
    lastPurchase: snapshot.purchases[0],
    addPurchase,
    setAutoRecharge,
  };
}
