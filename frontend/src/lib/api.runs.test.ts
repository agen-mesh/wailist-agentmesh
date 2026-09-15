import { afterEach, describe, expect, it, vi } from "vitest";
import { fixtureRunPage } from "./runFixtures";

const NOW = Date.UTC(2026, 8, 14, 12, 0, 0);

describe("fixtureRunPage", () => {
  it("pages by cursor without gaps or repeats", () => {
    const all = fixtureRunPage({ limit: 50 }, NOW).runs.map((r) => r.id);
    const seen: string[] = [];
    let cursor: string | null = null;
    for (let i = 0; i < 20; i++) {
      const page = fixtureRunPage({ limit: 3, cursor }, NOW);
      seen.push(...page.runs.map((r) => r.id));
      cursor = page.nextCursor;
      if (!cursor) break;
    }
    expect(all.length).toBeGreaterThan(3);
    expect(seen).toEqual(all);
  });

  it("filters to one workflow", () => {
    const page = fixtureRunPage({ workflowId: "wf-brief", limit: 50 }, NOW);
    expect(page.runs.length).toBeGreaterThan(0);
    expect(page.runs.every((r) => r.workflowId === "wf-brief")).toBe(true);
    expect(page.nextCursor).toBeNull();
  });

  it("gives a running run no finish time and every other run one", () => {
    for (const r of fixtureRunPage({ limit: 50 }, NOW).runs) {
      if (r.status === "running") expect(r.finishedAt).toBeUndefined();
      else expect(r.finishedAt).toBeDefined();
    }
  });
});

// BASE is read when lib/api.ts loads, so each case sets the environment and
// then imports a fresh copy of the module.
describe("run history against a backend", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    vi.resetModules();
  });

  async function load(response: Response) {
    vi.stubEnv("NEXT_PUBLIC_API_URL", "https://api.example.com");
    vi.stubEnv("NEXT_PUBLIC_NATIVE_CLIENT", "");
    const fetchMock = vi.fn(async () => response);
    vi.stubGlobal("fetch", fetchMock);
    vi.resetModules();
    const api = await import("./api");
    return { api, fetchMock };
  }

  // chi answers an unknown route with plain text, which is what an older
  // backend without these routes sends.
  it("treats a plain-text 404 as run history being unavailable", async () => {
    const { api } = await load(
      new Response("404 page not found", { status: 404 }),
    );
    await expect(api.runs.recent()).rejects.toBeInstanceOf(
      api.RunsUnavailableError,
    );
  });

  it("keeps a JSON 404 as an ordinary not-found error", async () => {
    const { api } = await load(
      new Response(JSON.stringify({ error: "workflow not found" }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      }),
    );
    const err = await api.runs.listForWorkflow("wf-1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(Error);
    expect(err).not.toBeInstanceOf(api.RunsUnavailableError);
    expect((err as Error).message).toBe("workflow not found");
  });

  it("sends the cursor and limit and returns the page", async () => {
    const page = { runs: [], nextCursor: null };
    const { api, fetchMock } = await load(
      new Response(JSON.stringify(page), { status: 200 }),
    );
    await expect(
      api.runs.listForWorkflow("wf 1", { cursor: "abc", limit: 5 }),
    ).resolves.toEqual(page);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/workflows/wf%201/runs?limit=5&cursor=abc",
      expect.anything(),
    );
  });
});
