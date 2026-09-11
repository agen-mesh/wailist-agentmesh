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
});
