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
  description?: string;
  price?: string;
  unit?: string;
  provider?: string;
  priceLive?: boolean;
  discoveredParams?: Array<{ name: string; type: string; required: boolean; description: string; default?: string }>;
  paramDefaults?: Record<string, string>;
  // "use ours / yours" toggles
  useOurKey?: boolean;
  useOurEmail?: boolean;
  selfFundWallet?: boolean;
  // agent limits
  maxCostPerRun?: string;
  maxSpendTotal?: string;
  // webhook trigger live config
  webhookMethod?: string;
  webhookPayloadSchema?: string;
  webhookLiveURL?: string;
  // trigger-specific
  source?: string;
  // email action-specific
  emailTo?: string;
  emailFrom?: string;
  emailSubject?: string;
  emailBody?: string;
  emailApiKey?: string;
  emailProvider?: string;
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
  updatedAt?: string;
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

export interface ParamDef {
  name: string;
  type: string;
  required: boolean;
  description: string;
  default?: string;
}

export interface MarketplaceEndpoint {
  id: string;
  name: string;
  description: string;
  provider: string;
  price: string;
  unit: string;
  category: "search" | "data" | "ai" | "finance" | "media" | "util";
  icon?: string;
  tags: string[];
  author?: string;
  calls?: number;
  rating?: number;
  featured?: boolean;
  // Bazaar-sourced fields — absent on static entries
  endpoint?: string;
  discoveredParams?: ParamDef[];
  source?: "static" | "bazaar";
}

export interface MarketplaceWorkflow {
  id: string;
  name: string;
  description: string;
  author: string;
  tags: string[];
  nodes: number;
  runs: number;
  stars: number;
  price?: string;
  featured?: boolean;
  previewNodes: Array<{ type: string; label: string; template?: string }>;
}
