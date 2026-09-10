import { describe, it, expect, vi, beforeEach } from "vitest";

// What boot() and onSignedOut owe the notification feature.
//
// The re-arm is not a convenience. push.ts keeps its FCM token in memory
// only, so without it a user who turned notifications on yesterday has no
// token to unregister today, and the switch cannot be moved back. And the
// clear at sign-out is what stops a shared phone registering the next person
// for notifications they were never asked about.

function harness(opts: { token: string | null; optedIn: boolean }) {
  const calls = { enable: 0, disable: 0, clears: 0, taps: 0, flushes: 0 };
  const prefs = { optedIn: opts.optedIn };

  vi.doMock("./auth", () => ({
    loadToken: async () => opts.token,
    saveToken: async () => {},
    clearToken: async () => {},
  }));
  vi.doMock("./geofence", () => ({
    flush: async () => {
      calls.flushes += 1;
    },
    start: async () => true,
    stop: async () => {},
  }));
  vi.doMock("./api", () => ({
    setGeofence: async () => {},
    clearGeofence: async () => {},
  }));
  vi.doMock("./push", () => ({
    restorePush: async () => {
      if (prefs.optedIn) calls.enable += 1;
    },
    enablePush: async () => {
      calls.enable += 1;
      return "granted";
    },
    disablePush: async () => {
      calls.disable += 1;
      prefs.optedIn = false;
    },
    listenForTaps: async () => {
      calls.taps += 1;
    },
    notificationState: async () => "granted",
  }));
  vi.doMock("./pushPrefs", () => ({
    hasOptedIn: async () => prefs.optedIn,
    setOptedIn: async () => {
      prefs.optedIn = true;
    },
    clearOptedIn: async () => {
      calls.clears += 1;
      prefs.optedIn = false;
    },
  }));

  return { calls, prefs };
}

// boot() fires the re-arm without awaiting it, deliberately -- a slow FCM
// registration must not hold up the launch. So the assertion has to let the
// microtask queue drain first.
const settle = () => new Promise((r) => setTimeout(r, 0));

describe("boot", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  it("re-registers a device that was already turned on", async () => {
    const { calls } = harness({ token: "tok_session", optedIn: true });
    const { boot } = await import("./index");

    expect(await boot()).toBe("tok_session");
    await settle();
    expect(calls.enable).toBe(1);
  });

  it("does not register a device nobody turned on", async () => {
    const { calls } = harness({ token: "tok_session", optedIn: false });
    const { boot } = await import("./index");

    await boot();
    await settle();
    expect(calls.enable).toBe(0);
  });

  it("does not register when there is no session to register under", async () => {
    // registerDevice is authenticated. Asking on a signed-out launch would
    // spend a round trip to be told 401.
    const { calls } = harness({ token: null, optedIn: true });
    const { boot } = await import("./index");

    expect(await boot()).toBeNull();
    await settle();
    expect(calls.enable).toBe(0);
  });

  it("still attaches the tap listener and flushes whatever the push state", async () => {
    // The re-arm is an addition to boot(), not a gate on it. A notification
    // tapped from a cold start arrives during launch, so the listener has to
    // go on regardless.
    const { calls } = harness({ token: null, optedIn: false });
    const { boot } = await import("./index");

    await boot();
    expect(calls.taps).toBe(1);
    expect(calls.flushes).toBe(1);
  });
});

describe("onSignedOut", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  it("leaves nothing for the next user of this phone to inherit", async () => {
    const { calls, prefs } = harness({ token: "tok_session", optedIn: true });
    const { shell } = await import("./index");

    await shell.onSignedOut();
    expect(calls.disable).toBe(1);
    expect(prefs.optedIn).toBe(false);
    // Cleared explicitly as well as inside disablePush: that call is one
    // await away from a plugin that can throw on a device with no push
    // provider, and the cost of the flag surviving is the next person being
    // registered without being asked.
    expect(calls.clears).toBe(1);
  });

  it("signs the next person in without inheriting the last one's opt-in", async () => {
    const { calls } = harness({ token: "tok_session", optedIn: true });
    const { boot, shell } = await import("./index");

    await shell.onSignedOut();
    await shell.onSignedIn("tok_other_person");
    await settle();
    // A fresh sign-in must not arm anything by itself: nobody has been asked
    // under this identity. Only a RESTORED session re-arms, and only if this
    // person's own opt-in survived.
    expect(calls.enable).toBe(0);

    await boot();
    await settle();
    expect(calls.enable).toBe(0);
  });
});
