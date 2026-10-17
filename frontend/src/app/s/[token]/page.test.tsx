// @vitest-environment node
import { afterEach, beforeEach, expect, it, vi } from "vitest";

vi.mock("@/components/share/SharePreview", () => ({
  SharePreview: () => null,
}));
vi.mock("next/og", () => ({
  ImageResponse: class {
    constructor(public element: unknown) {}
  },
}));

const request = vi.hoisted(() => ({ headers: vi.fn() }));
vi.mock("next/headers", () => ({ headers: request.headers }));

const fetchMock = vi.fn();
const params = Promise.resolve({ token: "new-token-after-build" });

beforeEach(() => {
  vi.resetModules();
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  vi.stubEnv("BACKEND_URL", "https://backend.test");
  vi.stubEnv("VERCEL", "1");
  vi.stubEnv("SHARE_READ_RELAY_KEY", "01".repeat(32));
  request.headers.mockReset().mockResolvedValue(
    new Headers({ "x-vercel-forwarded-for": "198.51.100.7" }),
  );
  vi.stubEnv("MOBILE_BUILD", "0");
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

it("renders web shares without opting new tokens into static generation", async () => {
  const page = await import("./page");
  const image = await import("./opengraph-image");
  expect(page.generateStaticParams).toBeUndefined();
  expect(image.generateStaticParams).toBeUndefined();
});

it("refreshes metadata after revocation without retaining the shared name", async () => {
  fetchMock
    .mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        share: { name: "Private snapshot", nodeCount: 1, edgeCount: 0 },
      }),
    })
    .mockResolvedValueOnce({ ok: false });
  const { generateMetadata } = await import("./page");
  expect((await generateMetadata({ params })).title).toBe(
    "Private snapshot, shared on AgentMesh",
  );
  const revoked = await generateMetadata({ params });
  expect(revoked.title).toBe("Shared workflow");
  expect(JSON.stringify(revoked)).not.toContain("Private snapshot");
  expect(fetchMock).toHaveBeenCalledTimes(2);
  for (const call of fetchMock.mock.calls) {
    expect(call).toEqual([
      "https://backend.test/shares/new-token-after-build",
      expect.objectContaining({
        cache: "no-store",
        redirect: "error",
        headers: expect.objectContaining({ "X-AgentMesh-Share-Visitor": "198.51.100.7" }),
      }),
    ]);
  }
});

it("refreshes the preview image after revocation", async () => {
  fetchMock
    .mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        share: { name: "Private snapshot", nodeCount: 1, edgeCount: 0 },
      }),
    })
    .mockResolvedValueOnce({ ok: false });
  const { default: image } = await import("./opengraph-image");
  expect(JSON.stringify(await image({ params }))).toContain("Private snapshot");
  expect(JSON.stringify(await image({ params }))).not.toContain("Private snapshot");
  expect(fetchMock).toHaveBeenCalledTimes(2);
  expect(fetchMock.mock.calls.every((call) => call[1].cache === "no-store")).toBe(true);
});

it("exports the mobile shell without a server lookup", async () => {
  vi.stubEnv("MOBILE_BUILD", "1");
  const page = await import("./page");
  const image = await import("./opengraph-image");
  expect(page.generateStaticParams?.()).toEqual([{ token: "app" }]);
  expect(image.generateStaticParams?.()).toEqual([{ token: "app" }]);
  expect((await page.generateMetadata({ params })).title).toBe("Shared workflow");
  await page.default({ params });
  await image.default({ params });
  expect(fetchMock).not.toHaveBeenCalled();
  expect(request.headers).not.toHaveBeenCalled();
});


it("keeps metadata, server content and OG independent for visitors sharing one Next process", async () => {
  const reads = new Map<string, number>();
  fetchMock.mockImplementation(async (_url: string, options: RequestInit) => {
    const visitor = new Headers(options.headers).get("X-AgentMesh-Share-Visitor") ?? "shared-egress";
    const count = (reads.get(visitor) ?? 0) + 1;
    reads.set(visitor, count);
    return {
      ok: count <= 60,
      json: async () => ({
        share: { name: "Independent visitor snapshot", nodeCount: 2, edgeCount: 1 },
      }),
    };
  });
  const page = await import("./page");
  const image = await import("./opengraph-image");
  request.headers.mockResolvedValue(new Headers({ "x-vercel-forwarded-for": "198.51.100.7" }));
  for (let count = 0; count < 60; count++) await page.generateMetadata({ params });
  expect((await page.generateMetadata({ params })).title).toBe("Shared workflow");
  expect(JSON.stringify(await page.default({ params }))).not.toContain("Independent visitor snapshot");
  expect(JSON.stringify(await image.default({ params }))).not.toContain("Independent visitor snapshot");

  request.headers.mockResolvedValue(new Headers({ "x-vercel-forwarded-for": "198.51.100.8" }));
  expect((await page.generateMetadata({ params })).title).toBe(
    "Independent visitor snapshot, shared on AgentMesh",
  );
  expect(JSON.stringify(await page.default({ params }))).toContain("Independent visitor snapshot");
  expect(JSON.stringify(await image.default({ params }))).toContain("Independent visitor snapshot");
  expect(reads.get("198.51.100.8")).toBe(3);
  expect(reads.has("shared-egress")).toBe(false);
});
