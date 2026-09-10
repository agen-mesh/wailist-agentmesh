import { describe, it, expect, vi, beforeEach } from "vitest";

// The flag is small; what it has to survive is not. Each test here pins one
// of the reasons it exists rather than the getter and setter agreeing with
// each other, which they would whatever the storage did.

type Store = Record<string, string>;

function fakePreferences(store: Store, opts: { failing?: boolean } = {}) {
  return {
    Preferences: {
      get: async ({ key }: { key: string }) => {
        if (opts.failing) throw new Error("preferences unavailable");
        return { value: key in store ? store[key] : null };
      },
      set: async ({ key, value }: { key: string; value: string }) => {
        if (opts.failing) throw new Error("preferences unavailable");
        store[key] = value;
      },
      remove: async ({ key }: { key: string }) => {
        if (opts.failing) throw new Error("preferences unavailable");
        delete store[key];
      },
    },
  };
}

describe("the notification opt-in flag", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  it("reads false on a device that has never been asked", async () => {
    const store: Store = {};
    vi.doMock("@capacitor/preferences", () => fakePreferences(store));
    const { hasOptedIn } = await import("./pushPrefs");

    expect(await hasOptedIn()).toBe(false);
  });

  it("survives being written and read back", async () => {
    const store: Store = {};
    vi.doMock("@capacitor/preferences", () => fakePreferences(store));
    const { hasOptedIn, setOptedIn } = await import("./pushPrefs");

    await setOptedIn();
    expect(await hasOptedIn()).toBe(true);
  });

  it("is gone after clearing, so the next user is not opted in by the last", async () => {
    // The reason this is a flag and not just the OS permission: permission is
    // granted to the app and outlives a sign-out, so on a shared phone it
    // would answer "yes, register" for somebody who was never asked.
    const store: Store = {};
    vi.doMock("@capacitor/preferences", () => fakePreferences(store));
    const { hasOptedIn, setOptedIn, clearOptedIn } =
      await import("./pushPrefs");

    await setOptedIn();
    await clearOptedIn();
    expect(await hasOptedIn()).toBe(false);
  });

  it("answers false rather than throwing when the store cannot be read", async () => {
    // A false negative asks the user again, which is recoverable. A thrown
    // read on the boot path is not: it would take out the re-arm and, since
    // boot() runs before anything is on screen, take the launch with it.
    const store: Store = {};
    vi.doMock("@capacitor/preferences", () =>
      fakePreferences(store, { failing: true }),
    );
    const { hasOptedIn } = await import("./pushPrefs");

    await expect(hasOptedIn()).resolves.toBe(false);
  });

  it("lets a sign-out finish even when the store refuses the write", async () => {
    // clearOptedIn runs on the sign-out path, which has to complete on a
    // device that cannot write to disk any more than it can reach the network.
    const store: Store = {};
    vi.doMock("@capacitor/preferences", () =>
      fakePreferences(store, { failing: true }),
    );
    const { clearOptedIn } = await import("./pushPrefs");

    await expect(clearOptedIn()).resolves.toBeUndefined();
  });
});
