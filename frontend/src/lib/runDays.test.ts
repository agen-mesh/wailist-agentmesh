import { describe, expect, it } from "vitest";
import { groupRunsByDay } from "./runDays";
import type { RunSummary } from "./types";

// Dates are built with the local-time constructor, so these hold in any time
// zone the tests happen to run in.
function runAt(id: string, when: Date): RunSummary {
  return {
    id,
    workflowId: "wf-1",
    workflowName: "Workflow",
    triggeredBy: "manual",
    status: "success",
    startedAt: when.toISOString(),
    spendUsdMicros: 0,
  };
}

const NOW = new Date(2026, 8, 14, 9, 30);

describe("groupRunsByDay", () => {
  it("labels today, yesterday and older days by the local calendar", () => {
    const groups = groupRunsByDay(
      [
        runAt("a", new Date(2026, 8, 14, 8, 0)),
        runAt("b", new Date(2026, 8, 14, 0, 5)),
        runAt("c", new Date(2026, 8, 13, 23, 55)),
        runAt("d", new Date(2026, 8, 11, 12, 0)),
      ],
      NOW,
      "en-US",
    );
    expect(groups).toHaveLength(3);
    expect(groups[0].label).toBe("Today");
    expect(groups[1].label).toBe("Yesterday");
    expect(groups[0].runs.map((r) => r.id)).toEqual(["a", "b"]);
    expect(groups[1].runs.map((r) => r.id)).toEqual(["c"]);
    expect(groups[2].label).toContain("Sep");
    expect(groups[2].label).toContain("11");
    expect(groups[2].key).toBe("2026-09-11");
  });

  // Five minutes apart, but on either side of midnight: different days.
  it("splits runs either side of local midnight", () => {
    const groups = groupRunsByDay(
      [
        runAt("after", new Date(2026, 8, 14, 0, 2)),
        runAt("before", new Date(2026, 8, 13, 23, 58)),
      ],
      NOW,
      "en-US",
    );
    expect(groups.map((g) => g.label)).toEqual(["Today", "Yesterday"]);
  });

  it("adds the year for a day in another year", () => {
    const [group] = groupRunsByDay(
      [runAt("old", new Date(2025, 11, 30, 10, 0))],
      NOW,
      "en-US",
    );
    expect(group.label).toContain("2025");
  });

  it("skips a run whose start time cannot be read", () => {
    const bad = { ...runAt("bad", NOW), startedAt: "not a date" };
    expect(groupRunsByDay([bad], NOW)).toEqual([]);
  });
});
