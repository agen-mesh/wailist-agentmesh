import { WorkflowNode, NodeType, PortCoord, PortName } from "./types";
import { NODE_TYPES } from "./data";

type PortFn = (W: number, H: number) => PortCoord;

const PORT_POS: Record<NodeType, Partial<Record<PortName, PortFn>>> = {
  trigger:  { out:   (W, H) => ({ x: W,        y: H / 2 }) },
  agent:    {
    in:    ()     => ({ x: 0,          y: 38 }),
    out:   (W)    => ({ x: W,          y: 38 }),
    model: (W, H) => ({ x: W * 0.28,   y: H }),
    tools: (W, H) => ({ x: W * 0.72,   y: H }),
  },
  provider: { top:   (W)    => ({ x: W / 2,    y: 0 }) },
  tool:     { top:   (W)    => ({ x: W / 2,    y: 0 }) },
  tool402:  { top:   (W)    => ({ x: W / 2,    y: 0 }) },
  action:   {
    in:    (W, H) => ({ x: 0,    y: H / 2 }),
    out:   (W, H) => ({ x: W,    y: H / 2 }),
  },
  end:      { in:    (W, H) => ({ x: 0,    y: H / 2 }) },
};

export function portWorld(node: WorkflowNode, port: PortName): PortCoord {
  const t = NODE_TYPES[node.type];
  if (!t) return { x: node.x, y: node.y };
  const portMap = PORT_POS[node.type];
  const fn = portMap?.[port];
  if (!fn) return { x: node.x + t.w / 2, y: node.y + t.h / 2 };
  const p = fn(t.w, t.h);
  return { x: node.x + p.x, y: node.y + p.y };
}

export function portForFrom(n: WorkflowNode): PortName {
  if (n.type === "trigger" || n.type === "agent" || n.type === "action") return "out";
  if (n.type === "provider" || n.type === "tool" || n.type === "tool402") return "top";
  return "out";
}

export function portForTo(n: WorkflowNode): PortName {
  if (n.type === "agent" || n.type === "action" || n.type === "end") return "in";
  return "in";
}

export function isValidConnection(
  from: WorkflowNode, fromPort: PortName,
  to: WorkflowNode, toPort: PortName
): boolean {
  // attach: provider/tool/tool402 → agent bottom ports
  if ((from.type === "provider" || from.type === "tool" || from.type === "tool402") && to.type === "agent") {
    return toPort === "model" || toPort === "tools";
  }
  // flow: trigger/agent/action → agent/action/end (in port)
  if (toPort === "in") {
    return (["trigger", "agent", "action"] as NodeType[]).includes(from.type) &&
           (["agent", "action", "end"] as NodeType[]).includes(to.type);
  }
  return false;
}
