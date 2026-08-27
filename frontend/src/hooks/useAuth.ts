"use client";
import { useCallback, useSyncExternalStore } from "react";
import { auth, AuthUser } from "@/lib/api";

const UI_COOKIE = "agentmesh_ui";
const TTL = 60 * 60 * 24 * 7; // 7 days -- matches backend JWT TTL

function setUICookie() {
  document.cookie = `${UI_COOKIE}=1; Path=/; SameSite=Lax; Max-Age=${TTL}`;
}

function clearUICookie() {
  document.cookie = `${UI_COOKIE}=; Path=/; SameSite=Lax; Max-Age=0`;
}

// One signed-in user, shared by every component that asks for it.
//
// This was per-component useState, which meant each caller held its own copy:
// saving a new display name in settings updated that page's copy while the top
// bar kept rendering the old name until a full reload. It also fired /auth/me
// once per consumer. The module store is the same shape lib/currency/store.ts
// and lib/credits/store.ts already use.

type Snapshot = {
  signedIn: boolean;
  loading: boolean;
  user: AuthUser | null;
};

// One stable object per state change: useSyncExternalStore compares snapshots
// by identity, so returning a fresh literal per call would loop forever.
let snapshot: Snapshot = { signedIn: false, loading: true, user: null };
// Identical to the initial client value, so the hydration render matches the
// HTML the server sent.
const SERVER_SNAPSHOT: Snapshot = {
  signedIn: false,
  loading: true,
  user: null,
};

const listeners = new Set<() => void>();
let started = false;

function commit(next: Snapshot): void {
  snapshot = next;
  listeners.forEach((l) => l());
}

async function load(): Promise<void> {
  try {
    const u = await auth.me();
    setUICookie();
    commit({ signedIn: true, loading: false, user: u });
  } catch {
    clearUICookie();
    commit({ signedIn: false, loading: false, user: null });
  }
}

// Fetched from subscribe rather than during render: subscribe runs in an
// effect, after hydration, so the first client paint still matches the server.
function ensureLoaded(): void {
  if (started || typeof window === "undefined") return;
  started = true;
  void load();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  ensureLoaded();
  return () => {
    listeners.delete(listener);
  };
}

const getSnapshot = () => snapshot;
const getServerSnapshot = () => SERVER_SNAPSHOT;

export function useAuth() {
  const snap = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);

  const signIn = useCallback(async (email: string, password: string) => {
    await auth.signIn(email, password);
    setUICookie();
    commit({ ...snapshot, signedIn: true });
    // Re-read the account rather than waiting for a reload. With per-component
    // state the next page's own fetch used to cover this; a shared store loads
    // once, so signing in has to ask for the new identity explicitly.
    await load();
  }, []);

  const signUp = useCallback(
    async (email: string, password: string, name: string, org: string) => {
      await auth.signUp(email, password, name, org);
      setUICookie();
      commit({ ...snapshot, signedIn: true });
      await load();
    },
    [],
  );

  const signOut = useCallback(async () => {
    await auth.signOut();
    clearUICookie();
    commit({ signedIn: false, loading: false, user: null });
  }, []);

  // Completes the post-OAuth onboarding prompt (or a later profile edit) --
  // updates the backend then reflects it locally so callers don't need a full
  // re-fetch just to clear needsOnboarding. Because the store is shared, the
  // top bar picks the new name up in the same render.
  const completeOnboarding = useCallback(async (name: string, org: string) => {
    const updated = await auth.updateProfile(name, org);
    // Merged, not replaced. The response is authoritative for what it carries,
    // but a frontend can be deployed against an older backend whose PATCH reply
    // omits fields /auth/me returns -- and because those fields are optional on
    // AuthUser, dropping one type-checks silently and quietly resets the app's
    // display currency to USD.
    commit({ ...snapshot, user: { ...snapshot.user, ...updated } });
  }, []);

  return {
    signedIn: snap.signedIn,
    loading: snap.loading,
    user: snap.user,
    signIn,
    signUp,
    signOut,
    completeOnboarding,
  };
}

/** Test-only: drop the shared auth state between cases. */
export function __resetAuthStoreForTest(): void {
  started = false;
  commit({ signedIn: false, loading: true, user: null });
}
