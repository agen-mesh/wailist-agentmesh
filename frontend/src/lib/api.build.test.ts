import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// BASE is read once when api.ts loads, so each test loads it fresh with a
// backend configured.
async function loadApi() {
  vi.resetModules();
  vi.stubEnv("NEXT_PUBLIC_API_URL", "http://backend.test");
  return import("./api");
}

function respond(status: number, body: string) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(body, { status })),
  );
}

describe("workflows.build errors", () => {
  beforeEach(() => vi.unstubAllGlobals());
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  // The backend replied, so the build is over and its reason is the message.
  it("marks an error the backend itself returned as answered", async () => {
    const api = await loadApi();
    respond(409, JSON.stringify({ error: "the workflow changed while the builder was working" }));
    const err = await api.workflows.build("wf1", "hi", "b-12345678").catch((e) => e);
    expect(err).toBeInstanceOf(api.BuildRequestError);
    expect(err.answered).toBe(true);
    expect(err.message).toContain("workflow changed");
  });

  // A gateway timeout page is not the backend answering: the build may
  // still be running, and the chat has to wait for it.
  it("marks a proxy failure as not answered", async () => {
    const api = await loadApi();
    respond(504, "<html>Gateway Timeout</html>");
    const err = await api.workflows.build("wf1", "hi", "b-12345678").catch((e) => e);
    expect(err).toBeInstanceOf(api.BuildRequestError);
    expect(err.answered).toBe(false);
  });

  it("sends the browser timezone", async () => {
    const api = await loadApi();
    const fetchMock = vi.fn(
      async () => new Response(JSON.stringify({ reply: "ok", workflow: {} }), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await api.workflows.build("wf1", "every morning at 9", "b-12345678");
    const init = (fetchMock.mock.calls[0] as unknown as [string, RequestInit])[1];
    const body = JSON.parse(String(init.body));
    expect(body.timeZone).toBe(api.browserTimeZone());
    expect(body.timeZone).not.toBe("");
  });
});
