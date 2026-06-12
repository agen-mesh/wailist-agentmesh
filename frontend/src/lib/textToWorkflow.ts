import { WorkflowNode, WorkflowEdge, MarketplaceEndpoint } from "./types";

// ── Public types ───────────────────────────────────────────────────────────

export type TriggerTemplate = "manual" | "chat" | "webhook" | "cron";
export type ActionTemplate  = "email" | "slack" | "discord";

export interface DraftSlot {
  role: "trigger" | "agent" | "provider" | "tool" | "action" | "end";
  resolved: boolean;
  skip?: boolean;                        // tier-decision: user chose "let AI handle it"
  template?: string;
  endpoint?: MarketplaceEndpoint;
  candidates?: MarketplaceEndpoint[];    // populated for unresolved TOOL_PICK slots
  meta: Partial<WorkflowNode>;
}

export type QuestionKind =
  | "TRIGGER_TYPE"
  | "TOOL_PICK"
  | "TIER_DECISION"
  | "EMAIL_RECIPIENT";

export interface ClarifyQuestion {
  id: string;
  kind: QuestionKind;
  prompt: string;
  options?: Array<{ label: string; value: string }>;
  freeText?: boolean;
  slotIndex: number;
}

export interface DraftPlan {
  name: string;
  slots: DraftSlot[];
  questions: ClarifyQuestion[];
}

// ── Internal keyword maps ──────────────────────────────────────────────────

const TRIGGER_WORDS: Record<string, TriggerTemplate> = {
  schedule: "cron",  daily: "cron",    hourly: "cron",  weekly: "cron",
  morning:  "cron",  nightly: "cron",  cron: "cron",    every: "cron",
  webhook:  "webhook", incoming: "webhook",
  chat:     "chat",  message: "chat",  input: "chat",   user: "chat",
  manual:   "manual", click: "manual",
};

const ACTION_WORDS: Record<string, ActionTemplate> = {
  email:   "email",   mail:   "email",   send: "email",
  notify:  "email",   alert:  "email",   report: "email",
  slack:   "slack",
  discord: "discord",
};

// Verbs/adjectives the LLM handles natively — no external tool needed
const LLM_ONLY_VERBS = new Set([
  "summarize", "summary", "translate", "analyze", "analysis",
  "classify",  "write",   "draft",     "compose", "answer",
  "explain",   "review",  "format",    "extract", "organize",
  "compare",   "list",    "describe",  "plan",    "generate",
]);

// Known external service names and their base URLs
const HTTP_SERVICE_URLS: Record<string, string> = {
  github:    "https://api.github.com",
  stripe:    "https://api.stripe.com/v1",
  twitter:   "https://api.twitter.com/2",
  notion:    "https://api.notion.com/v1",
  airtable:  "https://api.airtable.com/v0",
  linear:    "https://api.linear.app/graphql",
  jira:      "https://your-org.atlassian.net/rest/api/3",
  shopify:   "https://your-store.myshopify.com/admin/api/2024-01",
  salesforce:"https://your-org.my.salesforce.com/services/data/v59.0",
};

const TRIGGER_LABELS: Record<TriggerTemplate, string> = {
  manual: "Manual Trigger", chat: "On Chat Message",
  webhook: "Webhook",       cron: "Schedule",
};
const TRIGGER_ICONS: Record<TriggerTemplate, string> = {
  manual: "▶", chat: "◴", webhook: "◷", cron: "◵",
};
const ACTION_LABELS: Record<ActionTemplate, string> = {
  email: "Send Email", slack: "Slack Message", discord: "Discord Message",
};
const ACTION_ICONS: Record<ActionTemplate, string> = {
  email: "✉", slack: "#", discord: "d",
};

const STOP_WORDS = new Set([
  "a","an","the","i","want","make","create","build","that","and","or",
  "my","me","us","it","to","for","with","in","on","at","is","are",
  "can","will","should","would","could","please","need","get","use",
  "new","some","any","all","this","that","when","where","how","what",
]);
