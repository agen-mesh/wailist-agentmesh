export type NodeType = "trigger" | "agent" | "provider" | "tool" | "tool402" | "action" | "end";
export type EdgeKind = "flow" | "attach";
export type PortName = "in" | "out" | "model" | "tools" | "top";

export interface WorkflowNode {
  id: string;
  type: NodeType;
  template?: string;
  x: number;
  y: number;
  // display
  name?: string;
  label?: string;
  icon?: string;
  sub?: string;
  custom?: boolean;
  // agent-specific
  systemPrompt?: string;
  wallet?: string;
  balance?: string;
  spent?: string;
  // provider-specific
  apiKey?: string;
  model?: string;
  // tool-specific
  url?: string;
  method?: string;
  // tool402-specific
  endpoint?: string;
  price?: string;
  unit?: string;
  provider?: string;
  priceLive?: boolean;
  // trigger-specific
  source?: string;
}

export interface WorkflowEdge {
  id: string;
  from: string;
  to: string;
  kind: EdgeKind;
  toPort?: PortName;
}

export interface Workflow {
  id: string;
  name: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  status?: "active" | "paused" | "draft";
  updated?: string;
  agents?: number;
  runs?: number;
  spend?: string;
  tags?: string[];
}

export interface NodeTypeMeta {
  w: number;
  h: number;
  ports: PortName[];
}

export interface PortCoord {
  x: number;
  y: number;
}
