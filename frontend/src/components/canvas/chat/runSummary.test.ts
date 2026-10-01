import { describe, it, expect } from "vitest";
import { runSummary, resolveReply } from "./resolveReply";
import type { LogEvent } from "../useRunTranscript";

function log(partial: Partial<LogEvent>): LogEvent {
  return {
    stepIndex: 0,
    nodeId: "n1",
    nodeType: "tool",
    status: "success",
    output: null,
    durationMs: 1,
    ts: "2026-09-16T09:00:00.000Z",
    ...partial,
  };
}

describe("runSummary", () => {
  it("reports a clean run", () => {
    expect(
      runSummary([
        log({ nodeId: "a" }),
        log({ nodeId: "b" }),
      ]),
    ).toEqual({ failed: false, degraded: 0, succeeded: 2, total: 2 });
  });

  it("counts a degraded node without calling the run failed", () => {
    expect(
      runSummary([
        log({ nodeId: "a", status: "degraded" }),
        log({ nodeId: "b" }),
      ]),
    ).toEqual({ failed: false, degraded: 1, succeeded: 1, total: 2 });
  });

  it("still reports a real failure", () => {
    expect(
      runSummary([
        log({ nodeId: "a", status: "degraded" }),
        log({ nodeId: "b", status: "failed" }),
      ]),
    ).toEqual({ failed: true, degraded: 1, succeeded: 0, total: 2 });
  });

  it("judges a node by its last attempt, not its first", () => {
    // A resumed node that failed and then succeeded must not read as degraded.
    expect(
      runSummary([
        log({ nodeId: "a", status: "degraded" }),
        log({ nodeId: "a", status: "success" }),
      ]),
    ).toEqual({ failed: false, degraded: 0, succeeded: 1, total: 1 });
  });
});

describe("resolveReply with a degraded step", () => {
  it("answers, and says the answer may be incomplete", () => {
    const reply = resolveReply([
      log({ nodeId: "fetch", status: "degraded" }),
      log({
        nodeId: "agent",
        nodeType: "agent",
        output: { text: "BTC is around $64,000 according to a web search." },
      }),
    ]);
    expect(reply.isError).toBe(false);
    expect(reply.text).toContain("BTC is around $64,000");
    expect(reply.text).toContain("1 step failed");
  });

  it("says nothing extra when every step succeeded", () => {
    const reply = resolveReply([
      log({ nodeId: "fetch" }),
      log({ nodeId: "agent", nodeType: "agent", output: { text: "All good." } }),
    ]);
    expect(reply.text).toBe("All good.");
  });

  it("leaves a real failure's error message alone", () => {
    const reply = resolveReply([
      log({ nodeId: "fetch", status: "degraded" }),
      log({ nodeId: "send", status: "failed", output: "slack: 404" }),
    ]);
    expect(reply.isError).toBe(true);
    expect(reply.text).not.toContain("step failed during this run");
  });
});
