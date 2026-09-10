import { describe, it, expect, vi, beforeEach } from "vitest";

// Listener bookkeeping for @capacitor/push-notifications.
//
// Both bugs these tests pin are about which listeners survive which call, so
// the fake keeps a live registry and the assertions read it directly. Nothing
// here touches navigation: a tap that routes correctly but was detached at
// sign-out is still broken, and the detachment is the part that regressed.
type Registry = { event: string; removed: boolean }[];

function fakePlugin(registry: Registry, token: string | null) {
  const emit: Record<string, (arg: unknown) => void> = {};
  return {
    PushNotifications: {
      checkPermissions: async () => ({ receive: "granted" }),
      requestPermissions: async () => ({ receive: "granted" }),
      addListener: async (event: string, cb: (arg: unknown) => void) => {
        const entry = { event, removed: false };
        registry.push(entry);
        emit[event] = cb;
        return {
          remove: async () => {
            entry.removed = true;
          },
        };
      },
      register: async () => {
        // FCM answers on the event, not by resolving register(). Deferred so
        // the listener is attached first, as it is on a device.
        queueMicrotask(() => {
          if (token !== null) emit.registration?.({ value: token });
          else emit.registrationError?.({ error: "no google-services.json" });
        });
      },
      unregister: async () => {},
      removeAllListeners: async () => {
        for (const e of registry) e.removed = true;
      },
    },
  };
}

const live = (r: Registry, event: string) =>
  r.filter((e) => e.event === event && !e.removed).length;

describe("push listener lifecycle", () => {
  beforeEach(() => {
    vi.resetModules();
  });

  it("keeps the tap listener attached across a sign-out", async () => {
    // The regression: disablePush() called removeAllListeners(), which took
    // the tap listener boot() had attached. Signing out and back in without a
    // restart then left a tapped notification going nowhere until the next
    // cold start.
    const registry: Registry = [];
    vi.doMock("@capacitor/push-notifications", () =>
      fakePlugin(registry, "tok_fcm"),
    );
    vi.doMock("./api", () => ({
      registerDevice: async () => {},
      unregisterDevice: async () => {},
    }));
    const { listenForTaps, enablePush, disablePush } = await import("./push");

    await listenForTaps();
    expect(live(registry, "pushNotificationActionPerformed")).toBe(1);

    expect(await enablePush()).toBe("granted");
    await disablePush();

    expect(live(registry, "pushNotificationActionPerformed")).toBe(1);
  });

  it("removes the registration listeners it added, and only those", async () => {
    // They were never removed, so every enable/disable cycle left two more
    // attached for the life of the process.
    const registry: Registry = [];
    vi.doMock("@capacitor/push-notifications", () =>
      fakePlugin(registry, "tok_fcm"),
    );
    vi.doMock("./api", () => ({
      registerDevice: async () => {},
      unregisterDevice: async () => {},
    }));
    const { listenForTaps, enablePush } = await import("./push");

    await listenForTaps();
    await enablePush();
    await enablePush();
    await enablePush();

    expect(live(registry, "registration")).toBe(0);
    expect(live(registry, "registrationError")).toBe(0);
    // Three cycles really did attach them; they were removed, not skipped.
    expect(registry.filter((e) => e.event === "registration").length).toBe(3);
    // And the one listener that is meant to outlive a session did.
    expect(live(registry, "pushNotificationActionPerformed")).toBe(1);
  });

  it("cleans up after a registration that fails", async () => {
    // The failure path settles through registrationError rather than
    // registration, and has to tidy up the same two listeners.
    const registry: Registry = [];
    vi.doMock("@capacitor/push-notifications", () =>
      fakePlugin(registry, null),
    );
    vi.doMock("./api", () => ({
      registerDevice: async () => {},
      unregisterDevice: async () => {},
    }));
    const { enablePush } = await import("./push");

    expect(await enablePush()).toBe("unavailable");
    expect(live(registry, "registration")).toBe(0);
    expect(live(registry, "registrationError")).toBe(0);
  });

  it("attaches the tap listener once however often it is called", async () => {
    const registry: Registry = [];
    vi.doMock("@capacitor/push-notifications", () =>
      fakePlugin(registry, "tok_fcm"),
    );
    vi.doMock("./api", () => ({
      registerDevice: async () => {},
      unregisterDevice: async () => {},
    }));
    const { listenForTaps } = await import("./push");

    await listenForTaps();
    await listenForTaps();
    expect(
      registry.filter((e) => e.event === "pushNotificationActionPerformed"),
    ).toHaveLength(1);
  });
});

// A permission plugin that records whether anyone asked, and can be told what
// to answer. The recording is the point: reading the state must never spend
// the one-shot dialog.
function permissionPlugin(receive: string, opts: { throws?: boolean } = {}) {
  const calls = { checked: 0, requested: 0 };
  const plugin = {
    PushNotifications: {
      checkPermissions: async () => {
        calls.checked += 1;
        if (opts.throws) throw new Error("no push provider on this device");
        return { receive };
      },
      requestPermissions: async () => {
        calls.requested += 1;
        return { receive };
      },
      addListener: async () => ({ remove: async () => {} }),
      register: async () => {},
      unregister: async () => {},
      removeAllListeners: async () => {},
    },
  };
  return { plugin, calls };
}

function fakePrefs() {
  const state = { optedIn: false };
  return {
    state,
    module: {
      hasOptedIn: async () => state.optedIn,
      setOptedIn: async () => {
        state.optedIn = true;
      },
      clearOptedIn: async () => {
        state.optedIn = false;
      },
    },
  };
}

describe("reading notification state without asking for anything", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  // Two facts, not one. Android's permission is necessary and not sufficient:
  // turning notifications off in this app cannot revoke it, so permission
  // alone would keep answering "granted" after the user had switched off.
  it.each([
    ["granted", true, "granted"],
    ["granted", false, "off"],
    ["denied", true, "denied"],
    ["denied", false, "denied"],
    ["prompt", false, "off"],
    ["prompt", true, "off"],
    ["prompt-with-rationale", false, "off"],
  ])(
    "permission %s with optedIn=%s reads as %s",
    async (receive, optedIn, expected) => {
      const { plugin } = permissionPlugin(receive);
      const prefs = fakePrefs();
      prefs.state.optedIn = optedIn as boolean;
      vi.doMock("@capacitor/push-notifications", () => plugin);
      vi.doMock("./pushPrefs", () => prefs.module);
      vi.doMock("./api", () => ({
        registerDevice: async () => {},
        unregisterDevice: async () => {},
      }));
      const { notificationState } = await import("./push");

      expect(await notificationState()).toBe(expected);
    },
  );

  it("reads as off immediately after the user turns notifications off", async () => {
    // The reported bug, end to end. disablePush() unregisters and clears the
    // opt-in but cannot revoke Android's permission, so the sheet re-read the
    // state, was told "granted", and snapped back to its "on" panel with a
    // Turn off button -- right after the user pressed Turn off.
    const { plugin } = permissionPlugin("granted");
    const prefs = fakePrefs();
    prefs.state.optedIn = true;
    vi.doMock("@capacitor/push-notifications", () => plugin);
    vi.doMock("./pushPrefs", () => prefs.module);
    vi.doMock("./api", () => ({
      registerDevice: async () => {},
      unregisterDevice: async () => {},
    }));
    const { notificationState, disablePush } = await import("./push");

    expect(await notificationState()).toBe("granted");
    await disablePush();
    expect(await notificationState()).toBe("off");
  });

  it("never requests permission, whatever the current state", async () => {
    // The one thing this function must not do. Android 13+ shows the
    // notification dialog once; a state read that asked would spend it to
    // draw a switch, and a user who dismissed that dialog could never be
    // asked again from inside the app.
    for (const receive of ["granted", "denied", "prompt"]) {
      vi.resetModules();
      const { plugin, calls } = permissionPlugin(receive);
      vi.doMock("@capacitor/push-notifications", () => plugin);
      vi.doMock("./pushPrefs", () => fakePrefs().module);
      vi.doMock("./api", () => ({
        registerDevice: async () => {},
        unregisterDevice: async () => {},
      }));
      const { notificationState } = await import("./push");

      await notificationState();
      expect(calls.requested).toBe(0);
      expect(calls.checked).toBe(1);
    }
  });

  it("reports unavailable when the plugin cannot answer at all", async () => {
    // No google-services.json, or no Play services. Distinct from "denied":
    // the user refused nothing, so a screen must not offer them a route to
    // Settings that would change nothing.
    const { plugin } = permissionPlugin("granted", { throws: true });
    vi.doMock("@capacitor/push-notifications", () => plugin);
    vi.doMock("./pushPrefs", () => fakePrefs().module);
    vi.doMock("./api", () => ({
      registerDevice: async () => {},
      unregisterDevice: async () => {},
    }));
    const { notificationState } = await import("./push");

    expect(await notificationState()).toBe("unavailable");
  });
});

describe("the opt-in flag follows the registration", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  it("is not set when the server refuses the registration", async () => {
    // Set before registerDevice landed, the flag would have boot() re-arming
    // a device that was never registered -- and the user would see a switch
    // reading "on" for notifications that cannot arrive.
    const registry: Registry = [];
    const prefs = fakePrefs();
    vi.doMock("@capacitor/push-notifications", () =>
      fakePlugin(registry, "tok_fcm"),
    );
    vi.doMock("./pushPrefs", () => prefs.module);
    vi.doMock("./api", () => ({
      registerDevice: async () => {
        throw new Error("401");
      },
      unregisterDevice: async () => {},
    }));
    const { enablePush } = await import("./push");

    expect(await enablePush()).toBe("unavailable");
    expect(prefs.state.optedIn).toBe(false);
  });

  it("is set once the device is registered", async () => {
    const registry: Registry = [];
    const prefs = fakePrefs();
    vi.doMock("@capacitor/push-notifications", () =>
      fakePlugin(registry, "tok_fcm"),
    );
    vi.doMock("./pushPrefs", () => prefs.module);
    vi.doMock("./api", () => ({
      registerDevice: async () => {},
      unregisterDevice: async () => {},
    }));
    const { enablePush } = await import("./push");

    expect(await enablePush()).toBe("granted");
    expect(prefs.state.optedIn).toBe(true);
  });

  it("is cleared on disable even when there is no token to unregister", async () => {
    // The cold-start case. currentToken is null after a restart, so gating
    // the clear on having a token would leave the flag set on exactly the
    // devices that most need it cleared, and boot() would re-arm what the
    // user had just turned off.
    const registry: Registry = [];
    const prefs = fakePrefs();
    prefs.state.optedIn = true;
    let unregisterCalls = 0;
    vi.doMock("@capacitor/push-notifications", () =>
      fakePlugin(registry, "tok_fcm"),
    );
    vi.doMock("./pushPrefs", () => prefs.module);
    vi.doMock("./api", () => ({
      registerDevice: async () => {},
      unregisterDevice: async () => {
        unregisterCalls += 1;
      },
    }));
    const { disablePush } = await import("./push");

    await disablePush();
    expect(prefs.state.optedIn).toBe(false);
    expect(unregisterCalls).toBe(0);
  });
});
