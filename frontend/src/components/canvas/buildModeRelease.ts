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

// Whether the graph has everything a run actually needs: a trigger that
// flows into at least one step, every agent with a provider on its "model"
// port, and -- when there are agents -- at least one of them reachable from
// the trigger. Anything less and starting a run would fail, so chat should
// stay in build mode.
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

  // Walk forward from every trigger along flow edges only. Attach edges are
  // not part of the execution path -- counting them would let a provider
  // shared by two agents look like a route between them.
  const next = new Map<string, string[]>();
  for (const e of edges) {
    if (e.kind !== "flow") continue;
    const out = next.get(e.from);
    if (out) out.push(e.to);
    else next.set(e.from, [e.to]);
  }
  const seen = new Set<string>();
  const queue = nodes.filter((n) => n.type === "trigger").map((n) => n.id);
  let reachesStep = false;
  let reachesAgent = false;
  while (queue.length > 0) {
    const id = queue.shift()!;
    if (seen.has(id)) continue;
    seen.add(id);
    const type = byId.get(id)?.type;
    if (type && type !== "trigger") reachesStep = true;
    if (type === "agent") reachesAgent = true;
    for (const to of next.get(id) ?? []) queue.push(to);
  }
  if (!reachesStep) return false;
  return agents.length === 0 || reachesAgent;
}
