import { describe, expect, it } from "vitest";
import type { Workflow } from "./types";
import { describeWorkflow, workflowAgents } from "./describeWorkflow";

function wf(overrides: Partial<Workflow>): Workflow {
  return { id: "wf", name: "Workflow", nodes: [], edges: [], ...overrides };
}

const GRAPH: Partial<Workflow> = {
  nodes: [
    { id: "t", type: "trigger", template: "manual", x: 0, y: 0 },
    { id: "a", type: "agent", template: "agent", name: "Analyst", x: 0, y: 0 },
    {
      id: "p",
      type: "provider",
      template: "gemini",
      model: "gemini-2.5-flash",
      x: 0,
      y: 0,
    },
    { id: "h", type: "tool", template: "http", name: "Fetch", x: 0, y: 0 },
    { id: "w", type: "tool402", name: "Weather", x: 0, y: 0 },
  ],
  edges: [
    { id: "e1", from: "t", to: "a", kind: "flow" },
    { id: "e2", from: "p", to: "a", kind: "attach", toPort: "model" },
    { id: "e3", from: "h", to: "a", kind: "attach", toPort: "tools" },
    { id: "e4", from: "w", to: "a", kind: "attach", toPort: "tools" },
  ],
};

describe("workflowAgents", () => {
  it("names each agent with its model and tool count", () => {
    expect(workflowAgents(wf(GRAPH))).toEqual([
      { id: "a", name: "Analyst", model: "gemini-2.5-flash", tools: 2 },
    ]);
  });

  it("leaves the model empty when no provider is attached", () => {
    const agents = workflowAgents(
      wf({
        nodes: [{ id: "a", type: "agent", template: "router", x: 0, y: 0 }],
        edges: [],
      }),
    );
    expect(agents).toEqual([
      { id: "a", name: "router", model: null, tools: 0 },
    ]);
  });
});

describe("describeWorkflow", () => {
  it("says how it starts and what it is made of", () => {
    expect(describeWorkflow(wf(GRAPH))).toBe(
      "Runs when started. 1 agent (gemini-2.5-flash) using 2 tools.",
    );
  });

  it("prefers the schedule, then a location zone, over the trigger node", () => {
    expect(
      describeWorkflow(wf({ ...GRAPH, scheduleCron: "0 9 * * *" })),
    ).toMatch(/^Runs on a schedule\./);
    expect(describeWorkflow(wf({ ...GRAPH, geofenceRadiusM: 200 }))).toMatch(
      /^Runs on arriving at or leaving a place\./,
    );
  });

  it("describes a chat or webhook trigger, and an empty graph", () => {
    const chat = wf({
      nodes: [{ id: "t", type: "trigger", template: "chat", x: 0, y: 0 }],
    });
    expect(describeWorkflow(chat)).toBe(
      "Starts from a chat message. Nothing has been added to it yet.",
    );
    const hook = wf({
      nodes: [
        { id: "t", type: "trigger", template: "webhook", x: 0, y: 0 },
        { id: "x", type: "action", x: 0, y: 0 },
      ],
    });
    expect(describeWorkflow(hook)).toBe(
      "Runs when its webhook is called. 1 step, no agents.",
    );
  });
});
