import { beforeEach, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  permission: vi.fn(), register: vi.fn(), nativeClear: vi.fn(),
  queueClear: vi.fn(), removeListener: vi.fn(), addListener: vi.fn(),
  pending: vi.fn(), pushFix: vi.fn(), queueRemove: vi.fn(), save: vi.fn(), unregister: vi.fn(), clearServer: vi.fn(),
  fences: new Set<string>(), listeners: new Set<symbol>(), queue: [] as string[],
}));
vi.mock("@capacitor/network", () => ({ Network: { addListener: state.addListener } }));
vi.mock("./nativeGeofence", () => ({ Geofence: {
  hasPermission: state.permission, addGeofence: state.register, clearAccountData: state.nativeClear,
  removeGeofence: state.unregister,
} }));
vi.mock("./queue", () => ({ pending: state.pending, clear: state.queueClear, remove: state.queueRemove }));
vi.mock("./api", () => ({ pushFix: state.pushFix, setGeofence: state.save, clearGeofence: state.clearServer }));

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function listener() {
  const id = Symbol("listener");
  state.listeners.add(id);
  return { remove: async () => { await state.removeListener(); state.listeners.delete(id); } };
}

let geofences: typeof import("./geofence");
const owned = { workflowId: "owned", lat: 1, lng: 2, radiusM: 200 };

beforeEach(async () => {
  vi.resetAllMocks();
  vi.resetModules();
  state.fences.clear();
  state.listeners.clear();
  state.queue = [];
  state.permission.mockResolvedValue({ granted: true });
  state.register.mockImplementation(async ({ id }: { id: string }) => { state.fences.add(id); });
  state.unregister.mockImplementation(async ({ id }: { id: string }) => { state.fences.delete(id); });
  state.nativeClear.mockImplementation(async () => { state.fences.clear(); });
  state.queueClear.mockImplementation(async () => { state.queue = []; });
  state.pending.mockResolvedValue([]);
  state.addListener.mockImplementation(async () => listener());
  geofences = await import("./geofence");
});

function expectEmpty() {
  expect([...state.fences]).toEqual([]);
  expect([...state.listeners]).toEqual([]);
  expect(state.queue).toEqual([]);
}

it("disarms persisted fences and clears queued fixes even when the bridge reports failure", async () => {
  await geofences.start(owned);
  state.nativeClear.mockRejectedValueOnce(new Error("OS unavailable"));
  await expect(geofences.clearAccountGeofences()).rejects.toThrow("Location cleanup incomplete");
  expect(state.nativeClear).toHaveBeenCalledOnce();
  expect(state.queueClear).toHaveBeenCalledOnce();
  expect(state.removeListener).toHaveBeenCalledOnce();
  await geofences.clearAccountGeofences();
  expect(state.removeListener).toHaveBeenCalledOnce();
  expectEmpty();
});

it("cancels a start paused before native registration when the account is cleared", async () => {
  const permission = deferred<{ granted: boolean }>();
  state.permission.mockReturnValueOnce(permission.promise);
  state.queue = ["old location"];
  const start = geofences.start(owned);
  await vi.waitFor(() => expect(state.permission).toHaveBeenCalledOnce());
  await geofences.clearAccountGeofences();
  permission.resolve({ granted: true });
  await expect(start).resolves.toBe(false);
  expect(state.register).not.toHaveBeenCalled();
  expect(state.addListener).not.toHaveBeenCalled();
  expect(state.pending).not.toHaveBeenCalled();
  expectEmpty();
});

it("cancels registration while its server save is pending", async () => {
  const saved = deferred<void>();
  state.save.mockReturnValueOnce(saved.promise);
  const { shell } = await import("./index");
  const start = shell.setGeofence(owned.workflowId, owned);
  await vi.waitFor(() => expect(state.save).toHaveBeenCalledOnce());
  await geofences.clearAccountGeofences();
  saved.resolve(undefined);
  await expect(start).resolves.toBe(false);
  expect(state.permission).not.toHaveBeenCalled();
  expect(state.register).not.toHaveBeenCalled();
  expectEmpty();
});

it.each(["resolve", "reject"] as const)("ignores a stale native registration that later %ss", async (result) => {
  const registration = deferred<void>();
  state.register.mockImplementationOnce(({ id }: { id: string }) => {
    state.fences.add(id);
    return registration.promise;
  });
  const start = geofences.start(owned);
  await vi.waitFor(() => expect(state.register).toHaveBeenCalledOnce());
  await geofences.clearAccountGeofences();
  if (result === "resolve") registration.resolve(undefined);
  else registration.reject(new Error("registration cancelled"));
  await expect(start).resolves.toBe(false);
  expect(state.addListener).not.toHaveBeenCalled();
  expectEmpty();
});

it("removes a listener whose creation finishes during cleanup", async () => {
  const added = deferred<ReturnType<typeof listener>>();
  state.addListener.mockReturnValueOnce(added.promise);
  const start = geofences.start(owned);
  await vi.waitFor(() => expect(state.addListener).toHaveBeenCalledOnce());
  const clearing = geofences.clearAccountGeofences();
  added.resolve(listener());
  await clearing;
  await expect(start).resolves.toBe(false);
  expect(state.removeListener).toHaveBeenCalledOnce();
  expectEmpty();
});

it("waits for cleanup before a new account registers and ignores the old completion", async () => {
  const oldRegistration = deferred<void>();
  const nativeCleanup = deferred<void>();
  state.register.mockReturnValueOnce(oldRegistration.promise);
  const oldStart = geofences.start(owned);
  await vi.waitFor(() => expect(state.register).toHaveBeenCalledOnce());
  state.nativeClear.mockReturnValueOnce(nativeCleanup.promise);
  const clearing = geofences.clearAccountGeofences();
  const newStart = geofences.start({ ...owned, workflowId: "new-account" });
  await vi.waitFor(() => expect(state.nativeClear).toHaveBeenCalledOnce());
  expect(state.register).toHaveBeenCalledOnce();
  nativeCleanup.resolve(undefined);
  await clearing;
  await expect(newStart).resolves.toBe(true);
  oldRegistration.resolve(undefined);
  await expect(oldStart).resolves.toBe(false);
  expect([...state.fences]).toEqual(["new-account"]);
  expect(state.listeners.size).toBe(1);
  expect(state.addListener).toHaveBeenCalledOnce();
  await geofences.clearAccountGeofences();
  expectEmpty();
});

it("can retry cleanup after listener removal fails without losing the handle", async () => {
  await geofences.start(owned);
  const notify = state.addListener.mock.calls[0][1];
  state.removeListener.mockRejectedValueOnce(new Error("listener unavailable"));
  await expect(geofences.clearAccountGeofences()).rejects.toThrow("Location cleanup incomplete");
  notify({ connected: true });
  expect(state.pending).toHaveBeenCalledOnce();
  await geofences.clearAccountGeofences();
  expect(state.removeListener).toHaveBeenCalledTimes(2);
  expectEmpty();
  await expect(geofences.start({ ...owned, workflowId: "new-account" })).resolves.toBe(true);
  await geofences.clearAccountGeofences();
});

it("blocks new registration until a failed native cleanup is successfully retried", async () => {
  state.nativeClear.mockRejectedValueOnce(new Error("OS unavailable"));
  await expect(geofences.clearAccountGeofences()).rejects.toThrow("Location cleanup incomplete");
  await expect(geofences.start(owned)).rejects.toThrow("Location cleanup incomplete");
  expect(state.register).not.toHaveBeenCalled();
  await geofences.clearAccountGeofences();
  await expect(geofences.start(owned)).resolves.toBe(true);
  await geofences.clearAccountGeofences();
  expectEmpty();
});

it("returns false without registration when permission is denied", async () => {
  state.permission.mockResolvedValueOnce({ granted: false });
  await expect(geofences.start(owned)).resolves.toBe(false);
  expect(state.register).not.toHaveBeenCalled();
  expectEmpty();
});

it("propagates a current registration error and permits a later successful start", async () => {
  state.register.mockRejectedValueOnce(new Error("permission revoked"));
  await expect(geofences.start(owned)).rejects.toThrow("permission revoked");
  expectEmpty();
  await expect(geofences.start(owned)).resolves.toBe(true);
  await geofences.clearAccountGeofences();
  expectEmpty();
});

it("does not send more old queued locations after cleanup starts", async () => {
  const firstPush = deferred<void>();
  state.pending.mockResolvedValueOnce([{ workflowId: "old-1" }, { workflowId: "old-2" }]);
  state.pushFix.mockReturnValueOnce(firstPush.promise);
  const flushing = geofences.flush();
  await vi.waitFor(() => expect(state.pushFix).toHaveBeenCalledOnce());
  await geofences.clearAccountGeofences();
  firstPush.resolve(undefined);
  await flushing;
  expect(state.pushFix).toHaveBeenCalledOnce();
  expect(state.queueRemove).not.toHaveBeenCalled();
  expectEmpty();
});

it("waits for cleanup before a direct flush can read the queue", async () => {
  const nativeCleanup = deferred<void>();
  state.nativeClear.mockReturnValueOnce(nativeCleanup.promise);
  const clearing = geofences.clearAccountGeofences();
  await vi.waitFor(() => expect(state.nativeClear).toHaveBeenCalledOnce());
  const flushing = geofences.flush();
  expect(state.pending).not.toHaveBeenCalled();
  nativeCleanup.resolve(undefined);
  await clearing;
  await flushing;
  expect(state.pending).toHaveBeenCalledOnce();
  expectEmpty();
});

it("does not remove a new account listener when an old stop completes", async () => {
  await geofences.start(owned);
  const removed = deferred<void>();
  state.unregister.mockReturnValueOnce(removed.promise);
  const stopping = geofences.stop(owned.workflowId);
  await geofences.clearAccountGeofences();
  await geofences.start(owned);
  removed.resolve(undefined);
  await stopping;
  expect(state.listeners.size).toBe(1);
  expect([...state.fences]).toEqual([owned.workflowId]);
  await geofences.clearAccountGeofences();
  expectEmpty();
});

it("cancels a pending server removal before it can disarm a later account with the same ID", async () => {
  await geofences.start(owned);
  const response = deferred<void>();
  state.clearServer.mockReturnValueOnce(response.promise);
  const { shell } = await import("./index");
  const removing = shell.clearGeofence(owned.workflowId);
  await vi.waitFor(() => expect(state.clearServer).toHaveBeenCalledWith(owned.workflowId));
  expect(state.unregister).not.toHaveBeenCalled();
  await geofences.clearAccountGeofences();
  await geofences.start({ ...owned, lat: 55 });
  const currentListeners = [...state.listeners];
  response.resolve(undefined);
  await removing;
  expect(state.unregister).not.toHaveBeenCalled();
  expect([...state.fences]).toEqual([owned.workflowId]);
  expect([...state.listeners]).toEqual(currentListeners);
  expect(state.listeners.size).toBe(1);
  expect(state.removeListener).toHaveBeenCalledOnce();
  await geofences.clearAccountGeofences();
  expectEmpty();
});

it("preserves server-first ordering for a current removal", async () => {
  await geofences.start(owned);
  const response = deferred<void>();
  state.clearServer.mockReturnValueOnce(response.promise);
  const { shell } = await import("./index");
  const removing = shell.clearGeofence(owned.workflowId);
  await vi.waitFor(() => expect(state.clearServer).toHaveBeenCalledWith(owned.workflowId));
  expect(state.unregister).not.toHaveBeenCalled();
  expect([...state.fences]).toEqual([owned.workflowId]);
  expect(state.listeners.size).toBe(1);
  response.resolve(undefined);
  await removing;
  expect(state.unregister).toHaveBeenCalledExactlyOnceWith({ id: owned.workflowId });
  expectEmpty();
});

it("preserves the native fence and listener after a server removal failure", async () => {
  await geofences.start(owned);
  state.clearServer.mockRejectedValueOnce(new Error("server unavailable"));
  const { shell } = await import("./index");
  await expect(shell.clearGeofence(owned.workflowId)).rejects.toThrow("server unavailable");
  expect(state.unregister).not.toHaveBeenCalled();
  expect([...state.fences]).toEqual([owned.workflowId]);
  expect(state.listeners.size).toBe(1);
  expect(state.removeListener).not.toHaveBeenCalled();
  await geofences.clearAccountGeofences();
  expectEmpty();
});
