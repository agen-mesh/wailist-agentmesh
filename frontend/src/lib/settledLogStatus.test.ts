import { describe, it, expect } from "vitest";
import { isSettledLogStatus, type RunLogRecord } from "./api";

// Both readers of the stored run log -- the console's DB reconciliation
// (useRunTranscript.mergeDBLogs) and the chat's stranded-turn recovery
// (recoverPendingTurn.toLogEvents) -- decide what to keep with this one
// function, so its answer for each status is the thing worth pinning down.
describe("isSettledLogStatus", () => {
  const cases: Array<[RunLogRecord["status"], boolean]> = [
    ["pending", false],
    ["running", false],
    ["success", true],
    ["failed", true],
    ["degraded", true],
  ];

  it.each(cases)("%s is settled: %s", (status, want) => {
    expect(isSettledLogStatus(status)).toBe(want);
  });

  // The reason this is a Record and not an inline comparison: a degraded step
  // is finished, and a reader that drops it replays a run that lost a source
  // as a clean one.
  it("treats a degraded step as a finished one", () => {
    expect(isSettledLogStatus("degraded")).toBe(true);
  });
});
