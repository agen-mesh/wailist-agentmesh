"use client";
import { useCallback, useSyncExternalStore } from "react";
import { auth, AuthUser } from "@/lib/api";
import { IS_NATIVE, setAuthToken, authReady } from "@/lib/nativeAuth";
import { resetCredits, setLowBalanceThresholdUSD } from "@/lib/credits/store";
import { microsToUSDNumber } from "@/components/settings/ui";

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
    // On native, wait for NativeBoot to finish restoring (or fail to restore)
    // the persisted token before asking who's signed in -- calling auth.me()
    // first would race it and 401 with no Authorization header attached yet.
    // Already-resolved on the web.
    await authReady;
    const u = await auth.me();
    setUICookie();
    // Applied here rather than only from the settings page: the low-balance
    // banner and the canvas indicator render on pages a user may never leave,
    // and without this they warn off the store's built-in default instead of
    // the threshold the account actually set.
    if (typeof u.lowBalanceUsdMicros === "number") {
      setLowBalanceThresholdUSD(microsToUSDNumber(u.lowBalanceUsdMicros));
    }
    commit({ signedIn: true, loading: false, user: u });
  } catch {
    clearUICookie();
    commit({ signedIn: false, loading: false, user: null });
  }
}

// Hands a fresh session to the native shell so it survives the app being
// closed. Inert on the web, where the token is null and the HttpOnly cookie is
// the session. Dynamic and IS_NATIVE-guarded for the same reason as NativeBoot:
// a browser build must not pull Capacitor in.
function persistNativeSession(token: string | null): void {
  if (!token || !IS_NATIVE) return;
  setAuthToken(token);
  // Logged, not swallowed: a failed native persist (e.g. a Keystore write
  // error) would otherwise leave the UI showing signed-in while the shell never
  // actually saved the token, silently failing to survive the app being killed.
  void import("@/native")
    .then(({ shell }) => shell.onSignedIn(token))
    .catch((err) =>
      console.error("native shell failed to persist sign-in", err),
    );
}

// The local half of signing out. A module function rather than a useCallback,
// because the state it clears is module-level now -- nothing here needs a
// component to be mounted.
function clearLocalSession(): void {
  if (IS_NATIVE) {
    setAuthToken(null);
    // Logged for the mirror-image reason: a shared device that fails to clear
    // the persisted token would otherwise silently keep the old user's session
    // live in Keystore after the UI has already moved on.
    void import("@/native")
      .then(({ shell }) => shell.onSignedOut())
      .catch((err) =>
        console.error("native shell failed to clear sign-out", err),
      );
  }
  clearUICookie();
  // Balance and purchase history are module singletons that survive a
  // client-side route change, so they have to be dropped explicitly here --
  // otherwise the next account to sign in in this tab sees the previous one's
  // money until its own fetch lands. resetCredits also bumps the store's epoch,
  // which discards any refresh still in flight.
  resetCredits();
  commit({ signedIn: false, loading: false, user: null });
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
    const token = await auth.signIn(email, password);
    persistNativeSession(token);
    setUICookie();
    commit({ ...snapshot, signedIn: true });
    // Re-read the account rather than waiting for a reload. With per-component
    // state the next page's own fetch used to cover this; a shared store loads
    // once, so signing in has to ask for the new identity explicitly.
    await load();
  }, []);

  const signUp = useCallback(
    async (email: string, password: string, name: string, org: string) => {
      const token = await auth.signUp(email, password, name, org);
      persistNativeSession(token);
      setUICookie();
      commit({ ...snapshot, signedIn: true });
      await load();
    },
    [],
  );

  const signOut = useCallback(async () => {
    // The local teardown runs in a finally: auth.signOut() is a network call,
    // and letting a failed request skip the cleanup would leave the previous
    // account's balance, purchase history and (on native) persisted token live
    // in a UI that has already moved on. Signing out locally is the part that
    // must not be optional -- the server-side cookie clear is best-effort by
    // comparison.
    try {
      await auth.signOut();
    } catch (err) {
      console.error(
        "sign-out request failed; clearing local session anyway",
        err,
      );
    } finally {
      clearLocalSession();
    }
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
