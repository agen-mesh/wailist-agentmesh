import type { WorkflowNode, WorkflowEdge } from "@/lib/types";

// Whether the graph has everything a run actually needs: a trigger that
// flows into an agent, and a provider attached to that agent's "model"
// port. Anything less and starting a run would fail — so chat should stay
// in build mode.
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
  if (agentsWithModel.size === 0) return false;

  return edges.some(
    (e) =>
      e.kind === "flow" &&
      byId.get(e.from)?.type === "trigger" &&
      agentsWithModel.has(e.to),
  );
}
