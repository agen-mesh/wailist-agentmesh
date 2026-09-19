import { afterEach, describe, expect, it, vi } from "vitest";

// IS_NATIVE is read from the environment when lib/nativeAuth.ts loads, so each
// case sets the variable and then imports a fresh copy of the module.
async function load(native: boolean) {
  vi.stubEnv("NEXT_PUBLIC_NATIVE_CLIENT", native ? "1" : "");
  vi.resetModules();
  return import("./routes");
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("workflowHref on the web", () => {
  it("puts the id in the path", async () => {
    const { workflowHref } = await load(false);
    expect(workflowHref("wf-triage")).toBe("/workflows/wf-triage");
    expect(workflowHref("wf-triage", { geofence: true })).toBe(
      "/workflows/wf-triage/geofence",
    );
    expect(workflowHref("wf-triage", { query: { add: "eyJ0_-1" } })).toBe(
      "/workflows/wf-triage?add=eyJ0_-1",
    );
  });

  it("encodes an id that is not URL-safe", async () => {
    const { workflowHref } = await load(false);
    expect(workflowHref("a b&c")).toBe("/workflows/a%20b%26c");
  });
});

// The native export has a single page per route, /workflows/app, so the id
// has to travel in the query string. Before this helper existed every list
// link built /workflows/<id>, and tapping Open or Zone in the app reloaded
// the page it was on.
describe("workflowHref in the native app", () => {
  it("points at the shell page and carries the id as ?id=", async () => {
    const { workflowHref } = await load(true);
    expect(workflowHref("wf-triage")).toBe("/workflows/app?id=wf-triage");
    expect(workflowHref("wf-triage", { geofence: true })).toBe(
      "/workflows/app/geofence?id=wf-triage",
    );
    expect(workflowHref("wf-triage", { query: { add: "eyJ0_-1" } })).toBe(
      "/workflows/app?id=wf-triage&add=eyJ0_-1",
    );
  });

  it("encodes an id that is not URL-safe", async () => {
    const { workflowHref } = await load(true);
    expect(workflowHref("a b&c")).toBe("/workflows/app?id=a+b%26c");
  });

  it("does not let the query replace the id", async () => {
    const { workflowHref } = await load(true);
    expect(workflowHref("wf-triage", { query: { id: "other" } })).toBe(
      "/workflows/app?id=wf-triage",
    );
  });
});
