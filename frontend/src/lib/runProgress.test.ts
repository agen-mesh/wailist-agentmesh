import { describe, expect, it } from "vitest";
import { runProgress, workflowSteps } from "./runProgress";
import type { Workflow, WorkflowNode } from "./types";

// A trigger, an agent with a model provider and a tool attached, then two
// nodes after it. The engine runs the trigger, the agent and those two; the
// provider and the tool hang off the agent and are never steps of their own.
function workflow(): Workflow {
  return {
    id: "wf-1",
    name: "Support triage",
    nodes: [
      { id: "n3", type: "action", name: "Reply", x: 0, y: 0 },
      { id: "t", type: "trigger", template: "manual", x: 0, y: 0 },
      { id: "a", type: "agent", name: "Triage Agent", x: 0, y: 0 },
      { id: "p", type: "provider", name: "Anthropic", x: 0, y: 0 },
      { id: "tool", type: "tool402", name: "Lookup", x: 0, y: 0 },
      { id: "n4", type: "end", name: "Done", x: 0, y: 0 },
    ],
    edges: [
      { id: "e1", from: "t", to: "a", kind: "flow" },
      { id: "e2", from: "a", to: "n3", kind: "flow" },
      { id: "e3", from: "n3", to: "n4", kind: "flow" },
      { id: "e4", from: "p", to: "a", kind: "attach", toPort: "model" },
      { id: "e5", from: "tool", to: "a", kind: "attach", toPort: "tools" },
    ],
  };
}

describe("workflowSteps", () => {
  it("lists the nodes the engine runs, in the order it reaches them", () => {
    expect(workflowSteps(workflow()).map((s) => s.id)).toEqual([
      "t",
      "a",
      "n3",
      "n4",
    ]);
  });

  it("leaves out what is attached to another node", () => {
    const ids = workflowSteps(workflow()).map((s) => s.id);
    // The provider and the agent's tool are configuration, not steps -- the
    // same rule the engine applies (attach edge, not node type).
    expect(ids).not.toContain("p");
    expect(ids).not.toContain("tool");
  });

  it("names a node by what it is called", () => {
    const steps = workflowSteps(workflow());
    expect(steps.map((s) => s.name)).toEqual([
      "manual",
      "Triage Agent",
      "Reply",
      "Done",
    ]);
  });

  it("keeps every node even when the graph has a cycle", () => {
    const wf = workflow();
    wf.edges.push({ id: "e6", from: "n4", to: "t", kind: "flow" });
    expect(workflowSteps(wf)).toHaveLength(4);
  });

  it("copes with a workflow that has no edges", () => {
    const wf = { ...workflow(), edges: [] };
    expect(workflowSteps(wf).map((s) => s.id)).toEqual([
      "n3",
      "t",
      "a",
      "p",
      "tool",
      "n4",
    ]);
  });

  // engine/graph.go adds a dependency for every "{{ node.<id> }}" reference,
  // so the backend runs the producer first even with no edge joining them.
  // Ordering on edges alone put the consumer first and the dock then called
  // it current while the engine was still on the producer.
  it("waits for a node it only references", () => {
    const ordered = workflowSteps({
      id: "wf-2",
      name: "Reference only",
      nodes: [
        {
          id: "consumer",
          type: "action",
          name: "Send",
          x: 0,
          y: 0,
          config: { body: "Total: {{ node.producer.amount }}" },
        },
        { id: "producer", type: "action", name: "Price", x: 0, y: 0 },
      ],
      edges: [],
    });
    expect(ordered.map((s) => s.id)).toEqual(["producer", "consumer"]);
  });

  it("reads references from every field the engine resolves", () => {
    const steps = (n: Partial<WorkflowNode>) =>
      workflowSteps({
        id: "wf-3",
        name: "Fields",
        nodes: [
          { id: "b", type: "action", name: "B", x: 0, y: 0, ...n },
          { id: "a", type: "action", name: "A", x: 0, y: 0 },
        ],
        edges: [],
      }).map((s) => s.id);

    const ref = "{{ node.a }}";
    expect(steps({ emailBody: ref })).toEqual(["a", "b"]);
    expect(steps({ config: { to: ref } })).toEqual(["a", "b"]);
    expect(steps({ paramDefaults: { q: ref } })).toEqual(["a", "b"]);
    expect(
      steps({ customParams: [{ name: "q", kind: "text", value: ref }] }),
    ).toEqual(["a", "b"]);
    // Not resolved at runtime, so not an ordering constraint -- the same
    // fields engine/graph.go leaves out of templateEligibleStrings.
    expect(steps({ systemPrompt: ref })).toEqual(["b", "a"]);
    expect(steps({ description: ref })).toEqual(["b", "a"]);
    expect(steps({ bodyTemplate: ref })).toEqual(["b", "a"]);
    // Text, not a reference: the engine requires the closing braces too.
    expect(steps({ config: { to: "{{ node.a" } })).toEqual(["b", "a"]);
  });

  // A pair that is both an edge and a reference must count once, or the node
  // never reaches an in-degree of zero and drops out of the ordered walk.
  it("counts an edge that is also a reference once", () => {
    const ordered = workflowSteps({
      id: "wf-4",
      name: "Both",
      nodes: [
        { id: "a", type: "action", name: "A", x: 0, y: 0 },
        {
          id: "b",
          type: "action",
          name: "B",
          x: 0,
          y: 0,
          config: { body: "{{ node.a }}" },
        },
      ],
      edges: [{ id: "e1", from: "a", to: "b", kind: "flow" }],
    });
    expect(ordered.map((s) => s.id)).toEqual(["a", "b"]);
  });
});

describe("runProgress", () => {
  const steps = workflowSteps(workflow());

  it("has nothing done before the first log", () => {
    const p = runProgress(steps, [], "running");
    expect(p.completed).toBe(0);
    expect(p.total).toBe(4);
    expect(p.percent).toBe(0);
    // The first milestone is the one being worked on: the server only says a
    // node finished, never that it started.
    expect(p.current?.id).toBe("t");
    expect(p.steps.map((s) => s.state)).toEqual([
      "running",
      "pending",
      "pending",
      "pending",
    ]);
  });

  it("moves along as each node answers", () => {
    const p = runProgress(
      steps,
      [
        { nodeId: "t", status: "success", durationMs: 12 },
        { nodeId: "a", status: "success", durationMs: 5200 },
      ],
      "running",
    );
    expect(p.completed).toBe(2);
    expect(p.percent).toBe(50);
    expect(p.current?.id).toBe("n3");
    expect(p.steps[1].durationMs).toBe(5200);
  });

  it("counts a degraded node as done", () => {
    const p = runProgress(
      steps,
      [{ nodeId: "t", status: "degraded" }],
      "running",
    );
    expect(p.steps[0].state).toBe("done");
    expect(p.completed).toBe(1);
  });

  it("stops at a failure instead of pretending the next one started", () => {
    const p = runProgress(
      steps,
      [
        { nodeId: "t", status: "success" },
        { nodeId: "a", status: "failed" },
      ],
      "failed",
    );
    expect(p.failed).toBe(true);
    expect(p.current).toBeNull();
    expect(p.steps.map((s) => s.state)).toEqual([
      "done",
      "failed",
      "pending",
      "pending",
    ]);
  });

  it("leaves nothing running once the run has stopped", () => {
    const p = runProgress(
      steps,
      [{ nodeId: "t", status: "success" }],
      "stopped",
    );
    expect(p.current).toBeNull();
    expect(p.steps[1].state).toBe("pending");
  });

  it("is complete when every node has answered", () => {
    const p = runProgress(
      steps,
      steps.map((s) => ({ nodeId: s.id, status: "success" })),
      "success",
    );
    expect(p.percent).toBe(100);
    expect(p.current).toBeNull();
  });

  // An attached tool that charges gets a log row of its own, keyed by a node
  // that is not a milestone. It must not count towards the total.
  it("ignores a log for a node that is not a step", () => {
    const p = runProgress(
      steps,
      [
        { nodeId: "t", status: "success" },
        { nodeId: "tool", status: "success" },
      ],
      "running",
    );
    expect(p.completed).toBe(1);
    expect(p.total).toBe(4);
  });

  it("takes the newest attempt of a retried node", () => {
    const p = runProgress(
      steps,
      [
        { nodeId: "t", status: "failed" },
        { nodeId: "t", status: "success", durationMs: 30 },
      ],
      "running",
    );
    expect(p.steps[0].state).toBe("done");
    expect(p.failed).toBe(false);
  });

  // Found on a phone: a run finished, but two of its four nodes never
  // reported (a branch not taken), so the dock sat at "2/4 Finished".
  it("counts what a finished run never reached as behind it", () => {
    const p = runProgress(
      steps,
      [
        { nodeId: "t", status: "success" },
        { nodeId: "a", status: "success" },
      ],
      "success",
    );
    expect(p.percent).toBe(100);
    expect(p.completed).toBe(4);
    expect(p.steps.map((s) => s.state)).toEqual([
      "done",
      "done",
      "skipped",
      "skipped",
    ]);
  });

  // A stopped run is different: those nodes were going to run, and did not.
  it("leaves a stopped run's remaining steps pending", () => {
    const p = runProgress(
      steps,
      [{ nodeId: "t", status: "success" }],
      "stopped",
    );
    expect(p.steps[2].state).toBe("pending");
    expect(p.percent).toBe(25);
  });

  // The runner writes a "running" row before it executes a node, so a level
  // running four at once says so. Guessing showed only the first.
  it("shows every node the server says is running", () => {
    const p = runProgress(
      steps,
      [
        { nodeId: "t", status: "success" },
        { nodeId: "a", status: "running" },
        { nodeId: "n3", status: "running" },
      ],
      "running",
    );
    expect(p.steps.map((s) => s.state)).toEqual([
      "done",
      "running",
      "running",
      "pending",
    ]);
    expect(p.current?.id).toBe("a");
  });

  // A branch the run did not take has no log at all. The first-unanswered
  // guess named it, announcing work on a branch nothing was running.
  it("names the branch that is running, not the one passed over", () => {
    const p = runProgress(
      steps,
      [
        { nodeId: "t", status: "success" },
        { nodeId: "n3", status: "running" },
      ],
      "running",
    );
    // "a" was skipped over; it is not what the run is working on.
    expect(p.steps[1].state).toBe("pending");
    expect(p.steps[2].state).toBe("running");
    expect(p.current?.id).toBe("n3");
  });

  it("still guesses while no node has reported running", () => {
    const p = runProgress(
      steps,
      [{ nodeId: "t", status: "success" }],
      "running",
    );
    expect(p.current?.id).toBe("a");
  });

  // GetRunLogs orders by (stepIndex, ts) and Resume recomputes step indexes,
  // so a retry that moved to a lower level arrives BEFORE the failed row it
  // replaces. Taking the last entry called a successful run failed.
  it("takes the newest attempt by time, not by position", () => {
    const p = runProgress(
      steps,
      [
        {
          nodeId: "a",
          status: "success",
          durationMs: 30,
          stepIndex: 0,
          ts: "2026-09-23T10:05:00Z",
        },
        {
          nodeId: "a",
          status: "failed",
          stepIndex: 2,
          ts: "2026-09-23T10:00:00Z",
        },
      ],
      "success",
    );
    expect(p.steps[1].state).toBe("done");
    expect(p.steps[1].durationMs).toBe(30);
    expect(p.failed).toBe(false);
  });

  // Go marshals times as RFC3339Nano, which drops trailing zeros, so a
  // fractional-second row and an exact-second one differ in length. Compared
  // as text, "10:00:00.1Z" sorts BEFORE "10:00:00Z" -- "." is below "Z" --
  // and the older attempt won.
  it("prefers a fractional-second attempt over an earlier exact second", () => {
    const p = runProgress(
      steps,
      [
        { nodeId: "a", status: "failed", ts: "2026-09-23T10:00:00Z" },
        {
          nodeId: "a",
          status: "success",
          durationMs: 30,
          ts: "2026-09-23T10:00:00.1Z",
        },
      ],
      "success",
    );
    expect(p.steps[1].state).toBe("done");
    expect(p.failed).toBe(false);
  });

  it("keeps the fractional winner whichever order it arrives in", () => {
    const p = runProgress(
      steps,
      [
        {
          nodeId: "a",
          status: "success",
          durationMs: 30,
          ts: "2026-09-23T10:00:00.1Z",
        },
        { nodeId: "a", status: "failed", ts: "2026-09-23T10:00:00Z" },
      ],
      "success",
    );
    expect(p.steps[1].state).toBe("done");
    expect(p.failed).toBe(false);
  });

  it("falls back to array order for a timestamp it cannot read", () => {
    const p = runProgress(
      steps,
      [
        { nodeId: "a", status: "failed", ts: "not a time" },
        { nodeId: "a", status: "success", ts: "also not a time" },
      ],
      "success",
    );
    expect(p.steps[1].state).toBe("done");
  });

  it("calls a workflow with no steps complete rather than 0%", () => {
    expect(runProgress([], [], "success").percent).toBe(100);
  });
});
