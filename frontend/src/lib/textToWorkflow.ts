import { WorkflowNode, WorkflowEdge, MarketplaceEndpoint } from "./types";

// ── Public types ───────────────────────────────────────────────────────────

export type TriggerTemplate = "manual" | "chat" | "webhook" | "cron";
export type ActionTemplate  = "email" | "slack" | "discord";

export interface DraftSlot {
  role: "trigger" | "agent" | "provider" | "tool" | "action" | "end";
  resolved: boolean;
  skip?: boolean;                        // tier-decision: user chose "let AI handle it"
  template?: string;                     // trigger/action template id, e.g. "chat" or "email"
  endpoint?: MarketplaceEndpoint;        // set when resolved is true for tool slots
  candidates?: MarketplaceEndpoint[];    // TOOL_PICK only: options the user will choose between
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

// Known external service names → base URLs (org-specific entries are placeholders the user fills in Inspector)
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

// ── Scorer ─────────────────────────────────────────────────────────────────

function scoreEndpoint(tokens: string[], ep: MarketplaceEndpoint): number {
  let score = 0;
  const desc = ep.description.toLowerCase();
  const name = ep.name.toLowerCase();
  for (const token of tokens) {
    for (const tag of ep.tags) {
      if (tag === token || tag.startsWith(token) || token.startsWith(tag)) score += 3;
    }
    if (name.includes(token)) score += 2;
    if (desc.includes(token)) score += 1;
    if (ep.category === token) score += 2;
  }
  return score;
}

function deriveWorkflowName(text: string): string {
  const clean = text.toLowerCase().replace(/[^a-z\s]/g, "").trim();
  const tokens = (clean.match(/[a-z]+/g) ?? []).filter(
    (t) => !STOP_WORDS.has(t) && t.length > 2,
  );
  const top = tokens.slice(0, 3).map((t) => t[0].toUpperCase() + t.slice(1));
  return top.join(" ") || "New Workflow";
}

// ── buildDraftPlan ──────────────────────────────────────────────────────────
//
// Call this with the user's raw text and the full merged endpoint list
// (MARKETPLACE_ENDPOINTS + live GoPlausible). Returns a DraftPlan whose
// .questions array contains at most 3 items — one per ambiguous slot.

export function buildDraftPlan(
  text: string,
  allEndpoints: MarketplaceEndpoint[],
): DraftPlan {
  const raw     = text.toLowerCase();
  const tokens  = (raw.match(/[a-z]+/g) ?? []).filter(
    (t) => t.length > 2 && !STOP_WORDS.has(t),
  );

  const slots: DraftSlot[]       = [];
  const questions: ClarifyQuestion[] = [];

  // ── 1. Trigger ───────────────────────────────────────────────────────────
  let triggerTemplate: TriggerTemplate | null = null;
  for (const t of tokens) {
    if (TRIGGER_WORDS[t]) { triggerTemplate = TRIGGER_WORDS[t]; break; }
  }

  if (triggerTemplate) {
    slots.push({
      role: "trigger", resolved: true, template: triggerTemplate,
      meta: {
        type: "trigger", template: triggerTemplate,
        label: TRIGGER_LABELS[triggerTemplate],
        icon:  TRIGGER_ICONS[triggerTemplate],
      },
    });
  } else {
    const slotIdx = slots.length;
    slots.push({ role: "trigger", resolved: false, meta: { type: "trigger" } });
    questions.push({
      id: "q-trigger", kind: "TRIGGER_TYPE",
      prompt: "How should this workflow start?",
      options: [
        { label: "Chat / user message",     value: "chat"    },
        { label: "On a schedule (cron)",    value: "cron"    },
        { label: "Webhook / HTTP POST",     value: "webhook" },
        { label: "Manual (click to run)",   value: "manual"  },
      ],
      slotIndex: slotIdx,
    });
  }

  // ── 2. Agent (always) ────────────────────────────────────────────────────
  slots.push({
    role: "agent", resolved: true,
    meta: {
      type: "agent", template: "agent", name: "AI Agent", icon: "◇",
      systemPrompt: `You are a helpful AI agent. ${text.trim()}`,
    },
  });

  // ── 3. Provider (always Gemini 2.5 Flash) ───────────────────────────────
  slots.push({
    role: "provider", resolved: true,
    meta: { type: "provider", template: "gemini", name: "Gemini 2.5 Flash", model: "gemini-2.5-flash", icon: "G" },
  });

  // ── 4. Tools — score all endpoints, resolve or ask ──────────────────────
  const scored = allEndpoints
    .map((ep) => ({ ep, score: scoreEndpoint(tokens, ep) }))
    .filter(({ score }) => score >= 2)
    .sort((a, b) => b.score - a.score);

  // Group top results by category; pick best per category
  const seenCategories = new Set<string>();
  const seenIds        = new Set<string>();
  const grouped: Array<{ category: string; items: typeof scored }> = [];
  for (const item of scored) {
    const cat = item.ep.category;
    if (seenCategories.has(cat)) {
      grouped.find((g) => g.category === cat)!.items.push(item);
    } else {
      seenCategories.add(cat);
      grouped.push({ category: cat, items: [item] });
    }
  }

  for (const { items } of grouped) {
    if (seenIds.has(items[0].ep.id)) continue;

    const ambiguous =
      items.length > 1 && items[0].score - items[1].score < 2;

    if (ambiguous && questions.length < 3) {
      const slotIdx = slots.length;
      const candidates = items.slice(0, 4).map(({ ep }) => ep);
      // Register all candidates so later categories don't re-pick them
      for (const ep of candidates) seenIds.add(ep.id);
      slots.push({
        role: "tool", resolved: false,
        candidates,
        meta: { type: "tool402" },
      });
      questions.push({
        id: `q-tool-${items[0].ep.category}`,
        kind: "TOOL_PICK",
        prompt: `Multiple ${items[0].ep.category} tools matched. Which should I use?`,
        options: candidates.map((ep) => ({
          label: `${ep.icon ?? "◌"} ${ep.name} ($${ep.price}/${ep.unit})`,
          value: ep.id,
        })),
        slotIndex: slotIdx,
      });
    } else {
      // Clear winner — auto-resolve
      const ep = items[0].ep;
      seenIds.add(ep.id);
      slots.push({
        role: "tool", resolved: true, endpoint: ep,
        meta: {
          type: "tool402", name: ep.name, icon: ep.icon,
          description: ep.description, price: ep.price, unit: ep.unit,
          endpoint: ep.endpoint, discoveredParams: ep.discoveredParams,
        },
      });
    }
  }

  // ── 5. Unrecognized nouns — HTTP fallback or tier-decision ───────────────
  const recognizedTerms = new Set([
    ...Object.keys(TRIGGER_WORDS),
    ...Object.keys(ACTION_WORDS),
    ...LLM_ONLY_VERBS,
    ...allEndpoints.flatMap((e) => e.tags),
    ...allEndpoints.map((e) => e.name.toLowerCase().split(/\s+/)).flat(),
  ]);
  const unknownNouns = tokens.filter(
    (t) => !recognizedTerms.has(t) && t.length > 3,
  );

  for (const noun of unknownNouns.slice(0, 1)) {
    const httpUrl = HTTP_SERVICE_URLS[noun];
    const slotIdx = slots.length;
    slots.push({
      role: "tool", resolved: !!httpUrl,
      meta: { type: "tool", name: httpUrl ? `${noun[0].toUpperCase()}${noun.slice(1)} API` : "HTTP Request", url: httpUrl ?? "", icon: "⟶" },
    });
    if (!httpUrl && questions.length < 3) {
      questions.push({
        id: `q-tier-${noun}`,
        kind: "TIER_DECISION",
        prompt: `"${noun}" — is this an external API to call, or should the AI handle it?`,
        options: [
          { label: "External API (HTTP node)",    value: "http" },
          { label: "Let the AI handle it",        value: "llm"  },
        ],
        slotIndex: slotIdx,
      });
    }
  }

  // ── 6. Action / output ───────────────────────────────────────────────────
  let actionTemplate: ActionTemplate | null = null;
  for (const t of tokens) {
    if (ACTION_WORDS[t]) { actionTemplate = ACTION_WORDS[t]; break; }
  }

  if (actionTemplate) {
    const slotIdx = slots.length;
    slots.push({
      role: "action", resolved: actionTemplate !== "email",
      template: actionTemplate,
      meta: {
        type: "action", template: actionTemplate,
        name: ACTION_LABELS[actionTemplate],
        icon: ACTION_ICONS[actionTemplate],
      },
    });
    if (actionTemplate === "email" && questions.length < 3) {
      questions.push({
        id: "q-email-to",
        kind: "EMAIL_RECIPIENT",
        prompt: "Who should receive the email? (leave blank to configure later)",
        freeText: true,
        slotIndex: slotIdx,
      });
    }
  }

  // ── 7. End (always) ──────────────────────────────────────────────────────
  slots.push({
    role: "end", resolved: true,
    meta: { type: "end", template: "done", label: "End", icon: "■" },
  });

  return { name: deriveWorkflowName(text), slots, questions };
}

// ── applyAnswers ────────────────────────────────────────────────────────────
//
// Takes the DraftPlan and a map of question.id → user answer string.
// Returns a new DraftPlan with all answerable slots resolved.

export function applyAnswers(
  draft: DraftPlan,
  answers: Record<string, string>,
): DraftPlan {
  const slots = draft.slots.map((s) => ({ ...s, meta: { ...s.meta } }));

  for (const q of draft.questions) {
    const answer = answers[q.id];
    if (answer === undefined || answer === "") continue;

    const slot = slots[q.slotIndex];

    switch (q.kind) {
      case "TRIGGER_TYPE": {
        const tpl = answer as TriggerTemplate;
        slot.template = tpl;
        slot.meta     = { type: "trigger", template: tpl, label: TRIGGER_LABELS[tpl], icon: TRIGGER_ICONS[tpl] };
        slot.resolved = true;
        break;
      }
      case "TOOL_PICK": {
        const ep = slot.candidates?.find((c) => c.id === answer);
        if (ep) {
          slot.endpoint = ep;
          slot.meta     = {
            type: "tool402", name: ep.name, icon: ep.icon,
            description: ep.description, price: ep.price, unit: ep.unit,
            endpoint: ep.endpoint, discoveredParams: ep.discoveredParams,
          };
          slot.resolved = true;
        }
        break;
      }
      case "TIER_DECISION": {
        if (answer === "llm") {
          slot.skip     = true;
          slot.resolved = true;
        } else {
          // "http" — HTTP node already has the right meta; just mark resolved
          slot.resolved = true;
        }
        break;
      }
      case "EMAIL_RECIPIENT": {
        slot.meta     = { ...slot.meta, emailTo: answer };
        slot.resolved = true;
        break;
      }
    }
  }

  // Any remaining unresolved slots → auto-resolve with sensible defaults
  for (const q of draft.questions) {
    if (answers[q.id] === "" || answers[q.id] === undefined) {
      const slot = slots[q.slotIndex];
      if (!slot.resolved) {
        if (q.kind === "EMAIL_RECIPIENT") slot.resolved = true;
        if (q.kind === "TRIGGER_TYPE") {
          slot.template = "manual";
          slot.meta     = { type: "trigger", template: "manual", label: TRIGGER_LABELS.manual, icon: TRIGGER_ICONS.manual };
          slot.resolved = true;
        }
        if (q.kind === "TOOL_PICK") { slot.skip = true; slot.resolved = true; }
        if (q.kind === "TIER_DECISION") slot.resolved = true;
      }
    }
  }

  return { ...draft, slots };
}

// ── buildWorkflow ───────────────────────────────────────────────────────────
//
// Converts a fully-resolved DraftPlan to WorkflowNode[] + WorkflowEdge[].
// Layout: flow nodes in a horizontal row at y=200; attach nodes (provider,
// tools) centred below the agent at y=420.

export function buildWorkflow(plan: DraftPlan): {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
} {
  const active = plan.slots.filter((s) => s.resolved && !s.skip);

  const flowSlots   = active.filter((s) =>
    s.role === "trigger" || s.role === "agent" || s.role === "action" || s.role === "end",
  );
  const attachSlots = active.filter((s) => s.role === "provider" || s.role === "tool");

  const FLOW_X0    = 80;
  const FLOW_Y     = 200;
  const FLOW_GAP   = 280;
  const ATTACH_Y   = 420;
  const ATTACH_GAP = 200;

  const now     = Date.now();
  let nodeCount = 0;
  let edgeCount = 0;
  const nid = () => `n_${now + nodeCount++}`;
  const eid = () => `e_${now + 1000 + edgeCount++}`;

  const nodes: WorkflowNode[] = [];
  const edges: WorkflowEdge[] = [];

  // Flow nodes
  flowSlots.forEach((slot, i) => {
    nodes.push({ id: nid(), x: FLOW_X0 + i * FLOW_GAP, y: FLOW_Y, ...slot.meta } as WorkflowNode);
  });

  // Attach nodes — centred below the agent node (found by role, not position)
  const agentNode = nodes[flowSlots.findIndex((s) => s.role === "agent")] ?? null;
  const agentX    = agentNode?.x ?? FLOW_X0 + FLOW_GAP;
  const attachX0  = agentX - Math.floor(attachSlots.length / 2) * ATTACH_GAP;

  attachSlots.forEach((slot, i) => {
    nodes.push({ id: nid(), x: attachX0 + i * ATTACH_GAP, y: ATTACH_Y, ...slot.meta } as WorkflowNode);
  });

  // Flow edges (trigger → agent → action → end)
  for (let i = 0; i < flowSlots.length - 1; i++) {
    edges.push({ id: eid(), from: nodes[i].id, to: nodes[i + 1].id, kind: "flow", toPort: "in" });
  }

  // Attach edges (provider → agent:model, tool → agent:tools)
  if (agentNode) {
    const attachNodes = nodes.slice(flowSlots.length);
    attachSlots.forEach((slot, i) => {
      edges.push({
        id: eid(),
        from: attachNodes[i].id,
        to:   agentNode.id,
        kind: "attach",
        toPort: slot.role === "provider" ? "model" : "tools",
      });
    });
  }

  return { nodes, edges };
}
