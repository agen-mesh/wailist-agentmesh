import { createHash, createHmac, randomUUID } from "node:crypto";
import { readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { expect, request, test, type APIRequestContext, type BrowserContext, type Page } from "@playwright/test";

const backend = `http://127.0.0.1:${Number(process.env.SMOKE_API_PORT ?? 3218)}`;
const visitorBOrigin = `http://127.0.0.1:${Number(
  process.env.SMOKE_VISITOR_B_PORT ?? Number(process.env.SMOKE_SERVER_PORT ?? 3227) + 2,
)}`;

async function mintScratchShare(owner: APIRequestContext) {
  const build = join(__dirname, "../.next/BUILD_ID");
  const started = Date.now();
  expect(readFileSync(build, "utf8").trim()).not.toBe("");
  expect(statSync(build).mtimeMs).toBeLessThanOrEqual(started);
  const marker = randomUUID();
  const name = `Isolated visitor share ${marker}`;
  const description = `Fresh private snapshot ${marker}`;
  expect((await owner.post("/auth/signup", {
    data: { email: `${marker}@example.test`, password: randomUUID(), name: "Relay owner", org: "Scratch" },
  })).ok()).toBe(true);
  const created = await owner.post("/workflows", { data: { name } });
  expect(created.status()).toBe(201);
  const workflow = await created.json();
  expect((await owner.put(`/workflows/${workflow.id}`, {
    data: {
      name, description,
      nodes: [{ id: "manual", type: "trigger", label: "Manual start", triggerType: "manual", x: 0, y: 0 }],
      edges: [],
    },
  })).status()).toBe(200);
  const minted = await owner.post(`/workflows/${workflow.id}/share`, { data: {} });
  expect(minted.status()).toBe(201);
  const { share } = await minted.json();
  if (typeof share.token !== "string" || typeof workflow.id !== "string") {
    throw new Error("Scratch workflow/share did not return identifiers");
  }
  expect(share.token).toMatch(/^[A-Za-z0-9_-]{16,64}$/);
  expect(share.name).toBe(name);
  expect(Date.parse(share.createdAt)).toBeGreaterThanOrEqual(started - 1000);
  return { token: share.token, workflowId: workflow.id, name, description, marker };
}

async function imagePath(page: Page, token: string) {
  const value = await page.locator('meta[property="og:image"]').getAttribute("content");
  if (!value) throw new Error("Share metadata did not advertise its image route");
  const url = new URL(value, page.url());
  expect(url.pathname).toMatch(new RegExp(`^/s/${token}/opengraph-image(?:/|$)`));
  // Keep the advertised route on the scratch ingress, never the production host.
  return url.pathname + url.search;
}

async function imageDigest(context: BrowserContext, path: string) {
  const response = await context.request.get(path);
  expect(response.status()).toBe(200);
  expect(response.headers()["content-type"]).toContain("image/png");
  const cache = response.headers()["cache-control"] ?? "";
  expect(cache.includes("no-store") || cache.includes("no-cache") ||
    (cache.includes("max-age=0") && cache.includes("must-revalidate"))).toBe(true);
  expect(cache).not.toMatch(/\b(?:s-maxage|max-age)=[1-9]\d*/);
  const bytes = await response.body();
  expect(bytes.subarray(0, 8)).toEqual(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]));
  expect(bytes.readUInt32BE(16)).toBe(1200);
  expect(bytes.readUInt32BE(20)).toBe(630);
  return createHash("sha256").update(bytes).digest("hex");
}

function signedHeaders(token: string, visitor = "192.0.2.206") {
  const key = process.env.SMOKE_SHARE_READ_RELAY_KEY;
  if (!key || !/^[a-fA-F0-9]{64}$/.test(key)) {
    throw new Error("Missing disposable relay key from the server-test configuration");
  }
  const timestamp = String(Math.floor(Date.now() / 1000));
  const signature = createHmac("sha256", Buffer.from(key, "hex"))
    .update(["v1", "GET", `/shares/${encodeURIComponent(token)}`, visitor, timestamp].join("\n"))
    .digest("hex");
  return {
    "X-AgentMesh-Share-Visitor": visitor,
    "X-AgentMesh-Share-Timestamp": timestamp,
    "X-AgentMesh-Share-Signature": signature,
  };
}

test("a newly minted share has anonymous server content and uncached revocation", async ({ page, context }) => {
  const build = join(__dirname, "../.next/BUILD_ID");
  expect(readFileSync(build, "utf8").trim()).not.toBe("");
  const started = Date.now();
  expect(statSync(build).mtimeMs).toBeLessThanOrEqual(started);
  const marker = randomUUID();
  const name = `Fresh server share ${marker}`;
  const description = `Created after the production build ${marker}`;
  const owner = await request.newContext({ baseURL: backend });
  const anonymous = await request.newContext({ baseURL: backend });
  try {
    const signup = await owner.post("/auth/signup", {
      data: { email: `${marker}@example.test`, password: randomUUID(), name: "Smoke owner", org: "Scratch" },
    });
    expect(signup.ok()).toBe(true);
    const created = await owner.post("/workflows", { data: { name } });
    expect(created.status()).toBe(201);
    const workflow = await created.json();
    const saved = await owner.put(`/workflows/${workflow.id}`, {
      data: {
        name, description,
        nodes: [{ id: "manual", type: "trigger", label: "Manual start", triggerType: "manual", x: 0, y: 0 }],
        edges: [],
      },
    });
    expect(saved.status()).toBe(200);
    const minted = await owner.post(`/workflows/${workflow.id}/share`, { data: {} });
    expect(minted.status()).toBe(201);
    const { share } = await minted.json();
    expect(share.token).toMatch(/^[A-Za-z0-9_-]{16,64}$/);
    expect(share.name).toBe(name);
    expect(share.description).toBe(description);
    expect(Date.parse(share.createdAt)).toBeGreaterThanOrEqual(started - 1000);

    expect((await anonymous.get(`/shares/${share.token}`)).status()).toBe(200);
    expect((await anonymous.get("/shares")).status()).toBe(401);
    expect((await owner.get("/shares")).status()).toBe(200);
    expect(await context.cookies()).toEqual([]);
    const response = await page.goto(`/s/${share.token}`);
    expect(response?.status()).toBe(200);
    expect(new URL(page.url()).pathname).toBe(`/s/${share.token}`);
    expect(response?.headers()["cache-control"]).toContain("no-store");
    await expect(page).toHaveTitle(new RegExp(name));
    await expect(page.locator('meta[property="og:title"]')).toHaveAttribute("content", name);
    await expect(page.locator('meta[name="description"]')).toHaveAttribute("content", description);
    await expect(page.getByRole("heading", { name, exact: true })).toHaveCount(1);
    expect(await context.cookies()).toEqual([]);

    const revoked = await owner.delete(`/shares/${share.token}`);
    expect(revoked.ok()).toBe(true);
    expect((await anonymous.get(`/shares/${share.token}`)).status()).toBe(404);
    const refreshed = await page.reload();
    if (!refreshed) throw new Error("Revoked share did not return a document response");
    expect(refreshed?.status()).toBe(200);
    await expect(page).not.toHaveTitle(new RegExp(name));
    expect(await refreshed.text()).not.toContain(marker);
    await expect(page.locator('meta[property="og:title"][content="' + name + '"]')).toHaveCount(0);

    await page.goto("/account/links");
    const redirect = new URL(page.url());
    expect(redirect.pathname).toBe("/signin");
    expect(redirect.searchParams.get("next")).toBe("/account/links");
    const runs = await owner.get(`/workflows/${workflow.id}/runs`);
    expect(runs.status()).toBe(200);
    expect((await runs.json()).runs).toEqual([]);
    expect((await owner.delete(`/workflows/${workflow.id}`)).ok()).toBe(true);
  } finally {
    await owner.dispose();
    await anonymous.dispose();
  }
});

test("public share reads reject wrong or partial relay authentication", async () => {
  const owner = await request.newContext({ baseURL: backend });
  const anonymous = await request.newContext({ baseURL: backend });
  try {
    const share = await mintScratchShare(owner);
    const path = `/shares/${share.token}`;
    const headers = signedHeaders(share.token);
    const signature = headers["X-AgentMesh-Share-Signature"];
    const wrong = (signature.startsWith("0") ? "1" : "0") + signature.slice(1);
    expect((await anonymous.get(path, { headers: {
      ...headers, "X-AgentMesh-Share-Signature": wrong,
    } })).status()).toBe(401);
    expect((await anonymous.get(path, { headers: {
      "X-AgentMesh-Share-Visitor": headers["X-AgentMesh-Share-Visitor"],
      "X-AgentMesh-Share-Timestamp": headers["X-AgentMesh-Share-Timestamp"],
    } })).status()).toBe(401);
    expect((await anonymous.get(path, { headers })).status()).toBe(200);
    expect((await anonymous.get(path)).status()).toBe(200);
    expect((await anonymous.get("/shares")).status()).toBe(401);
    expect((await owner.delete(`/shares/${share.token}`)).ok()).toBe(true);
    expect((await anonymous.get(path, { headers: signedHeaders(share.token) })).status()).toBe(404);
    expect((await owner.delete(`/workflows/${share.workflowId}`)).ok()).toBe(true);
  } finally {
    await anonymous.dispose();
    await owner.dispose();
  }
});

// Run the exhaustion control last because the backend limiter spans tests.
test("one visitor cannot exhaust another visitor behind the same frontend", async ({ page, context, browser }) => {
  test.setTimeout(90_000);
  const owner = await request.newContext({ baseURL: backend });
  const quota = await request.newContext({ baseURL: backend });
  const visitorB = await browser.newContext({ baseURL: visitorBOrigin, javaScriptEnabled: false });
  try {
    const share = await mintScratchShare(owner);
    const path = `/s/${share.token}`;
    const quotaStarted = Date.now();
    expect((await page.goto(path))?.status()).toBe(200);
    await expect(page.getByRole("heading", { name: share.name, exact: true })).toHaveCount(1);
    const image = await imagePath(page, share.token);
    const originalImage = await imageDigest(context, image);
    const otherPage = await visitorB.newPage();
    expect((await otherPage.goto(path))?.status()).toBe(200);
    await expect(otherPage.getByRole("heading", { name: share.name, exact: true })).toHaveCount(1);
    expect(await imageDigest(visitorB, image)).toBe(originalImage);

    let exhausted = false;
    for (let reads = 0; reads < 61; reads++) {
      expect(Date.now() - quotaStarted, "Quota control must finish within its one-minute window").toBeLessThan(55_000);
      const response = await context.request.get(path);
      expect(response.status()).toBe(200);
      if (!(await response.text()).includes(share.name)) {
        exhausted = true;
        break;
      }
    }
    expect(exhausted, "The first visitor must remain rate limited").toBe(true);
    expect((await page.reload())?.status()).toBe(200);
    await expect(page.getByRole("heading", { name: share.name, exact: true })).toHaveCount(0);
    await expect(page).not.toHaveTitle(new RegExp(share.name));
    const deniedImage = await imageDigest(context, image);
    expect(deniedImage).not.toBe(originalImage);

    const spoofed = await context.request.get(path, { headers: {
      "x-vercel-forwarded-for": "2001:db8::206",
      "x-forwarded-for": "2001:db8::206",
      "X-AgentMesh-Share-Visitor": "2001:db8::206",
      "X-AgentMesh-Share-Timestamp": "1",
      "X-AgentMesh-Share-Signature": "0".repeat(64),
    } });
    expect((await spoofed.text()).includes(share.name), "Ingress must replace caller identity headers").toBe(false);

    const unaffected = await otherPage.reload();
    expect(unaffected?.status()).toBe(200);
    if (!unaffected) throw new Error("Second visitor did not receive an HTML response");
    expect(await unaffected.text(), "Distinct visitor must keep server-rendered content").toContain(share.name);
    await expect(otherPage).toHaveTitle(new RegExp(share.name));
    await expect(otherPage.locator('meta[property="og:title"]')).toHaveAttribute("content", share.name);
    await expect(otherPage.locator('meta[name="description"]')).toHaveAttribute("content", share.description);
    await expect(otherPage.getByRole("heading", { name: share.name, exact: true })).toHaveCount(1);
    expect(await imageDigest(visitorB, image)).toBe(originalImage);
    const limited = await quota.get(`/shares/${share.token}`, {
      headers: signedHeaders(share.token, "198.51.100.206"),
    });
    expect(limited.status()).toBe(429);
    expect(limited.headers()["retry-after"]).toBe("60");
    expect((await quota.get(`/shares/${share.token}`, {
      headers: signedHeaders(share.token, "2001:db8::206"),
    })).status()).toBe(200);
    expect(await context.cookies()).toEqual([]);
    expect(await visitorB.cookies()).toEqual([]);

    expect((await owner.delete(`/shares/${share.token}`)).ok()).toBe(true);
    const revoked = await otherPage.reload();
    if (!revoked) throw new Error("Revoked share did not return an HTML response");
    expect(revoked.status()).toBe(200);
    expect(await revoked.text()).not.toContain(share.marker);
    await expect(otherPage).not.toHaveTitle(new RegExp(share.name));
    expect(await imageDigest(visitorB, image)).toBe(deniedImage);
    const runs = await owner.get(`/workflows/${share.workflowId}/runs`);
    expect(runs.status()).toBe(200);
    expect((await runs.json()).runs).toEqual([]);
    expect((await owner.delete(`/workflows/${share.workflowId}`)).ok()).toBe(true);
  } finally {
    await visitorB.close();
    await quota.dispose();
    await owner.dispose();
  }
});
