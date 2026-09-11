import { describe, it, expect } from "vitest";
import { isGraphRunnable, agentMissingModel, firstUnreachedStep } from "./buildModeRelease";
import type { WorkflowNode, WorkflowEdge, NodeType, PortName } from "@/lib/types";

const node = (id: string, type: NodeType): WorkflowNode => ({
  id,
  type,
  template: "x",
  name: id,
  x: 0,
  y: 0,
});

const edge = (
  id: string,
  from: string,
  to: string,
  kind: WorkflowEdge["kind"],
  toPort: PortName,
): WorkflowEdge => ({ id, from, to, kind, toPort });

describe("isGraphRunnable", () => {
  it("is false for an empty graph", () => {
    expect(isGraphRunnable([], [])).toBe(false);
  });

  it("is false when there is a provider but no agent", () => {
    expect(isGraphRunnable([node("p1", "provider")], [])).toBe(false);
  });

  it("is false when the agent has no provider attached to its model port", () => {
    const nodes = [
      node("t1", "trigger"),
      node("a1", "agent"),
      node("p1", "provider"),
    ];
    const edges = [edge("e1", "t1", "a1", "flow", "in")];
    expect(isGraphRunnable(nodes, edges)).toBe(false);
  });

  it("is false when the agent has a model but no trigger reaches it", () => {
    const nodes = [node("a1", "agent"), node("p1", "provider")];
    const edges = [edge("e2", "p1", "a1", "attach", "model")];
    expect(isGraphRunnable(nodes, edges)).toBe(false);
  });

  it("is true for trigger -> agent with a provider on the model port", () => {
    const nodes = [
      node("t1", "trigger"),
      node("a1", "agent"),
      node("p1", "provider"),
    ];
    const edges = [
      edge("e1", "t1", "a1", "flow", "in"),
      edge("e2", "p1", "a1", "attach", "model"),
    ];
    expect(isGraphRunnable(nodes, edges)).toBe(true);
  });

  it("ignores a model-port edge whose source is not a provider", () => {
    const nodes = [
      node("t1", "trigger"),
      node("a1", "agent"),
      node("x1", "tool"),
    ];
    const edges = [
      edge("e1", "t1", "a1", "flow", "in"),
      edge("e2", "x1", "a1", "attach", "model"),
    ];
    expect(isGraphRunnable(nodes, edges)).toBe(false);
  });

  // The shape the builder produces for an API-backed workflow: fetch, then
  // reason. The agent is reached through a tool, not straight off the
  // trigger. Requiring a DIRECT trigger -> agent edge stranded exactly these
  // workflows in build mode.
  it("is true when the agent is reached through a tool step", () => {
    const nodes = [
      node("t1", "trigger"),
      node("h1", "tool"),
      node("j1", "tool"),
      node("a1", "agent"),
      node("p1", "provider"),
    ];
    const edges = [
      edge("e1", "t1", "h1", "flow", "in"),
      edge("e2", "h1", "j1", "flow", "in"),
      edge("e3", "j1", "a1", "flow", "in"),
      edge("e4", "p1", "a1", "attach", "model"),
    ];
    expect(isGraphRunnable(nodes, edges)).toBe(true);
  });

  it("is false when a second agent has no model", () => {
    const nodes = [
      node("t1", "trigger"),
      node("a1", "agent"),
      node("a2", "agent"),
      node("p1", "provider"),
    ];
    const edges = [
      edge("e1", "t1", "a1", "flow", "in"),
      edge("e2", "a1", "a2", "flow", "in"),
      edge("e3", "p1", "a1", "attach", "model"),
    ];
    // a2 is reached by the run and has nothing to think with, so the run
    // would fail there -- releasing into run mode would just hand the user
    // that failure.
    expect(isGraphRunnable(nodes, edges)).toBe(false);
  });

  it("is false when the modelled agent is on an island the trigger never reaches", () => {
    const nodes = [
      node("t1", "trigger"),
      node("e1", "end"),
      node("a1", "agent"),
      node("p1", "provider"),
    ];
    const edges = [
      edge("x1", "t1", "e1", "flow", "in"),
      edge("x2", "p1", "a1", "attach", "model"),
    ];
    expect(isGraphRunnable(nodes, edges)).toBe(false);
  });

  // Attach edges are not part of the execution path, so they must not count
  // towards reachability -- otherwise a provider wired to two agents would
  // look like a route from one to the other.
  it("does not treat attach edges as a path", () => {
    const nodes = [
      node("t1", "trigger"),
      node("a1", "agent"),
      node("a2", "agent"),
      node("p1", "provider"),
      node("p2", "provider"),
    ];
    const edges = [
      edge("e1", "t1", "a1", "flow", "in"),
      edge("e2", "p1", "a1", "attach", "model"),
      edge("e3", "p2", "a2", "attach", "model"),
    ];
    // The provider edges must not make a2 reachable from a1. And an agent
    // nothing flows into is not skipped at run time -- the engine runs it
    // first, on an empty input -- so a2 makes the graph not runnable.
    expect(isGraphRunnable(nodes, edges)).toBe(false);
  });

  // The shape the builder now produces for "fetch the Nifty 50 price daily":
  // no language step at all, so no agent and no provider. The engine runs a
  // tool-only flow, and requiring an agent stranded it in build mode with a
  // "No model attached yet" card for a workflow that needs no model.
  it("is true for a tool-only pipeline with no agent", () => {
    const nodes = [
      node("t1", "trigger"),
      node("h1", "tool"),
      node("j1", "tool"),
      node("e1", "end"),
    ];
    const edges = [
      edge("x1", "t1", "h1", "flow", "in"),
      edge("x2", "h1", "j1", "flow", "in"),
      edge("x3", "j1", "e1", "flow", "in"),
    ];
    expect(isGraphRunnable(nodes, edges)).toBe(true);
  });

  // The failed Nifty/Sensex run: "Fetch Sensex" was never wired into
  // "Extract Sensex Price". Nothing flowed into the extract step, so the
  // engine ran it first on an empty input and the run died in 3ms. The canvas
  // let it through because some other step was reachable.
  it("is false when a flow step has nothing flowing into it", () => {
    const nodes = [
      node("start", "trigger"),
      node("fetchN", "tool"),
      node("extractN", "tool"),
      node("fetchS", "tool"),
      node("extractS", "tool"),
      node("combine", "tool"),
      node("agent", "agent"),
      node("model", "provider"),
    ];
    const edges = [
      edge("1", "start", "fetchN", "flow", "in"),
      edge("2", "fetchN", "extractN", "flow", "in"),
      edge("3", "start", "fetchS", "flow", "in"),
      edge("4", "extractN", "combine", "flow", "in"),
      edge("5", "extractS", "combine", "flow", "in"),
      edge("6", "combine", "agent", "flow", "in"),
      edge("7", "model", "agent", "attach", "model"),
    ];
    expect(isGraphRunnable(nodes, edges)).toBe(false);
    // ...and wiring the missing edge makes it runnable.
    expect(
      isGraphRunnable(nodes, [...edges, edge("8", "fetchS", "extractS", "flow", "in")]),
    ).toBe(true);
  });

  // A tool attached to an agent's tools port is called by the agent, not run
  // as a flow step, so it needs no flow edge into it.
  it("does not require an agent-attached tool to be reached", () => {
    const nodes = [
      node("t1", "trigger"),
      node("a1", "agent"),
      node("p1", "provider"),
      node("search", "tool"),
    ];
    const edges = [
      edge("1", "t1", "a1", "flow", "in"),
      edge("2", "p1", "a1", "attach", "model"),
      edge("3", "search", "a1", "attach", "tools"),
    ];
    expect(isGraphRunnable(nodes, edges)).toBe(true);
  });

  it("is false for a trigger wired to nothing", () => {
    expect(isGraphRunnable([node("t1", "trigger")], [])).toBe(false);
  });
});

describe("agentMissingModel", () => {
  it("is true when an agent has no provider on its model port", () => {
    const nodes = [node("t1", "trigger"), node("a1", "agent")];
    expect(agentMissingModel(nodes, [edge("x", "t1", "a1", "flow", "in")])).toBe(true);
  });

  it("is false when every agent has a model, or there are no agents", () => {
    const nodes = [node("a1", "agent"), node("p1", "provider")];
    expect(agentMissingModel(nodes, [edge("x", "p1", "a1", "attach", "model")])).toBe(false);
    expect(agentMissingModel([node("t1", "trigger"), node("h1", "tool")], [])).toBe(false);
  });
});

describe("firstUnreachedStep", () => {
  it("names the step nothing flows into", () => {
    const nodes = [
      node("start", "trigger"),
      node("fetchS", "tool"),
      { ...node("extractS", "tool"), name: "Extract Sensex Price" },
      node("end", "end"),
    ];
    const edges = [
      edge("1", "start", "fetchS", "flow", "in"),
      edge("2", "extractS", "end", "flow", "in"),
    ];
    expect(firstUnreachedStep(nodes, edges)?.name).toBe("Extract Sensex Price");
  });

  it("is null when every flow step is reached", () => {
    const nodes = [node("t", "trigger"), node("h", "tool")];
    expect(firstUnreachedStep(nodes, [edge("1", "t", "h", "flow", "in")])).toBeNull();
  });
});
