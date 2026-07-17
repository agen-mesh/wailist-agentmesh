export type NodeType =
  "trigger" | "agent" | "provider" | "tool" | "tool402" | "action" | "end";
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
  discoveredParams?: Array<{
    name: string;
    type: string;
    required: boolean;
    description: string;
    default?: string;
  }>;
  paramDefaults?: Record<string, string>;
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

// ── Usage & Credits ─────────────────────────────────────────────────────────
export type UsageRange = "24h" | "7d" | "30d";
export type UsageCategory = "x402" | "llm" | "action";

export interface UsageSummary {
  totalAlgo: number; // x402 spend actually settled on-chain
  x402Calls: number;
  llmTokens: number;
  llmEstAlgo: number | null; // null = backend can't price tokens yet
  budget: { limit: number; used: number; resetsAt: string } | null;
  deltas: { totalAlgoPct: number };
}

export interface UsagePoint {
  ts: string; // pre-formatted bucket label (day / hour)
  x402Algo: number;
  llmAlgo: number;
  calls: number; // x402 calls in this bucket (usage series)
}

export interface WorkflowSpend {
  workflowId: string;
  name: string;
  status?: string;
  algo: number;
  calls: number;
}

export interface EndpointUsage {
  endpoint: string;
  host: string;
  provider: string;
  type: UsageCategory;
  calls: number;
  unitPrice: number | null; // null for token-priced LLM rows
  unit: string;
  totalAlgo: number;
  pctOfSpend: number;
  successRate: number | null;
  lastUsedAt: string; // ISO
}

export interface Settlement {
  ts: string; // ISO
  endpoint: string;
  amountAlgo: number;
  txId: string;
  explorerURL: string;
  workflowId: string;
}

export interface UsagePayload {
  summary: UsageSummary;
  timeseries: UsagePoint[];
  byWorkflow: WorkflowSpend[];
  byEndpoint: EndpointUsage[];
  settlements: Settlement[];
}

export interface PortCoord {
  x: number;
  y: number;
}
