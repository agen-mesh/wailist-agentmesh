import type { WorkflowNode, WorkflowEdge } from "@/lib/types";

// Whether the graph has everything a run actually needs: every agent has a
// provider attached to its "model" port, and at least one agent is reachable
// from a trigger along flow edges. Anything less and starting a run would
// fail -- so chat should stay in build mode.
//
// Reachable, not directly connected. The builder's API-backed workflows are
// shaped trigger -> http -> json_extract -> agent: the agent is reached
// through tool steps, never straight off the trigger. Requiring a direct
// trigger -> agent edge stranded exactly those workflows in build mode.
//
// Must stay in step with auditGraph (backend/internal/engine/nodes/
// graphvalidate.go), which applies the same rule from the other side. If the
// two disagree, the builder can declare a graph finished that this still
// refuses to release, and the user is stuck in build mode.
//
// Mirrors the backend's own attach semantics (BuildAttachMap in
// backend/internal/engine/graph.go drops a model-attach edge whose source
// is not a Provider node), which is why the source type is checked here
// rather than trusting toPort alone.
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
  if (agents.length === 0) return false;

  const agentsWithModel = new Set(
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
  // Every agent, not just one: an agent without a model fails the run the
  // moment execution reaches it.
  if (!agents.every((id) => agentsWithModel.has(id))) return false;

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
  while (queue.length > 0) {
    const id = queue.shift()!;
    if (seen.has(id)) continue;
    seen.add(id);
    if (byId.get(id)?.type === "agent") return true;
    for (const to of next.get(id) ?? []) queue.push(to);
  }
  return false;
}
