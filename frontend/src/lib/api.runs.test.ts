import { afterEach, describe, expect, it, vi } from "vitest";
import { WORKFLOWS } from "./data";
import {
  fixtureRunDetail,
  fixtureRunPage,
  recordStartedRun,
} from "./runFixtures";
import { fixtureWorkflow } from "./workflowFixtures";

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

// Every run sheet used to show the same weather run, whatever was tapped.
describe("fixtureRunDetail", () => {
  const rows = fixtureRunPage({ limit: 50 }, NOW).runs;
  const detailOf = (id: string) => {
    const detail = fixtureRunDetail(id, NOW);
    if (!detail) throw new Error(`no detail for ${id}`);
    return detail;
  };

  it("agrees with its list row", () => {
    for (const row of rows) {
      const { run } = detailOf(row.id);
      expect(run).toMatchObject({
        id: row.id,
        workflowId: row.workflowId,
        triggeredBy: row.triggeredBy,
        status: row.status,
        startedAt: row.startedAt,
      });
      expect(run.finishedAt).toBe(row.finishedAt);
    }
  });

  it("pays exactly what the row says was spent", () => {
    for (const row of rows) {
      const paid = detailOf(row.id)
        .logs.map((l) => l.output as { settledUsdMicros?: number })
        .reduce((sum, o) => sum + (o.settledUsdMicros ?? 0), 0);
      expect(paid).toBe(row.spendUsdMicros);
    }
  });

  it("gives every run its own result", () => {
    const results = rows.flatMap((row) =>
      detailOf(row.id).logs.flatMap((l) => {
        const m = (l.output as { message?: string }).message;
        return m ? [m] : [];
      }),
    );
    expect(results.length).toBeGreaterThan(5);
    expect(new Set(results).size).toBe(results.length);
  });

  it("explains a failure and leaves a stopped or running run without a result", () => {
    for (const row of rows) {
      const detail = detailOf(row.id);
      const hasResult = detail.logs.some(
        (l) =>
          l.status === "success" && (l.output as { message?: string }).message,
      );
      if (row.status === "failed") {
        expect(detail.deadLetters.length).toBeGreaterThan(0);
        expect(detail.logs.some((l) => l.status === "failed")).toBe(true);
      } else {
        expect(detail.deadLetters).toEqual([]);
      }
      if (row.status === "success") expect(hasResult).toBe(true);
      else expect(hasResult).toBe(false);
    }
  });

  it("names each step after a node in the run's own workflow", () => {
    for (const row of rows) {
      const wf = fixtureWorkflow(row.workflowId);
      const ids = new Set(wf?.nodes.map((n) => n.id));
      for (const log of detailOf(row.id).logs) {
        expect(ids.has(log.nodeId)).toBe(true);
      }
    }
  });

  it("plays a run started from the app through to success", () => {
    recordStartedRun("r-1850", "wf-brief", NOW);
    const early = fixtureRunDetail("r-1850", NOW + 2_000);
    expect(early?.run.status).toBe("running");
    expect(early?.logs.at(-1)?.status).toBe("running");
    expect(
      fixtureRunPage({ workflowId: "wf-brief" }, NOW + 2_000).runs[0].id,
    ).toBe("r-1850");

    const late = fixtureRunDetail("r-1850", NOW + 60_000);
    expect(late?.run.status).toBe("success");
    expect(late?.run.finishedAt).toBeDefined();
  });

  it("knows nothing about an id it never made", () => {
    expect(fixtureRunDetail("r-unknown", NOW)).toBeNull();
  });
});

describe("fixtureWorkflow", () => {
  it("opens a different workflow for every row in the list", () => {
    const opened = WORKFLOWS.map((row) => fixtureWorkflow(row.id));
    expect(opened.every((wf) => wf !== null && wf.nodes.length > 0)).toBe(true);
    expect(opened.map((wf) => wf?.id)).toEqual(WORKFLOWS.map((w) => w.id));
    expect(new Set(opened.map((wf) => wf?.name)).size).toBe(WORKFLOWS.length);
    const firstAgent = opened.map(
      (wf) => wf?.nodes.find((n) => n.type === "agent")?.name,
    );
    expect(new Set(firstAgent).size).toBe(WORKFLOWS.length);
  });

  it("returns null for an id the list does not have", () => {
    expect(fixtureWorkflow("wf-weather")).toBeNull();
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
