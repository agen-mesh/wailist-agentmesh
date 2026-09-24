import { beforeEach, describe, expect, it, vi } from "vitest";

// A build with no google-services.json. The push plugin's register() and
// unregister() crash the app natively in that build, so the only acceptable
// number of calls into them is zero -- a rejected promise here would prove
// nothing, because on a device there is no promise left to reject.
const mocks = vi.hoisted(() => ({
  available: false,
  checkPermissions: vi.fn(),
  requestPermissions: vi.fn(),
  register: vi.fn(),
  unregister: vi.fn(),
  registerDevice: vi.fn(),
  unregisterDevice: vi.fn(),
  optedIn: true,
}));

vi.mock("./pushAvailability", () => ({
  pushAvailable: async () => mocks.available,
}));
vi.mock("./api", () => ({
  registerDevice: mocks.registerDevice,
  unregisterDevice: mocks.unregisterDevice,
}));
vi.mock("./pushPrefs", () => ({
  hasOptedIn: async () => mocks.optedIn,
  setOptedIn: async () => {
    mocks.optedIn = true;
  },
  clearOptedIn: async () => {
    mocks.optedIn = false;
  },
}));
vi.mock("@capacitor/push-notifications", () => ({
  PushNotifications: {
    checkPermissions: mocks.checkPermissions,
    requestPermissions: mocks.requestPermissions,
    register: mocks.register,
    unregister: mocks.unregister,
    addListener: async () => ({ remove: async () => {} }),
  },
}));

beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  mocks.available = false;
  mocks.optedIn = true;
  mocks.checkPermissions.mockResolvedValue({ receive: "prompt" });
  mocks.requestPermissions.mockResolvedValue({ receive: "granted" });
  mocks.unregisterDevice.mockResolvedValue(undefined);
  mocks.unregister.mockResolvedValue(undefined);
});

describe("push in a build without Firebase", () => {
  it("reports unavailable, so the sheet never offers the switch", async () => {
    const { notificationState } = await import("./push");
    expect(await notificationState()).toBe("unavailable");
    expect(mocks.checkPermissions).not.toHaveBeenCalled();
  });

  it("does not ask for permission or register when turned on", async () => {
    const { enablePush } = await import("./push");
    expect(await enablePush()).toBe("unavailable");
    expect(mocks.requestPermissions).not.toHaveBeenCalled();
    expect(mocks.register).not.toHaveBeenCalled();
    expect(mocks.registerDevice).not.toHaveBeenCalled();
  });

  it("does not register on boot for a device that opted in earlier", async () => {
    const { restorePush } = await import("./push");
    await restorePush();
    expect(mocks.register).not.toHaveBeenCalled();
  });

  it("signs out without touching FCM, and still clears the opt-in", async () => {
    const { disablePush } = await import("./push");
    await disablePush();
    expect(mocks.unregister).not.toHaveBeenCalled();
    expect(mocks.optedIn).toBe(false);
  });

  it("still unregisters from FCM when Firebase is built in", async () => {
    mocks.available = true;
    const { disablePush } = await import("./push");
    await disablePush();
    expect(mocks.unregister).toHaveBeenCalledTimes(1);
  });
});
