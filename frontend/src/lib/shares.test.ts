// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";

const nativeState = vi.hoisted(() => ({ current: false }));

vi.mock("./nativeAuth", () => ({
  get IS_NATIVE() {
    return nativeState.current;
  },
  authHeaders: () => ({}),
}));

import { BASE } from "./api";
import { createShare, shareOrigin } from "./shares";

const entityKey = "0x" + "ab".repeat(32);
const txHash = "0x" + "cd".repeat(32);

afterEach(() => {
  nativeState.current = false;
  vi.unstubAllGlobals();
});

it("posts the bytes as octet-stream and returns the key and transaction for a Rabdi receipt", async () => {
  const bytes = new Uint8Array([1, 2, 3]);
  const fetchMock = vi.fn(async () =>
    new Response(JSON.stringify({ entityKey, chainId: 7794095, txHash }), { status: 201 }),
  );
  vi.stubGlobal("fetch", fetchMock);

  await expect(createShare(bytes)).resolves.toEqual({ entityKey, chainId: 7794095, txHash });

  expect(fetchMock).toHaveBeenCalledTimes(1);
  const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
  expect(url).toBe(`${BASE}/shares`);
  expect(init.method).toBe("POST");
  expect(new Headers(init.headers).get("content-type")).toBe("application/octet-stream");
  expect(init.body).toBe(bytes);
});

it("does not post from a read-only client", async () => {
  nativeState.current = true;
  const fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
  await expect(createShare(new Uint8Array([1]))).rejects.toThrow(
    "Workflows can only be edited in the AgentMesh desktop app.",
  );
  expect(fetchMock).not.toHaveBeenCalled();
});

it("keeps a rate limit and a disabled share distinct from an unconfirmed write", async () => {
  const fetchMock = vi.fn(async () => new Response(JSON.stringify({ error: "share_rate" }), { status: 429 }));
  vi.stubGlobal("fetch", fetchMock);
  await expect(createShare(new Uint8Array([1]))).rejects.toThrow("share_rate");

  fetchMock.mockResolvedValue(new Response(JSON.stringify({ error: "share_disabled" }), { status: 503 }));
  await expect(createShare(new Uint8Array([1]))).rejects.toThrow("share_disabled");
});

it("rejects a Rabdi receipt that has no transaction hash", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ entityKey, chainId: 7794095 }), { status: 201 })),
  );
  await expect(createShare(new Uint8Array([1]))).rejects.toThrow("share_unconfirmed");
});

it("throws on 502 and does not return the key from the body", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ entityKey }), { status: 502 })),
  );
  vi.stubGlobal("window", { location: { origin: "capacitor://localhost" } });

  await expect(createShare(new Uint8Array([1]))).rejects.toThrow();
  try {
    await createShare(new Uint8Array([1]));
  } catch (err) {
    expect(String(err)).not.toContain(entityKey);
  }
});

it("uses the public web origin on the native shell", () => {
  nativeState.current = true;
  vi.stubGlobal("window", { location: { origin: "capacitor://localhost" } });
  expect(shareOrigin()).toBe("https://www.agent-mesh.app");
});
