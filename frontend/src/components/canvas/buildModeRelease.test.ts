import { describe, it, expect } from "vitest";
import { isGraphRunnable } from "./buildModeRelease";
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
    // a1 is reachable and modelled, a2 is modelled but unreached: runnable.
    expect(isGraphRunnable(nodes, edges)).toBe(true);
  });
});
