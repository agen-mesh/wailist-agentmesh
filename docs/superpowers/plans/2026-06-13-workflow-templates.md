# Workflow Templates — 3 Demo Examples

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add 3 polished starter templates to the workflows page so a new user can go from empty state → working canvas in one click.

**Architecture:** Templates are defined as static data in `data.ts` (full nodes + edges + canvas positions). The WorkflowsPage shows a `TemplatesSection` when the list is empty; clicking "Use template" calls the existing `createFromTemplate` API and navigates straight to the canvas. No backend changes needed.

**Tech Stack:** Next.js 16, React 19, TypeScript, inline CSS vars — matching the existing WorkflowsPage pattern exactly.

---

## Files

| File | Change |
|------|--------|
| `frontend/src/lib/data.ts` | Add `WORKFLOW_TEMPLATES` export — 3 template objects with full node/edge graphs |
| `frontend/src/components/workflows/WorkflowsPage.tsx` | Import templates, add `handleUseTemplate`, render `TemplatesSection` in empty state |

---

### Task 1: Define WORKFLOW_TEMPLATES in data.ts

**Files:**
- Modify: `frontend/src/lib/data.ts`

The three templates and their canvas layouts (all nodes fit in ~1300×600px so nothing is off-screen on first open):

**Template A — Lead Research Agent**
```
[Trigger]          [Researcher Agent]               [Email]          [End]
x:60,y:200         x:340,y:160                     x:700,y:160      x:960,y:170
               [Gemini]        [Tavily Search]
               x:260,y:400     x:500,y:400
```

**Template B — Live Market Brief**
```
[Trigger]      [Market Analyst]                [Email]          [End]
x:60,y:200     x:340,y:150                    x:720,y:150      x:980,y:160
          [Gemini]  [AlpacaQuote]  [Tavily]
          x:220,y:390  x:440,y:390  x:440,y:510
```

**Template C — Content Pipeline (multi-agent)**
```
[Trigger]   [Researcher]                 [Writer]             [Email]      [End]
x:60,y:200  x:300,y:160                  x:660,y:160          x:1000,y:160 x:1260,y:170
        [Gemini A]  [Firecrawl]      [Gemini B]
        x:220,y:390 x:440,y:390      x:580,y:390
```

- [ ] **Step 1: Add the WORKFLOW_TEMPLATES export to data.ts**

Append after the closing `};` of `MARKETPLACE_WORKFLOWS` (the last export in the file):

```typescript
export interface WorkflowTemplate {
  id: string;
  name: string;
  description: string;
  tags: string[];
  icon: string;
  previewNodes: Array<{ type: string; label: string }>;
  nodes: import("./types").WorkflowNode[];
  edges: import("./types").WorkflowEdge[];
}

export const WORKFLOW_TEMPLATES: WorkflowTemplate[] = [
  {
    id: "tpl-lead-research",
    name: "Lead Research Agent",
    description: "Drop in a company name — the agent searches the web and emails you a structured research brief.",
    tags: ["research", "sales", "email"],
    icon: "⌕",
    previewNodes: [
      { type: "trigger", label: "Manual" },
      { type: "agent",   label: "Researcher" },
      { type: "tool402", label: "Tavily Search" },
      { type: "action",  label: "Email report" },
      { type: "end",     label: "Done" },
    ],
    nodes: [
      { id: "n1", type: "trigger",  template: "manual",  x: 60,  y: 200, label: "Manual Trigger", icon: "▶" },
      { id: "n2", type: "agent",    template: "agent",   x: 340, y: 160,
        name: "Lead Researcher", icon: "◇",
        systemPrompt: "You are a lead research agent. The user will give you a company name or domain. Use the Tavily Search tool to find: what the company does, their key products, estimated company size, recent news, and any funding information. Return a structured report with sections: Overview, Products, Size & Stage, Recent News, Key People." },
      { id: "n3", type: "provider", template: "gemini",  x: 260, y: 400, name: "Gemini 2.5 Flash", model: "gemini-2.5-flash", icon: "G" },
      { id: "n4", type: "tool402",  template: "tavily",  x: 500, y: 400, name: "Tavily Search", icon: "⌕",
        description: "Real-time web search optimised for AI agents. Returns structured results with snippets and URLs.",
        price: "0.002", unit: "call" },
      { id: "n5", type: "action",   template: "email",   x: 700, y: 160, name: "Send Report Email", icon: "✉" },
      { id: "n6", type: "end",      template: "done",    x: 960, y: 170, label: "Done", icon: "■" },
    ],
    edges: [
      { id: "e1", from: "n1", to: "n2", kind: "flow",   toPort: "in" },
      { id: "e2", from: "n3", to: "n2", kind: "attach", toPort: "model" },
      { id: "e3", from: "n4", to: "n2", kind: "attach", toPort: "tools" },
      { id: "e4", from: "n2", to: "n5", kind: "flow",   toPort: "in" },
      { id: "e5", from: "n5", to: "n6", kind: "flow",   toPort: "in" },
    ],
  },
  {
    id: "tpl-market-brief",
    name: "Daily Market Brief",
    description: "Run it each morning — pulls live stock quotes and web headlines, then emails your team a concise brief.",
    tags: ["finance", "research", "email"],
    icon: "$",
    previewNodes: [
      { type: "trigger", label: "Manual" },
      { type: "agent",   label: "Analyst" },
      { type: "tool402", label: "AlpacaQuote" },
      { type: "tool402", label: "Tavily" },
      { type: "action",  label: "Email brief" },
      { type: "end",     label: "Done" },
    ],
    nodes: [
      { id: "n1", type: "trigger",  template: "manual",  x: 60,  y: 200, label: "Manual Trigger", icon: "▶" },
      { id: "n2", type: "agent",    template: "agent",   x: 340, y: 150,
        name: "Market Analyst", icon: "◇",
        systemPrompt: "You are a financial analyst agent. Use AlpacaQuote to fetch the latest prices for SPY, QQQ, BTC/USD, and ETH/USD. Use Tavily Search to find the top 3 market-moving headlines from today. Then compose a concise morning brief: key levels, whether markets are risk-on or risk-off, and the most important headline. Keep it under 200 words, professional tone." },
      { id: "n3", type: "provider", template: "gemini",  x: 220, y: 390, name: "Gemini 2.5 Flash", model: "gemini-2.5-flash", icon: "G" },
      { id: "n4", type: "tool402",  template: "alpaca",  x: 440, y: 390, name: "AlpacaQuote", icon: "$",
        description: "Live stock and crypto quotes from Alpaca Markets.",
        price: "0.001", unit: "quote" },
      { id: "n5", type: "tool402",  template: "tavily",  x: 440, y: 510, name: "Tavily Search", icon: "⌕",
        description: "Real-time web search for market news and headlines.",
        price: "0.002", unit: "call" },
      { id: "n6", type: "action",   template: "email",   x: 720, y: 150, name: "Send Morning Brief", icon: "✉" },
      { id: "n7", type: "end",      template: "done",    x: 980, y: 160, label: "Done", icon: "■" },
    ],
    edges: [
      { id: "e1", from: "n1", to: "n2", kind: "flow",   toPort: "in" },
      { id: "e2", from: "n3", to: "n2", kind: "attach", toPort: "model" },
      { id: "e3", from: "n4", to: "n2", kind: "attach", toPort: "tools" },
      { id: "e4", from: "n5", to: "n2", kind: "attach", toPort: "tools" },
      { id: "e5", from: "n2", to: "n6", kind: "flow",   toPort: "in" },
      { id: "e6", from: "n6", to: "n7", kind: "flow",   toPort: "in" },
    ],
  },
  {
    id: "tpl-content-pipeline",
    name: "Content Pipeline",
    description: "Two-agent system: first agent scrapes a URL and researches the topic, second agent writes a polished article and emails it.",
    tags: ["content", "marketing", "multi-agent"],
    icon: "◐",
    previewNodes: [
      { type: "trigger", label: "Manual" },
      { type: "agent",   label: "Researcher" },
      { type: "tool402", label: "Firecrawl" },
      { type: "agent",   label: "Writer" },
      { type: "action",  label: "Email draft" },
      { type: "end",     label: "Done" },
    ],
    nodes: [
      { id: "n1", type: "trigger",  template: "manual",    x: 60,  y: 200, label: "Manual Trigger", icon: "▶" },
      { id: "n2", type: "agent",    template: "agent",     x: 300, y: 160,
        name: "Researcher", icon: "◇",
        systemPrompt: "You are a research agent. The user will give you a URL or topic. Use Firecrawl to scrape the URL into clean text, then extract: main thesis, key facts and data points, notable quotes, and related angles worth exploring. Return a structured research brief that a writer can use." },
      { id: "n3", type: "provider", template: "gemini",    x: 220, y: 390, name: "Gemini 2.5 Flash", model: "gemini-2.5-flash", icon: "G" },
      { id: "n4", type: "tool402",  template: "firecrawl", x: 440, y: 390, name: "Firecrawl Scrape", icon: "◐",
        description: "Turn any URL into clean markdown for LLM ingestion.",
        price: "0.005", unit: "page" },
      { id: "n5", type: "agent",    template: "agent",     x: 660, y: 160,
        name: "Writer", icon: "◇",
        systemPrompt: "You are a content writer agent. You will receive a research brief from the Researcher agent. Write a polished, engaging article: compelling headline, strong intro hook, 3–4 body sections with subheadings, and a clear conclusion. Target 600–800 words. Tone: professional but readable. Do not fabricate facts not in the research brief." },
      { id: "n6", type: "provider", template: "gemini",    x: 580, y: 390, name: "Gemini 2.5 Flash", model: "gemini-2.5-flash", icon: "G" },
      { id: "n7", type: "action",   template: "email",     x: 1000, y: 160, name: "Email Draft", icon: "✉" },
      { id: "n8", type: "end",      template: "done",      x: 1260, y: 170, label: "Done", icon: "■" },
    ],
    edges: [
      { id: "e1", from: "n1", to: "n2", kind: "flow",   toPort: "in" },
      { id: "e2", from: "n3", to: "n2", kind: "attach", toPort: "model" },
      { id: "e3", from: "n4", to: "n2", kind: "attach", toPort: "tools" },
      { id: "e4", from: "n2", to: "n5", kind: "flow",   toPort: "in" },
      { id: "e5", from: "n6", to: "n5", kind: "attach", toPort: "model" },
      { id: "e6", from: "n5", to: "n7", kind: "flow",   toPort: "in" },
      { id: "e7", from: "n7", to: "n8", kind: "flow",   toPort: "in" },
    ],
  },
];
```

- [ ] **Step 2: Run TypeScript check**

```bash
cd frontend && npx tsc --noEmit 2>&1 | head -20
```

Expected: no output (clean).

---

### Task 2: Add TemplatesSection to WorkflowsPage

**Files:**
- Modify: `frontend/src/components/workflows/WorkflowsPage.tsx`

The section appears:
- **Empty state** (no workflows): replaces the dashed empty-state box entirely — full-width, 3 cards prominent
- **Has workflows**: shown as a smaller "Quick start" strip at the bottom of the page (collapsed by default, expand on demand — keep it simple, just always show it at 75% opacity so it doesn't distract)

For simplicity and cleanliness: **show the section only when `wfList.length === 0 && !loading`** — the power-user onboarding moment. After creating a workflow, users know templates exist from having seen them once.

- [ ] **Step 1: Add import and handleUseTemplate callback**

At the top of `WorkflowsPage.tsx`, add to the existing import line:

```typescript
import { WORKFLOW_TEMPLATES, WorkflowTemplate } from "@/lib/data";
```

Inside `WorkflowsPage`, after `handleSignOut`, add:

```typescript
const [spawning, setSpawning] = useState<string | null>(null);

const handleUseTemplate = useCallback(async (tpl: WorkflowTemplate) => {
  if (spawning) return;
  setSpawning(tpl.id);
  try {
    const wf = await workflowsApi.createFromTemplate(tpl.name, tpl.nodes, tpl.edges);
    router.push(`/workflows/${wf.id}`);
  } catch {
    setSpawning(null);
  }
}, [spawning, router]);
```

- [ ] **Step 2: Replace the empty state block with TemplatesSection**

Find and replace in the JSX:

Old:
```tsx
          {!loading && filtered.length === 0 && (
            <div style={{ padding: 48, textAlign: "center", border: "1px dashed var(--border)", borderRadius: "var(--r-3)", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
              {wfList.length === 0 ? "no workflows yet — create one to get started" : "no workflows match"}
            </div>
          )}
```

New:
```tsx
          {!loading && filtered.length === 0 && wfList.length === 0 && (
            <TemplatesSection templates={WORKFLOW_TEMPLATES} spawning={spawning} onUse={handleUseTemplate} />
          )}
          {!loading && filtered.length === 0 && wfList.length > 0 && (
            <div style={{ padding: 48, textAlign: "center", border: "1px dashed var(--border)", borderRadius: "var(--r-3)", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
              no workflows match
            </div>
          )}
```

- [ ] **Step 3: Add the TemplatesSection and TemplateCard components**

Add after the `WorkflowGrid` function (before the `fmtDate` helper):

```tsx
function TemplatesSection({ templates, spawning, onUse }: {
  templates: WorkflowTemplate[];
  spawning: string | null;
  onUse: (t: WorkflowTemplate) => void;
}) {
  return (
    <div>
      <div style={{ marginBottom: 20 }}>
        <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--fg-dim)", marginBottom: 6 }}>
          quick start
        </div>
        <div style={{ fontSize: 22, fontWeight: 500, letterSpacing: "-0.02em", color: "var(--fg)", marginBottom: 4 }}>
          Start from a template
        </div>
        <p style={{ margin: 0, fontSize: 13, color: "var(--fg-muted)" }}>
          Pre-built workflows you can customise and run in minutes.
        </p>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
        {templates.map((tpl) => (
          <TemplateCard
            key={tpl.id}
            tpl={tpl}
            loading={spawning === tpl.id}
            disabled={spawning !== null}
            onUse={() => onUse(tpl)}
          />
        ))}
      </div>
    </div>
  );
}

const NODE_TYPE_COLOR: Record<string, { bg: string; fg: string }> = {
  trigger:  { bg: "var(--bg-elev-3)",            fg: "var(--fg-muted)" },
  agent:    { bg: "rgba(167,139,250,0.12)",       fg: "var(--accent)" },
  provider: { bg: "rgba(167,139,250,0.08)",       fg: "var(--accent)" },
  tool:     { bg: "var(--bg-elev-3)",            fg: "var(--fg-muted)" },
  tool402:  { bg: "rgba(232,121,249,0.10)",       fg: "#E879F9" },
  action:   { bg: "rgba(255,181,71,0.10)",        fg: "var(--warm)" },
  end:      { bg: "var(--bg-elev-3)",            fg: "var(--fg-dim)" },
};

function TemplateCard({ tpl, loading, disabled, onUse }: {
  tpl: WorkflowTemplate;
  loading: boolean;
  disabled: boolean;
  onUse: () => void;
}) {
  const [hovered, setHovered] = useState(false);
  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        background: "var(--bg-elev-1)",
        border: `1px solid ${hovered ? "var(--accent-line)" : "var(--border)"}`,
        borderRadius: "var(--r-3)",
        padding: "20px",
        display: "flex",
        flexDirection: "column",
        gap: 14,
        transition: "border-color 0.15s",
      }}
    >
      {/* Header */}
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <div style={{ width: 40, height: 40, borderRadius: "var(--r-2)", background: "var(--bg-elev-3)", border: "1px solid var(--border-strong)", display: "flex", alignItems: "center", justifyContent: "center", fontSize: 20, flexShrink: 0 }}>
          {tpl.icon}
        </div>
        <div>
          <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>{tpl.name}</div>
          <div style={{ display: "flex", gap: 5, marginTop: 3 }}>
            {tpl.tags.map((t) => (
              <span key={t} style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.05em" }}>#{t}</span>
            ))}
          </div>
        </div>
      </div>

      {/* Description */}
      <p style={{ margin: 0, fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.65 }}>{tpl.description}</p>

      {/* Mini pipeline preview */}
      <div style={{ display: "flex", alignItems: "center", gap: 4, flexWrap: "wrap" }}>
        {tpl.previewNodes.map((n, i) => {
          const c = NODE_TYPE_COLOR[n.type] ?? NODE_TYPE_COLOR.end;
          return (
            <div key={i} style={{ display: "flex", alignItems: "center", gap: 4 }}>
              <span style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: c.fg, background: c.bg, border: `1px solid ${c.fg}22`, borderRadius: 4, padding: "2px 7px" }}>
                {n.label}
              </span>
              {i < tpl.previewNodes.length - 1 && (
                <span style={{ fontSize: 10, color: "var(--fg-dim)" }}>→</span>
              )}
            </div>
          );
        })}
      </div>

      {/* CTA */}
      <button
        onClick={onUse}
        disabled={disabled}
        style={{
          height: 34,
          background: hovered && !disabled ? "var(--accent)" : "transparent",
          border: `1px solid ${hovered && !disabled ? "var(--accent)" : "var(--border-strong)"}`,
          borderRadius: "var(--r-2)",
          color: hovered && !disabled ? "var(--accent-fg)" : "var(--fg-muted)",
          fontSize: 12,
          fontWeight: 600,
          cursor: disabled ? "default" : "pointer",
          fontFamily: "var(--font-sans)",
          transition: "background 0.15s, border-color 0.15s, color 0.15s",
          opacity: disabled && !loading ? 0.5 : 1,
        }}
      >
        {loading ? "Creating…" : "Use template →"}
      </button>
    </div>
  );
}
```

Note: `WorkflowTemplate` must be imported at the top of `WorkflowsPage.tsx`.

- [ ] **Step 4: Run TypeScript check**

```bash
cd frontend && npx tsc --noEmit 2>&1 | head -20
```

Expected: no output.

---

### Task 3: Verify and commit

- [ ] **Step 1: Run lint**

```bash
cd frontend && npm run lint 2>&1 | tail -10
```

Expected: no errors (warnings are fine).

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/data.ts frontend/src/components/workflows/WorkflowsPage.tsx
git commit -m "feat(frontend): add 3 workflow templates to empty state

- Lead Research Agent: Gemini + Tavily → email report
- Daily Market Brief: Gemini + AlpacaQuote + Tavily → morning brief
- Content Pipeline: two-agent research→write→email flow
- TemplatesSection shown on empty workflows page
- One-click createFromTemplate → navigate to canvas

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- ✅ 3 demo workflows defined with real nodes/edges/positions
- ✅ Smooth one-click UX: click → create → navigate to canvas
- ✅ Canvas positions designed so all nodes are visible without panning
- ✅ Each template has a real, useful system prompt — not placeholder text
- ✅ Multi-tool (Template B) and multi-agent (Template C) showcased
- ✅ No backend changes — uses existing `createFromTemplate` API call

**Placeholder scan:** None. All system prompts are fully written, all positions are exact numbers, all node fields match the `WorkflowNode` type from `types.ts`.

**Type consistency:**
- `WorkflowTemplate.nodes` is `WorkflowNode[]` — all node fields (`id`, `type`, `x`, `y`, etc.) match the existing interface
- `WorkflowTemplate.edges` is `WorkflowEdge[]` — `kind` is `"flow" | "attach"`, `toPort` is `PortName` (`"in" | "out" | "model" | "tools" | "top"`) — all values match
- `createFromTemplate(name, nodes, edges)` accepts `unknown[]` for nodes/edges — compatible

**Edge cases:**
- `spawning` state prevents double-clicks from firing multiple API calls
- Error path resets `spawning` to `null` so the user can retry
- When `wfList.length > 0` but `filtered.length === 0` (search with no match), shows the original "no workflows match" message — templates not shown, correct
