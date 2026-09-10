import { beforeEach, expect, it, vi } from "vitest";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

const mocks = vi.hoisted(() => ({
  optedIn: true,
  registerDevice: vi.fn(),
  unregisterDevice: vi.fn(),
  checkPermissions: vi.fn(),
  requestPermissions: vi.fn(),
  setOptedIn: vi.fn(),
  hasOptedIn: vi.fn(),
  register: vi.fn(),
  unregister: vi.fn(),
  onRegistration: vi.fn<(token: { value: string }) => void>(),
}));
vi.mock("./api", () => mocks);
vi.mock("./pushPrefs", () => ({
  hasOptedIn: mocks.hasOptedIn,
  setOptedIn: mocks.setOptedIn,
  clearOptedIn: async () => { mocks.optedIn = false; },
}));
vi.mock("@capacitor/push-notifications", () => ({ PushNotifications: {
  ...mocks,
  addListener: async (event: string, callback: typeof mocks.onRegistration) => {
    if (event === "registration") mocks.onRegistration.mockImplementation(callback);
    return { remove: async () => {} };
  },
} }));

beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  mocks.optedIn = true;
  mocks.registerDevice.mockResolvedValue(undefined);
  mocks.unregisterDevice.mockResolvedValue(undefined);
  mocks.checkPermissions.mockResolvedValue({ receive: "granted" });
  mocks.requestPermissions.mockResolvedValue({ receive: "granted" });
  mocks.hasOptedIn.mockImplementation(async () => mocks.optedIn);
  mocks.setOptedIn.mockImplementation(async () => { mocks.optedIn = true; });
  mocks.register.mockImplementation(async () => { mocks.onRegistration({ value: "device-token" }); });
  mocks.unregister.mockResolvedValue(undefined);
});

it.each([false, true])("restores notifications only with saved opt-in=%s", async (optedIn) => {
  mocks.optedIn = optedIn;
  const { restorePush } = await import("./push");
  await restorePush();
  expect(mocks.registerDevice).toHaveBeenCalledTimes(optedIn ? 1 : 0);
});

it("keeps opt-out and deletes the server row when registration finishes late", async () => {
  const server = deferred<void>();
  mocks.registerDevice.mockReturnValueOnce(server.promise);
  const { enablePush, disablePush, notificationState } = await import("./push");
  const registration = enablePush();
  await vi.waitFor(() => expect(mocks.registerDevice).toHaveBeenCalled());
  const disabling = disablePush();
  expect(await notificationState()).toBe("off");
  server.resolve();
  await disabling;
  expect(await registration).toBe("unavailable");
  expect(await notificationState()).toBe("off");
  expect(mocks.unregisterDevice).toHaveBeenCalledWith("device-token");
  expect(mocks.setOptedIn).not.toHaveBeenCalled();
});

it("clears a preference write that was already in flight", async () => {
  const preference = deferred<void>();
  mocks.setOptedIn.mockImplementationOnce(async () => {
    await preference.promise;
    mocks.optedIn = true;
  });
  const { enablePush, disablePush, notificationState } = await import("./push");
  const registration = enablePush();
  await vi.waitFor(() => expect(mocks.setOptedIn).toHaveBeenCalled());
  const disabling = disablePush();
  preference.resolve();
  await Promise.all([registration, disabling]);
  expect(await notificationState()).toBe("off");
  expect(mocks.optedIn).toBe(false);
});

it("cancels while checking permission without waiting for the bridge", async () => {
  const permission = deferred<{ receive: string }>();
  mocks.checkPermissions.mockReturnValueOnce(permission.promise);
  const { enablePush, disablePush } = await import("./push");
  const registration = enablePush();
  await vi.waitFor(() => expect(mocks.checkPermissions).toHaveBeenCalled());
  await disablePush();
  permission.resolve({ receive: "prompt" });
  expect(await registration).toBe("unavailable");
  expect(mocks.requestPermissions).not.toHaveBeenCalled();
  expect(mocks.registerDevice).not.toHaveBeenCalled();
});

it("ignores a saved opt-in read that completes after sign-out", async () => {
  const preference = deferred<boolean>();
  mocks.hasOptedIn.mockReturnValueOnce(preference.promise);
  const { restorePush, disablePush } = await import("./push");
  const restoring = restorePush();
  await disablePush();
  preference.resolve(true);
  await restoring;
  expect(mocks.registerDevice).not.toHaveBeenCalled();
});

it("discards a stale preference read when reporting notification state", async () => {
  const preference = deferred<boolean>();
  mocks.hasOptedIn.mockReturnValueOnce(preference.promise);
  const { notificationState, disablePush } = await import("./push");
  const reading = notificationState();
  await vi.waitFor(() => expect(mocks.hasOptedIn).toHaveBeenCalled());
  await disablePush();
  preference.resolve(true);
  expect(await reading).toBe("off");
});

it("allows an explicit re-enable after canceled registration has been cleaned up", async () => {
  const server = deferred<void>();
  mocks.registerDevice.mockReturnValueOnce(server.promise);
  const { enablePush, disablePush, notificationState } = await import("./push");
  const oldRegistration = enablePush();
  await vi.waitFor(() => expect(mocks.registerDevice).toHaveBeenCalled());
  const disabling = disablePush();
  const newRegistration = enablePush();
  server.resolve();
  await Promise.all([oldRegistration, disabling]);
  expect(await newRegistration).toBe("granted");
  expect(await notificationState()).toBe("granted");
  expect(mocks.registerDevice).toHaveBeenCalledTimes(2);
  expect(mocks.unregisterDevice).toHaveBeenCalledTimes(1);
});
