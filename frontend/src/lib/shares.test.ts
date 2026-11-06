// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";

vi.mock("./nativeAuth", () => ({
  IS_NATIVE: true,
  authHeaders: () => ({}),
}));

import { BASE } from "./api";
import { createShare, shareOrigin } from "./shares";

const entityKey = "0x" + "ab".repeat(32);

afterEach(() => {
  vi.unstubAllGlobals();
});

it("posts the bytes as octet-stream and returns the key only for a Rabdi receipt", async () => {
  const bytes = new Uint8Array([1, 2, 3]);
  const fetchMock = vi.fn(async () =>
    new Response(JSON.stringify({ entityKey, chainId: 7794095 }), { status: 201 }),
  );
  vi.stubGlobal("fetch", fetchMock);

  await expect(createShare(bytes)).resolves.toEqual({ entityKey, chainId: 7794095 });

  expect(fetchMock).toHaveBeenCalledTimes(1);
  const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
  expect(url).toBe(`${BASE}/shares`);
  expect(init.method).toBe("POST");
  expect(new Headers(init.headers).get("content-type")).toBe("application/octet-stream");
  expect(init.body).toBe(bytes);
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
  vi.stubGlobal("window", { location: { origin: "capacitor://localhost" } });
  expect(shareOrigin()).toBe("https://www.agent-mesh.app");
});
