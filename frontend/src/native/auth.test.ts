import { describe, it, expect, vi } from "vitest";
import { migrateToken, type TokenStore } from "./auth";

// A TokenStore backed by a variable, plus counters, so each test can assert
// what was actually touched rather than only what came back.
function fakeStore(initial: string | null = null) {
  const calls = { get: 0, set: 0, remove: 0 };
  let value = initial;
  const store: TokenStore = {
    async get() {
      calls.get++;
      return value;
    },
    async set(v) {
      calls.set++;
      value = v;
    },
    async remove() {
      calls.remove++;
      value = null;
    },
  };
  return {
    store,
    calls,
    get value() {
      return value;
    },
  };
}

describe("migrateToken", () => {
  it("returns null when neither store holds anything", async () => {
    const secure = fakeStore(null);
    const legacy = fakeStore(null);
    expect(await migrateToken(secure.store, legacy.store)).toBeNull();
    expect(secure.calls.set).toBe(0);
  });

  it("copies a token out of the old store and removes it", async () => {
    const secure = fakeStore(null);
    const legacy = fakeStore("tok_abc");

    expect(await migrateToken(secure.store, legacy.store)).toBe("tok_abc");
    expect(secure.value).toBe("tok_abc");
    // The old plaintext copy must not survive -- leaving it there would mean
    // the change bought nothing for an upgrading install, which is most of
    // them.
    expect(legacy.value).toBeNull();
  });

  it("is idempotent: a second run copies nothing", async () => {
    const secure = fakeStore(null);
    const legacy = fakeStore("tok_abc");

    await migrateToken(secure.store, legacy.store);
    const setsAfterFirst = secure.calls.set;
    expect(await migrateToken(secure.store, legacy.store)).toBe("tok_abc");
    expect(secure.calls.set).toBe(setsAfterFirst);
  });

  it("prefers the secure store and never overwrites it from the old one", async () => {
    // The state after a sign-out/sign-in that left a straggler behind: the
    // secure store is authoritative and the stale one must not win.
    const secure = fakeStore("tok_new");
    const legacy = fakeStore("tok_stale");

    expect(await migrateToken(secure.store, legacy.store)).toBe("tok_new");
    expect(secure.value).toBe("tok_new");
    expect(secure.calls.set).toBe(0);
  });

  it("clears a straggler left in the old store once migrated", async () => {
    const secure = fakeStore("tok_new");
    const legacy = fakeStore("tok_stale");

    await migrateToken(secure.store, legacy.store);
    expect(legacy.value).toBeNull();
  });

  // The rule the whole migration turns on.
  it("keeps the session when the secure write fails", async () => {
    const legacy = fakeStore("tok_abc");
    const secure: TokenStore = {
      get: async () => null,
      set: async () => {
        throw new Error("keystore unavailable");
      },
      remove: async () => {},
    };

    // Signed in, not signed out: failing to upgrade the storage is not a
    // reason to destroy what it holds.
    expect(await migrateToken(secure, legacy.store)).toBe("tok_abc");
    // And crucially the old copy is still there, so the next launch can retry.
    expect(legacy.value).toBe("tok_abc");
    expect(legacy.calls.remove).toBe(0);
  });

  it("still returns the token when the old store cannot be cleaned up", async () => {
    const secure = fakeStore(null);
    const legacy: TokenStore = {
      get: async () => "tok_abc",
      set: async () => {},
      remove: async () => {
        throw new Error("write failed");
      },
    };

    // The copy succeeded, so the session is migrated. A failed tidy-up is
    // untidy, not broken, and must not surface as a sign-out.
    expect(await migrateToken(secure.store, legacy)).toBe("tok_abc");
    expect(secure.value).toBe("tok_abc");
  });

  it("does not read the old store at all once the secure one is populated", async () => {
    const secure = fakeStore("tok_new");
    const legacy = fakeStore(null);

    await migrateToken(secure.store, legacy.store);
    // Steady state is every launch after the first, so it should cost one
    // encrypted read and nothing else beyond the tidy-up.
    expect(legacy.calls.get).toBe(0);
  });

  // A device whose Keystore has failed in a way SecureStorePlugin's own
  // recovery could not fix. Being signed out by a storage upgrade nobody asked
  // for is the worst possible outcome here.
  it("falls back to the old store when the secure one cannot be read", async () => {
    const legacy = fakeStore("tok_abc");
    const secure: TokenStore = {
      get: async () => {
        throw new Error("keystore unavailable");
      },
      set: async () => {},
      remove: async () => {},
    };

    expect(await migrateToken(secure, legacy.store)).toBe("tok_abc");
    // Nothing is destroyed on the way past: the old copy is the only one that
    // still works, so it has to survive.
    expect(legacy.value).toBe("tok_abc");
    expect(legacy.calls.remove).toBe(0);
  });

  it("returns null when neither store can be read", async () => {
    const boom: TokenStore = {
      get: async () => {
        throw new Error("unavailable");
      },
      set: async () => {},
      remove: async () => {},
    };
    expect(await migrateToken(boom, boom)).toBeNull();
  });

  it("treats an empty string as a value, not as absence", async () => {
    // Not expected from the backend, but "" is falsy and a `||` in the wrong
    // place would silently turn it into a migration that never happens. The
    // source uses an explicit null check; this pins that.
    const secure = fakeStore(null);
    const legacy = fakeStore("");

    expect(await migrateToken(secure.store, legacy.store)).toBe("");
    expect(secure.value).toBe("");
  });
});

describe("clearToken", () => {
  it("clears both stores, not just the secure one", async () => {
    // An install that never managed to migrate still has the old key. Signing
    // out has to mean signed out everywhere, or the next launch migrates the
    // session straight back in.
    vi.resetModules();
    const removed: string[] = [];
    vi.doMock("./secureStore", () => ({
      SecureStore: {
        get: async () => ({ value: null }),
        set: async () => {},
        remove: async () => {
          removed.push("secure");
        },
      },
    }));
    vi.doMock("@capacitor/preferences", () => ({
      Preferences: {
        get: async () => ({ value: null }),
        set: async () => {},
        remove: async () => {
          removed.push("legacy");
        },
      },
    }));
    const { clearToken, loadToken } = await import("./auth");

    await clearToken();
    expect(removed.sort()).toEqual(["legacy", "secure"]);
    // And the memoised read must not still be handing out the old answer.
    expect(await loadToken()).toBeNull();

    vi.doUnmock("./secureStore");
    vi.doUnmock("@capacitor/preferences");
  });
});

describe("clearToken when the secure store refuses to delete", () => {
  it("does not report signed out while the token is still on disk", async () => {
    // The failure this pins: `pending` used to be set to null-resolved before
    // the remove was attempted, so a rejected remove left the app believing it
    // was signed out while the token sat in the store. The next launch read it
    // straight back and signed the user in again.
    vi.resetModules();
    let stored: string | null = "tok_live";
    vi.doMock("./secureStore", () => ({
      SecureStore: {
        get: async () => ({ value: stored }),
        set: async () => {},
        remove: async () => {
          throw new Error("keystore locked");
        },
      },
    }));
    vi.doMock("@capacitor/preferences", () => ({
      Preferences: {
        get: async () => ({ value: null }),
        set: async () => {},
        remove: async () => {},
      },
    }));
    const { clearToken, loadToken } = await import("./auth");

    // The caller still hears about it. That part was already right.
    await expect(clearToken()).rejects.toThrow("keystore locked");

    // And the in-memory view agrees with the disk rather than contradicting
    // it: the token was not removed, so the session is still live.
    expect(await loadToken()).toBe("tok_live");

    // Once the store does let go, sign-out settles as it should.
    stored = null;
    expect(await loadToken()).toBe("tok_live"); // memoised, as designed

    vi.doUnmock("./secureStore");
    vi.doUnmock("@capacitor/preferences");
  });
});

describe("a migration that fails after saveToken has landed", () => {
  it("does not discard the freshly saved token", async () => {
    // loadToken()'s .catch used to null `pending` unconditionally. If a
    // sign-in completed while a doomed migration was still in flight, the late
    // rejection wiped the memo holding the new token.
    //
    // Reaching it takes some doing, which is worth recording: migrateToken
    // handles a failing SECURE store internally and resolves, so that path
    // never reaches this .catch at all. The one unguarded await is
    // `legacy.get()` on the already-migrated-nothing path, so the legacy store
    // is what has to fail here.
    vi.resetModules();
    let releaseSecureGet: (() => void) | null = null;
    const secureGetBlocked = new Promise<void>((r) => {
      releaseSecureGet = r;
    });
    vi.doMock("./secureStore", () => ({
      SecureStore: {
        get: async () => {
          await secureGetBlocked;
          return { value: null };
        },
        set: async () => {},
        remove: async () => {},
      },
    }));
    vi.doMock("@capacitor/preferences", () => ({
      Preferences: {
        get: async () => {
          throw new Error("legacy store unreadable");
        },
        set: async () => {},
        remove: async () => {},
      },
    }));
    const { loadToken, saveToken } = await import("./auth");

    const inFlight = loadToken();
    // Sign-in lands while the migration is still stuck on the secure read.
    await saveToken("tok_new");
    releaseSecureGet!();
    // The migration now rejects, and its handler must leave the memo alone
    // because it no longer owns it.
    expect(await inFlight).toBeNull();

    expect(await loadToken()).toBe("tok_new");

    vi.doUnmock("./secureStore");
    vi.doUnmock("@capacitor/preferences");
  });
});
