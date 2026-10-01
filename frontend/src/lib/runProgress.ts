// A run's progress as milestones: one per node the engine actually runs.
//
// The pieces this is built from already exist and disagree with each other,
// which is the reason for a module rather than a few lines in a component:
//
//   - `describeWorkflow` counts steps by node TYPE (everything but trigger and
//     provider). The engine does not: `runner.go` excludes a node when it is
//     the source of an `attach` edge, which is how a provider and an agent's
//     tools are left out. A trigger DOES run and gets a log row. Counting by
//     type therefore names milestones the run will never reach, and misses one
//     it always does.
//   - `stepIndex` on a log is the topological LEVEL, not a position: every node
//     in a level runs at once and shares the number. It cannot order a list.
//   - Logs arrive one per attempt, and an attached tool that charges gets a
//     synthetic row of its own, keyed by a node that is not a step.
//
// So the order comes from the graph, and the logs only say how far it got.
import type { Workflow, WorkflowNode } from "./types";

/**
 * What a milestone can be.
 *
 * "skipped" is a node a finished run never reported: a branch it did not
 * take, or one the engine had no work for. Without it, a run that succeeded
 * sat at "2 of 4" forever, which reads as unfinished.
 */
export type StepState = "pending" | "running" | "done" | "failed" | "skipped";

export interface RunStep {
  id: string;
  name: string;
  state: StepState;
  /** Milliseconds the node took, once it has finished. */
  durationMs?: number;
}

export interface RunProgressSummary {
  steps: RunStep[];
  /** Milestones finished, successfully or not. */
  completed: number;
  total: number;
  /** 0-100, for the bar. */
  percent: number;
  /** The milestone being worked on, if any. */
  current: RunStep | null;
  failed: boolean;
}

/** What a log row has to carry to be placed on a milestone. */
export interface ProgressLog {
  nodeId: string;
  status: string;
  durationMs?: number;
  stepIndex?: number;
  /**
   * When the row was written. Array order cannot stand in for it: the API
   * orders logs by (stepIndex, ts), and Resume recomputes step indexes
   * against the current workflow, so after a topology edit a newer retry at
   * a lower level sorts before an older row at a higher one.
   */
  ts?: string;
}

function nodeName(n: WorkflowNode): string {
  return n.name || n.label || n.template || n.type;
}

// Kept in step with engine/graph.go's nodeRefPattern, which is itself kept in
// step with the resolver's "node.<id>" / "node.<id>.field" forms. The closing
// braces are required there and here: an unterminated "{{ node.n5" is text,
// not a reference, and treating it as one would invent an ordering the
// engine does not impose.
const NODE_REF = /\{\{\s*node\.([A-Za-z0-9_-]+)(?:\.[A-Za-z0-9_.-]+)?\s*\}\}/g;

/**
 * Every node id this node's template-eligible fields refer to.
 *
 * The field list mirrors engine/graph.go's templateEligibleStrings: the
 * fields a connector actually runs through the resolver. systemPrompt,
 * bodyTemplate and description are deliberately left out there -- none is
 * resolved at runtime, so "{{ node.x }}" in one of them is prose -- and
 * leaving them out here keeps the two readings of the same graph the same.
 * Credentials are not template text and are never scanned.
 */
function referencedNodeIds(n: WorkflowNode): string[] {
  const fields = [
    n.emailBody,
    ...Object.values(n.config ?? {}),
    ...Object.values(n.paramDefaults ?? {}),
    ...(n.customParams ?? []).map((p) => p.value),
  ];
  const ids: string[] = [];
  for (const field of fields) {
    if (!field || !field.includes("{{")) continue;
    for (const m of field.matchAll(NODE_REF)) ids.push(m[1]);
  }
  return ids;
}

/**
 * The nodes a run walks, in the order it reaches them.
 *
 * Kahn's algorithm over `flow` edges, which is what the engine's own
 * topological sort walks. Ties keep the order the nodes were saved in, so the
 * list is stable between reads. A cycle cannot be ordered, and the nodes left
 * in it are appended rather than dropped: a milestone missing from the list
 * would be worse than one in an odd position.
 */
export function workflowSteps(wf: Workflow): { id: string; name: string }[] {
  const nodes = wf.nodes ?? [];
  const edges = wf.edges ?? [];
  // A node that hangs off another one -- a provider, an agent's tools -- is
  // configuration for the node it attaches to, and never a step of its own.
  const attached = new Set(
    edges.filter((e) => e.kind === "attach").map((e) => e.from),
  );
  const steps = nodes.filter((n) => !attached.has(n.id));
  const isStep = new Set(steps.map((n) => n.id));

  const waitingFor = new Map<string, number>(steps.map((n) => [n.id, 0]));
  const after = new Map<string, string[]>();
  // Both kinds of dependency go through here, so a pair that is both a flow
  // edge and a reference is counted once -- the same dedupe engine/graph.go
  // does with seenDep, and for the same reason: counted twice, the node
  // never reaches an in-degree of zero and falls out of the order entirely.
  const seen = new Set<string>();
  const dependsOn = (from: string, to: string) => {
    if (from === to || !isStep.has(from) || !isStep.has(to)) return;
    // Node ids are [A-Za-z0-9_-], so `>` cannot appear inside one.
    const key = `${from}>${to}`;
    if (seen.has(key)) return;
    seen.add(key);
    waitingFor.set(to, (waitingFor.get(to) ?? 0) + 1);
    after.set(from, [...(after.get(from) ?? []), to]);
  };

  for (const e of edges) {
    if (e.kind !== "attach") dependsOn(e.from, e.to);
  }
  // A "{{ node.x }}" reference makes this node wait for x's output whether or
  // not an edge joins them. The engine orders on that (engine/graph.go), so a
  // dock that ordered on flow edges alone could call a node current while the
  // backend was still running the node it reads from.
  for (const n of steps) {
    for (const refId of referencedNodeIds(n)) dependsOn(refId, n.id);
  }

  const ordered: WorkflowNode[] = [];
  const ready = steps.filter((n) => (waitingFor.get(n.id) ?? 0) === 0);
  while (ready.length) {
    const n = ready.shift()!;
    ordered.push(n);
    for (const nextId of after.get(n.id) ?? []) {
      const left = (waitingFor.get(nextId) ?? 0) - 1;
      waitingFor.set(nextId, left);
      if (left === 0) {
        const next = steps.find((s) => s.id === nextId);
        if (next) ready.push(next);
      }
    }
  }
  // Anything left is in a cycle. Keep it, at the end.
  for (const n of steps) {
    if (!ordered.includes(n)) ordered.push(n);
  }
  return ordered.map((n) => ({ id: n.id, name: nodeName(n) }));
}

/**
 * Folds a run's logs onto its milestones.
 *
 * `runStatus` decides what an unfinished milestone means: while the run is
 * going, the first one without an answer is the one being worked on; once it
 * has stopped, nothing is running any more.
 *
 * The runner writes a node's row with status "running" before it executes it
 * and updates that row when it finishes, so the server usually does say which
 * node is working. Those rows are believed. The first-unanswered guess below
 * is only for the gap before one arrives: guessing when the answer is on hand
 * hides the other half of a parallel level, and names the branch that was not
 * taken once a later sibling was chosen.
 *
 * A failure still stops the guess: after a failed node, the ones behind it
 * never started.
 */
export function runProgress(
  steps: { id: string; name: string }[],
  logs: ProgressLog[],
  runStatus: string,
): RunProgressSummary {
  // One log per node: the newest attempt wins, and rows for anything that is
  // not a milestone (an attached tool's payment row) are ignored.
  //
  // Newest by timestamp, not by position. GetRunLogs orders by (stepIndex,
  // ts) and Resume recomputes step indexes against the current workflow, so
  // after a topology edit a retry that moved to a lower level sorts BEFORE
  // the older failed row it replaces -- and taking the last entry would show
  // a run that succeeded as failed.
  //
  // Compared as instants, never as text. Go marshals times as RFC3339Nano,
  // which omits trailing zeros in the fractional part, so the two rows of a
  // legitimate pair can differ in length: "...10:00:00.1Z" is later than
  // "...10:00:00Z" but sorts before it as a string, because "." is below "Z".
  // A row with no timestamp, or one that will not parse, keeps the old rule
  // -- later entry wins -- which is also the tie-break for equal instants.
  const byNode = new Map<string, ProgressLog>();
  const isStep = new Set(steps.map((s) => s.id));
  const at = (log: ProgressLog): number | null => {
    if (!log.ts) return null;
    const instant = Date.parse(log.ts);
    return Number.isNaN(instant) ? null : instant;
  };
  for (const log of logs) {
    if (!isStep.has(log.nodeId)) continue;
    const held = byNode.get(log.nodeId);
    if (held) {
      const incoming = at(log);
      const kept = at(held);
      if (incoming !== null && kept !== null && incoming < kept) continue;
    }
    byNode.set(log.nodeId, log);
  }

  const running = runStatus === "running";
  // What the server says is in flight, which beats any guess made here.
  const saidRunning = new Set(
    [...byNode.entries()]
      .filter(([, log]) => log.status === "running")
      .map(([nodeId]) => nodeId),
  );
  // A run that ended well has nothing left to do: whatever never reported was
  // not reached, rather than still pending.
  const succeeded = runStatus === "success";
  let firstUnanswered = true;
  let failed = false;

  const placed: RunStep[] = steps.map((step) => {
    const log = byNode.get(step.id);
    const status = log?.status;
    if (status === "success" || status === "degraded") {
      return {
        id: step.id,
        name: step.name,
        state: "done",
        durationMs: log?.durationMs,
      };
    }
    if (status === "failed") {
      failed = true;
      return {
        id: step.id,
        name: step.name,
        state: "failed",
        durationMs: log?.durationMs,
      };
    }
    // The server named this node as the one it is working on. Every node it
    // names is shown, so a level running four at once reads as four.
    if (status === "running") {
      return { id: step.id, name: step.name, state: "running" };
    }
    // No answer yet, and the server has not named anything either: the first
    // node still unanswered is the one being worked on, as long as the run is
    // going and nothing has failed.
    if (running && !failed && firstUnanswered && saidRunning.size === 0) {
      firstUnanswered = false;
      return { id: step.id, name: step.name, state: "running" };
    }
    return {
      id: step.id,
      name: step.name,
      state: succeeded ? "skipped" : "pending",
    };
  });

  // Everything that will not happen again counts as behind us, so a finished
  // run reads as finished whether or not every node had work to do.
  const completed = placed.filter(
    (s) => s.state !== "pending" && s.state !== "running",
  ).length;
  const total = placed.length;
  return {
    steps: placed,
    completed,
    total,
    // A run with no steps is finished by definition rather than 0%.
    percent: total === 0 ? 100 : Math.round((completed / total) * 100),
    current: placed.find((s) => s.state === "running") ?? null,
    failed,
  };
}
