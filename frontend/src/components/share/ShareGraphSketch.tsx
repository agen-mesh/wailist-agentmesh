import type { WorkflowEdge, WorkflowNode } from "@/lib/types";

// A small picture of the workflow, for somebody deciding whether to import it.
//
// The preview described the graph only as a list of node names, which says
// what is in it but nothing about its shape -- whether it is a straight line,
// a fan-out, or an agent with four tools hanging off it. That shape is most of
// what "is this what I want?" actually means.
//
// Deliberately not the canvas. This is a thumbnail: no labels, no ports, no
// interaction. It reuses the canvas's own coordinates, so the sketch matches
// what the sender was looking at.

// Colour carries the one distinction worth making at this size: what starts
// the workflow, what thinks, and what costs money. Everything else is a plain
// dot rather than a legend nobody asked for.
const FILL: Record<string, string> = {
  trigger: "var(--accent)",
  agent: "var(--accent)",
  provider: "var(--fg-muted)",
  tool: "var(--fg-muted)",
  tool402: "var(--warm)",
  tendril: "var(--warm)",
  google: "var(--fg-muted)",
  action: "var(--fg-muted)",
  state: "var(--fg-dim)",
  end: "var(--fg-dim)",
};

const W = 520;
const H = 132;
const PAD = 14;
const R = 5;

export function ShareGraphSketch({
  nodes,
  edges,
}: {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
}) {
  if (nodes.length === 0) return null;

  // A node saved before the canvas had coordinates -- or one pasted in a code
  // that never carried them -- has no x/y. Falling back to a row keeps the
  // sketch honest rather than stacking every node at the origin.
  const hasCoords = nodes.some(
    (n) => typeof n.x === "number" && typeof n.y === "number",
  );

  const placed = hasCoords
    ? nodes.map((n) => ({ id: n.id, type: n.type, x: n.x ?? 0, y: n.y ?? 0 }))
    : nodes.map((n, i) => ({ id: n.id, type: n.type, x: i * 100, y: 0 }));

  const xs = placed.map((p) => p.x);
  const ys = placed.map((p) => p.y);
  const minX = Math.min(...xs);
  const maxX = Math.max(...xs);
  const minY = Math.min(...ys);
  const maxY = Math.max(...ys);
  // Guard the degenerate cases: one node, or a perfectly straight row, makes
  // the span zero and the scale infinite.
  const spanX = maxX - minX || 1;
  const spanY = maxY - minY || 1;

  const at = (p: { x: number; y: number }) => ({
    x: PAD + ((p.x - minX) / spanX) * (W - PAD * 2),
    // A single row sits on the centre line rather than pinned to the top.
    y: maxY === minY ? H / 2 : PAD + ((p.y - minY) / spanY) * (H - PAD * 2),
  });

  const points = new Map(placed.map((p) => [p.id, at(p)]));

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      width="100%"
      height={H}
      role="img"
      aria-label={`${nodes.length} nodes and ${edges.length} connections`}
      style={{ display: "block", marginBottom: 18 }}
    >
      {edges.map((e, i) => {
        const a = points.get(e.from);
        const b = points.get(e.to);
        if (!a || !b) return null;
        return (
          <line
            key={e.id || `${e.from}-${e.to}-${i}`}
            x1={a.x}
            y1={a.y}
            x2={b.x}
            y2={b.y}
            stroke="var(--border-strong)"
            strokeWidth={1.5}
            // An attach edge -- a model or tool hanging off an agent -- is a
            // different kind of relationship from the flow, and reads as one.
            strokeDasharray={e.kind === "attach" ? "3 3" : undefined}
          />
        );
      })}
      {placed.map((p) => {
        const c = points.get(p.id);
        if (!c) return null;
        return (
          <circle
            key={p.id}
            cx={c.x}
            cy={c.y}
            r={R}
            fill={FILL[p.type] ?? "var(--fg-muted)"}
          />
        );
      })}
    </svg>
  );
}
