import { beforeEach, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";

const state = vi.hoisted(() => ({
  values: new Map<string, string>(),
  exchange: vi.fn(async () => "session"),
  open: vi.fn<(options: { url: string }) => Promise<void>>(),
  close: vi.fn(async () => {}),
  addListener: vi.fn(),
  getLaunchUrl: vi.fn(),
}));
vi.mock("./secureStore", () => ({ SecureStore: {
  get: async ({ key }: { key: string }) => ({ value: state.values.get(key) ?? null }),
  set: async ({ key, value }: { key: string; value: string }) => { state.values.set(key, value); },
  remove: async ({ key }: { key: string }) => { state.values.delete(key); },
} }));
vi.mock("@capacitor/app", () => ({ App: state }));
vi.mock("@capacitor/browser", () => ({ Browser: state }));
vi.mock("@/lib/api", () => ({ auth: {
  nativeOAuthURL: async (provider: string) => `https://app.test/api/auth/oauth/${provider}`,
  oauthExchange: state.exchange,
} }));

const scheme = readFileSync("../mobile/capacitor.config.ts", "utf8").match(/appId:\s*"([^"]+)"/)![1];
const callback = `${scheme}://auth?code=launch-code`;
const key = "agentmesh.oauth.verifier";

beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  state.values.clear();
  state.open.mockResolvedValue(undefined);
  state.getLaunchUrl.mockResolvedValue(undefined);
  state.addListener.mockResolvedValue({ remove: vi.fn(async () => {}) });
});

it("persists before opening the frontend origin and completes after a fresh module load", async () => {
  const { start } = await import("./oauth");
  state.open.mockImplementationOnce(async ({ url }) => {
    expect(state.values.get(key)).toHaveLength(64);
    const startURL = new URL(url);
    expect(startURL.origin).toBe("https://app.test");
    expect(startURL.pathname).toBe("/api/auth/oauth/google");
    expect(startURL.searchParams.get("challenge")).toHaveLength(43);
  });
  await start("google");
  const verifier = state.values.get(key);
  vi.resetModules();
  state.getLaunchUrl.mockResolvedValue({ url: callback });
  const { listenForCallback } = await import("./oauth");
  const result = vi.fn();
  await listenForCallback(result);
  expect(result).toHaveBeenCalledWith({ ok: true, token: "session" });
  expect(state.exchange).toHaveBeenCalledWith("launch-code", verifier);
  expect(state.values.has(key)).toBe(false);
  vi.resetModules();
  const reloaded = await import("./oauth");
  const repeated = vi.fn();
  await reloaded.listenForCallback(repeated);
  expect(repeated).not.toHaveBeenCalled();
  expect(state.exchange).toHaveBeenCalledTimes(1);
});

it("handles a callback once when the initial intent and a live event overlap", async () => {
  state.values.set(key, "verifier");
  let fire: (event: { url: string }) => void = () => {};
  state.addListener.mockImplementation(async (_name, listener) => {
    fire = listener;
    return { remove: vi.fn() };
  });
  state.getLaunchUrl.mockImplementation(async () => {
    fire({ url: callback });
    return { url: callback };
  });
  const { listenForCallback } = await import("./oauth");
  const result = vi.fn();
  await Promise.all([listenForCallback(result), listenForCallback(result)]);
  await vi.waitFor(() => expect(result).toHaveBeenCalledTimes(1));
  expect(state.addListener).toHaveBeenCalledTimes(1);
  expect(state.exchange).toHaveBeenCalledTimes(1);
});

it("clears pending storage if opening the browser fails", async () => {
  state.open.mockRejectedValueOnce(new Error("browser unavailable"));
  const { start } = await import("./oauth");
  await expect(start("github")).rejects.toThrow("browser unavailable");
  expect(state.values.has(key)).toBe(false);
});

it("ignores a lookalike callback host without consuming the pending verifier", async () => {
  state.values.set(key, "verifier");
  state.getLaunchUrl.mockResolvedValue({ url: `${scheme}://auth-other?code=x` });
  const { listenForCallback } = await import("./oauth");
  const result = vi.fn();
  await listenForCallback(result);
  expect(result).not.toHaveBeenCalled();
  expect(state.values.get(key)).toBe("verifier");
});
