import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import type { OAuthResult } from "./oauth";

// What boot() and onSignedOut owe the notification feature.
//
// The re-arm is not a convenience. push.ts keeps its FCM token in memory
// only, so without it a user who turned notifications on yesterday has no
// token to unregister today, and the switch cannot be moved back. And the
// clear at sign-out is what stops a shared phone registering the next person
// for notifications they were never asked about.

function harness(opts: {
  token: string | null;
  optedIn: boolean;
  restore?: () => Promise<void>;
  // What the stored token is by the time a clear runs. Defaults to `token`;
  // set it to model a sign-in that has already replaced it.
  storedAtClear?: string | null;
}) {
  const calls = {
    enable: 0,
    disable: 0,
    clears: 0,
    taps: 0,
    back: 0,
    flushes: 0,
    oauth: 0,
    tokenClears: [] as (string | null)[],
  };
  const prefs = { optedIn: opts.optedIn };
  let stored: string | null = opts.token;
  const navigate = vi.fn();
  const persistNativeSession = vi
    .fn<(token: string) => Promise<void>>()
    .mockResolvedValue(undefined);
  let onOAuth: ((result: OAuthResult) => void | Promise<void>) | undefined;

  vi.doMock("@/lib/nativeNav", () => ({ navigateInApp: navigate }));
  vi.doMock("@/hooks/useAuth", () => ({ persistNativeSession }));
  vi.doMock("./oauth", () => ({
    listenForCallback: async (callback: typeof onOAuth) => {
      calls.oauth += 1;
      onOAuth = callback;
    },
  }));
  vi.doMock("./back", () => ({
    listenForBack: async () => {
      calls.back += 1;
    },
  }));

  vi.doMock("./auth", () => ({
    loadToken: async () => stored,
    saveToken: async () => {},
    clearTokenIf: async (expected: string | null) => {
      if (opts.storedAtClear !== undefined) stored = opts.storedAtClear;
      if (stored !== expected) return false;
      calls.tokenClears.push(expected);
      stored = null;
      return true;
    },
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
      await opts.restore?.();
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

  return {
    calls,
    prefs,
    navigate,
    persistNativeSession,
    async deliverOAuth(result: OAuthResult) {
      if (!onOAuth)
        throw new Error("OAuth callback listener was not registered");
      await onOAuth(result);
    },
  };
}

afterEach(() => {
  vi.restoreAllMocks();
});

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

  it("still attaches the tap and Back listeners and flushes whatever the push state", async () => {
    // The re-arm is an addition to boot(), not a gate on it. A notification
    // tapped from a cold start arrives during launch, so the listener has to
    // go on regardless.
    const { calls } = harness({ token: null, optedIn: false });
    const { boot } = await import("./index");

    await boot();
    expect(calls.taps).toBe(1);
    expect(calls.back).toBe(1);
    expect(calls.flushes).toBe(1);
  });

  it("handles an OAuth callback on a signed-out launch without opting into push", async () => {
    const h = harness({ token: null, optedIn: false });
    const { boot } = await import("./index");

    expect(await boot()).toBeNull();
    await h.deliverOAuth({ ok: true, token: "tok_oauth" });

    expect(h.calls.oauth).toBe(1);
    expect(h.calls.taps).toBe(1);
    expect(h.calls.enable).toBe(0);
    expect(h.persistNativeSession).toHaveBeenCalledWith("tok_oauth");
    // Inside the app, replacing the sign-in screen, not a page load.
    expect(h.navigate).toHaveBeenCalledWith("/workflows", { replace: true });
  });

  it("handles OAuth while notification restoration is still pending", async () => {
    let finishRestore!: () => void;
    const pending = new Promise<void>((resolve) => {
      finishRestore = resolve;
    });
    const h = harness({
      token: "tok_session",
      optedIn: true,
      restore: () => pending,
    });
    const { boot } = await import("./index");

    expect(await boot()).toBe("tok_session");
    await h.deliverOAuth({ ok: true, token: "tok_oauth" });
    expect(h.calls.enable).toBe(0);
    expect(h.calls.taps).toBe(1);
    expect(h.persistNativeSession).toHaveBeenCalledWith("tok_oauth");
    expect(h.navigate).toHaveBeenCalledWith("/workflows", { replace: true });

    finishRestore();
    await settle();
    expect(h.calls.enable).toBe(1);
  });

  it("keeps OAuth available when notification restoration fails", async () => {
    const h = harness({
      token: "tok_session",
      optedIn: true,
      restore: async () => {
        throw new Error("push unavailable");
      },
    });
    const { boot } = await import("./index");

    expect(await boot()).toBe("tok_session");
    await settle();
    await h.deliverOAuth({ ok: true, token: "tok_oauth" });

    expect(h.persistNativeSession).toHaveBeenCalledWith("tok_oauth");
    expect(h.navigate).toHaveBeenCalledWith("/workflows", { replace: true });
    expect(h.calls.taps).toBe(1);
    expect(h.calls.enable).toBe(0);
  });

  it("restores notifications even when an OAuth callback reports an error", async () => {
    const h = harness({ token: "tok_session", optedIn: true });
    const { boot } = await import("./index");

    await boot();
    await h.deliverOAuth({ ok: false, reason: "cancelled" });
    await settle();

    expect(h.calls.enable).toBe(1);
    expect(h.persistNativeSession).not.toHaveBeenCalled();
    // The reason survives to the sign-in screen.
    expect(h.navigate).toHaveBeenCalledWith("/signin?error=cancelled", {
      replace: true,
    });
  });

  it("sends a session the device could not keep back to sign-in with its reason", async () => {
    const h = harness({ token: null, optedIn: false });
    h.persistNativeSession.mockRejectedValueOnce(new Error("keystore"));
    const { boot } = await import("./index");

    await boot();
    await h.deliverOAuth({ ok: true, token: "tok_oauth" });

    expect(h.navigate).toHaveBeenCalledWith("/signin?error=session_persist", {
      replace: true,
    });
  });

  // Password sign-in follows ?next=; social sign-in must too, or a
  // notification tapped while signed out loses its workflow.
  it("goes where sign-in was headed, and keeps it on a failed attempt", async () => {
    const h = harness({ token: null, optedIn: false });
    const { boot } = await import("./index");
    await boot();

    await h.deliverOAuth({
      ok: true,
      token: "tok_oauth",
      next: "/workflows/app?id=wf-1",
    });
    expect(h.navigate).toHaveBeenLastCalledWith("/workflows/app?id=wf-1", {
      replace: true,
    });

    await h.deliverOAuth({
      ok: false,
      reason: "cancelled",
      next: "/workflows/app?id=wf-1",
    });
    expect(h.navigate).toHaveBeenLastCalledWith(
      "/signin?error=cancelled&next=%2Fworkflows%2Fapp%3Fid%3Dwf-1",
      { replace: true },
    );
  });

  it("ignores a next target that leaves the app", async () => {
    const h = harness({ token: null, optedIn: false });
    const { boot } = await import("./index");
    await boot();

    await h.deliverOAuth({ ok: true, token: "tok_oauth", next: "//evil.test" });
    expect(h.navigate).toHaveBeenLastCalledWith("/workflows", {
      replace: true,
    });
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
    expect(calls.tokenClears).toEqual(["tok_session"]);
  });

  // Unregistering awaits the network. A sign-in that lands meanwhile has
  // saved a different token, and the sign-out must not take it with it.
  it("leaves a session signed in while it was still cleaning up", async () => {
    const { calls } = harness({
      token: "tok_session",
      optedIn: true,
      storedAtClear: "tok_next",
    });
    const { shell } = await import("./index");

    await shell.onSignedOut();
    expect(calls.tokenClears).toEqual([]);
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

// The server refused a token. Unlike a sign-out, nothing authenticated can be
// done with it any more, so it is removed before the slow notification work,
// and only if a sign-in has not already replaced it.
describe("onSessionRejected", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  it("clears the refused token and what the device held for it", async () => {
    const { calls, prefs } = harness({ token: "tok_dead", optedIn: true });
    const { shell } = await import("./index");

    await shell.onSessionRejected("tok_dead");
    expect(calls.tokenClears).toEqual(["tok_dead"]);
    expect(calls.disable).toBe(1);
    expect(prefs.optedIn).toBe(false);
    expect(calls.clears).toBe(1);
  });

  it("touches nothing once a sign-in has replaced the refused token", async () => {
    const { calls, prefs } = harness({
      token: "tok_dead",
      optedIn: true,
      storedAtClear: "tok_fresh",
    });
    const { shell } = await import("./index");

    await shell.onSessionRejected("tok_dead");
    expect(calls.tokenClears).toEqual([]);
    expect(calls.disable).toBe(0);
    expect(calls.clears).toBe(0);
    expect(prefs.optedIn).toBe(true);
  });
});
