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
