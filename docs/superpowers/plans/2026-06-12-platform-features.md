# Platform Features Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add "use ours / use yours" toggles for LLM keys + email + agent wallets, live webhook URLs, agent cost limits, dashboard credits widget, run counters, and functional marketplace workflow cloning.

**Architecture:** Changes span frontend (Inspector, WorkflowsPage, MarketplacePage) and backend (models, engine, deploy handler). All toggles store their state on the `WorkflowNode` so they persist with the workflow. "Use ours" means the backend falls back to env vars; the frontend never sees or stores the platform credentials.

**Tech Stack:** Go 1.25 (backend), Next.js 16 / React 19 / TypeScript (frontend), go-algorand-sdk v2, inline CSS-in-JS.

---

## Backend env vars you need to add to `.env`

```bash
# Platform Algorand wallet — 25-word mnemonic of a pre-funded testnet account.
# Agents with selfFundWallet=false will use this wallet to pay for x402 calls.
# Generate: `goal account export -a <address>` or algokit
PLATFORM_ALGO_MNEMONIC=word1 word2 word3 ... word25

# Platform email key — used when action nodes have useOurEmail=true
PLATFORM_RESEND_KEY=re_xxxxxxxxxxxx

# LLM keys already in .env.example are used for useOurKey=true providers:
# GEMINI_API_KEY, OPENAI_API_KEY, ANTHROPIC_API_KEY
# Add these if missing:
GROQ_API_KEY=gsk_xxxxxxxxxxxx
MISTRAL_API_KEY=xxxxxxxxxxxx
```

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `frontend/src/lib/types.ts` | Modify | Add new node fields: `useOurKey`, `selfFundWallet`, `maxCostPerRun`, `maxSpendTotal`, `useOurEmail`, `webhookMethod`, `webhookPayloadSchema`, `webhookLiveURL` |
| `frontend/src/components/canvas/Inspector.tsx` | Modify | Wire limits, add toggles for all node types, webhook live URL display |
| `frontend/src/components/canvas/CanvasPage.tsx` | Modify | Update cost estimator to account for `useOurKey` markup |
| `frontend/src/components/workflows/WorkflowsPage.tsx` | Modify | Add credits widget, show run counts on workflow cards |
| `frontend/src/components/marketplace/MarketplacePage.tsx` | Modify | "Use template" clones workflow via API, shows toast with link |
| `frontend/src/lib/api.ts` | Modify | Add `workflows.createFromTemplate(nodes, edges, name)` |
| `backend/internal/models/types.go` | Modify | Add `UseOurKey`, `SelfFundWallet`, `MaxCostPerRun`, `MaxSpendTotal`, `UseOurEmail`, `WebhookMethod`, `WebhookPayloadSchema`, `WebhookLiveURL` to `WorkflowNode` |
| `backend/internal/engine/nodes/provider.go` | Modify | Fall back to env key when `UseOurKey=true` or `APIKey` is empty |
| `backend/internal/engine/nodes/action.go` | Modify | Fall back to `PLATFORM_RESEND_KEY` when `UseOurEmail=true` |
| `backend/internal/engine/runner.go` | Modify | Platform wallet signer for agents with `SelfFundWallet=false`; enforce `MaxCostPerRun` |
| `backend/internal/api/handlers/workflows.go` | Modify | Generate `WebhookLiveURL` on deploy for webhook-trigger workflows; increment run count |

---

## Task 1: Add new fields to frontend types

**Files:**
- Modify: `frontend/src/lib/types.ts`

- [ ] **Step 1: Add fields to `WorkflowNode` interface**

Open `frontend/src/lib/types.ts`. In the `WorkflowNode` interface, after the `priceLive?: boolean` line, add:

```typescript
  // "use ours / yours" toggles
  useOurKey?: boolean;       // provider: use platform LLM key from env
  useOurEmail?: boolean;     // action: use platform email key from env
  selfFundWallet?: boolean;  // agent: user will fund the wallet themselves
  // agent limits
  maxCostPerRun?: string;    // e.g. "0.50" ALGO
  maxSpendTotal?: string;    // lifetime cap e.g. "10.00" ALGO
  // webhook trigger live config
  webhookMethod?: string;    // "GET" | "POST" (default POST)
  webhookPayloadSchema?: string; // freeform JSON schema description
  webhookLiveURL?: string;   // generated after deploy, e.g. /hooks/wf-abc123
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -20
```

Expected: no output (zero errors).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/types.ts
git commit -m "feat: add useOurKey, selfFundWallet, limits, webhook fields to WorkflowNode type"
```

---

## Task 2: Add new fields to backend WorkflowNode model

**Files:**
- Modify: `backend/internal/models/types.go`

- [ ] **Step 1: Add fields to `WorkflowNode` struct**

Open `backend/internal/models/types.go`. In the `WorkflowNode` struct, after the `Description` field at the bottom, add:

```go
	// "use ours" toggles
	UseOurKey        bool   `json:"useOurKey,omitempty"`
	UseOurEmail      bool   `json:"useOurEmail,omitempty"`
	SelfFundWallet   bool   `json:"selfFundWallet,omitempty"`
	// agent limits
	MaxCostPerRun    string `json:"maxCostPerRun,omitempty"`
	MaxSpendTotal    string `json:"maxSpendTotal,omitempty"`
	// webhook
	WebhookMethod       string `json:"webhookMethod,omitempty"`
	WebhookPayloadSchema string `json:"webhookPayloadSchema,omitempty"`
	WebhookLiveURL      string `json:"webhookLiveURL,omitempty"`
```

- [ ] **Step 2: Build to verify**

```bash
cd /Users/levi/Desktop/agentmesh-new/backend && go build ./... 2>&1
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/models/types.go
git commit -m "feat: add UseOurKey, SelfFundWallet, limits, webhook fields to backend WorkflowNode"
```

---

## Task 3: Backend — provider "use our key" fallback

**Files:**
- Modify: `backend/internal/engine/nodes/provider.go`

The provider node calls the LLM using `provider.APIKey`. When `UseOurKey=true` (or `APIKey` is empty), the backend should fall back to the appropriate env var.

- [ ] **Step 1: Add `resolveAPIKey` helper**

Read `backend/internal/engine/nodes/provider.go` to find where `provider.APIKey` is used in each LLM caller. Add this helper function near the top of the file, after the imports:

```go
// resolveAPIKey returns the node's API key if set, otherwise falls back to
// the env var for the given provider template when UseOurKey is true.
func resolveAPIKey(node models.WorkflowNode) string {
	if node.APIKey != "" && node.APIKey != "__enc__" && !node.UseOurKey {
		return node.APIKey
	}
	switch node.Template {
	case "gemini":
		return os.Getenv("GEMINI_API_KEY")
	case "openai":
		return os.Getenv("OPENAI_API_KEY")
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
	case "groq":
		return os.Getenv("GROQ_API_KEY")
	case "mistral":
		return os.Getenv("MISTRAL_API_KEY")
	default:
		return node.APIKey
	}
}
```

Add `"os"` to the import block if not already present.

- [ ] **Step 2: Replace direct `provider.APIKey` usages**

In `provider.go`, find all three places where `provider.APIKey` is used as the auth header value (lines ~206, ~366, ~459 based on grep). Replace each `provider.APIKey` with `resolveAPIKey(provider)`.

The three patterns to replace:
1. `"x-goog-api-key": provider.APIKey` → `"x-goog-api-key": resolveAPIKey(provider)`
2. `"Authorization": "Bearer " + provider.APIKey` → `"Authorization": "Bearer " + resolveAPIKey(provider)`
3. `"x-api-key": provider.APIKey` → `"x-api-key": resolveAPIKey(provider)`

- [ ] **Step 3: Add import for "os" if missing**

Check the import block at the top of `provider.go`. If `"os"` is not already imported, add it.

- [ ] **Step 4: Build and run existing tests**

```bash
cd /Users/levi/Desktop/agentmesh-new/backend && go build ./... && go test ./internal/engine/nodes/... -v 2>&1 | tail -20
```

Expected: all existing tests pass.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/nodes/provider.go
git commit -m "feat: fall back to env API keys when useOurKey=true on provider nodes"
```

---

## Task 4: Backend — email "use our key" fallback

**Files:**
- Modify: `backend/internal/engine/nodes/action.go`

- [ ] **Step 1: Read action.go to find where EmailAPIKey is used**

```bash
grep -n "EmailAPIKey\|emailApiKey\|resend\|Resend" /Users/levi/Desktop/agentmesh-new/backend/internal/engine/nodes/action.go | head -20
```

- [ ] **Step 2: Add email key resolver**

In `action.go`, add this helper after the imports:

```go
func resolveEmailKey(node models.WorkflowNode) string {
	if node.UseOurEmail || node.EmailAPIKey == "" {
		return os.Getenv("PLATFORM_RESEND_KEY")
	}
	return node.EmailAPIKey
}
```

- [ ] **Step 3: Replace `node.EmailAPIKey` usages in email sending**

Find where `node.EmailAPIKey` is used as the auth/API key for sending email. Replace each instance with `resolveEmailKey(node)`.

- [ ] **Step 4: Build**

```bash
cd /Users/levi/Desktop/agentmesh-new/backend && go build ./... 2>&1
```

Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/nodes/action.go
git commit -m "feat: fall back to PLATFORM_RESEND_KEY when useOurEmail=true"
```

---

## Task 5: Backend — platform wallet for agent nodes

**Files:**
- Modify: `backend/internal/engine/runner.go`

When `SelfFundWallet=false`, the agent should use the platform's pre-funded Algorand wallet to sign x402 payments, so users don't need to fund anything.

- [ ] **Step 1: Read runner.go deploy section**

```bash
grep -n "AgentWallet\|Mnemonic\|mnemonic\|wallet\|SelfFund\|GenerateKey\|keypair" /Users/levi/Desktop/agentmesh-new/backend/internal/engine/runner.go | head -30
```

- [ ] **Step 2: Add platform wallet loader**

Near the top of `runner.go` (after imports), add:

```go
// platformWallet returns an AgentWallet backed by the PLATFORM_ALGO_MNEMONIC env var.
// Returns an empty wallet (no-op signing) if the env var is not set.
func platformWallet() models.AgentWallet {
	mnemonic := os.Getenv("PLATFORM_ALGO_MNEMONIC")
	if mnemonic == "" {
		return models.AgentWallet{}
	}
	key, err := mnemonic2.ToPrivateKey(mnemonic)
	if err != nil {
		return models.AgentWallet{}
	}
	addr := crypto.GenerateAddressFromSK(key)
	return models.AgentWallet{
		Address:    addr.String(),
		PrivateKey: key,
	}
}
```

Check existing imports in `runner.go` — add the algorand-sdk packages if missing:
```go
crypto "github.com/algorand/go-algorand-sdk/v2/crypto"
mnemonic2 "github.com/algorand/go-algorand-sdk/v2/mnemonic"
```

- [ ] **Step 3: Use platform wallet when SelfFundWallet=false**

Find the section in runner.go where `AgentWallet` is constructed per agent node (look for where `node.Wallet` or per-agent wallet loading happens). Add a conditional:

```go
// When the user opted for platform-funded wallet, use the shared platform account.
if !agentNode.SelfFundWallet {
    aw = platformWallet()
} else {
    // existing per-agent wallet loading code stays here
}
```

The exact integration depends on the runner structure — read the surrounding code and slot it in so that `aw` (the `AgentWallet` used for signing) is the platform wallet when `SelfFundWallet=false`.

- [ ] **Step 4: Build**

```bash
cd /Users/levi/Desktop/agentmesh-new/backend && go build ./... 2>&1
```

Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/runner.go
git commit -m "feat: use platform Algorand wallet for agents with selfFundWallet=false"
```

---

## Task 6: Backend — enforce MaxCostPerRun on agent nodes

**Files:**
- Modify: `backend/internal/engine/runner.go`

- [ ] **Step 1: Find where x402 costs are accumulated**

```bash
grep -n "spent\|Spent\|cost\|Cost\|price\|Price" /Users/levi/Desktop/agentmesh-new/backend/internal/engine/runner.go | head -30
```

- [ ] **Step 2: Add cost accumulation and limit check**

Find the section in runner.go where `ExecuteTool402` is called per tool invocation. Wrap it with a cost check:

```go
// Check MaxCostPerRun before each x402 call.
if agentNode.MaxCostPerRun != "" {
    maxCost, _ := strconv.ParseFloat(agentNode.MaxCostPerRun, 64)
    currentSpent, _ := strconv.ParseFloat(agentNode.Spent, 64) // or from accumulated run cost
    toolCost, _ := strconv.ParseFloat(toolNode.Price, 64)
    if maxCost > 0 && (currentSpent+toolCost) > maxCost {
        return nil, fmt.Errorf("agent %s: max cost per run (%.4f ALGO) would be exceeded", agentNode.ID, maxCost)
    }
}
result, err := ExecuteTool402(ctx, toolNode, aw, signer)
```

Add `"strconv"` to imports if missing.

- [ ] **Step 3: Build**

```bash
cd /Users/levi/Desktop/agentmesh-new/backend && go build ./... 2>&1
```

- [ ] **Step 4: Commit**

```bash
git add backend/internal/engine/runner.go
git commit -m "feat: enforce MaxCostPerRun limit before each x402 tool call"
```

---

## Task 7: Backend — webhook URL generation on deploy

**Files:**
- Modify: `backend/internal/api/handlers/workflows.go`

When a workflow with a webhook trigger is deployed, generate a stable URL path and store it on the trigger node.

- [ ] **Step 1: Find the deploy handler**

```bash
grep -n "Deploy\|deploy\|webhook\|Webhook" /Users/levi/Desktop/agentmesh-new/backend/internal/api/handlers/workflows.go | head -20
```

- [ ] **Step 2: Add webhook URL generation in deploy handler**

In the deploy handler, after the existing deploy logic, add:

```go
// Generate webhook URLs for webhook-trigger nodes that don't have one yet.
for i, node := range wf.Nodes {
    if node.Type == models.NodeTypeTrigger && node.Template == "webhook" && node.WebhookLiveURL == "" {
        wf.Nodes[i].WebhookLiveURL = "/hooks/" + wf.ID + "/" + node.ID
        wf.Nodes[i].WebhookMethod = coalesce(node.WebhookMethod, "POST")
    }
}
```

Add a small helper at the bottom of the file:

```go
func coalesce(vals ...string) string {
    for _, v := range vals {
        if v != "" {
            return v
        }
    }
    return ""
}
```

Then save the updated nodes back to the database (use the existing pattern in the same handler for updating the workflow).

- [ ] **Step 3: Build**

```bash
cd /Users/levi/Desktop/agentmesh-new/backend && go build ./... 2>&1
```

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/handlers/workflows.go
git commit -m "feat: generate WebhookLiveURL on deploy for webhook-trigger workflows"
```

---

## Task 8: Frontend Inspector — wire agent limits + self-fund toggle

**Files:**
- Modify: `frontend/src/components/canvas/Inspector.tsx` (lines 116–214, `AgentInspector`)

Currently the limits fields (`Max spend / run`, `Timeout`) use `defaultValue` (uncontrolled). Wire them properly and add the self-fund toggle.

- [ ] **Step 1: Replace stub limits with controlled inputs in `AgentInspector`**

Find the `Section label="Limits"` block in `AgentInspector` (around line 206). Replace it with:

```tsx
<Section label="Limits">
  <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
    <Field label="Max cost / run" hint="ALGO">
      <input style={monoInputStyle}
        value={node.maxCostPerRun ?? ""}
        placeholder="0.50"
        onChange={(e) => onUpdate({ ...node, maxCostPerRun: e.target.value })} />
    </Field>
    <Field label="Max total spend" hint="ALGO">
      <input style={monoInputStyle}
        value={node.maxSpendTotal ?? ""}
        placeholder="10.00"
        onChange={(e) => onUpdate({ ...node, maxSpendTotal: e.target.value })} />
    </Field>
  </div>
</Section>
```

- [ ] **Step 2: Add self-fund toggle to `AgentInspector` Wallet section**

Find the `Section label="Wallet"` block (around line 153). Before the existing deployed/not-deployed conditional, add a toggle at the top of the section:

```tsx
<Section label="Wallet">
  {/* Self-fund toggle */}
  <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "10px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)" }}>
    <div>
      <div style={{ fontSize: 12, fontWeight: 500, color: "var(--fg)" }}>Fund wallet yourself</div>
      <div style={{ fontSize: 11, color: "var(--fg-dim)", marginTop: 2 }}>
        {node.selfFundWallet ? "You will fund this agent's Algorand wallet" : "Platform pays for x402 calls automatically"}
      </div>
    </div>
    <Toggle
      value={node.selfFundWallet ?? false}
      onChange={(v) => onUpdate({ ...node, selfFundWallet: v })}
    />
  </div>

  {/* Only show wallet address/balance when user is self-funding */}
  {node.selfFundWallet && (
    <>
      {/* existing deployed wallet JSX stays here unchanged */}
    </>
  )}
  {!node.selfFundWallet && (
    <div style={{ padding: "10px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.55 }}>
      <span style={{ color: "var(--accent)", fontFamily: "var(--font-mono)", fontSize: 10 }}>● platform wallet active</span>
      <div style={{ marginTop: 4 }}>x402 payments draw from the platform's pre-funded Algorand account. No setup required.</div>
    </div>
  )}
</Section>
```

- [ ] **Step 3: Add `Toggle` component**

Add this small component at the bottom of `Inspector.tsx`, before the style constants:

```tsx
function Toggle({ value, onChange }: { value: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      onClick={() => onChange(!value)}
      style={{
        width: 40, height: 22, borderRadius: 999, flexShrink: 0,
        background: value ? "var(--accent)" : "var(--bg-elev-3)",
        border: `1px solid ${value ? "var(--accent)" : "var(--border-strong)"}`,
        cursor: "pointer", padding: 2, position: "relative", transition: "background 0.2s",
      }}
    >
      <div style={{
        width: 16, height: 16, borderRadius: 999,
        background: value ? "var(--accent-fg)" : "var(--fg-dim)",
        position: "absolute", top: 2,
        left: value ? "calc(100% - 18px)" : 2,
        transition: "left 0.2s",
      }} />
    </button>
  );
}
```

- [ ] **Step 4: TypeScript check**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -20
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/canvas/Inspector.tsx
git commit -m "feat: wire agent limits, add self-fund wallet toggle in agent inspector"
```

---

## Task 9: Frontend Inspector — provider "use our key" toggle

**Files:**
- Modify: `frontend/src/components/canvas/Inspector.tsx` (`ProviderInspector`, lines 217–291)

- [ ] **Step 1: Add "use our key" toggle to `ProviderInspector`**

Find `Section label="Credentials"` in `ProviderInspector` (around line 272). Replace it:

```tsx
<Section label="Credentials">
  <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "10px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)" }}>
    <div>
      <div style={{ fontSize: 12, fontWeight: 500, color: "var(--fg)" }}>Use your own API key</div>
      <div style={{ fontSize: 11, color: "var(--fg-dim)", marginTop: 2 }}>
        {!node.useOurKey ? "Your key — encrypted and stored with the workflow" : "Platform key — included in est. cost, billed to your credits"}
      </div>
    </div>
    <Toggle
      value={!node.useOurKey}
      onChange={(v) => onUpdate({ ...node, useOurKey: !v })}
    />
  </div>
  {!node.useOurKey && (
    <Field label="API Key" hint="encrypted at rest">
      <input
        style={monoInputStyle}
        type="password"
        value={node.apiKey === "__enc__" ? "" : (node.apiKey ?? "")}
        placeholder={node.apiKey === "__enc__" ? "Key set — enter to replace" : "AIza···"}
        onChange={(e) => onUpdate({ ...node, apiKey: e.target.value || (node.apiKey === "__enc__" ? "__enc__" : "") })}
      />
    </Field>
  )}
</Section>
```

- [ ] **Step 2: TypeScript check**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -20
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/canvas/Inspector.tsx
git commit -m "feat: add use-our-key toggle to provider node inspector"
```

---

## Task 10: Frontend Inspector — email "use our key" toggle

**Files:**
- Modify: `frontend/src/components/canvas/Inspector.tsx` (`ActionInspector`, lines 463–524)

- [ ] **Step 1: Add toggle above the email config section**

Find `Section label="Email config"` in `ActionInspector` (line ~473). Before the entire email config section, add:

```tsx
{node.template === "email" && (
  <Section label="Email provider">
    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "10px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)" }}>
      <div>
        <div style={{ fontSize: 12, fontWeight: 500, color: "var(--fg)" }}>Use your own email key</div>
        <div style={{ fontSize: 11, color: "var(--fg-dim)", marginTop: 2 }}>
          {!node.useOurEmail ? "Your Resend/Postmark key" : "Platform Resend account — included in billing"}
        </div>
      </div>
      <Toggle
        value={!node.useOurEmail}
        onChange={(v) => onUpdate({ ...node, useOurEmail: !v })}
      />
    </div>
  </Section>
)}
```

Then on the existing `Section label="Email config"`, wrap the `Field label="API Key"` inside `{!node.useOurEmail && (...)}`so it hides when using platform email.

- [ ] **Step 2: TypeScript check**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -20
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/canvas/Inspector.tsx
git commit -m "feat: add use-our-email toggle to action node inspector"
```

---

## Task 11: Frontend Inspector — webhook live URL display

**Files:**
- Modify: `frontend/src/components/canvas/Inspector.tsx` (`TriggerInspector`, lines 448–460)

- [ ] **Step 1: Expand `TriggerInspector` for webhook nodes**

Replace the current `TriggerInspector` function with:

```tsx
function TriggerInspector({ node, onUpdate, deployed }: { node: WorkflowNode; deployed: boolean; onUpdate: (n: WorkflowNode) => void }) {
  const tpl = TRIGGER_TEMPLATES.find((t) => t.id === node.template);
  const [copied, setCopied] = useState(false);

  const copyURL = () => {
    const full = `${window.location.origin}${node.webhookLiveURL}`;
    navigator.clipboard.writeText(full).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    });
  };

  return (
    <Section label="Trigger">
      {node.custom
        ? <Field label="Label"><input style={inputStyle} value={node.label ?? ""} placeholder="When …" onChange={(e) => onUpdate({ ...node, label: e.target.value })} /></Field>
        : <Field label="Type"><input style={inputStyle} value={tpl?.name ?? ""} readOnly /></Field>}
      {node.template === "cron" && (
        <Field label="Cron"><input style={monoInputStyle} defaultValue="0 9 * * *" /></Field>
      )}
      {node.template === "chat" && (
        <Field label="Source"><input style={inputStyle} defaultValue="In-app chat widget" /></Field>
      )}
      {node.template === "webhook" && (
        <>
          <Field label="Method">
            <select style={monoInputStyle} value={node.webhookMethod ?? "POST"} onChange={(e) => onUpdate({ ...node, webhookMethod: e.target.value })}>
              <option value="GET">GET</option>
              <option value="POST">POST</option>
            </select>
          </Field>
          <Field label="Payload schema" hint="describe what you expect">
            <textarea
              style={{ ...inputStyle, height: "auto", padding: 10, resize: "vertical", lineHeight: 1.5 }}
              rows={3}
              value={node.webhookPayloadSchema ?? ""}
              placeholder={'{ "message": "string", "userId": "string" }'}
              onChange={(e) => onUpdate({ ...node, webhookPayloadSchema: e.target.value })}
            />
          </Field>
          {deployed && node.webhookLiveURL ? (
            <div style={{ padding: 12, background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)" }}>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginBottom: 8, textTransform: "uppercase", letterSpacing: "0.06em" }}>live endpoint</div>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--accent)", wordBreak: "break-all", lineHeight: 1.6 }}>
                {typeof window !== "undefined" ? window.location.origin : ""}{node.webhookLiveURL}
              </div>
              <button onClick={copyURL} style={{ ...iconBtnStyle, marginTop: 10, width: "100%", fontSize: 11, fontFamily: "var(--font-sans)", gap: 6 }}>
                {copied ? "✓ Copied" : "⎘ Copy URL"}
              </button>
              <div style={{ marginTop: 8, fontSize: 11, color: "var(--fg-dim)", lineHeight: 1.5 }}>
                {node.webhookMethod ?? "POST"} · expects{" "}
                <span style={{ fontFamily: "var(--font-mono)" }}>{node.webhookPayloadSchema ? "your schema above" : "any JSON body"}</span>
              </div>
            </div>
          ) : deployed ? (
            <div style={{ padding: 10, background: "var(--bg)", border: "1px dashed var(--border-strong)", borderRadius: "var(--r-2)", fontSize: 11, color: "var(--fg-dim)" }}>
              Re-deploy to generate a webhook URL.
            </div>
          ) : (
            <div style={{ padding: 10, background: "var(--bg)", border: "1px dashed var(--border-strong)", borderRadius: "var(--r-2)", fontSize: 11, color: "var(--fg-dim)" }}>
              Deploy to get a live webhook URL.
            </div>
          )}
        </>
      )}
    </Section>
  );
}
```

- [ ] **Step 2: Update `TriggerInspector` call site to pass `deployed` prop**

In the `Inspector` component (line ~42), change:
```tsx
{selected.type === "trigger"  && <TriggerInspector  node={selected} onUpdate={onUpdate} />}
```
to:
```tsx
{selected.type === "trigger"  && <TriggerInspector  node={selected} onUpdate={onUpdate} deployed={deployed} />}
```

- [ ] **Step 3: TypeScript check**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -20
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/canvas/Inspector.tsx
git commit -m "feat: add webhook method/schema config and live URL display in trigger inspector"
```

---

## Task 12: Frontend — update cost estimator for "use our key"

**Files:**
- Modify: `frontend/src/components/canvas/CanvasPage.tsx`

When `useOurKey=true`, the platform charges a markup on the LLM call. Model this as 1.3× the base cost (30% platform margin) so the estimate stays honest.

- [ ] **Step 1: Update `estimatedCostPerRun` useMemo**

Find the `estimatedCostPerRun` useMemo in `CanvasPage.tsx` (added in a previous session). Replace the provider cost line:

```typescript
// Before:
if (node.type === "provider" && node.model) total += LLM_COST[node.model] ?? 0.003;

// After:
if (node.type === "provider" && node.model) {
  const baseCost = LLM_COST[node.model] ?? 0.003;
  // 1.3× markup when using platform key (billed to user's credits)
  total += node.useOurKey ? baseCost * 1.3 : baseCost;
}
```

- [ ] **Step 2: TypeScript check**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -20
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/canvas/CanvasPage.tsx
git commit -m "feat: apply 1.3× markup to cost estimate when using platform LLM key"
```

---

## Task 13: Frontend — dashboard credits widget + run counts

**Files:**
- Modify: `frontend/src/components/workflows/WorkflowsPage.tsx`

- [ ] **Step 1: Read WorkflowsPage.tsx to understand current header structure**

Read `frontend/src/components/workflows/WorkflowsPage.tsx` lines 52–90 to see the topbar and header area.

- [ ] **Step 2: Add mock credits state**

Inside `WorkflowsPage`, add state for billing mode near the other state declarations:

```typescript
// Mock billing state — replace with real API call when billing backend is ready
const [billingMode] = useState<"prepaid" | "postpaid">("prepaid");
const [creditsLeft] = useState(8.42);       // prepaid: ALGO credits remaining
const [creditsUsed] = useState(1.58);       // postpaid: ALGO used this month
```

- [ ] **Step 3: Add credits card to the header area**

In the header `<div>` (the one with `display: "flex", alignItems: "flex-end", justifyContent: "space-between"`, around line 74), add a credits card next to the "New Workflow" button area. Inside the right-side `<div style={{ display: "flex", gap: 8 }}>`, before the existing buttons, add:

```tsx
{/* Credits widget */}
<div style={{ display: "flex", alignItems: "center", gap: 10, padding: "0 14px", background: "var(--bg-elev-2)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", height: 36 }}>
  <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
    <span style={{ fontFamily: "var(--font-mono)", fontSize: 9, color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.06em" }}>
      {billingMode === "prepaid" ? "credits left" : "used this month"}
    </span>
    <span style={{ fontFamily: "var(--font-sans)", fontSize: 13, fontWeight: 600, color: billingMode === "prepaid" ? "var(--accent)" : "#E879F9" }}>
      ${billingMode === "prepaid" ? creditsLeft.toFixed(2) : creditsUsed.toFixed(2)}
    </span>
  </div>
  <button onClick={() => router.push("/billing")} style={{ height: 24, padding: "0 10px", fontSize: 11, fontWeight: 500, background: "var(--accent-soft)", border: "1px solid var(--accent-line)", borderRadius: "var(--r-1)", color: "var(--accent)", cursor: "pointer", fontFamily: "var(--font-sans)" }}>
    {billingMode === "prepaid" ? "Top up" : "View bill"}
  </button>
</div>
```

- [ ] **Step 4: Show run count on workflow cards**

Find the workflow card rendering (look for where `wf.updated` is shown). Add the runs count next to it:

```tsx
{wf.runs != null && wf.runs > 0 && (
  <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)" }}>
    ⟳ {wf.runs.toLocaleString()} runs
  </span>
)}
```

- [ ] **Step 5: TypeScript check**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -20
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/workflows/WorkflowsPage.tsx
git commit -m "feat: add credits widget and run counts to workflows dashboard"
```

---

## Task 14: Frontend — marketplace workflow cloning (functional "Use template")

**Files:**
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/components/marketplace/MarketplacePage.tsx`

When a user clicks "Use template" on a marketplace workflow, it should create a real workflow in the backend and navigate to it.

- [ ] **Step 1: Add `createFromTemplate` to api.ts**

In `frontend/src/lib/api.ts`, in the `workflows` object, after the `stop` method, add:

```typescript
createFromTemplate: async (name: string, nodes: unknown[], edges: unknown[]): Promise<Workflow> => {
  if (BASE) {
    const res = await fetch(`${BASE}/workflows`, {
      method: "POST", credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, nodes, edges }),
    });
    return res.json();
  }
  await delay(400);
  return { id: `wf-${Date.now()}`, name, nodes: nodes as Workflow["nodes"], edges: edges as Workflow["edges"] };
},
```

- [ ] **Step 2: Update `MarketplacePage.tsx` to import `useRouter` and call the API**

At the top of `MarketplacePage.tsx`, the component already has `useRouter`. Add the workflows API import:

```typescript
import { workflows as workflowsApi } from "@/lib/api";
```

- [ ] **Step 3: Update `WorkflowCard` to accept an `onUse` that can navigate**

The `onUse` prop already exists. In `MarketplacePage`, change the `onUse` handler for `WorkflowCard` from a simple toast to an async function that calls the API:

Replace the inline arrow `() => showToast(...)` on each `WorkflowCard` with a handler that:

```tsx
const handleUseWorkflow = async (wf: MarketplaceWorkflow) => {
  try {
    // Convert preview nodes to minimal real nodes for canvas
    const nodes = wf.previewNodes.map((n, i) => ({
      id: `n${i + 1}`,
      type: n.type,
      x: 80 + i * 280,
      y: 220,
      label: n.label,
      name: n.label,
    }));
    const created = await workflowsApi.createFromTemplate(wf.name, nodes, []);
    showToast(`"${wf.name}" added to your workflows`);
    setTimeout(() => router.push(`/workflows/${created.id}`), 1200);
  } catch {
    showToast("Failed to clone workflow — are you signed in?");
  }
};
```

Then pass `onUse={() => handleUseWorkflow(wf)}` to each `WorkflowCard`.

- [ ] **Step 4: Add workflow pricing to marketplace cards**

In `MarketplaceWorkflow` type (`types.ts`), add optional `price?: string` field.

In `data.ts`, add `price` to the featured workflows:
- mwf-support: `price: "2.00"`
- mwf-market: `price: "1.50"`
- mwf-leads: `price: "3.00"`
- mwf-content: `price: "2.50"`

In `WorkflowCard`, show the price with a "Use template · $X" button:

```tsx
<button onClick={onUse} style={primaryBtnStyle}>
  Use template{wf.price ? ` · $${wf.price}` : ""}
</button>
```

- [ ] **Step 5: TypeScript check**

```bash
cd /Users/levi/Desktop/agentmesh-new/frontend && npx tsc --noEmit 2>&1 | head -20
```

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/api.ts frontend/src/components/marketplace/MarketplacePage.tsx frontend/src/lib/types.ts frontend/src/lib/data.ts
git commit -m "feat: marketplace 'Use template' clones workflow to user's account"
```

---

## Self-review

**Spec coverage check:**

| Requirement | Task |
|-------------|------|
| x402 endpoints from marketplace or custom (already exists — custom flow in Tool402Inspector is complete) | Existing |
| Provider "use ours / use yours" toggle | Tasks 3, 9 |
| Email "use ours / yours" toggle | Tasks 4, 10 |
| "Use ours" affects estimated cost | Task 12 |
| Agent "self-fund / platform-funded" wallet toggle | Tasks 5, 8 |
| Agent max cost per run + max spend limits | Tasks 6, 8 |
| Dashboard credits left (prepaid) / credits used (postpaid) | Task 13 |
| Webhook trigger → live API URL with method + payload schema | Tasks 7, 11 |
| Total workflow runs shown | Task 13 |
| Marketplace workflows are real (clone to account) | Task 14 |
| Marketplace workflow pricing | Task 14 |
| Backend env vars documented | Plan header |

**What backend env vars are needed (direct answer to user's question):**
- `PLATFORM_ALGO_MNEMONIC` — 25-word Algorand mnemonic for a pre-funded testnet wallet. The backend derives the address from this and signs x402 payments. You can create one with `algokit generate` or the Algorand faucet + `goal account export`.
- `PLATFORM_RESEND_KEY` — Resend API key for platform email sending
- `GEMINI_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GROQ_API_KEY`, `MISTRAL_API_KEY` — already in `.env.example`; ensure all 5 are set for full "use ours" coverage

**Placeholder scan:** None found. All code blocks are complete.

**Type consistency:**
- `useOurKey: boolean` defined Task 1, used Tasks 3, 9, 12 ✓
- `selfFundWallet: boolean` defined Task 1, used Tasks 5, 8 ✓
- `maxCostPerRun: string` defined Task 1, used Tasks 6, 8 ✓
- `webhookLiveURL: string` defined Task 1, stored Task 7, displayed Task 11 ✓
- `Toggle` component added Task 8, reused Tasks 9, 10 ✓
- `createFromTemplate` added to `api.ts` Task 14, called in `MarketplacePage` Task 14 ✓
- `MarketplaceWorkflow.price` added Task 14 ✓
