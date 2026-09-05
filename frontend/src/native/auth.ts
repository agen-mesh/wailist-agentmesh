// Session persistence for the shell.
//
// The token comes from the backend's sign-in response, which hands one out
// only to a client identifying itself via X-AgentMesh-Client (see
// backend/internal/api/handlers/auth.go). The web app never gets one and keeps
// using its HttpOnly cookie.
//
// Stored through SecureStorePlugin, an app-module plugin wrapping Android's
// EncryptedSharedPreferences. What that buys, stated no more strongly than it
// deserves:
//
//   - The file on disk is AES-GCM ciphertext, and the key that decrypts it
//     lives in the Android Keystore -- on a device with a hardware-backed
//     implementation, the key material never enters app memory at all. So
//     someone holding the filesystem (a rooted phone, an image of one) has
//     ciphertext and no key.
//   - It is outside the WebView's origin, so script running in the WebView
//     cannot reach it.
//
// What it does NOT buy, and this is the part an earlier version of this file
// got wrong by claiming encryption it did not have: it is no defence against
// code running AS this app on an unlocked device. Anything the app can
// decrypt, an attacker with the app's identity can decrypt too. A security
// comment that overstates its guarantee is worse than no comment at all.
import { Preferences } from "@capacitor/preferences";
import { SecureStore } from "./secureStore";

const TOKEN_KEY = "agentmesh.session.token";

/**
 * The two-method surface the migration needs, so it can be tested without a
 * device or a Capacitor bridge.
 */
export interface TokenStore {
  get(): Promise<string | null>;
  set(value: string): Promise<void>;
  remove(): Promise<void>;
}

const secureStore: TokenStore = {
  async get() {
    const { value } = await SecureStore.get({ key: TOKEN_KEY });
    return value ?? null;
  },
  async set(value) {
    await SecureStore.set({ key: TOKEN_KEY, value });
  },
  async remove() {
    await SecureStore.remove({ key: TOKEN_KEY });
  },
};

// The store this used to live in. Kept only so an install upgrading from a
// previous build does not lose its session, and read exactly once per launch.
const legacyStore: TokenStore = {
  async get() {
    const { value } = await Preferences.get({ key: TOKEN_KEY });
    return value ?? null;
  },
  async set(value) {
    await Preferences.set({ key: TOKEN_KEY, value });
  },
  async remove() {
    await Preferences.remove({ key: TOKEN_KEY });
  },
};

/**
 * Moves a token out of the old plain store and into the encrypted one,
 * returning whichever token the session should now use.
 *
 * Idempotent: once the secure store holds a token this stops copying and only
 * tidies up. Safe to call on every read, which is what makes it possible to
 * run without a separate "have I migrated yet" flag -- a flag would be a third
 * piece of state that can disagree with the other two.
 *
 * The failure rule is the important one. If the secure write fails, the old
 * token is LEFT WHERE IT IS and returned anyway. A migration that cannot
 * complete must not sign the user out: being unable to upgrade the storage is
 * not a reason to destroy the session it holds, and the next launch will try
 * again. The reverse order -- delete first, write second -- would lose the
 * session outright on a device whose Keystore is unavailable.
 */
export async function migrateToken(
  secure: TokenStore,
  legacy: TokenStore,
): Promise<string | null> {
  let existing: string | null;
  try {
    existing = await secure.get();
  } catch {
    // The encrypted store is unreadable and SecureStorePlugin's own recovery
    // could not rescue it. Fall back to whatever the old store still holds and
    // do not touch either one: a device whose Keystore is broken should stay
    // signed in on the copy it already has, rather than being signed out by a
    // storage upgrade it never asked for.
    return legacy.get().catch(() => null);
  }
  if (existing !== null) {
    // Already migrated. Clear any straggler in the old store rather than
    // leaving a stale plaintext copy of a live token sitting next to the
    // encrypted one -- the whole point of this change is that it should not
    // be there. Failure to remove it is not worth failing the read over.
    await legacy.remove().catch(() => {});
    return existing;
  }

  const old = await legacy.get();
  if (old === null) return null;

  try {
    await secure.set(old);
  } catch {
    // See above: keep the session, try again next launch.
    return old;
  }
  await legacy.remove().catch(() => {});
  return old;
}

// Memoised so the migration runs once per launch rather than on every call.
// loadToken() is called by boot(), by every request in native/api.ts, and by
// the geofence flush -- several of which can be in flight at once, and two
// concurrent migrations would race on the same two keys.
let pending: Promise<string | null> | null = null;

export async function loadToken(): Promise<string | null> {
  if (!pending) {
    const attempt: Promise<string | null> = migrateToken(
      secureStore,
      legacyStore,
    ).catch((err) => {
      // A store that cannot be read means signed out, not a crash. Reset so
      // the next call retries rather than caching the failure for the life of
      // the process.
      //
      // Only if this attempt is still the current one. A saveToken() that
      // lands while the migration is in flight replaces `pending` with the
      // freshly saved token; this handler must not then null out a memo it no
      // longer owns and throw away that answer.
      if (pending === attempt) pending = null;
      console.error("secure token storage unavailable", err);
      return null;
    });
    pending = attempt;
  }
  return pending;
}

export async function saveToken(token: string): Promise<void> {
  await secureStore.set(token);
  // The memoised read has to reflect the new token immediately -- boot() and
  // the geofence flush both call loadToken(), and a stale resolved promise
  // would hand them the pre-sign-in answer.
  pending = Promise.resolve(token);
}

// Called on sign-out and on any 401. Clearing on 401 matters: a token that has
// expired or been revoked otherwise sits there failing every request forever,
// with the app unable to explain why.
export async function clearToken(): Promise<void> {
  // Both stores, not just the secure one. An install that failed to migrate
  // still has the old key, and signing out has to mean signed out everywhere
  // rather than leaving something for the next launch to migrate back in.
  //
  // The old store's failure is swallowed and the secure store's is not. They
  // are not the same kind of failure: not tidying away a legacy key is untidy,
  // but not deleting the live token means the session is still on the device
  // after the user asked for it to be gone -- which the caller has to hear
  // about rather than see reported as a successful sign-out.
  const legacyRemoved = legacyStore.remove().catch(() => {});
  try {
    await secureStore.remove();
  } catch (err) {
    // The token is still on disk, so the in-memory view must not say
    // otherwise. Claiming "signed out" here is the worse half of the bug: the
    // caller does hear the rejection, but the next launch reads the token
    // straight back out of the secure store and silently signs the user in
    // again -- exactly what sign-out was supposed to prevent.
    //
    // Reset to null rather than to a guessed value: the store is the only
    // thing that knows what survived, and the next loadToken() re-derives the
    // answer from it instead of trusting a memo written during a failure.
    pending = null;
    await legacyRemoved;
    throw err;
  }
  // Flipped only once the destructive remove has actually resolved.
  pending = Promise.resolve(null);
  await legacyRemoved;
}

// Test seam. Nothing in the app calls this; it exists so a test can reset the
// memoised migration between cases without reloading the module.
export function __resetTokenCacheForTests(): void {
  pending = null;
}
