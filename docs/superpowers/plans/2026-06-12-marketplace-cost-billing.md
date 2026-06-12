# Marketplace, Cost Estimator & Billing — Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three frontend-only features: (1) a marketplace page for browsing/uploading x402 endpoints and workflows, (2) a live cost estimator in the canvas topbar, and (3) a billing/credits page explaining prepaid credits vs. pay-per-use postpaid.

**Architecture:** All changes are pure frontend — no new API calls, all marketplace data is static mock data in `lib/data.ts`, cost estimation is derived from existing node state in `CanvasPage.tsx`, and the billing page is a static marketing/explainer page. New routes follow the existing App Router pattern.

**Tech Stack:** Next.js App Router, React 19, TypeScript, inline CSS-in-JS (matching existing pattern — no Tailwind classes, only CSS vars from `globals.css`).

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `frontend/src/lib/data.ts` | Modify | Add marketplace mock data (endpoints + workflows) |
| `frontend/src/lib/types.ts` | Modify | Add `MarketplaceItem` and `MarketplaceWorkflow` types |
| `frontend/src/app/marketplace/page.tsx` | Create | App Router page that renders `MarketplacePage` |
| `frontend/src/components/marketplace/MarketplacePage.tsx` | Create | Full marketplace UI with tabs, cards, upload modal |
| `frontend/src/app/billing/page.tsx` | Create | App Router page that renders `BillingPage` |
| `frontend/src/components/billing/BillingPage.tsx` | Create | Credits vs. pay-per-use explainer page |
| `frontend/src/components/canvas/CanvasPage.tsx` | Modify | Add `CostEstimator` component to `CanvasTopbar` |
| `frontend/src/app/page.tsx` or layout/nav | Modify | Add nav links to Marketplace and Billing |
| `frontend/src/components/workflows/WorkflowsPage.tsx` | Modify | Add "Marketplace" and "Billing" nav links in header |

---

## Task 1: Add types for marketplace data

**Files:**
- Modify: `frontend/src/lib/types.ts`

- [ ] **Step 1: Add marketplace types to `types.ts`**

Open `frontend/src/lib/types.ts` and append at the end:

```typescript
export interface MarketplaceEndpoint {
  id: string;
  name: string;
  description: string;
  provider: string;
  price: string;
  unit: string;
  category: "search" | "data" | "ai" | "finance" | "media" | "util";
  icon: string;
  tags: string[];
  author: string;
  calls: number;
  rating: number;
  featured?: boolean;
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
  featured?: boolean;
  previewNodes: Array<{ type: string; label: string }>;
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -20
```

Expected: no errors (or only pre-existing errors unrelated to these types).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/types.ts
git commit -m "feat: add MarketplaceEndpoint and MarketplaceWorkflow types"
```

---

## Task 2: Add marketplace mock data to `data.ts`

**Files:**
- Modify: `frontend/src/lib/data.ts`

- [ ] **Step 1: Add marketplace endpoint data**

Add to the bottom of `frontend/src/lib/data.ts`:

```typescript
import type { MarketplaceEndpoint, MarketplaceWorkflow } from "./types";

export const MARKETPLACE_ENDPOINTS: MarketplaceEndpoint[] = [
  {
    id: "mp-tavily",
    name: "Tavily Search",
    description: "Real-time web search optimised for AI agents. Returns structured results with snippets, URLs, and relevance scores.",
    provider: "tavily.x402",
    price: "0.002",
    unit: "call",
    category: "search",
    icon: "⌕",
    tags: ["search", "web", "research"],
    author: "Tavily Inc.",
    calls: 128400,
    rating: 4.8,
    featured: true,
  },
  {
    id: "mp-firecrawl",
    name: "Firecrawl Scrape",
    description: "Turn any URL into clean markdown for LLM ingestion. Handles SPAs, paywalls, and JS-heavy pages.",
    provider: "firecrawl.x402",
    price: "0.005",
    unit: "page",
    category: "data",
    icon: "◐",
    tags: ["scraping", "markdown", "web"],
    author: "Firecrawl",
    calls: 84200,
    rating: 4.6,
    featured: true,
  },
  {
    id: "mp-flux",
    name: "FluxImage Gen",
    description: "State-of-the-art image generation via Flux.1. High-resolution output, fast inference, prompt adherence.",
    provider: "flux.x402",
    price: "0.020",
    unit: "image",
    category: "ai",
    icon: "✦",
    tags: ["image", "generation", "creative"],
    author: "Black Forest Labs",
    calls: 31700,
    rating: 4.9,
    featured: true,
  },
  {
    id: "mp-alpaca",
    name: "AlpacaQuote",
    description: "Live and historical stock/crypto quotes from Alpaca Markets. Streaming and snapshot modes.",
    provider: "alpaca.x402",
    price: "0.001",
    unit: "quote",
    category: "finance",
    icon: "$",
    tags: ["finance", "stocks", "crypto"],
    author: "Alpaca Markets",
    calls: 249000,
    rating: 4.7,
  },
  {
    id: "mp-ocr",
    name: "OCR.space",
    description: "Extract text from images and PDFs. Supports 30+ languages and table detection.",
    provider: "ocr.x402",
    price: "0.003",
    unit: "page",
    category: "ai",
    icon: "⊟",
    tags: ["ocr", "pdf", "text-extraction"],
    author: "OCR.space",
    calls: 56800,
    rating: 4.4,
  },
  {
    id: "mp-weather",
    name: "WeatherKit",
    description: "Real-time and forecast weather for any city worldwide. Temperature, wind, precipitation, UV index.",
    provider: "weatherkit.x402",
    price: "0.0008",
    unit: "call",
    category: "data",
    icon: "◌",
    tags: ["weather", "forecast", "geo"],
    author: "WeatherKit",
    calls: 189300,
    rating: 4.5,
  },
  {
    id: "mp-perplexity",
    name: "Perplexity Search",
    description: "AI-powered answer engine with citations. Best for knowledge questions and fact-checking.",
    provider: "perplexity.x402",
    price: "0.008",
    unit: "query",
    category: "search",
    icon: "◎",
    tags: ["search", "ai", "answers"],
    author: "Perplexity AI",
    calls: 42100,
    rating: 4.7,
  },
  {
    id: "mp-exa",
    name: "Exa Neural Search",
    description: "Semantic search over the live web. Finds conceptually similar content rather than keyword matches.",
    provider: "exa.x402",
    price: "0.004",
    unit: "call",
    category: "search",
    icon: "⟲",
    tags: ["search", "semantic", "neural"],
    author: "Exa",
    calls: 18900,
    rating: 4.6,
  },
];

export const MARKETPLACE_WORKFLOWS: MarketplaceWorkflow[] = [
  {
    id: "mwf-support",
    name: "Customer Support Triage",
    description: "Classifies inbound support tickets, looks up account data, drafts a reply, and routes to the right team — fully automated.",
    author: "AgentMesh Team",
    tags: ["support", "classification", "email"],
    nodes: 6,
    runs: 12400,
    stars: 284,
    featured: true,
    previewNodes: [
      { type: "trigger", label: "Webhook" },
      { type: "agent",   label: "Classifier" },
      { type: "tool402", label: "CRM Lookup" },
      { type: "agent",   label: "Reply Drafter" },
      { type: "action",  label: "Send Email" },
      { type: "end",     label: "Done" },
    ],
  },
  {
    id: "mwf-market",
    name: "Daily Market Brief",
    description: "Pulls live prices, searches recent news, synthesises a morning brief, and emails it to your team at 7 AM.",
    author: "AgentMesh Team",
    tags: ["finance", "research", "schedule"],
    nodes: 5,
    runs: 1820,
    stars: 147,
    featured: true,
    previewNodes: [
      { type: "trigger", label: "Schedule" },
      { type: "tool402", label: "AlpacaQuote" },
      { type: "tool402", label: "Tavily Search" },
      { type: "agent",   label: "Brief Writer" },
      { type: "action",  label: "Send Email" },
    ],
  },
  {
    id: "mwf-leads",
    name: "Lead Enrichment Pipeline",
    description: "Takes a CSV of company names, enriches each with web scraping and LinkedIn data, and writes structured profiles to your CRM.",
    author: "sales-tools",
    tags: ["sales", "enrichment", "crm"],
    nodes: 7,
    runs: 4300,
    stars: 203,
    previewNodes: [
      { type: "trigger", label: "Webhook" },
      { type: "tool402", label: "Firecrawl" },
      { type: "tool402", label: "Exa Search" },
      { type: "agent",   label: "Enricher" },
      { type: "action",  label: "CRM Write" },
      { type: "end",     label: "Done" },
    ],
  },
  {
    id: "mwf-content",
    name: "Content to Social Pipeline",
    description: "Feed in a blog post URL — the agent scrapes it, generates a Twitter thread, LinkedIn post, and image, then schedules them.",
    author: "marketing-kit",
    tags: ["marketing", "social", "content"],
    nodes: 8,
    runs: 2100,
    stars: 118,
    previewNodes: [
      { type: "trigger", label: "Webhook" },
      { type: "tool402", label: "Firecrawl" },
      { type: "agent",   label: "Thread Writer" },
      { type: "tool402", label: "FluxImage" },
      { type: "action",  label: "Post to X" },
      { type: "action",  label: "Post to LinkedIn" },
    ],
  },
];
```

- [ ] **Step 2: Fix the import — `data.ts` already has its own imports; just add to the existing imports section**

The `data.ts` file imports from `"./types"` already on line 1. Change the existing import to include the new types:

```typescript
// existing line 1:
import { NodeTypeMeta, Workflow } from "./types";
// change to:
import { NodeTypeMeta, Workflow, MarketplaceEndpoint, MarketplaceWorkflow } from "./types";
```

- [ ] **Step 3: Verify TypeScript**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -20
```

Expected: no new errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/data.ts
git commit -m "feat: add marketplace mock data for endpoints and workflows"
```

---

## Task 3: Create the Marketplace page component

**Files:**
- Create: `frontend/src/components/marketplace/MarketplacePage.tsx`

This is the main marketplace UI. It has:
- A header with title, search bar, and an "Upload Endpoint" / "Share Workflow" button
- Two tabs: "Endpoints" and "Workflows"
- Filter chips for categories
- A 3-column card grid
- Featured items with a distinct treatment
- An "Upload / Share" modal (frontend-only, shows a success toast)

- [ ] **Step 1: Create the component file**

Create `frontend/src/components/marketplace/MarketplacePage.tsx` with:

```tsx
"use client";
import { useState, useMemo } from "react";
import { Logo, Pill, Tag, IconSearch, Toast } from "@/components/ui";
import {
  MARKETPLACE_ENDPOINTS,
  MARKETPLACE_WORKFLOWS,
} from "@/lib/data";
import type { MarketplaceEndpoint, MarketplaceWorkflow } from "@/lib/types";
import { useRouter } from "next/navigation";

const ENDPOINT_CATEGORIES = ["all", "search", "data", "ai", "finance", "media", "util"] as const;
type Category = (typeof ENDPOINT_CATEGORIES)[number];

const NODE_TYPE_COLORS: Record<string, string> = {
  trigger: "var(--accent)",
  agent:   "var(--accent-strong)",
  tool402: "#E879F9",
  tool:    "var(--info)",
  action:  "var(--warm)",
  end:     "var(--fg-dim)",
};

export function MarketplacePage() {
  const router = useRouter();
  const [tab, setTab] = useState<"endpoints" | "workflows">("endpoints");
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<Category>("all");
  const [uploadOpen, setUploadOpen] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const showToast = (msg: string) => {
    setToast(msg);
    setTimeout(() => setToast(null), 2600);
  };

  const filteredEndpoints = useMemo(() => {
    return MARKETPLACE_ENDPOINTS.filter((ep) => {
      const matchCat = category === "all" || ep.category === category;
      const matchQ =
        !query ||
        ep.name.toLowerCase().includes(query.toLowerCase()) ||
        ep.description.toLowerCase().includes(query.toLowerCase()) ||
        ep.tags.some((t) => t.includes(query.toLowerCase()));
      return matchCat && matchQ;
    });
  }, [query, category]);

  const filteredWorkflows = useMemo(() => {
    return MARKETPLACE_WORKFLOWS.filter((wf) => {
      return (
        !query ||
        wf.name.toLowerCase().includes(query.toLowerCase()) ||
        wf.description.toLowerCase().includes(query.toLowerCase()) ||
        wf.tags.some((t) => t.includes(query.toLowerCase()))
      );
    });
  }, [query]);

  return (
    <div style={{ minHeight: "100vh", background: "var(--bg)", display: "flex", flexDirection: "column" }}>
      {/* ── Nav ── */}
      <header style={{
        height: 52, flexShrink: 0,
        background: "var(--bg-elev-1)", borderBottom: "1px solid var(--border)",
        display: "flex", alignItems: "center", padding: "0 24px", gap: 16,
      }}>
        <button onClick={() => router.push("/")} style={ghostStyle}>
          <Logo size={16} />
        </button>
        <div style={{ width: 1, height: 20, background: "var(--border)" }} />
        <button onClick={() => router.push("/workflows")} style={navLinkStyle}>Workflows</button>
        <span style={{ color: "var(--fg-muted)", fontSize: 13 }}>Marketplace</span>
        <div style={{ flex: 1 }} />
        <button onClick={() => router.push("/billing")} style={navLinkStyle}>Billing</button>
        <button
          onClick={() => setUploadOpen(true)}
          style={primaryBtnStyle}
        >
          + Publish
        </button>
      </header>

      {/* ── Hero ── */}
      <div style={{
        padding: "48px 24px 32px",
        textAlign: "center",
        borderBottom: "1px solid var(--border)",
        background: "linear-gradient(180deg, var(--bg-elev-1) 0%, var(--bg) 100%)",
      }}>
        <div style={{ display: "inline-flex", alignItems: "center", gap: 6, marginBottom: 12 }}>
          <Tag>x402 Marketplace</Tag>
        </div>
        <h1 style={{ margin: 0, fontSize: 32, fontWeight: 700, letterSpacing: "-0.03em", color: "var(--fg)", lineHeight: 1.2 }}>
          Plug-and-pay AI tools &amp; workflows
        </h1>
        <p style={{ margin: "12px auto 0", maxWidth: 520, color: "var(--fg-muted)", fontSize: 15, lineHeight: 1.7 }}>
          Browse x402-enabled endpoints and ready-made workflows. Every tool is pay-per-call — no API keys, no subscriptions.
        </p>

        {/* Search */}
        <div style={{ display: "flex", alignItems: "center", gap: 10, maxWidth: 480, margin: "24px auto 0", background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)", padding: "0 14px", height: 40 }}>
          <IconSearch size={14} />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search endpoints, workflows, tags…"
            style={{ flex: 1, background: "transparent", border: "none", outline: "none", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)" }}
          />
        </div>
      </div>

      {/* ── Tabs + content ── */}
      <div style={{ flex: 1, maxWidth: 1120, margin: "0 auto", width: "100%", padding: "0 24px 48px" }}>
        {/* Tabs */}
        <div style={{ display: "flex", gap: 0, borderBottom: "1px solid var(--border)", marginTop: 24, marginBottom: 24 }}>
          {(["endpoints", "workflows"] as const).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              style={{
                padding: "8px 20px", fontSize: 13, fontWeight: 500,
                background: "transparent", border: "none",
                borderBottom: tab === t ? "2px solid var(--accent)" : "2px solid transparent",
                color: tab === t ? "var(--accent)" : "var(--fg-muted)",
                cursor: "pointer", fontFamily: "var(--font-sans)",
                marginBottom: -1,
              }}
            >
              {t === "endpoints" ? `Endpoints (${MARKETPLACE_ENDPOINTS.length})` : `Workflows (${MARKETPLACE_WORKFLOWS.length})`}
            </button>
          ))}
        </div>

        {tab === "endpoints" && (
          <>
            {/* Category chips */}
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 24 }}>
              {ENDPOINT_CATEGORIES.map((cat) => (
                <button
                  key={cat}
                  onClick={() => setCategory(cat)}
                  style={{
                    height: 28, padding: "0 12px", fontSize: 12, fontWeight: 500,
                    borderRadius: 999, cursor: "pointer", fontFamily: "var(--font-sans)",
                    background: category === cat ? "var(--accent-soft)" : "var(--bg-elev-2)",
                    border: `1px solid ${category === cat ? "var(--accent-line)" : "var(--border)"}`,
                    color: category === cat ? "var(--accent)" : "var(--fg-muted)",
                  }}
                >
                  {cat === "all" ? "All" : cat.charAt(0).toUpperCase() + cat.slice(1)}
                </button>
              ))}
            </div>

            {/* Featured strip */}
            {category === "all" && !query && (
              <div style={{ marginBottom: 32 }}>
                <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: 14 }}>Featured</div>
                <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
                  {MARKETPLACE_ENDPOINTS.filter((e) => e.featured).map((ep) => (
                    <EndpointCard key={ep.id} ep={ep} featured onAdd={() => showToast(`${ep.name} added to clipboard — drop into canvas`)} />
                  ))}
                </div>
              </div>
            )}

            {/* All results */}
            <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: 14 }}>
              {category === "all" && !query ? "All Endpoints" : `Results · ${filteredEndpoints.length}`}
            </div>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
              {filteredEndpoints.map((ep) => (
                <EndpointCard key={ep.id} ep={ep} onAdd={() => showToast(`${ep.name} added to clipboard — drop into canvas`)} />
              ))}
            </div>
            {filteredEndpoints.length === 0 && (
              <div style={{ textAlign: "center", padding: "48px 0", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 13 }}>
                No endpoints match &ldquo;{query}&rdquo;
              </div>
            )}
          </>
        )}

        {tab === "workflows" && (
          <>
            {/* Featured */}
            {!query && (
              <div style={{ marginBottom: 32 }}>
                <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: 14 }}>Featured</div>
                <div style={{ display: "grid", gridTemplateColumns: "repeat(2, 1fr)", gap: 16 }}>
                  {MARKETPLACE_WORKFLOWS.filter((w) => w.featured).map((wf) => (
                    <WorkflowCard key={wf.id} wf={wf} onUse={() => showToast(`"${wf.name}" cloned to your workspace`)} />
                  ))}
                </div>
              </div>
            )}

            <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: 14 }}>
              {!query ? "All Workflows" : `Results · ${filteredWorkflows.length}`}
            </div>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(2, 1fr)", gap: 16 }}>
              {filteredWorkflows.map((wf) => (
                <WorkflowCard key={wf.id} wf={wf} onUse={() => showToast(`"${wf.name}" cloned to your workspace`)} />
              ))}
            </div>
            {filteredWorkflows.length === 0 && (
              <div style={{ textAlign: "center", padding: "48px 0", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 13 }}>
                No workflows match &ldquo;{query}&rdquo;
              </div>
            )}
          </>
        )}
      </div>

      {/* ── Upload modal ── */}
      {uploadOpen && (
        <UploadModal
          onClose={() => setUploadOpen(false)}
          onSubmit={(name) => {
            setUploadOpen(false);
            showToast(`"${name}" submitted for review — usually live within 24h`);
          }}
        />
      )}

      {toast && <Toast message={toast} />}
    </div>
  );
}

// ── Endpoint Card ─────────────────────────────────────────────────────────
function EndpointCard({ ep, featured = false, onAdd }: { ep: MarketplaceEndpoint; featured?: boolean; onAdd: () => void }) {
  return (
    <div style={{
      background: featured ? "var(--bg-elev-2)" : "var(--bg-elev-1)",
      border: `1px solid ${featured ? "var(--border-strong)" : "var(--border)"}`,
      borderRadius: "var(--r-3)",
      padding: "18px 20px",
      display: "flex", flexDirection: "column", gap: 12,
      transition: "border-color 0.15s",
    }}
      onMouseEnter={(e) => (e.currentTarget.style.borderColor = "var(--accent-line)")}
      onMouseLeave={(e) => (e.currentTarget.style.borderColor = featured ? "var(--border-strong)" : "var(--border)")}
    >
      <div style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
        <div style={{
          width: 40, height: 40, borderRadius: "var(--r-2)",
          background: "rgba(232,121,249,0.12)", border: "1px solid rgba(232,121,249,0.25)",
          display: "flex", alignItems: "center", justifyContent: "center",
          fontSize: 18, flexShrink: 0,
        }}>{ep.icon}</div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 3 }}>
            <span style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>{ep.name}</span>
            {featured && <Pill tone="accent">Featured</Pill>}
          </div>
          <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>{ep.provider}</div>
        </div>
        <div style={{ textAlign: "right", flexShrink: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 700, color: "#E879F9" }}>${ep.price}</div>
          <div style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>per {ep.unit}</div>
        </div>
      </div>

      <p style={{ margin: 0, fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.6 }}>{ep.description}</p>

      <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
        {ep.tags.map((t) => (
          <span key={t} style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", background: "var(--bg-elev-3)", borderRadius: "var(--r-1)", padding: "2px 7px", border: "1px solid var(--border)" }}>
            {t}
          </span>
        ))}
      </div>

      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginTop: 4 }}>
        <div style={{ display: "flex", gap: 14 }}>
          <span style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>
            ⟳ {(ep.calls / 1000).toFixed(0)}k calls
          </span>
          <span style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--warm)" }}>
            ★ {ep.rating}
          </span>
        </div>
        <button
          onClick={onAdd}
          style={{
            height: 28, padding: "0 14px", fontSize: 12, fontWeight: 500,
            background: "var(--bg-elev-3)", border: "1px solid var(--border-strong)",
            borderRadius: "var(--r-2)", color: "var(--fg)", cursor: "pointer",
            fontFamily: "var(--font-sans)",
          }}
        >
          + Add to workflow
        </button>
      </div>
    </div>
  );
}

// ── Workflow Card ─────────────────────────────────────────────────────────
function WorkflowCard({ wf, onUse }: { wf: MarketplaceWorkflow; onUse: () => void }) {
  return (
    <div style={{
      background: "var(--bg-elev-1)",
      border: "1px solid var(--border)",
      borderRadius: "var(--r-3)",
      padding: "20px 22px",
      display: "flex", flexDirection: "column", gap: 14,
      transition: "border-color 0.15s",
    }}
      onMouseEnter={(e) => (e.currentTarget.style.borderColor = "var(--accent-line)")}
      onMouseLeave={(e) => (e.currentTarget.style.borderColor = "var(--border)")}
    >
      <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between" }}>
        <div>
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 4 }}>
            <span style={{ fontSize: 15, fontWeight: 600, color: "var(--fg)" }}>{wf.name}</span>
            {wf.featured && <Pill tone="accent">Featured</Pill>}
          </div>
          <span style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>by {wf.author}</span>
        </div>
        <div style={{ display: "flex", gap: 12 }}>
          <span style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>⟳ {wf.runs.toLocaleString()}</span>
          <span style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--warm)" }}>★ {wf.stars}</span>
        </div>
      </div>

      <p style={{ margin: 0, fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.65 }}>{wf.description}</p>

      {/* Mini node preview */}
      <div style={{ display: "flex", gap: 6, alignItems: "center", overflowX: "auto", padding: "8px 0" }}>
        {wf.previewNodes.map((n, i) => (
          <div key={i} style={{ display: "flex", alignItems: "center", gap: 6, flexShrink: 0 }}>
            <div style={{
              height: 26, padding: "0 10px", borderRadius: "var(--r-1)",
              background: `${NODE_TYPE_COLORS[n.type] ?? "var(--fg-dim)"}18`,
              border: `1px solid ${NODE_TYPE_COLORS[n.type] ?? "var(--fg-dim)"}40`,
              fontSize: 11, fontFamily: "var(--font-mono)",
              color: NODE_TYPE_COLORS[n.type] ?? "var(--fg-dim)",
              display: "flex", alignItems: "center",
            }}>
              {n.label}
            </div>
            {i < wf.previewNodes.length - 1 && (
              <span style={{ color: "var(--fg-dim)", fontSize: 10 }}>→</span>
            )}
          </div>
        ))}
      </div>

      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
          {wf.tags.map((t) => (
            <span key={t} style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", background: "var(--bg-elev-3)", borderRadius: "var(--r-1)", padding: "2px 7px", border: "1px solid var(--border)" }}>
              {t}
            </span>
          ))}
        </div>
        <button
          onClick={onUse}
          style={{
            height: 28, padding: "0 14px", fontSize: 12, fontWeight: 600,
            background: "var(--accent)", border: "1px solid var(--accent)",
            borderRadius: "var(--r-2)", color: "var(--accent-fg)", cursor: "pointer",
            fontFamily: "var(--font-sans)",
          }}
        >
          Use template
        </button>
      </div>
    </div>
  );
}

// ── Upload Modal ──────────────────────────────────────────────────────────
function UploadModal({ onClose, onSubmit }: { onClose: () => void; onSubmit: (name: string) => void }) {
  const [type, setType] = useState<"endpoint" | "workflow">("endpoint");
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [desc, setDesc] = useState("");
  const [price, setPrice] = useState("");

  const valid = name.trim().length > 2 && (type === "workflow" || url.trim().length > 4);

  return (
    <div style={{ position: "fixed", inset: 0, background: "rgba(8,7,12,0.7)", backdropFilter: "blur(4px)", zIndex: 100, display: "flex", alignItems: "center", justifyContent: "center" }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div style={{ width: 500, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-3)", padding: "28px 28px 24px", display: "flex", flexDirection: "column", gap: 18 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div>
            <div style={{ fontSize: 15, fontWeight: 600, color: "var(--fg)" }}>Publish to Marketplace</div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginTop: 3 }}>Share your x402 endpoint or workflow with the community</div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--fg-muted)", cursor: "pointer", fontSize: 18, padding: 4 }}>✕</button>
        </div>

        {/* Type toggle */}
        <div style={{ display: "flex", gap: 0, background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", padding: 3 }}>
          {(["endpoint", "workflow"] as const).map((t) => (
            <button key={t} onClick={() => setType(t)} style={{
              flex: 1, height: 30, fontSize: 12, fontWeight: 500,
              background: type === t ? "var(--bg-elev-3)" : "transparent",
              border: "none", borderRadius: "var(--r-1)",
              color: type === t ? "var(--fg)" : "var(--fg-muted)", cursor: "pointer",
              fontFamily: "var(--font-sans)",
            }}>
              {t.charAt(0).toUpperCase() + t.slice(1)}
            </button>
          ))}
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <Field label="Name" value={name} onChange={setName} placeholder={type === "endpoint" ? "e.g. NewsAPI Search" : "e.g. Lead Enrichment Pipeline"} />
          {type === "endpoint" && (
            <>
              <Field label="Endpoint URL" value={url} onChange={setUrl} placeholder="https://your-api.com/endpoint" />
              <Field label="Price per call (USD)" value={price} onChange={setPrice} placeholder="0.005" />
            </>
          )}
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            <label style={{ fontSize: 12, fontWeight: 500, color: "var(--fg-muted)", fontFamily: "var(--font-sans)" }}>Description</label>
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              placeholder="What does this do? What inputs does it take?"
              style={{ width: "100%", minHeight: 80, padding: "8px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", resize: "vertical", outline: "none", lineHeight: 1.6, boxSizing: "border-box" }}
            />
          </div>
        </div>

        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <button onClick={onClose} style={ghostBtnStyle}>Cancel</button>
          <button onClick={() => valid && onSubmit(name)} disabled={!valid}
            style={{ ...primaryBtnStyle, opacity: valid ? 1 : 0.5, cursor: valid ? "pointer" : "default" }}>
            Submit for review
          </button>
        </div>
      </div>
    </div>
  );
}

function Field({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (v: string) => void; placeholder: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <label style={{ fontSize: 12, fontWeight: 500, color: "var(--fg-muted)", fontFamily: "var(--font-sans)" }}>{label}</label>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        style={{ height: 36, padding: "0 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", outline: "none" }}
      />
    </div>
  );
}

const ghostStyle: React.CSSProperties = {
  background: "transparent", border: "none", cursor: "pointer", padding: 0, display: "inline-flex",
};
const navLinkStyle: React.CSSProperties = {
  background: "transparent", border: "none", cursor: "pointer",
  fontSize: 13, color: "var(--fg-muted)", fontFamily: "var(--font-sans)", padding: "4px 6px",
};
const primaryBtnStyle: React.CSSProperties = {
  height: 32, padding: "0 16px", fontSize: 12, fontWeight: 600,
  background: "var(--accent)", border: "1px solid var(--accent)",
  borderRadius: "var(--r-2)", color: "var(--accent-fg)", cursor: "pointer",
  fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4,
};
const ghostBtnStyle: React.CSSProperties = {
  height: 32, padding: "0 14px", fontSize: 12, fontWeight: 500,
  background: "transparent", border: "1px solid var(--border-strong)",
  borderRadius: "var(--r-2)", color: "var(--fg-muted)", cursor: "pointer",
  fontFamily: "var(--font-sans)",
};
```

- [ ] **Step 2: Create the App Router page**

Create `frontend/src/app/marketplace/page.tsx`:

```tsx
import { MarketplacePage } from "@/components/marketplace/MarketplacePage";

export default function Page() {
  return <MarketplacePage />;
}
```

- [ ] **Step 3: Verify TypeScript**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: no new errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/marketplace/MarketplacePage.tsx frontend/src/app/marketplace/page.tsx
git commit -m "feat: add Marketplace page with endpoint and workflow browser"
```

---

## Task 4: Add cost estimator to canvas topbar

**Files:**
- Modify: `frontend/src/components/canvas/CanvasPage.tsx`

The estimator shows in `CanvasTopbar`. It reads the current nodes and calculates:
- Per-run LLM cost estimate based on provider/model (token-based rough estimate)
- Per-run x402 tool cost based on `node.price` values
- A total `~$X.XXX / run` display that updates live as nodes are added/removed

Cost constants (rough estimates, for display only):
- Gemini 2.5 Flash: $0.0012/run (avg 1k tokens)
- GPT-4.1: $0.0060/run
- Claude Sonnet: $0.0090/run
- Mistral Large: $0.0040/run
- Groq Llama: $0.0005/run
- x402 tools: sum of `node.price` values (already on the node)

- [ ] **Step 1: Add the cost computation logic in `CanvasPage.tsx`**

In `CanvasPage.tsx`, inside the `CanvasPage` function body, after the `totalSpend` line (~line 175), add:

```typescript
const estimatedCostPerRun = useMemo(() => {
  if (!workflow) return 0;
  const LLM_COST: Record<string, number> = {
    "gemini-2.5-flash": 0.0012,
    "gpt-4.1":          0.0060,
    "claude-sonnet-4":  0.0090,
    "mistral-large":    0.0040,
    "llama-3.3-70b":    0.0005,
  };
  let total = 0;
  for (const node of workflow.nodes) {
    if (node.type === "provider" && node.model) {
      total += LLM_COST[node.model] ?? 0.003;
    }
    if (node.type === "tool402" && node.price) {
      total += parseFloat(node.price) || 0;
    }
  }
  return total;
}, [workflow]);
```

- [ ] **Step 2: Pass `estimatedCostPerRun` down to `CanvasTopbar`**

In `CanvasPage.tsx`, find the `<CanvasTopbar` JSX (~line 204) and add the prop:

```tsx
<CanvasTopbar
  workflow={workflow} setWorkflow={setWorkflowNN}
  deployed={deployed} running={running}
  onDeploy={onDeploy} onRun={onRun}
  totalSpend={totalSpend} saveLabel={saveLabel}
  onBack={() => router.push("/workflows")}
  estimatedCostPerRun={estimatedCostPerRun}
/>
```

- [ ] **Step 3: Update `CanvasTopbar` to accept and display the prop**

In `CanvasPage.tsx`, update the `CanvasTopbar` function signature (around line 250):

```typescript
function CanvasTopbar({ workflow, setWorkflow, deployed, running, onDeploy, onRun, totalSpend, saveLabel, onBack, estimatedCostPerRun }: {
  workflow: Workflow;
  setWorkflow: React.Dispatch<React.SetStateAction<Workflow>>;
  deployed: boolean; running: boolean;
  onDeploy: () => void; onRun: () => void;
  totalSpend: string; saveLabel: string;
  onBack: () => void;
  estimatedCostPerRun: number;
}) {
```

- [ ] **Step 4: Add the cost display inside `CanvasTopbar`'s stats section**

Find the stats `<div>` inside `CanvasTopbar` that has `Stat` components (around line 276). Add one more `Stat` at the end of that block:

```tsx
<Stat label="est. cost / run" value={`~$${estimatedCostPerRun.toFixed(4)}`} color={estimatedCostPerRun > 0.05 ? "var(--warm)" : "var(--accent)"} />
```

Place it after the existing `<Stat label="spent / 24h" ... />` line, still inside the same wrapping `<div>`.

- [ ] **Step 5: Verify TypeScript**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/canvas/CanvasPage.tsx
git commit -m "feat: add live per-run cost estimator to canvas topbar"
```

---

## Task 5: Create the Billing page

**Files:**
- Create: `frontend/src/components/billing/BillingPage.tsx`
- Create: `frontend/src/app/billing/page.tsx`

The billing page is a marketing/explainer page with two plans side by side and some FAQ bullets. No forms or API calls.

- [ ] **Step 1: Create the billing page component**

Create `frontend/src/components/billing/BillingPage.tsx`:

```tsx
"use client";
import { useRouter } from "next/navigation";
import { Logo, Pill, Tag, Hairline } from "@/components/ui";

export function BillingPage() {
  const router = useRouter();

  return (
    <div style={{ minHeight: "100vh", background: "var(--bg)", display: "flex", flexDirection: "column" }}>
      {/* ── Nav ── */}
      <header style={{
        height: 52, flexShrink: 0,
        background: "var(--bg-elev-1)", borderBottom: "1px solid var(--border)",
        display: "flex", alignItems: "center", padding: "0 24px", gap: 16,
      }}>
        <button onClick={() => router.push("/")} style={{ background: "transparent", border: "none", cursor: "pointer", padding: 0, display: "inline-flex" }}>
          <Logo size={16} />
        </button>
        <div style={{ width: 1, height: 20, background: "var(--border)" }} />
        <button onClick={() => router.push("/workflows")} style={navLinkStyle}>Workflows</button>
        <button onClick={() => router.push("/marketplace")} style={navLinkStyle}>Marketplace</button>
        <span style={{ color: "var(--fg-muted)", fontSize: 13 }}>Billing</span>
        <div style={{ flex: 1 }} />
      </header>

      {/* ── Hero ── */}
      <div style={{ padding: "60px 24px 48px", textAlign: "center", borderBottom: "1px solid var(--border)" }}>
        <div style={{ marginBottom: 14 }}>
          <Tag>Pricing</Tag>
        </div>
        <h1 style={{ margin: 0, fontSize: 36, fontWeight: 700, letterSpacing: "-0.03em", color: "var(--fg)", lineHeight: 1.2 }}>
          Pay only for what you run
        </h1>
        <p style={{ margin: "14px auto 0", maxWidth: 500, color: "var(--fg-muted)", fontSize: 15, lineHeight: 1.7 }}>
          No seats, no tiers, no minimums. AgentMesh charges you for actual compute — LLM tokens and x402 tool calls your agents make.
        </p>
      </div>

      {/* ── Plans ── */}
      <div style={{ maxWidth: 860, margin: "0 auto", width: "100%", padding: "48px 24px" }}>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 20 }}>
          <PlanCard
            name="Credits"
            tagline="Prepay and never get surprised"
            price="$10"
            priceSub="minimum top-up"
            accent="var(--accent)"
            highlight={false}
            cta="Buy Credits"
            onCta={() => alert("Coming soon — join the waitlist to be notified")}
            features={[
              "Buy credit bundles starting at $10",
              "Credits never expire",
              "Instant top-up via card or crypto",
              "Spending dashboard with per-workflow breakdown",
              "Set per-workflow budget caps to avoid overruns",
              "Works across all LLM providers and x402 tools",
            ]}
            note="Best for: regular users who run workflows on a schedule and want predictable spend."
          />
          <PlanCard
            name="Pay-per-use"
            tagline="Run now, pay at month end"
            price="$0"
            priceSub="upfront — billed monthly"
            accent="#E879F9"
            highlight
            cta="Enable Postpaid"
            onCta={() => alert("Coming soon — postpaid is in closed beta")}
            features={[
              "No upfront payment required",
              "Run workflows immediately",
              "Aggregated invoice sent on the 1st of each month",
              "Line-item breakdown per workflow and run",
              "Pay by card, bank transfer, or ALGO",
              "Spending alerts at 80% and 100% of your monthly estimate",
            ]}
            note="Best for: teams exploring automation or running bursty, unpredictable workloads."
          />
        </div>

        {/* ── How it works ── */}
        <div style={{ marginTop: 56 }}>
          <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: 20 }}>How charges work</div>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
            {[
              {
                step: "01",
                title: "LLM token costs",
                body: "Each agent node calls an LLM provider. We pass through costs at the provider's published rate with no markup. The canvas cost estimator shows you the expected cost before you run.",
              },
              {
                step: "02",
                title: "x402 tool calls",
                body: "Tool402 nodes pay the endpoint operator directly using your Algorand agent wallet, also at the listed price. The amount is settled on-chain instantly — no billing delay.",
              },
              {
                step: "03",
                title: "Infrastructure",
                body: "Workflow execution, storage, and SSE streaming are included free. You only pay for the AI work your agents perform, not for using the platform.",
              },
            ].map((item) => (
              <div key={item.step} style={{ background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-3)", padding: "20px 22px" }}>
                <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--accent)", marginBottom: 10 }}>{item.step}</div>
                <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)", marginBottom: 8 }}>{item.title}</div>
                <div style={{ fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.65 }}>{item.body}</div>
              </div>
            ))}
          </div>
        </div>

        {/* ── FAQ ── */}
        <div style={{ marginTop: 56 }}>
          <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: 20 }}>Common questions</div>
          <div style={{ display: "flex", flexDirection: "column", gap: 0, border: "1px solid var(--border)", borderRadius: "var(--r-3)", overflow: "hidden" }}>
            {[
              {
                q: "What happens when my credits run out?",
                a: "Workflows will pause and you'll receive an email. You can top up instantly — there's no queue or wait. Switching to postpaid means this can never happen.",
              },
              {
                q: "Can I mix credits and postpaid?",
                a: "Not currently — you pick one billing mode per account. You can switch modes at any time from this page; the change takes effect at the start of the next billing cycle.",
              },
              {
                q: "How are x402 payments settled?",
                a: "x402 tool calls are paid from each agent's Algorand wallet, funded by your AgentMesh credits or charged to your postpaid account. Either way, the on-chain transaction is instant.",
              },
              {
                q: "Is there a free tier?",
                a: "You get $2 of free credits when you sign up — enough for thousands of lightweight runs. No credit card required to start.",
              },
              {
                q: "Where can I see a full cost breakdown?",
                a: "The workflow canvas shows a per-run estimate. After each run, the log drawer shows actual token counts and x402 payments. Full history is in the dashboard under Billing → Usage.",
              },
            ].map((item, i, arr) => (
              <div key={i} style={{ padding: "18px 22px", borderBottom: i < arr.length - 1 ? "1px solid var(--border)" : "none" }}>
                <div style={{ fontSize: 13, fontWeight: 600, color: "var(--fg)", marginBottom: 6 }}>{item.q}</div>
                <div style={{ fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.65 }}>{item.a}</div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

// ── Plan Card ──────────────────────────────────────────────────────────────
function PlanCard({ name, tagline, price, priceSub, accent, highlight, cta, onCta, features, note }: {
  name: string;
  tagline: string;
  price: string;
  priceSub: string;
  accent: string;
  highlight: boolean;
  cta: string;
  onCta: () => void;
  features: string[];
  note: string;
}) {
  return (
    <div style={{
      background: highlight ? "var(--bg-elev-2)" : "var(--bg-elev-1)",
      border: `1px solid ${highlight ? accent + "50" : "var(--border)"}`,
      borderRadius: "var(--r-3)",
      padding: "28px 24px",
      display: "flex", flexDirection: "column", gap: 20,
      position: "relative", overflow: "hidden",
    }}>
      {highlight && (
        <div style={{ position: "absolute", top: 0, left: 0, right: 0, height: 2, background: `linear-gradient(90deg, transparent, ${accent}, transparent)` }} />
      )}

      <div>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 6 }}>
          <span style={{ fontSize: 16, fontWeight: 700, color: "var(--fg)" }}>{name}</span>
          {highlight && <Pill tone="accent">Popular</Pill>}
        </div>
        <div style={{ fontSize: 12, color: "var(--fg-muted)" }}>{tagline}</div>
      </div>

      <div>
        <span style={{ fontSize: 36, fontWeight: 800, color: accent, letterSpacing: "-0.04em" }}>{price}</span>
        <span style={{ fontSize: 12, color: "var(--fg-dim)", marginLeft: 8 }}>{priceSub}</span>
      </div>

      <button
        onClick={onCta}
        style={{
          height: 38, width: "100%", fontSize: 13, fontWeight: 600,
          background: highlight ? accent : "var(--bg-elev-3)",
          border: `1px solid ${highlight ? accent : "var(--border-strong)"}`,
          borderRadius: "var(--r-2)", cursor: "pointer",
          color: highlight ? "var(--accent-fg)" : "var(--fg)",
          fontFamily: "var(--font-sans)",
        }}
      >
        {cta}
      </button>

      <div style={{ height: 1, background: "var(--border)" }} />

      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        {features.map((f, i) => (
          <div key={i} style={{ display: "flex", gap: 10, alignItems: "flex-start" }}>
            <span style={{ color: accent, fontSize: 13, flexShrink: 0, marginTop: 1 }}>✓</span>
            <span style={{ fontSize: 13, color: "var(--fg-muted)", lineHeight: 1.5 }}>{f}</span>
          </div>
        ))}
      </div>

      <div style={{ fontSize: 11, color: "var(--fg-dim)", background: "var(--bg)", borderRadius: "var(--r-1)", padding: "10px 12px", lineHeight: 1.6, fontStyle: "italic" }}>
        {note}
      </div>
    </div>
  );
}

const navLinkStyle: React.CSSProperties = {
  background: "transparent", border: "none", cursor: "pointer",
  fontSize: 13, color: "var(--fg-muted)", fontFamily: "var(--font-sans)", padding: "4px 6px",
};
```

- [ ] **Step 2: Create the App Router page**

Create `frontend/src/app/billing/page.tsx`:

```tsx
import { BillingPage } from "@/components/billing/BillingPage";

export default function Page() {
  return <BillingPage />;
}
```

- [ ] **Step 3: Verify TypeScript**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/billing/BillingPage.tsx frontend/src/app/billing/page.tsx
git commit -m "feat: add Billing page with Credits vs Pay-per-use explainer"
```

---

## Task 6: Add nav links to Marketplace and Billing from Workflows page

**Files:**
- Modify: `frontend/src/components/workflows/WorkflowsPage.tsx`

Add "Marketplace" and "Billing" links in the `WorkflowsPage` header so users can navigate there without manually typing URLs.

- [ ] **Step 1: Read the current WorkflowsPage header**

Read `frontend/src/components/workflows/WorkflowsPage.tsx` lines 1–60 to find the topbar/header structure.

- [ ] **Step 2: Add nav links**

In the header `<div>` that contains the Logo and action buttons, add links after the Logo:

```tsx
import { useRouter } from "next/navigation";
// inside component, if not already:
const router = useRouter();
```

Then in the header JSX, after the Logo, add:

```tsx
<div style={{ width: 1, height: 20, background: "var(--border)" }} />
<button onClick={() => router.push("/marketplace")} style={navBtnStyle}>Marketplace</button>
<button onClick={() => router.push("/billing")} style={navBtnStyle}>Billing</button>
```

Where `navBtnStyle` is:
```typescript
const navBtnStyle: React.CSSProperties = {
  background: "transparent", border: "none", cursor: "pointer",
  fontSize: 13, color: "var(--fg-muted)", fontFamily: "var(--font-sans)", padding: "4px 8px",
};
```

- [ ] **Step 3: Verify TypeScript**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/workflows/WorkflowsPage.tsx
git commit -m "feat: add Marketplace and Billing nav links to Workflows page"
```

---

## Task 7: Add Marketplace link to the middleware allowlist (public routes)

**Files:**
- Modify: `frontend/src/middleware.ts`

The middleware currently protects all routes except `/signin`, `/signup`, `/auth/callback`, and `/`. The new `/marketplace` and `/billing` routes should be public (no auth required to browse).

- [ ] **Step 1: Read current middleware**

```bash
cat frontend/src/middleware.ts
```

- [ ] **Step 2: Add public routes**

Find the array of public paths (typically contains `/signin`, `/signup`, etc.) and add `/marketplace` and `/billing`.

The exact edit depends on the middleware structure. Look for something like:
```typescript
const PUBLIC_PATHS = ["/", "/signin", "/signup", "/auth/callback"];
```
and change to:
```typescript
const PUBLIC_PATHS = ["/", "/signin", "/signup", "/auth/callback", "/marketplace", "/billing"];
```

- [ ] **Step 3: Verify TypeScript**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -30
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/middleware.ts
git commit -m "chore: allow /marketplace and /billing as public routes"
```

---

## Self-review

**Spec coverage check:**

| Requirement | Task |
|-------------|------|
| Marketplace page | Task 3 |
| Upload x402 endpoints to marketplace | Task 3 (UploadModal, type="endpoint") |
| Upload workflows to marketplace | Task 3 (UploadModal, type="workflow") |
| Example items on marketplace page | Task 2 (8 endpoints, 4 workflows with preview nodes) |
| Estimated cost in canvas (increases/decreases with nodes) | Task 4 (`estimatedCostPerRun` useMemo reacts to `workflow.nodes`) |
| Buy credits button/page | Task 5 (Credits plan card with "Buy Credits" CTA) |
| Pay-per-use / postpaid option | Task 5 (Pay-per-use plan card) |
| Postpaid billed end of month | Task 5 (explicitly stated in plan card features and FAQ) |
| Navigation to new pages | Tasks 6 and 7 |

**Placeholder scan:** None found — all code blocks are complete with actual JSX/TS.

**Type consistency:**
- `MarketplaceEndpoint` defined in Task 1, imported in Task 2 (data.ts) and Task 3 (MarketplacePage.tsx) ✓
- `MarketplaceWorkflow` defined in Task 1, imported in Task 2 and Task 3 ✓
- `estimatedCostPerRun: number` added to `CanvasTopbar` props in Task 4 ✓
- `MARKETPLACE_ENDPOINTS`, `MARKETPLACE_WORKFLOWS` exported in Task 2, imported in Task 3 ✓
