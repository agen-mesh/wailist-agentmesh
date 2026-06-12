# Bazaar Discovery Integration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the static marketplace endpoint list with live data from the Coinbase x402 Bazaar discovery API, and wire "Add to workflow" so clicking it on a Bazaar card creates a pre-configured `tool402` node in any of the user's workflows.

**Architecture:** A thin Go proxy handler forwards marketplace requests to the Bazaar HTTP API (`https://api.cdp.coinbase.com/platform/v2/x402/discovery`), normalizes the response into our existing `MarketplaceEndpoint` shape (extended with `endpoint` and `discoveredParams`), and returns it to the frontend. The marketplace page fetches this on mount and merges live results with the static curated list. When a user clicks "Add to workflow", a workflow-picker modal appears, stores the node in `localStorage`, and navigates to the chosen canvas — which reads and drops the node automatically.

**Tech Stack:** Go `net/http` (no new dependencies), Next.js App Router, React 19, `localStorage` for cross-page node handoff.

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| **Create** | `backend/internal/api/handlers/marketplace.go` | `BazaarList`, `BazaarSearch` handlers + Bazaar HTTP client + normalization |
| **Create** | `backend/internal/api/handlers/marketplace_test.go` | Unit tests for both handlers using a fake Bazaar server |
| **Modify** | `backend/internal/api/router.go:27` | Add 2 public GET routes |
| **Modify** | `frontend/src/lib/types.ts:92` | Add named `ParamDef` type; extend `MarketplaceEndpoint` with `endpoint?`, `discoveredParams?`, `source?` |
| **Modify** | `frontend/src/lib/api.ts` | Add `marketplace` namespace (`bazaarList`, `bazaarSearch`) |
| **Create** | `frontend/src/components/marketplace/WorkflowPickerModal.tsx` | Modal that lists user's workflows and navigates on selection |
| **Modify** | `frontend/src/components/marketplace/MarketplacePage.tsx` | Live fetch from backend, loading state, Bazaar cards, "Add to workflow" wired to modal |
| **Modify** | `frontend/src/components/canvas/CanvasPage.tsx:51-62` | On workflow load, check `localStorage` for `agentmesh:pendingNode` and inject it |

---

## Task 1: Backend — Bazaar proxy handler

**Files:**
- Create: `backend/internal/api/handlers/marketplace.go`

- [ ] **Step 1: Create the file with Bazaar types and normalization**

```go
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

// bazaarResource is the shape returned by the Coinbase Bazaar discovery API.
type bazaarResource struct {
	ID          string         `json:"id"`
	URL         string         `json:"url"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Price       bazaarPrice    `json:"price"`
	InputSchema map[string]any `json:"inputSchema"`
	Tags        []string       `json:"tags"`
	Category    string         `json:"category"`
	Provider    string         `json:"provider"`
}

type bazaarPrice struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Network  string `json:"network"`
}

// BazaarEndpoint is the normalized shape sent to the frontend.
// It is a superset of the static MarketplaceEndpoint — the extra fields
// (Endpoint, DiscoveredParams) let the canvas pre-configure a tool402 node.
type BazaarEndpoint struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Description      string           `json:"description"`
	Provider         string           `json:"provider"`
	Price            string           `json:"price"`
	Unit             string           `json:"unit"`
	Category         string           `json:"category"`
	Tags             []string         `json:"tags"`
	Endpoint         string           `json:"endpoint"`
	DiscoveredParams []models.ParamDef `json:"discoveredParams,omitempty"`
	Source           string           `json:"source"` // always "bazaar"
}

func bazaarBase() string {
	if u := os.Getenv("BAZAAR_BASE_URL"); u != "" {
		return u
	}
	return "https://api.cdp.coinbase.com/platform/v2/x402/discovery"
}

// BazaarList proxies GET /marketplace/bazaar → Bazaar /resources.
func (d *Deps) BazaarList(w http.ResponseWriter, r *http.Request) {
	limit := r.URL.Query().Get("limit")
	offset := r.URL.Query().Get("offset")
	if limit == "" {
		limit = "24"
	}
	if offset == "" {
		offset = "0"
	}
	endpoints, err := fetchBazaar(r.Context(), fmt.Sprintf("/resources?limit=%s&offset=%s", limit, offset))
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "bazaar unavailable")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"endpoints": endpoints})
}

// BazaarSearch proxies GET /marketplace/bazaar/search?q= → Bazaar /search.
func (d *Deps) BazaarSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		respond.Error(w, http.StatusBadRequest, "q required")
		return
	}
	endpoints, err := fetchBazaar(r.Context(), "/search?query="+url.QueryEscape(q))
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "bazaar unavailable")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"endpoints": endpoints})
}

func fetchBazaar(ctx context.Context, path string) ([]BazaarEndpoint, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bazaarBase()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key := os.Getenv("CDP_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw struct {
		Resources []bazaarResource `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("bazaar decode: %w", err)
	}

	out := make([]BazaarEndpoint, 0, len(raw.Resources))
	for _, res := range raw.Resources {
		out = append(out, normalizeBazaarResource(res))
	}
	return out, nil
}

func normalizeBazaarResource(res bazaarResource) BazaarEndpoint {
	return BazaarEndpoint{
		ID:               "bazaar-" + res.ID,
		Name:             res.Name,
		Description:      res.Description,
		Provider:         res.Provider,
		Price:            res.Price.Amount,
		Unit:             "call",
		Category:         normalizeBazaarCategory(res.Category),
		Tags:             res.Tags,
		Endpoint:         res.URL,
		DiscoveredParams: extractParamDefs(res.InputSchema),
		Source:           "bazaar",
	}
}

func normalizeBazaarCategory(cat string) string {
	switch strings.ToLower(cat) {
	case "search":
		return "search"
	case "data", "analytics":
		return "data"
	case "ai", "ml", "nlp":
		return "ai"
	case "finance", "payments", "crypto":
		return "finance"
	case "media", "image", "video", "audio":
		return "media"
	default:
		return "util"
	}
}

func extractParamDefs(schema map[string]any) []models.ParamDef {
	if schema == nil {
		return nil
	}
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return nil
	}
	requiredRaw, _ := schema["required"].([]any)
	requiredSet := make(map[string]bool, len(requiredRaw))
	for _, r := range requiredRaw {
		if s, ok := r.(string); ok {
			requiredSet[s] = true
		}
	}

	params := make([]models.ParamDef, 0, len(props))
	for name, v := range props {
		prop, _ := v.(map[string]any)
		typ, _ := prop["type"].(string)
		if typ == "" {
			typ = "string"
		}
		desc, _ := prop["description"].(string)
		params = append(params, models.ParamDef{
			Name:        name,
			Type:        typ,
			Required:    requiredSet[name],
			Description: desc,
		})
	}
	return params
}
```

- [ ] **Step 2: Build to verify it compiles**

```bash
cd backend && go build ./...
```

Expected: no output (clean build).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/api/handlers/marketplace.go
git commit -m "feat(backend): add Bazaar discovery proxy handler"
```

---

## Task 2: Backend — Wire the routes

**Files:**
- Modify: `backend/internal/api/router.go`

- [ ] **Step 1: Add 2 public GET routes to the public section**

In `backend/internal/api/router.go`, the public routes block currently ends at `r.Post("/hooks/...")`. Add immediately after it (before the protected `r.Group`):

```go
// Marketplace — public so the page loads without auth
r.Get("/marketplace/bazaar", d.BazaarList)
r.Get("/marketplace/bazaar/search", d.BazaarSearch)
```

The relevant section of `router.go` after the edit:

```go
r.Post("/run/{workflowId}", d.PublicTrigger)
r.Post("/hooks/{workflowId}/{nodeId}", d.PublicTrigger)

// Marketplace — public so the page loads without auth
r.Get("/marketplace/bazaar", d.BazaarList)
r.Get("/marketplace/bazaar/search", d.BazaarSearch)

// Protected routes — JWT required
r.Group(func(r chi.Router) {
```

- [ ] **Step 2: Build again to verify**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 3: Smoke test the route exists**

```bash
cd backend && go run ./cmd/server &
sleep 1
curl -s http://localhost:8080/marketplace/bazaar | head -c 200
kill %1
```

Expected: JSON response (either `{"endpoints":[...]}` from Bazaar, or a BadGateway error if Bazaar is unreachable in dev — both are fine, the route is wired).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/router.go
git commit -m "feat(backend): expose public /marketplace/bazaar routes"
```

---

## Task 3: Backend — Handler tests

**Files:**
- Create: `backend/internal/api/handlers/marketplace_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
)

// fakeBazaarServer returns an httptest.Server that serves a single Bazaar resource.
// Callers must call Close() on the server.
func fakeBazaarServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"resources": []map[string]any{
				{
					"id":          "abc123",
					"url":         "https://example.com/api",
					"name":        "Test API",
					"description": "A test API",
					"provider":    "TestCo",
					"price":       map[string]any{"amount": "0.005", "currency": "USDC", "network": "base-mainnet"},
					"tags":        []string{"test", "data"},
					"category":    "data",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{"type": "string", "description": "Search query"},
						},
						"required": []string{"query"},
					},
				},
			},
		})
	}))
}

func TestBazaarList(t *testing.T) {
	fake := fakeBazaarServer(t)
	defer fake.Close()
	t.Setenv("BAZAAR_BASE_URL", fake.URL)

	d := &handlers.Deps{}
	req := httptest.NewRequest(http.MethodGet, "/marketplace/bazaar", nil)
	w := httptest.NewRecorder()
	d.BazaarList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	eps, _ := resp["endpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("want 1 endpoint got %d", len(eps))
	}
	ep, _ := eps[0].(map[string]any)
	if ep["source"] != "bazaar" {
		t.Errorf("want source=bazaar got %v", ep["source"])
	}
	if ep["endpoint"] != "https://example.com/api" {
		t.Errorf("want endpoint URL got %v", ep["endpoint"])
	}
	params, _ := ep["discoveredParams"].([]any)
	if len(params) != 1 {
		t.Errorf("want 1 param got %d", len(params))
	}
}

func TestBazaarSearch(t *testing.T) {
	fake := fakeBazaarServer(t)
	defer fake.Close()
	t.Setenv("BAZAAR_BASE_URL", fake.URL)

	d := &handlers.Deps{}
	req := httptest.NewRequest(http.MethodGet, "/marketplace/bazaar/search?q=weather", nil)
	w := httptest.NewRecorder()
	d.BazaarSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	eps, _ := resp["endpoints"].([]any)
	if len(eps) < 1 {
		t.Error("want at least 1 endpoint")
	}
}

func TestBazaarSearchMissingQ(t *testing.T) {
	d := &handlers.Deps{}
	req := httptest.NewRequest(http.MethodGet, "/marketplace/bazaar/search", nil)
	w := httptest.NewRecorder()
	d.BazaarSearch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to see them fail first**

```bash
cd backend && go test ./internal/api/handlers/... -run TestBazaar -v
```

Expected: `FAIL` — functions `BazaarList`/`BazaarSearch` don't exist yet (will pass after Task 1 is done, so at this point in the sequence they should compile and pass because Task 1 already created them).

Actually, since Task 1 created the handlers already, run the tests now:

```bash
cd backend && go test ./internal/api/handlers/... -run TestBazaar -v
```

Expected output:
```
--- PASS: TestBazaarList (0.00s)
--- PASS: TestBazaarSearch (0.00s)
--- PASS: TestBazaarSearchMissingQ (0.00s)
PASS
```

- [ ] **Step 3: Commit**

```bash
git add backend/internal/api/handlers/marketplace_test.go
git commit -m "test(backend): add Bazaar proxy handler tests"
```

---

## Task 4: Frontend — Extend types

**Files:**
- Modify: `frontend/src/lib/types.ts`

The goal is to:
1. Add a named `ParamDef` type (currently only defined inline in `api.ts`)
2. Extend `MarketplaceEndpoint` with three optional Bazaar-specific fields

- [ ] **Step 1: Add `ParamDef` and extend `MarketplaceEndpoint`**

Find the `MarketplaceEndpoint` interface at line 92 in `frontend/src/lib/types.ts`. Add `ParamDef` **above** it, and add the three new fields to `MarketplaceEndpoint`:

```typescript
// Add this block above MarketplaceEndpoint:
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
  icon: string;
  tags: string[];
  author: string;
  calls: number;
  rating: number;
  featured?: boolean;
  // Bazaar-sourced fields — absent on static entries
  endpoint?: string;
  discoveredParams?: ParamDef[];
  source?: "static" | "bazaar";
}
```

- [ ] **Step 2: Update `api.ts` to use the named `ParamDef` type**

In `frontend/src/lib/api.ts` line 1, add `ParamDef` to the types import:

```typescript
import { Workflow, ParamDef } from "./types";
```

Then update the `tools.x402quote` return type (around line 224) to use `ParamDef`:

```typescript
x402quote: async (url: string): Promise<{
  price?: string; unit?: string; network?: string; recipient?: string; raw?: string; description?: string;
  params?: ParamDef[];
}> => {
```

- [ ] **Step 3: Type-check**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/types.ts frontend/src/lib/api.ts
git commit -m "feat(frontend): add ParamDef type, extend MarketplaceEndpoint for Bazaar"
```

---

## Task 5: Frontend — API layer

**Files:**
- Modify: `frontend/src/lib/api.ts`

- [ ] **Step 1: Add `MarketplaceEndpoint` to the import and add `marketplace` namespace**

First, update the types import at the top of `api.ts`:

```typescript
import { Workflow, ParamDef, MarketplaceEndpoint } from "./types";
```

Then add the `marketplace` export after the `tools` block (before `waitlist`):

```typescript
// -- Marketplace ----------------------------------------------------------
export const marketplace = {
  bazaarList: async (
    limit = 24,
    offset = 0,
  ): Promise<{ endpoints: MarketplaceEndpoint[] }> => {
    if (BASE) {
      const res = await fetch(
        `${BASE}/marketplace/bazaar?limit=${limit}&offset=${offset}`,
        { credentials: "include" },
      );
      if (!res.ok) return { endpoints: [] };
      return res.json();
    }
    return { endpoints: [] };
  },

  bazaarSearch: async (q: string): Promise<{ endpoints: MarketplaceEndpoint[] }> => {
    if (BASE) {
      const res = await fetch(
        `${BASE}/marketplace/bazaar/search?q=${encodeURIComponent(q)}`,
        { credentials: "include" },
      );
      if (!res.ok) return { endpoints: [] };
      return res.json();
    }
    return { endpoints: [] };
  },
};
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/api.ts
git commit -m "feat(frontend): add marketplace.bazaarList and bazaarSearch to API layer"
```

---

## Task 6: Frontend — Workflow picker modal

**Files:**
- Create: `frontend/src/components/marketplace/WorkflowPickerModal.tsx`

This modal fetches the user's workflows and lets them pick one. On selection it stores the pending node in `localStorage` and navigates to the canvas.

- [ ] **Step 1: Create the file**

```typescript
"use client";
import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { workflows as workflowsApi } from "@/lib/api";
import type { MarketplaceEndpoint } from "@/lib/types";

interface Props {
  endpoint: MarketplaceEndpoint;
  onClose: () => void;
}

export function WorkflowPickerModal({ endpoint, onClose }: Props) {
  const router = useRouter();
  const [items, setItems] = useState<{ id: string; name: string }[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    workflowsApi
      .list()
      .then((wfs) => setItems(wfs.map((w) => ({ id: w.id, name: w.name }))))
      .catch(() => setItems([]))
      .finally(() => setLoading(false));
  }, []);

  const pick = (workflowId: string) => {
    localStorage.setItem(
      "agentmesh:pendingNode",
      JSON.stringify({
        type: "tool402",
        name: endpoint.name,
        endpoint: endpoint.endpoint ?? "",
        description: endpoint.description,
        discoveredParams: endpoint.discoveredParams ?? [],
      }),
    );
    router.push(`/workflows/${workflowId}`);
    onClose();
  };

  return (
    <div
      style={{ position: "fixed", inset: 0, background: "rgba(8,7,12,0.7)", backdropFilter: "blur(4px)", zIndex: 200, display: "flex", alignItems: "center", justifyContent: "center" }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div style={{ width: 420, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-3)", padding: "24px", display: "flex", flexDirection: "column", gap: 16 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div>
            <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>Add to workflow</div>
            <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", marginTop: 2 }}>{endpoint.name}</div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--fg-muted)", cursor: "pointer", fontSize: 18 }}>✕</button>
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          {loading && (
            <div style={{ fontSize: 12, color: "var(--fg-dim)", fontFamily: "var(--font-mono)", padding: "16px 0", textAlign: "center" }}>
              loading workflows…
            </div>
          )}
          {!loading && items.length === 0 && (
            <div style={{ fontSize: 12, color: "var(--fg-dim)", fontFamily: "var(--font-mono)", padding: "16px 0", textAlign: "center" }}>
              No workflows yet — create one first.
            </div>
          )}
          {items.map((wf) => (
            <button
              key={wf.id}
              onClick={() => pick(wf.id)}
              style={{ textAlign: "left", padding: "10px 14px", background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, cursor: "pointer", fontFamily: "var(--font-sans)" }}
            >
              {wf.name}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/marketplace/WorkflowPickerModal.tsx
git commit -m "feat(frontend): add WorkflowPickerModal for marketplace → canvas handoff"
```

---

## Task 7: Frontend — MarketplacePage live fetch

**Files:**
- Modify: `frontend/src/components/marketplace/MarketplacePage.tsx`

Replace the static-only rendering with live Bazaar data. Static entries remain as "Curated" (featured). Bazaar entries appear in an "API Marketplace" section. Search: when `BASE` is configured, debounce-calls `bazaarSearch`; otherwise filters static list.

- [ ] **Step 1: Rewrite the top of MarketplacePage.tsx — imports and state**

Replace the existing import block and the `MarketplacePage` function opening with:

```typescript
"use client";
import { useState, useMemo, useEffect, useRef } from "react";
import { useRouter } from "next/navigation";
import { Logo, Pill, Tag, IconSearch, Toast } from "@/components/ui";
import { marketplace as marketplaceApi, workflows as workflowsApi } from "@/lib/api";
import { MARKETPLACE_ENDPOINTS } from "@/lib/data";
import type { MarketplaceEndpoint } from "@/lib/types";
import { WorkflowPickerModal } from "./WorkflowPickerModal";

const CATEGORIES = [
  { id: "all",     label: "All" },
  { id: "search",  label: "Search" },
  { id: "data",    label: "Data" },
  { id: "ai",      label: "AI" },
  { id: "finance", label: "Finance" },
  { id: "media",   label: "Media" },
  { id: "util",    label: "Util" },
] as const;
type CategoryId = (typeof CATEGORIES)[number]["id"];

export function MarketplacePage() {
  const router = useRouter();
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<CategoryId>("all");
  const [uploadOpen, setUploadOpen] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [pickerEndpoint, setPickerEndpoint] = useState<MarketplaceEndpoint | null>(null);

  // Bazaar live data
  const [bazaarEndpoints, setBazaarEndpoints] = useState<MarketplaceEndpoint[]>([]);
  const [bazaarLoading, setBazaarLoading] = useState(true);
  const [bazaarError, setBazaarError] = useState(false);

  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const showToast = (msg: string) => { setToast(msg); setTimeout(() => setToast(null), 2600); };

  // Initial load: fetch Bazaar catalog
  useEffect(() => {
    setBazaarLoading(true);
    marketplaceApi
      .bazaarList(24, 0)
      .then(({ endpoints }) => { setBazaarEndpoints(endpoints); setBazaarError(false); })
      .catch(() => setBazaarError(true))
      .finally(() => setBazaarLoading(false));
  }, []);

  // Search: debounce 400ms — live search via Bazaar when query changes
  useEffect(() => {
    if (!query.trim()) {
      // Reset to full catalog on empty query
      marketplaceApi.bazaarList(24, 0).then(({ endpoints }) => setBazaarEndpoints(endpoints)).catch(() => {});
      return;
    }
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => {
      marketplaceApi
        .bazaarSearch(query)
        .then(({ endpoints }) => setBazaarEndpoints(endpoints))
        .catch(() => {});
    }, 400);
    return () => { if (searchTimer.current) clearTimeout(searchTimer.current); };
  }, [query]);

  const showFeatured = category === "all" && !query;

  // Static entries filtered by category + query
  const filteredStatic = useMemo(() =>
    MARKETPLACE_ENDPOINTS.filter((ep) => {
      const matchCat = category === "all" || ep.category === category;
      const q = query.toLowerCase();
      const matchQ = !q || ep.name.toLowerCase().includes(q) || ep.description.toLowerCase().includes(q) || ep.tags.some((t) => t.includes(q));
      return matchCat && matchQ;
    }), [query, category]);

  // Bazaar entries filtered by category (search is server-side)
  const filteredBazaar = useMemo(() =>
    bazaarEndpoints.filter((ep) =>
      category === "all" || ep.category === category
    ), [bazaarEndpoints, category]);

  const handleAdd = (ep: MarketplaceEndpoint) => {
    if (ep.endpoint) {
      // Bazaar endpoint with a real URL — open workflow picker
      setPickerEndpoint(ep);
    } else {
      // Static / no endpoint — toast only
      showToast(`${ep.name} noted — paste the endpoint URL in the inspector after adding the node`);
    }
  };
```

- [ ] **Step 2: Update the JSX return — add Bazaar section and loading state**

Replace the JSX return (everything from `return (` to the closing `);`) with:

```typescript
  return (
    <div style={{ minHeight: "100vh", background: "var(--bg)", display: "flex", flexDirection: "column" }}>
      {/* ── Nav ── */}
      <header style={{ height: 52, flexShrink: 0, background: "var(--bg-elev-1)", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", padding: "0 24px", gap: 16 }}>
        <button onClick={() => router.push("/")} style={ghostStyle}><Logo size={16} /></button>
        <div style={{ width: 1, height: 20, background: "var(--border)" }} />
        <button onClick={() => router.push("/workflows")} style={navLinkStyle}>Workflows</button>
        <span style={{ color: "var(--fg)", fontSize: 13, fontWeight: 500 }}>Marketplace</span>
        <div style={{ flex: 1 }} />
        <button onClick={() => router.push("/billing")} style={navLinkStyle}>Billing</button>
        <button onClick={() => setUploadOpen(true)} style={primaryBtnStyle}>+ Publish</button>
      </header>

      {/* ── Hero ── */}
      <div style={{ padding: "48px 24px 32px", textAlign: "center", borderBottom: "1px solid var(--border)", background: "linear-gradient(180deg, var(--bg-elev-1) 0%, var(--bg) 100%)" }}>
        <div style={{ marginBottom: 12 }}><Tag>x402 Marketplace</Tag></div>
        <h1 style={{ margin: 0, fontSize: 32, fontWeight: 700, letterSpacing: "-0.03em", color: "var(--fg)", lineHeight: 1.2 }}>
          Plug-and-pay AI tools &amp; workflows
        </h1>
        <p style={{ margin: "12px auto 0", maxWidth: 520, color: "var(--fg-muted)", fontSize: 15, lineHeight: 1.7 }}>
          Browse x402-enabled endpoints and ready-made workflows. Every tool is pay-per-call — no API keys, no subscriptions.
        </p>
        <div style={{ display: "flex", alignItems: "center", gap: 10, maxWidth: 480, margin: "24px auto 0", background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)", padding: "0 14px", height: 40 }}>
          <IconSearch size={14} />
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search Bazaar endpoints…"
            style={{ flex: 1, background: "transparent", border: "none", outline: "none", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)" }} />
        </div>
      </div>

      {/* ── Content ── */}
      <div style={{ flex: 1, maxWidth: 1120, margin: "0 auto", width: "100%", padding: "0 24px 48px" }}>
        {/* Category chips */}
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 24, marginBottom: 28 }}>
          {CATEGORIES.map((cat) => (
            <button key={cat.id} onClick={() => setCategory(cat.id)} style={{
              height: 28, padding: "0 12px", fontSize: 12, fontWeight: 500, borderRadius: 999, cursor: "pointer",
              fontFamily: "var(--font-sans)",
              background: category === cat.id ? "var(--accent-soft)" : "var(--bg-elev-2)",
              border: `1px solid ${category === cat.id ? "var(--accent-line)" : "var(--border)"}`,
              color: category === cat.id ? "var(--accent)" : "var(--fg-muted)",
            }}>{cat.label}</button>
          ))}
        </div>

        {/* Curated / featured static section */}
        {showFeatured && filteredStatic.length > 0 && (
          <div style={{ marginBottom: 36 }}>
            <SectionLabel>Curated</SectionLabel>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
              {filteredStatic.filter((e) => e.featured).map((ep) => (
                <EndpointCard key={ep.id} ep={ep} featured onAdd={() => handleAdd(ep)} />
              ))}
            </div>
          </div>
        )}

        {/* Live Bazaar section */}
        <div style={{ marginBottom: 36 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 14 }}>
            <SectionLabel>{query ? `Bazaar results · ${filteredBazaar.length}` : "Live from Bazaar"}</SectionLabel>
            {bazaarLoading && <span style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>fetching…</span>}
            {bazaarError && <span style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "#f87171" }}>bazaar unreachable</span>}
          </div>
          {bazaarLoading && (
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
              {Array.from({ length: 6 }).map((_, i) => <SkeletonCard key={i} />)}
            </div>
          )}
          {!bazaarLoading && filteredBazaar.length === 0 && !bazaarError && (
            <div style={{ fontSize: 12, color: "var(--fg-dim)", fontFamily: "var(--font-mono)", padding: "24px 0" }}>
              {query ? `No Bazaar results for "${query}"` : "No endpoints found"}
            </div>
          )}
          {!bazaarLoading && filteredBazaar.length > 0 && (
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
              {filteredBazaar.map((ep) => (
                <EndpointCard key={ep.id} ep={ep} onAdd={() => handleAdd(ep)} />
              ))}
            </div>
          )}
        </div>
      </div>

      {pickerEndpoint && (
        <WorkflowPickerModal endpoint={pickerEndpoint} onClose={() => setPickerEndpoint(null)} />
      )}
      {uploadOpen && <UploadModal onClose={() => setUploadOpen(false)} onSubmit={(name) => { setUploadOpen(false); showToast(`"${name}" submitted for review`); }} />}
      {toast && <Toast message={toast} />}
    </div>
  );
}
```

- [ ] **Step 3: Add `SkeletonCard` component below `SectionLabel`**

Add this after the existing `SectionLabel` helper and before `EmptyState`:

```typescript
function SkeletonCard() {
  return (
    <div style={{ background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-3)", padding: "18px 20px", height: 140, display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ display: "flex", gap: 12, alignItems: "flex-start" }}>
        <div style={{ width: 40, height: 40, borderRadius: "var(--r-2)", background: "var(--bg-elev-3)" }} />
        <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 6 }}>
          <div style={{ height: 12, width: "60%", borderRadius: 4, background: "var(--bg-elev-3)" }} />
          <div style={{ height: 10, width: "40%", borderRadius: 4, background: "var(--bg-elev-3)" }} />
        </div>
      </div>
      <div style={{ height: 10, width: "90%", borderRadius: 4, background: "var(--bg-elev-3)" }} />
      <div style={{ height: 10, width: "70%", borderRadius: 4, background: "var(--bg-elev-3)" }} />
    </div>
  );
}
```

- [ ] **Step 4: Update `EndpointCard` to show a "Bazaar" badge for live entries**

In the existing `EndpointCard` function, update the name row to show the badge when `ep.source === "bazaar"`:

```typescript
<div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 3 }}>
  <span style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>{ep.name}</span>
  {featured && <Pill tone="accent">Featured</Pill>}
  {ep.source === "bazaar" && <Pill tone="success">Bazaar</Pill>}
</div>
```

**Note:** If `Pill` does not support `tone="success"`, use `tone="accent"` for now — check `frontend/src/components/ui` for available tones before applying.

- [ ] **Step 5: Type-check**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/marketplace/MarketplacePage.tsx
git commit -m "feat(frontend): live Bazaar feed in marketplace with loading state and search"
```

---

## Task 8: Frontend — Canvas pickup of pending node

**Files:**
- Modify: `frontend/src/components/canvas/CanvasPage.tsx`

When the canvas loads a workflow it checks `localStorage` for `agentmesh:pendingNode`. If present, it creates the node at a sensible default position, selects it, and clears the key.

- [ ] **Step 1: Add the pending node injection to the workflow load effect**

In `CanvasPage.tsx`, find the `.then((wf) => { ... setLoading(false); })` block inside the workflow load `useEffect` (around line 51). Add the pending-node check immediately after `setLoading(false)`:

```typescript
workflowsApi.get(workflowId)
  .then((wf) => {
    justLoaded.current = true;
    // Check for a pending node dropped from the marketplace
    const raw = localStorage.getItem("agentmesh:pendingNode");
    if (raw) {
      try {
        const meta = JSON.parse(raw) as Partial<WorkflowNode>;
        const id = `n_${Date.now()}`;
        const pendingNode: WorkflowNode = { id, x: 280, y: 180, ...meta } as WorkflowNode;
        wf = { ...wf, nodes: [...wf.nodes, pendingNode] };
        // selectedId will be set after setWorkflow via the state update below
        setTimeout(() => setSelectedId(id), 0);
      } catch {
        // malformed JSON — ignore
      } finally {
        localStorage.removeItem("agentmesh:pendingNode");
      }
    }
    setWorkflow(wf);
    if (wf.nodes.some((n) => n.type === "agent" && n.wallet)) {
      setDeployed(true);
    }
    setLoading(false);
  })
  .catch(() => { router.push("/workflows"); });
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/canvas/CanvasPage.tsx
git commit -m "feat(frontend): inject marketplace pending node on canvas load"
```

---

## Task 9: End-to-end smoke test

This task verifies the full integration path works: Bazaar → marketplace → workflow picker → canvas.

- [ ] **Step 1: Start backend and frontend**

```bash
# Terminal 1
cd backend && go run ./cmd/server

# Terminal 2
cd frontend && npm run dev
```

- [ ] **Step 2: Verify Bazaar endpoint directly**

```bash
curl -s http://localhost:8080/marketplace/bazaar | python3 -m json.tool | head -40
```

Expected: JSON with `{"endpoints": [...]}`. If Bazaar is unreachable in dev, you'll see `{"error":"bazaar unavailable"}` — that is correct behavior; the frontend handles it gracefully.

- [ ] **Step 3: Open the marketplace in the browser**

Navigate to `http://localhost:3000/marketplace`.

Expected:
- 6 skeleton cards animate while loading
- Bazaar results appear with green "Bazaar" badge
- Static "Curated" section still shows above

- [ ] **Step 4: Test search**

Type "weather" in the search box. Wait 400ms.

Expected: Bazaar results filter to weather-related APIs.

- [ ] **Step 5: Test "Add to workflow"**

Click "+ Add to workflow" on a Bazaar card (one with a green badge).

Expected:
- Workflow picker modal opens
- Lists the user's existing workflows
- Clicking a workflow navigates to `/workflows/{id}`
- On the canvas, the tool402 node appears pre-positioned at (280, 180) and is selected
- Inspector shows pre-filled endpoint URL and discovered params

- [ ] **Step 6: Verify node wiring still works**

Wire the new tool402 node to an existing agent node via the `tools` attach port. Run the workflow.

Expected: The agent picks up the Bazaar tool as a function declaration. No engine changes were needed — it works as-is.

---

## Self-Review

### Spec coverage

| Requirement | Implemented in |
|---|---|
| Bazaar proxy handler (list) | Task 1, 2 |
| Bazaar proxy handler (search) | Task 1, 2 |
| `BAZAAR_BASE_URL` injectable for tests | Task 1 (`bazaarBase()`) |
| `CDP_API_KEY` auth header passthrough | Task 1 (`fetchBazaar`) |
| JSON schema → `DiscoveredParams` extraction | Task 1 (`extractParamDefs`) |
| Category normalization | Task 1 (`normalizeBazaarCategory`) |
| Handler tests with fake server | Task 3 |
| `ParamDef` named type | Task 4 |
| `MarketplaceEndpoint` extended | Task 4 |
| `api.ts` marketplace namespace | Task 5 |
| `WorkflowPickerModal` component | Task 6 |
| MarketplacePage live fetch on mount | Task 7 |
| Skeleton loading state | Task 7 |
| Debounced live search | Task 7 |
| "Bazaar" badge on live cards | Task 7 |
| "Add to workflow" opens picker | Task 7 |
| localStorage handoff | Task 6, 8 |
| Canvas injects node on load | Task 8 |
| Node auto-selected in inspector | Task 8 |
| Engine untouched — zero changes needed | — |

### Placeholder scan

None found.

### Type consistency

- `MarketplaceEndpoint.discoveredParams` is `ParamDef[]` — matches `WorkflowPickerModal` localStorage write `discoveredParams: endpoint.discoveredParams ?? []` ✓
- `WorkflowNode` spread in `CanvasPage`: `{ id, x, y, ...meta }` — same pattern used in `CanvasGraph.tsx` onDrop ✓
- `BazaarEndpoint.DiscoveredParams` uses `models.ParamDef` — same struct the engine's `buildFuncDecls()` already reads ✓
