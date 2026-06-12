# Text-to-Workflow Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a ✨ Generate button to the canvas topbar that lets users describe a workflow in plain text, resolves ambiguity via up to 3 targeted clarifying questions, previews the proposed graph, and applies it to the canvas — no LLM required.

**Architecture:** A pure-TypeScript engine (`textToWorkflow.ts`) tokenizes the user's input, scores all known `MarketplaceEndpoint` items by tag/name/category overlap, classifies unmatched tokens as LLM-only capabilities or HTTP fallback nodes, and generates `ClarifyQuestion` objects for the ≤3 ambiguous slots it cannot auto-resolve. A 3-stage modal (`TextToWorkflowModal.tsx`) runs input → questions → preview, then merges the produced nodes + edges into the existing canvas via `setWorkflow`. Live GoPlausible endpoints are fetched on modal open and merged with the static `MARKETPLACE_ENDPOINTS` before matching.

**Tech Stack:** TypeScript, React 19, Next.js 16 App Router, existing `WorkflowNode`/`WorkflowEdge` types, `marketplace.goplausibleList()` API, CSS vars from `globals.css`. No new dependencies.

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `frontend/src/lib/textToWorkflow.ts` | Pure engine: types, keyword maps, scorer, draft builder, answer applier, workflow assembler |
| Create | `frontend/src/components/canvas/TextToWorkflowModal.tsx` | 3-stage modal: input → questions → preview → apply |
| Modify | `frontend/src/components/canvas/CanvasPage.tsx` | Add `t2wOpen` state, `onOpenGenerate` prop to `CanvasTopbar`, render `<TextToWorkflowModal>` |

---

## Task 1: `textToWorkflow.ts` — engine types and constants

**Files:**
- Create: `frontend/src/lib/textToWorkflow.ts`

- [ ] **Step 1.1: Write types and lookup tables**

Create `frontend/src/lib/textToWorkflow.ts` with the following content exactly:

```typescript
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
```

- [ ] **Step 1.2: Verify TypeScript compiles with no errors**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors (file is types-only so far — imports are valid, all types self-consistent).

- [ ] **Step 1.3: Commit**

```bash
cd /Users/levi/Desktop/agentmesh-new && git add frontend/src/lib/textToWorkflow.ts && git commit -m "feat: scaffold textToWorkflow engine types and keyword maps"
```

---

## Task 2: Engine — scorer, `buildDraftPlan`, `applyAnswers`, `buildWorkflow`

**Files:**
- Modify: `frontend/src/lib/textToWorkflow.ts` (append functions)

- [ ] **Step 2.1: Append the scorer and `buildDraftPlan` function**

Append the following to the end of `frontend/src/lib/textToWorkflow.ts`:

```typescript
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
      slots.push({
        role: "tool", resolved: false,
        candidates: items.slice(0, 4).map(({ ep }) => ep),
        meta: { type: "tool402" },
      });
      questions.push({
        id: `q-tool-${items[0].ep.category}`,
        kind: "TOOL_PICK",
        prompt: `Multiple ${items[0].ep.category} tools matched. Which should I use?`,
        options: items.slice(0, 4).map(({ ep }) => ({
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
    (t) => !recognizedTerms.has(t) && !LLM_ONLY_VERBS.has(t) && t.length > 3,
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
      slots[slotIdx].resolved = false;
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
```

- [ ] **Step 2.2: Verify compilation still clean**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: 0 errors.

- [ ] **Step 2.3: Append `applyAnswers` and `buildWorkflow`**

Append to the end of `frontend/src/lib/textToWorkflow.ts`:

```typescript
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

  // Any remaining unresolved questions (user left blank) → auto-resolve
  // EMAIL_RECIPIENT blank → mark resolved with no emailTo (user fills in Inspector later)
  for (const q of draft.questions) {
    if (answers[q.id] === "" || answers[q.id] === undefined) {
      const slot = slots[q.slotIndex];
      if (!slot.resolved) {
        if (q.kind === "EMAIL_RECIPIENT") slot.resolved = true;
        // TRIGGER_TYPE blank → default to "manual"
        if (q.kind === "TRIGGER_TYPE") {
          slot.template = "manual";
          slot.meta     = { type: "trigger", template: "manual", label: TRIGGER_LABELS.manual, icon: TRIGGER_ICONS.manual };
          slot.resolved = true;
        }
        // TOOL_PICK blank → skip the slot
        if (q.kind === "TOOL_PICK") { slot.skip = true; slot.resolved = true; }
        // TIER_DECISION blank → default to HTTP node
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
// tools) below the agent at y=420.

export function buildWorkflow(plan: DraftPlan): {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
} {
  const active = plan.slots.filter((s) => s.resolved && !s.skip);

  const flowSlots   = active.filter((s) =>
    s.role === "trigger" || s.role === "agent" || s.role === "action" || s.role === "end",
  );
  const attachSlots = active.filter((s) => s.role === "provider" || s.role === "tool");

  const FLOW_X0   = 80;
  const FLOW_Y    = 200;
  const FLOW_GAP  = 280;
  const ATTACH_Y  = 420;
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

  // Attach nodes — centre below the agent (2nd flow node, index 1)
  const agentNode = nodes[flowSlots.findIndex((s) => s.role === "agent")] ?? null;
  const agentX    = agentNode?.x ?? FLOW_X0 + FLOW_GAP;
  const attachX0  = agentX - Math.floor(attachSlots.length / 2) * ATTACH_GAP;

  attachSlots.forEach((slot, i) => {
    nodes.push({ id: nid(), x: attachX0 + i * ATTACH_GAP, y: ATTACH_Y, ...slot.meta } as WorkflowNode);
  });

  // Flow edges
  for (let i = 0; i < flowSlots.length - 1; i++) {
    edges.push({ id: eid(), from: nodes[i].id, to: nodes[i + 1].id, kind: "flow", toPort: "in" });
  }

  // Attach edges
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
```

- [ ] **Step 2.4: Verify compilation**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: 0 errors.

- [ ] **Step 2.5: Commit**

```bash
cd /Users/levi/Desktop/agentmesh-new && git add frontend/src/lib/textToWorkflow.ts && git commit -m "feat: text-to-workflow engine — scorer, buildDraftPlan, applyAnswers, buildWorkflow"
```

---

## Task 3: `TextToWorkflowModal.tsx` — 3-stage modal

**Files:**
- Create: `frontend/src/components/canvas/TextToWorkflowModal.tsx`

- [ ] **Step 3.1: Create the modal component**

Create `frontend/src/components/canvas/TextToWorkflowModal.tsx` with the following content:

```tsx
"use client";
import { useState, useEffect, useRef } from "react";
import { Workflow, WorkflowNode, WorkflowEdge, MarketplaceEndpoint } from "@/lib/types";
import { MARKETPLACE_ENDPOINTS } from "@/lib/data";
import { marketplace } from "@/lib/api";
import {
  buildDraftPlan,
  applyAnswers,
  buildWorkflow,
  DraftPlan,
  ClarifyQuestion,
} from "@/lib/textToWorkflow";

interface TextToWorkflowModalProps {
  setWorkflow: React.Dispatch<React.SetStateAction<Workflow>>;
  onClose: () => void;
}

type Stage = "input" | "questions" | "preview";

export function TextToWorkflowModal({ setWorkflow, onClose }: TextToWorkflowModalProps) {
  const [stage, setStage]       = useState<Stage>("input");
  const [text, setText]         = useState("");
  const [endpoints, setEndpoints] = useState<MarketplaceEndpoint[]>(MARKETPLACE_ENDPOINTS);
  const [draft, setDraft]       = useState<DraftPlan | null>(null);
  const [answers, setAnswers]   = useState<Record<string, string>>({});
  const [preview, setPreview]   = useState<{ nodes: WorkflowNode[]; edges: WorkflowEdge[] } | null>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  // Fetch live GoPlausible endpoints on open and merge with static list
  useEffect(() => {
    marketplace.goplausibleList(50, 0)
      .then((r) => setEndpoints([...MARKETPLACE_ENDPOINTS, ...r.endpoints]))
      .catch(() => {}); // gracefully fall back to static list
    setTimeout(() => inputRef.current?.focus(), 50);
  }, []);

  const onGenerate = () => {
    if (!text.trim()) return;
    const d = buildDraftPlan(text.trim(), endpoints);
    setDraft(d);
    if (d.questions.length === 0) {
      // No ambiguity — go straight to preview
      const result = buildWorkflow(d);
      setPreview(result);
      setStage("preview");
    } else {
      const initial: Record<string, string> = {};
      for (const q of d.questions) initial[q.id] = "";
      setAnswers(initial);
      setStage("questions");
    }
  };

  const onSubmitAnswers = () => {
    if (!draft) return;
    const resolved = applyAnswers(draft, answers);
    const result   = buildWorkflow(resolved);
    setPreview(result);
    setStage("preview");
  };

  const onApply = () => {
    if (!preview) return;
    setWorkflow((wf) => ({
      ...wf,
      nodes: [...wf.nodes, ...preview.nodes],
      edges: [...wf.edges, ...preview.edges],
    }));
    onClose();
  };

  return (
    <div
      style={{ position: "fixed", inset: 0, background: "rgba(8,7,12,0.75)", backdropFilter: "blur(4px)", zIndex: 200, display: "flex", alignItems: "center", justifyContent: "center" }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div style={{ width: 520, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: 12, padding: 24, display: "flex", flexDirection: "column", gap: 16 }}>
        {/* Header */}
        <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between" }}>
          <div>
            <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>
              {stage === "input"     && "✨ Generate workflow"}
              {stage === "questions" && "A few quick questions"}
              {stage === "preview"   && "Workflow preview"}
            </div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginTop: 2 }}>
              {stage === "input"     && "describe what you want — no LLM required"}
              {stage === "questions" && `${draft?.questions.length ?? 0} of 3 max · resolves ambiguous slots`}
              {stage === "preview"   && `${preview?.nodes.length ?? 0} nodes · ${preview?.edges.length ?? 0} edges — will be appended to canvas`}
            </div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--fg-muted)", cursor: "pointer", fontSize: 16, padding: 4 }}>✕</button>
        </div>

        {/* ── Stage: input ── */}
        {stage === "input" && (
          <>
            <textarea
              ref={inputRef}
              value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) onGenerate(); }}
              placeholder="e.g. I want a weather reporter that emails me every morning"
              style={{ width: "100%", minHeight: 90, padding: "10px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", resize: "vertical", outline: "none", lineHeight: 1.6, boxSizing: "border-box" }}
            />
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)" }}>⌘ Enter to generate</span>
              <div style={{ display: "flex", gap: 8 }}>
                <button onClick={onClose} style={ghostBtn}>Cancel</button>
                <button onClick={onGenerate} disabled={!text.trim()} style={{ ...primaryBtn, opacity: !text.trim() ? 0.5 : 1 }}>
                  Generate →
                </button>
              </div>
            </div>
          </>
        )}

        {/* ── Stage: questions ── */}
        {stage === "questions" && draft && (
          <>
            <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
              {draft.questions.map((q) => (
                <QuestionBlock
                  key={q.id}
                  question={q}
                  value={answers[q.id] ?? ""}
                  onChange={(v) => setAnswers((a) => ({ ...a, [q.id]: v }))}
                />
              ))}
            </div>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
              <button onClick={() => setStage("input")} style={ghostBtn}>← Back</button>
              <button onClick={onSubmitAnswers} style={primaryBtn}>Preview →</button>
            </div>
          </>
        )}

        {/* ── Stage: preview ── */}
        {stage === "preview" && preview && draft && (
          <>
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {preview.nodes.map((n) => (
                <NodePreviewRow key={n.id} node={n} />
              ))}
            </div>
            <div style={{ padding: "10px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)" }}>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginBottom: 4 }}>workflow name</div>
              <div style={{ fontSize: 13, fontWeight: 500, color: "var(--fg)" }}>{draft.name}</div>
            </div>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
              <button onClick={() => setStage("input")} style={ghostBtn}>Start over</button>
              <div style={{ display: "flex", gap: 8 }}>
                <button onClick={onClose} style={ghostBtn}>Cancel</button>
                <button onClick={onApply} style={primaryBtn}>Apply to canvas</button>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

// ── QuestionBlock ────────────────────────────────────────────────────────────

function QuestionBlock({ question, value, onChange }: {
  question: ClarifyQuestion;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <div style={{ fontSize: 13, fontWeight: 500, color: "var(--fg)" }}>{question.prompt}</div>
      {question.options && (
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          {question.options.map((opt) => (
            <button
              key={opt.value}
              onClick={() => onChange(opt.value)}
              style={{
                textAlign: "left", padding: "9px 12px",
                background: value === opt.value ? "var(--accent-soft)" : "var(--bg)",
                border: `1px solid ${value === opt.value ? "var(--accent-line)" : "var(--border)"}`,
                borderRadius: "var(--r-2)", color: value === opt.value ? "var(--accent)" : "var(--fg)",
                fontSize: 12, cursor: "pointer", fontFamily: "var(--font-sans)", transition: "all 0.1s",
              }}
            >
              {opt.label}
            </button>
          ))}
        </div>
      )}
      {question.freeText && (
        <input
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="e.g. you@example.com"
          style={{ height: 36, padding: "0 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", outline: "none" }}
        />
      )}
    </div>
  );
}

// ── NodePreviewRow ───────────────────────────────────────────────────────────

const NODE_TYPE_COLORS: Record<string, string> = {
  trigger:  "var(--fg-muted)",
  agent:    "var(--accent)",
  provider: "var(--accent)",
  tool:     "var(--fg-muted)",
  tool402:  "#E879F9",
  action:   "var(--fg-muted)",
  end:      "var(--fg-dim)",
};

function NodePreviewRow({ node }: { node: WorkflowNode }) {
  const color = NODE_TYPE_COLORS[node.type] ?? "var(--fg)";
  const label = node.name ?? node.label ?? node.type;
  const sub   = node.model
    ? node.model
    : node.price
    ? `x402 · $${node.price}/${node.unit}`
    : node.template ?? node.type;

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "8px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)" }}>
      <span style={{ width: 22, height: 22, borderRadius: 6, background: node.type === "tool402" ? "rgba(232,121,249,0.12)" : "var(--bg-elev-2)", color, display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 12, flexShrink: 0 }}>
        {node.icon ?? "·"}
      </span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 12, fontWeight: 500, color: "var(--fg)" }}>{label}</div>
        <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-muted)", textOverflow: "ellipsis", overflow: "hidden", whiteSpace: "nowrap" }}>{sub}</div>
      </div>
      <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color, flexShrink: 0 }}>{node.type}</span>
    </div>
  );
}

// ── Shared button styles ────────────────────────────────────────────────────

const ghostBtn: React.CSSProperties = {
  height: 32, padding: "0 12px", fontSize: 12, fontWeight: 500,
  background: "transparent", border: "1px solid var(--border-strong)",
  borderRadius: "var(--r-2)", color: "var(--fg-muted)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4,
};

const primaryBtn: React.CSSProperties = {
  height: 32, padding: "0 14px", fontSize: 12, fontWeight: 600,
  background: "var(--accent)", border: "1px solid var(--accent)",
  borderRadius: "var(--r-2)", color: "var(--accent-fg)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4,
};
```

- [ ] **Step 3.2: Verify compilation**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: 0 errors.

- [ ] **Step 3.3: Commit**

```bash
cd /Users/levi/Desktop/agentmesh-new && git add frontend/src/components/canvas/TextToWorkflowModal.tsx && git commit -m "feat: TextToWorkflowModal — 3-stage input/questions/preview UI"
```

---

## Task 4: Wire modal into `CanvasPage.tsx`

**Files:**
- Modify: `frontend/src/components/canvas/CanvasPage.tsx`

- [ ] **Step 4.1: Add import and state**

In `frontend/src/components/canvas/CanvasPage.tsx`, add the import for `TextToWorkflowModal` right after the existing imports block (after the `LogDrawer` import line):

Find:
```typescript
import { LogDrawer } from "./LogDrawer";
```

Replace with:
```typescript
import { LogDrawer } from "./LogDrawer";
import { TextToWorkflowModal } from "./TextToWorkflowModal";
```

- [ ] **Step 4.2: Add `t2wOpen` state to `CanvasPage`**

In `CanvasPage`, find the existing state declarations block. Find the line:
```typescript
  const [chatPrompt, setChatPrompt] = useState<string | null>(null); // null = closed
```

Replace with:
```typescript
  const [chatPrompt, setChatPrompt] = useState<string | null>(null); // null = closed
  const [t2wOpen, setT2wOpen] = useState(false);
```

- [ ] **Step 4.3: Add `onOpenGenerate` prop to the `CanvasTopbar` call**

Find the `<CanvasTopbar` JSX block:
```tsx
      <CanvasTopbar
        workflow={workflow} setWorkflow={setWorkflowNN}
        deployed={deployed} running={running}
        onDeploy={onDeploy} onRun={onRun}
        totalSpend={totalSpend} spend24h={spend24h} saveLabel={saveLabel}
        onBack={() => router.push("/workflows")}
        estimatedCost={estimatedCost}
      />
```

Replace with:
```tsx
      <CanvasTopbar
        workflow={workflow} setWorkflow={setWorkflowNN}
        deployed={deployed} running={running}
        onDeploy={onDeploy} onRun={onRun}
        totalSpend={totalSpend} spend24h={spend24h} saveLabel={saveLabel}
        onBack={() => router.push("/workflows")}
        estimatedCost={estimatedCost}
        onOpenGenerate={() => setT2wOpen(true)}
      />
```

- [ ] **Step 4.4: Render `<TextToWorkflowModal>` alongside `<ChatRunModal>`**

Find the block near the end of `CanvasPage`'s return:
```tsx
      {chatPrompt !== null && (
        <ChatRunModal
          value={chatPrompt}
          onChange={setChatPrompt}
          onSubmit={(msg) => startRun({ message: msg })}
          onClose={() => setChatPrompt(null)}
        />
      )}
```

Replace with:
```tsx
      {chatPrompt !== null && (
        <ChatRunModal
          value={chatPrompt}
          onChange={setChatPrompt}
          onSubmit={(msg) => startRun({ message: msg })}
          onClose={() => setChatPrompt(null)}
        />
      )}

      {t2wOpen && (
        <TextToWorkflowModal
          setWorkflow={setWorkflowNN}
          onClose={() => setT2wOpen(false)}
        />
      )}
```

- [ ] **Step 4.5: Add `onOpenGenerate` to `CanvasTopbar` props type and render the button**

Find the `CanvasTopbar` function signature:
```typescript
function CanvasTopbar({ workflow, setWorkflow, deployed, running, onDeploy, onRun, totalSpend, spend24h, saveLabel, onBack, estimatedCost }: {
  workflow: Workflow;
  setWorkflow: React.Dispatch<React.SetStateAction<Workflow>>;
  deployed: boolean; running: boolean;
  onDeploy: () => void; onRun: () => void;
  totalSpend: string; spend24h: string; saveLabel: string;
  onBack: () => void;
  estimatedCost: { usd: number; algo: number };
}) {
```

Replace with:
```typescript
function CanvasTopbar({ workflow, setWorkflow, deployed, running, onDeploy, onRun, totalSpend, spend24h, saveLabel, onBack, estimatedCost, onOpenGenerate }: {
  workflow: Workflow;
  setWorkflow: React.Dispatch<React.SetStateAction<Workflow>>;
  deployed: boolean; running: boolean;
  onDeploy: () => void; onRun: () => void;
  totalSpend: string; spend24h: string; saveLabel: string;
  onBack: () => void;
  estimatedCost: { usd: number; algo: number };
  onOpenGenerate: () => void;
}) {
```

Then find the Share button in `CanvasTopbar`:
```tsx
      <button style={{ ...ghostBtnSm, flexShrink: 0 }}>Share</button>
```

Replace with:
```tsx
      <button onClick={onOpenGenerate} style={{ ...ghostBtnSm, flexShrink: 0 }}>✨ Generate</button>
      <button style={{ ...ghostBtnSm, flexShrink: 0 }}>Share</button>
```

- [ ] **Step 4.6: Verify compilation**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: 0 errors.

- [ ] **Step 4.7: Commit**

```bash
cd /Users/levi/Desktop/agentmesh-new && git add frontend/src/components/canvas/CanvasPage.tsx && git commit -m "feat: wire TextToWorkflowModal into canvas topbar with ✨ Generate button"
```

---

## Task 5: Manual verification in browser

**Files:** none (browser testing only)

- [ ] **Step 5.1: Start the dev server**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npm run dev
```

Open `http://localhost:3000/workflows` in a browser. Open any workflow to reach the canvas.

- [ ] **Step 5.2: Test zero-ambiguity path**

Click the **✨ Generate** button in the topbar. Type:

```
I want a weather agent
```

Click **Generate →**. Expected: skips questions stage, goes straight to preview showing `trigger · agent · provider (Gemini) · tool402 (WeatherKit) · end`. Click **Apply to canvas**. Expected: 5 nodes appear on the canvas with correct edges.

- [ ] **Step 5.3: Test questions path**

Open a fresh workflow. Click **✨ Generate**. Type:

```
search the web and email me results
```

Click **Generate →**. Expected: questions stage appears with at least one question (likely TOOL_PICK for search — Tavily vs Perplexity vs Exa all match "search"), and EMAIL_RECIPIENT. Answer the tool question, type an email address, click **Preview →**. Expected: preview shows correct nodes. Click **Apply to canvas**.

- [ ] **Step 5.4: Test HTTP fallback path**

Open a fresh workflow. Click **✨ Generate**. Type:

```
pull data from github and summarize it
```

Click **Generate →**. Expected: github resolves to HTTP node with pre-filled URL `https://api.github.com` (no TIER_DECISION question since URL is known). "summarize" is an LLM-only verb — no tool added for it. Preview shows: trigger · agent · provider · HTTP tool · end.

- [ ] **Step 5.5: Test TIER_DECISION (unknown noun) path**

Open a fresh workflow. Click **✨ Generate**. Type:

```
monitor acme and notify me
```

Click **Generate →**. Expected: TIER_DECISION question appears for "acme". Select **External API**. EMAIL_RECIPIENT question for "notify". Preview shows the workflow with HTTP tool. Apply.

- [ ] **Step 5.6: Test backdrop close and back button**

Open the modal. Click outside the card → modal closes. Open again, type text, generate → click **← Back** → returns to input stage with text preserved.

- [ ] **Step 5.7: Final commit**

```bash
cd /Users/levi/Desktop/agentmesh-new && git add -A && git commit -m "feat: text-to-workflow complete — keyword matcher, clarifying questions, canvas apply"
```

---

## Self-Review

### Spec coverage

| Requirement | Task |
|---|---|
| Text input describing workflow | Task 3 — input stage |
| Search through tags/tools | Task 2 — `scoreEndpoint` against `allEndpoints` |
| x402 tools from live marketplace | Task 3 — `goplausibleList` on modal open |
| Ask follow-up questions for ambiguity | Task 2 — `buildDraftPlan` generates `ClarifyQuestion[]` |
| Cap questions at 3 | Task 2 — `questions.length < 3` guard |
| HTTP fallback for unknown nouns | Task 2 — `unknownNouns` → tool slot with `HTTP_SERVICE_URLS` lookup |
| LLM fallback for verbs/adjectives | Task 2 — `LLM_ONLY_VERBS` set, no tool node added |
| TIER_DECISION for truly ambiguous nouns | Task 2 — TIER_DECISION question with "http"/"llm" options |
| Preview before applying | Task 3 — preview stage with node list |
| Apply to canvas without LLM | Task 4 — `setWorkflow` merge in `onApply` |
| Correct edge wiring (flow + attach) | Task 2 — `buildWorkflow` with toPort logic |

### Placeholder scan

No TBD, TODO, or "similar to Task N" patterns in the plan.

### Type consistency

- `DraftSlot.meta` is `Partial<WorkflowNode>` throughout — spread into `WorkflowNode` in `buildWorkflow`.
- `ClarifyQuestion.slotIndex` refers to `DraftPlan.slots` array index — used consistently in `applyAnswers`.
- `TriggerTemplate` and `ActionTemplate` union types are used in both the engine and the `applyAnswers` switch — values match the string literals in `TRIGGER_LABELS`/`ACTION_LABELS`.
- `setWorkflow` prop type in modal matches the `setWorkflowNN` dispatch type in `CanvasPage` (`React.Dispatch<React.SetStateAction<Workflow>>`).
- Node IDs use `n_${now + nodeCount++}` — guaranteed unique within a single `buildWorkflow` call.
- Edge IDs use `e_${now + 1000 + edgeCount++}` — offset by 1000 from node IDs to avoid any collision.
