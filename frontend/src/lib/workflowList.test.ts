import { describe, expect, it } from "vitest";
import type { Workflow } from "./types";
import {
  filterWorkflows,
  isOptionActive,
  nextSort,
  sortWorkflows,
  type Sort,
} from "./workflowList";

function wf(overrides: Partial<Workflow>): Workflow {
  return { id: "wf", name: "Workflow", nodes: [], edges: [], ...overrides };
}

const LIST = [
  wf({
    id: "a",
    name: "Customer Support Triage",
    status: "deployed",
    tags: ["support"],
  }),
  wf({ id: "b", name: "Invoice check", status: "paused" }),
  wf({ id: "c", name: "Lead scoring", status: "draft" }),
];

const ids = (list: Workflow[]) => list.map((w) => w.id);

describe("filterWorkflows", () => {
  it("shows a deployed workflow under Active", () => {
    expect(ids(filterWorkflows(LIST, { query: "", status: "active" }))).toEqual(
      ["a"],
    );
    expect(
      ids(filterWorkflows(LIST, { query: "", status: "deployed" })),
    ).toEqual(["a"]);
  });

  it("filters the other statuses by name", () => {
    expect(ids(filterWorkflows(LIST, { query: "", status: "paused" }))).toEqual(
      ["b"],
    );
    expect(ids(filterWorkflows(LIST, { query: "", status: "all" }))).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  it("searches names and tags, ignoring case and outer spaces", () => {
    expect(
      ids(filterWorkflows(LIST, { query: "  INVOICE ", status: "all" })),
    ).toEqual(["b"]);
    expect(
      ids(filterWorkflows(LIST, { query: "Support", status: "all" })),
    ).toEqual(["a"]);
  });
});

describe("sortWorkflows", () => {
  const SORTABLE = [
    wf({
      id: "b",
      name: "beta",
      spend: "4.218",
      createdAt: "2026-09-01T00:00:00Z",
      lastRunAt: "2026-09-18T10:00:00Z",
    }),
    wf({
      id: "a",
      name: "Alpha 10",
      spend: "1.50",
      createdAt: "2026-09-10T00:00:00Z",
    }),
    wf({
      id: "c",
      name: "alpha 2",
      createdAt: "2026-08-01T00:00:00Z",
      lastRunAt: "2026-09-19T09:00:00Z",
    }),
  ];
  const order = (sort: Sort) => ids(sortWorkflows(SORTABLE, sort));

  it("keeps the server's order until a sort is chosen", () => {
    expect(order(null)).toEqual(["b", "a", "c"]);
  });

  it("sorts names naturally, ignoring case", () => {
    expect(order({ key: "alpha", dir: "asc" })).toEqual(["c", "a", "b"]);
    expect(order({ key: "alpha", dir: "desc" })).toEqual(["b", "a", "c"]);
  });

  it("puts the most recent first and never-run workflows last either way", () => {
    expect(order({ key: "recentRun", dir: "desc" })).toEqual(["c", "b", "a"]);
    expect(order({ key: "recentRun", dir: "asc" })).toEqual(["b", "c", "a"]);
    expect(order({ key: "recentCreated", dir: "desc" })).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  it("orders by spend, counting a missing spend as nothing", () => {
    expect(order({ key: "cost", dir: "desc" })).toEqual(["b", "a", "c"]);
    expect(order({ key: "cost", dir: "asc" })).toEqual(["c", "a", "b"]);
  });

  it("does not reorder the list it was given", () => {
    sortWorkflows(SORTABLE, { key: "alpha", dir: "asc" });
    expect(ids(SORTABLE)).toEqual(["b", "a", "c"]);
  });
});

describe("nextSort", () => {
  it("applies an option in its natural direction", () => {
    expect(nextSort(null, "alpha")).toEqual({ key: "alpha", dir: "asc" });
    expect(nextSort(null, "recentRun")).toEqual({
      key: "recentRun",
      dir: "desc",
    });
    expect(nextSort(null, "costHigh")).toEqual({ key: "cost", dir: "desc" });
    expect(nextSort(null, "costLow")).toEqual({ key: "cost", dir: "asc" });
  });

  it("reverses the option already applied", () => {
    const az = nextSort(null, "alpha");
    expect(nextSort(az, "alpha")).toEqual({ key: "alpha", dir: "desc" });
    expect(nextSort(nextSort(az, "alpha"), "alpha")).toEqual(az);
  });

  it("turns Highest cost into Lowest cost on a second tap", () => {
    const high = nextSort(null, "costHigh");
    const again = nextSort(high, "costHigh");
    expect(again).toEqual({ key: "cost", dir: "asc" });
    expect(isOptionActive(again, "costLow")).toBe(true);
    expect(isOptionActive(again, "costHigh")).toBe(false);
  });

  it("switches key without flipping when another option is chosen", () => {
    const high = nextSort(null, "costHigh");
    expect(nextSort(high, "alpha")).toEqual({ key: "alpha", dir: "asc" });
    expect(nextSort(high, "costLow")).toEqual({ key: "cost", dir: "asc" });
  });
});
