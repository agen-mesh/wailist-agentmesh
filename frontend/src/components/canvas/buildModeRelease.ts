import type { WorkflowNode, WorkflowEdge } from "@/lib/types";

/** Agents that have a provider on their "model" port. */
function agentsWithModel(
  byId: Map<string, WorkflowNode>,
  edges: WorkflowEdge[],
): Set<string> {
  return new Set(
    edges
      .filter(
        (e) =>
          e.kind === "attach" &&
          e.toPort === "model" &&
          byId.get(e.from)?.type === "provider" &&
          byId.get(e.to)?.type === "agent",
      )
      .map((e) => e.to),
  );
}

/**
 * Whether some agent in the graph has no provider attached to its "model"
 * port -- the one thing that makes a graph need a model before it can run.
 * Used to word the run-blocked card precisely: "no model attached" is only
 * true when an agent actually lacks one.
 */
export function agentMissingModel(
  nodes: WorkflowNode[],
  edges: WorkflowEdge[],
): boolean {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const modelled = agentsWithModel(byId, edges);
  return nodes.some((n) => n.type === "agent" && !modelled.has(n.id));
}

// Node types that only ever run as steps in the flow. A tool is a flow step
// only when a flow edge touches it; attached to an agent's tools port, the
// agent calls it instead. Mirrors alwaysFlowStep in graphvalidate.go.
const ALWAYS_FLOW_STEP = new Set(["agent", "action", "state", "end", "google", "tendril"]);

// Whether the graph has everything a run actually needs: a trigger that
// flows into at least one step, every agent with a provider on its "model"
// port, and EVERY flow step reachable from the trigger. Anything less and
// starting a run would fail, so chat should stay in build mode.
//
// Every flow step, not just one: a step nothing flows into is not skipped.
// The engine runs it first, beside the trigger, on an empty input. That is
// exactly how a builder-made Nifty/Sensex workflow died in 3ms -- one extract
// step was never wired to its fetch -- while this still said "runnable"
// because some other step was reachable.
//
// No agent is required. A pure data pipeline (trigger -> http -> json_extract
// -> end) needs no language step, the engine runs tool-only flows, and the
// chat builder now builds exactly that for requests like "fetch the Nifty 50
// price daily". Requiring an agent left such a workflow in build mode behind a
// "No model attached yet" card for a graph that needs no model.
//
// Reachable, not directly connected: the builder's API-backed workflows reach
// their agent through tool steps (trigger -> http -> agent).
//
// Must stay in step with auditGraph (backend/internal/engine/nodes/
// graphvalidate.go), which applies the same rule from the other side. If the
// two disagree, the builder can declare a graph finished that this still
// refuses to release, and the user is stuck in build mode.
//
// Pure and framework-free so the release rule is unit-testable on its own:
// getting this wrong strands the user in build mode, which is the exact bug
// this predicate exists to fix.
export function isGraphRunnable(
  nodes: WorkflowNode[],
  edges: WorkflowEdge[],
): boolean {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const agents = nodes.filter((n) => n.type === "agent").map((n) => n.id);

  // Every agent, not just one: an agent without a model fails the run the
  // moment execution reaches it. (Mirrors BuildAttachMap, which drops a
  // model-attach edge whose source is not a Provider -- hence the type check
  // rather than trusting toPort alone.)
  const modelled = agentsWithModel(byId, edges);
  if (!agents.every((id) => modelled.has(id))) return false;

  if (!flowReach(nodes, edges).reachesStep) return false;
  // A loop fails every run: the engine topologically sorts the flow and
  // rejects a cycle outright.
  if (hasFlowLoop(nodes, edges)) return false;
  return firstUnreachedStep(nodes, edges) === null;
}

/**
 * Whether the flow loops back on itself (A -> B -> A). The engine cannot run
 * a loop, and one drawn by hand never goes through the builder's own check.
 */
export function hasFlowLoop(
  nodes: WorkflowNode[],
  edges: WorkflowEdge[],
): boolean {
  const flow = edges.filter((e) => e.kind === "flow");
  const reaches = (src: string, dst: string): boolean => {
    const seen = new Set<string>();
    const queue = [src];
    while (queue.length > 0) {
      const id = queue.shift()!;
      if (id === dst) return true;
      if (seen.has(id)) continue;
      seen.add(id);
      for (const e of flow) if (e.from === id) queue.push(e.to);
    }
    return false;
  };
  const ids = new Set(nodes.map((n) => n.id));
  return flow.some((e) => ids.has(e.from) && reaches(e.to, e.from));
}

interface GraphShape {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
}

/**
 * Whether a finished build should switch chat back to run mode.
 *
 * Only when build mode was forced -- the graph could not run, or leave build
 * mode, before this build -- and the build made it runnable. A user who chose
 * Build themselves on an already-runnable workflow keeps that choice: flipping
 * them back to Run meant their next message started a real, billed run instead
 * of another edit.
 */
export function shouldReleaseBuildMode(
  before: GraphShape,
  after: GraphShape,
): boolean {
  const couldLeaveBefore =
    isGraphRunnable(before.nodes, before.edges) ||
    before.nodes.some((n) => n.type === "provider");
  return !couldLeaveBefore && isGraphRunnable(after.nodes, after.edges);
}

/**
 * The first flow step the trigger never reaches -- the node the run-blocked
 * card should name, since "nothing to run" is wrong for a graph full of steps
 * where one simply has nothing flowing into it. Null when every step is reached.
 */
export function firstUnreachedStep(
  nodes: WorkflowNode[],
  edges: WorkflowEdge[],
): WorkflowNode | null {
  const { seen, onFlowEdge } = flowReach(nodes, edges);
  return (
    nodes.find((n) => {
      if (n.type === "trigger" || n.type === "provider") return false;
      const isFlowStep = ALWAYS_FLOW_STEP.has(n.type) || onFlowEdge.has(n.id);
      return isFlowStep && !seen.has(n.id);
    }) ?? null
  );
}

// Walk forward from every trigger along flow edges only. Attach edges are not
// part of the execution path -- counting them would let a provider shared by
// two agents look like a route between them.
function flowReach(nodes: WorkflowNode[], edges: WorkflowEdge[]) {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const next = new Map<string, string[]>();
  const onFlowEdge = new Set<string>();
  for (const e of edges) {
    if (e.kind !== "flow") continue;
    onFlowEdge.add(e.from);
    onFlowEdge.add(e.to);
    const out = next.get(e.from);
    if (out) out.push(e.to);
    else next.set(e.from, [e.to]);
  }
  const seen = new Set<string>();
  const queue = nodes.filter((n) => n.type === "trigger").map((n) => n.id);
  let reachesStep = false;
  while (queue.length > 0) {
    const id = queue.shift()!;
    if (seen.has(id)) continue;
    seen.add(id);
    const type = byId.get(id)?.type;
    if (type && type !== "trigger") reachesStep = true;
    for (const to of next.get(id) ?? []) queue.push(to);
  }
  return { seen, onFlowEdge, reachesStep };
}
