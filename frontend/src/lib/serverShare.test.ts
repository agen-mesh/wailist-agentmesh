// @vitest-environment node
import { createHmac } from "node:crypto";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const request = vi.hoisted(() => ({ headers: vi.fn() }));
vi.mock("next/headers", () => ({ headers: request.headers }));
import { loadServerShare } from "./serverShare";

const key = "01".repeat(32);
const fetchMock = vi.fn();
const data = { share: { name: "Current snapshot", nodeCount: 2, edgeCount: 1 } };

beforeEach(() => {
  vi.stubEnv("VERCEL", "1");
  vi.stubEnv("MOBILE_BUILD", "0");
  vi.stubEnv("BACKEND_URL", "https://backend.test");
  vi.stubEnv("SHARE_READ_RELAY_KEY", key);
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset().mockResolvedValue({ ok: true, json: async () => data });
  request.headers.mockReset().mockResolvedValue(
    new Headers({
      "x-vercel-forwarded-for": "198.51.100.7",
      "x-forwarded-for": "203.0.113.99",
      cookie: "private-cookie",
      authorization: "private-authorization",
    }),
  );
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("server share relay", () => {
  it("signs the sanitized Vercel visitor and exact request without forwarding credentials", async () => {
    vi.spyOn(Date, "now").mockReturnValue(1792244800_000);
    expect(await loadServerShare("contract-token")).toEqual(data);
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith(
      "https://backend.test/shares/contract-token",
      {
        cache: "no-store",
        redirect: "error",
        headers: {
          "X-AgentMesh-Share-Visitor": "198.51.100.7",
          "X-AgentMesh-Share-Timestamp": "1792244800",
          "X-AgentMesh-Share-Signature": "86fb6cb9f2bd460726bf21b54cc2b6559b7e9b33c463e57eb178d5768bee5a60",
        },
      },
    );
    expect(JSON.stringify(await loadServerShare("contract-token"))).not.toContain(key);
  });

  it("binds encoded token paths, including delimiters, to the signature", async () => {
    vi.spyOn(Date, "now").mockReturnValue(1792244800_000);
    await loadServerShare("token?different/path");
    const path = "/shares/token%3Fdifferent%2Fpath";
    const signature = createHmac("sha256", Buffer.from(key, "hex"))
      .update(["v1", "GET", path, "198.51.100.7", "1792244800"].join("\n"))
      .digest("hex");
    expect(fetchMock).toHaveBeenCalledWith(
      `https://backend.test${path}`,
      expect.objectContaining({
        headers: expect.objectContaining({ "X-AgentMesh-Share-Signature": signature }),
      }),
    );
  });

  it.each([
    ["2001:0DB8:0000:0000:0000:0000:0000:0007", "2001:db8::7"],
    ["::ffff:192.0.2.7", "192.0.2.7"],
    ["192.0.2.7", "192.0.2.7"],
  ])("canonicalizes %s as %s", async (input, expected) => {
    request.headers.mockResolvedValue(new Headers({ "x-vercel-forwarded-for": input }));
    await loadServerShare("current");
    expect(fetchMock).toHaveBeenCalledWith(
      "https://backend.test/shares/current",
      expect.objectContaining({
        headers: expect.objectContaining({ "X-AgentMesh-Share-Visitor": expected }),
      }),
    );
  });

  it.each(["", "not-an-ip", "198.51.100.1, 198.51.100.2", "fe80::1%eth0"])(
    "refuses untrusted or invalid visitor %s without borrowing generic forwarded headers",
    async (visitor) => {
      request.headers.mockResolvedValue(new Headers({
        "x-vercel-forwarded-for": visitor,
        "x-forwarded-for": "198.51.100.7",
        "x-real-ip": "198.51.100.7",
      }));
      expect(await loadServerShare("current")).toBeNull();
      expect(fetchMock).not.toHaveBeenCalled();
    },
  );

  it.each(["", "true", "0"])("does not trust matching header names off Vercel (%s)", async (vercel) => {
    vi.stubEnv("VERCEL", vercel);
    expect(await loadServerShare("current")).toBeNull();
    expect(request.headers).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each(["", "short", "gg".repeat(32), "01".repeat(31)])(
    "fails closed without a valid dedicated key",
    async (relayKey) => {
      vi.stubEnv("SHARE_READ_RELAY_KEY", relayKey);
      expect(await loadServerShare("current")).toBeNull();
      expect(fetchMock).not.toHaveBeenCalled();
    },
  );

  it.each([
    "http://backend.test", "https://user:password@backend.test",
    "https://backend.test/base", "https://backend.test?key=value",
    "https://backend.test/#fragment", "file:///tmp/backend",
  ])("does not send relay authentication to unsafe backend %s", async (base) => {
    vi.stubEnv("BACKEND_URL", base);
    expect(await loadServerShare("current")).toBeNull();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("allows an explicit loopback backend for isolated verification", async () => {
    vi.stubEnv("BACKEND_URL", "http://127.0.0.1:13161");
    expect(await loadServerShare("current")).toEqual(data);
    expect(fetchMock).toHaveBeenCalledWith(
      "http://127.0.0.1:13161/shares/current",
      expect.objectContaining({ redirect: "error" }),
    );
  });

  it("keeps the mobile export independent of request headers and secrets", async () => {
    vi.stubEnv("MOBILE_BUILD", "1");
    expect(await loadServerShare("app")).toBeNull();
    expect(request.headers).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("keeps an expired, revoked, rate-limited or failed upstream neutral", async () => {
    for (const status of [401, 404, 429, 500]) {
      fetchMock.mockResolvedValueOnce({ ok: false, status });
      expect(await loadServerShare("current")).toBeNull();
    }
    fetchMock.mockRejectedValueOnce(new Error("upstream unavailable"));
    expect(await loadServerShare("current")).toBeNull();
  });
});
