"use client";
import { useSyncExternalStore } from "react";

// The module-store scaffold shared by the credits, currency and auth stores.
//
// All three hand-rolled the same thing: a module-level value, a Set of
// listeners, a commit that notifies them, and a useSyncExternalStore triple. A
// correctness fix to that plumbing had to be applied three times by hand, with
// nothing keeping the copies behaviourally identical.
//
// Two properties matter and are independently easy to get wrong, which is the
// real argument for one implementation:
//
//   1. getSnapshot must return a *stable* object identity between changes.
//      Returning a fresh literal per call makes useSyncExternalStore re-render
//      forever.
//   2. getServerSnapshot must equal the first client value. React uses it for
//      the hydration render, so anything read from the browser (localStorage,
//      window) has to arrive after hydration, never during render — that exact
//      mismatch already shipped once on this branch.

export interface ExternalStore<T> {
  /** Current value. One stable object per change. */
  get: () => T;
  /** Replace the value and notify subscribers. */
  set: (next: T) => void;
  /** Read the current value inside a component. */
  use: () => T;
  /** Run `hydrate` once, on the client, on the first client read. */
  onFirstClientRead: (hydrate: () => T | null) => void;
  /**
   * Test-only: restore a value and re-arm the one-shot hydration, so cases do
   * not leak a hydrated flag into each other.
   */
  resetForTest: (value: T) => void;
}

export function createExternalStore<T>(
  initial: T,
  serverSnapshot: T = initial,
): ExternalStore<T> {
  let snapshot = initial;
  let hydrator: (() => T | null) | null = null;
  let hydrated = false;
  const listeners = new Set<() => void>();

  const set = (next: T): void => {
    snapshot = next;
    listeners.forEach((l) => l());
  };

  // Deliberately does not notify: this runs inside React's own read, and the
  // new snapshot identity is what triggers the post-hydration re-render.
  const ensureHydrated = (): void => {
    if (hydrated || !hydrator || typeof window === "undefined") return;
    hydrated = true;
    const next = hydrator();
    if (next !== null) snapshot = next;
  };

  const subscribe = (listener: () => void): (() => void) => {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  };

  const get = (): T => {
    ensureHydrated();
    return snapshot;
  };

  return {
    get,
    set,
    use: () => useSyncExternalStore(subscribe, get, () => serverSnapshot),
    onFirstClientRead: (hydrate) => {
      hydrator = hydrate;
    },
    resetForTest: (value) => {
      hydrated = false;
      set(value);
    },
  };
}
