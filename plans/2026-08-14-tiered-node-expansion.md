# Tiered Node Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give AgentMesh deterministic node-to-node data plumbing, a set of compute nodes, and thirteen high-demand connectors plus a generic GraphQL client — without touching the x402 / Tendril payment path.

**Architecture:** Three layers, built bottom-up. (1) `RunContext` gains ordered outputs and a `{{ }}` reference resolver so a node can address a *specific* upstream node instead of "whatever the map iterator returned". (2) Eight pure-computation tool templates that call nothing external. (3) Thirteen connectors plus a generic GraphQL node, following the existing `connectors_*.go` → `action.go` switch → `data.ts` → `Inspector.tsx` pattern. Control flow (`If`/`Switch`/`Filter`) is specified but deliberately deferred to Phase 5.

**Tech Stack:** Go 1.25 (backend, `go test`), Next.js/TypeScript (frontend, `pnpm`), PostgreSQL via `pgx/v5`.

**Dependencies.** Tasks 1-19 and 22-23 add **no new Go dependency** — they are standard library only, and the Postgres connector uses the `github.com/jackc/pgx/v5` already in `go.mod:12`. Exactly two new dependencies are introduced, both in Phase 2b:

| Dependency | Task | Why |
|---|---|---|
| `github.com/PuerkitoBio/goquery` | 20 | CSS-selector HTML extraction. No usable stdlib equivalent; this is the de-facto Go choice and what n8n's HTML node does with cheerio. Pulls in `golang.org/x/net`. |
| `github.com/yuin/goldmark` | 21 | CommonMark-compliant Markdown rendering. Itself dependency-free and actively maintained (unlike blackfriday). |

Do not add any dependency beyond these two. If a task appears to need a third, stop and ask.

---

## Global Constraints

These apply to **every** task. Read before starting any of them.

### C1. The x402 / Tendril payment structure is FROZEN

The wallet topology must not change. Stated precisely, as it exists today:

- **Inbound leg:** Wallet 1 (`PLATFORM_SPEND_WALLET`, passed as `X402RelayConfig.PlatformSpendEncMnemonic`) signs a USDC group payment to Wallet 2 (`PLATFORM_WALLET`, `X402RelayConfig.PlatformWalletAddress`), settled through the GoPlausible facilitator. Entry point: `executeTool402V2Relay` → `usdcSigner.SignUSDCPaymentGroup(...)` at `backend/internal/engine/nodes/tool402.go:1409`.
- **Outbound leg:** Wallet 2 pays the real target/provider's `payTo`, via `nodes.PayTargetFromWallet2` called from `backend/internal/api/handlers/x402relay.go:883`.
- **Markup leg:** a second, real Wallet 1 → Wallet 2 settlement for the platform's flat fee (`SettlePlatformFee`, `tool402.go:1467`).

**Do not modify these files.** They are frozen for the duration of this plan:

```
backend/internal/engine/nodes/tool402.go
backend/internal/engine/nodes/runfund.go
backend/internal/engine/nodes/walletpay.go
backend/internal/engine/nodes/platformfee.go
backend/internal/engine/nodes/tendril.go
backend/internal/engine/nodes/billing.go
backend/internal/engine/nodes/tier.go
backend/internal/api/handlers/x402relay.go
backend/internal/x402/facilitator.go
```

Also frozen: the `models.NodeTypeTool402` and `models.NodeTypeTendril` branches of the `switch node.Type` in `backend/internal/engine/runner.go:703`, and every `TOOL402_TEMPLATES` / `TENDRIL_TEMPLATES` entry in `frontend/src/lib/data.ts`.

Task 1 installs an automated tripwire for this. If a task appears to require editing a frozen file, **stop and ask** rather than editing.

### C2. `RunContext.Message()` must stay behaviour-compatible

Two frozen files consume it, and it decides what bytes get paid for:

- `tool402.go:1085` — `payBody = []byte(rc.Message())` is the **request body sent to a paid x402 endpoint**.
- `tendril.go:700` — `payload := strings.TrimSpace(rc.Message())` is the **Python source sent to Tendril's metered `/x402/run`**.

Task 2 changes `Message()` from non-deterministic to deterministic. That is a strict bug fix — for the single-upstream case (which is every x402/Tendril node in practice) the returned value is byte-identical. But:

- The method signature `Message() string` must not change.
- The `RunContexter` interface in `backend/internal/engine/nodes/stubs.go` must not have methods **removed**; adding is fine.
- Task 2 ships a regression test proving the x402/Tendril body-selection is unchanged.

### C2b. Billing needs no change — do not edit `billing.go`

`BillableFlatFee` (`billing.go:25-34`) dispatches on **node type**, not template:

```go
case models.NodeTypeAgent, models.NodeTypeAction:  return true
case models.NodeTypeTool:                          return template == "http"
default:                                           return false
```

Consequences, both correct and intentional:

- Every Phase 3 connector is a `NodeTypeAction`, so it is **automatically charged the flat action fee** the moment its `case` is added. No billing wiring is needed, and none should be added.
- Every Phase 2 compute node is a `NodeTypeTool` whose template is not `http`, so it is **free to run**. That is right — they make no network call.

`billing.go` is on the frozen list (C1). If a task seems to need a billing change, that is a signal the node was given the wrong type, not that the freeze should be lifted.

### C3. SSRF guard is mandatory

Every new outbound HTTP call goes through `urlValidator` — in practice by using the existing `doValidatedRequest` / `postJSON` helpers in `connector_helpers.go`, never a bare `http.Client.Do`.

### C4. Verification gates

Before every commit:

```bash
cd backend && go build ./... && go vet ./... && go test ./...
```

For any task touching `frontend/`:

```bash
cd frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
```

### C5. Git rules (from `~/.claude/CLAUDE.md`)

- **Never** add Claude/Anthropic as co-author. No `Co-Authored-By:` trailer on any commit.
- **Never** push to a remote or open a PR without explicit per-action approval. A "go ahead" earlier in a session does not carry forward.

### C6. Scope discipline

Thirteen connectors, not seventy. The lists in Phase 3 and Phase 3b are closed — do not add "while I'm here" connectors. Anything else goes in a follow-up plan.

The full closed set: **Postgres, Stripe, Twilio, Shopify, Pipedrive, Zendesk, Monday.com, PagerDuty, Mattermost, RSS** (Phase 3) and **HackerNews, CoinGecko** (Phase 3b), plus the generic **GraphQL** node and the **QuickChart** URL builder.

---

## File Structure

**Created:**

| File | Responsibility |
|---|---|
| `backend/internal/engine/x402_freeze_test.go` | Tripwire: content hashes of the frozen payment files |
| `backend/internal/engine/nodes/resolve.go` | `{{ }}` reference resolver, shared by compute nodes and connectors |
| `backend/internal/engine/nodes/resolve_test.go` | Resolver tests |
| `backend/internal/engine/nodes/compute.go` | Pure-computation templates: `set`, `json_extract`, `crypto`, `datetime`, `xml`, `template` (Phase 2), plus `html_extract`, `markdown` (Phase 2b) and `quickchart` (Task 23) |
| `backend/internal/engine/nodes/compute_test.go` | Phase 2 tests |
| `backend/internal/engine/nodes/connectors_commerce.go` | Stripe, Shopify, Pipedrive |
| `backend/internal/engine/nodes/connectors_commerce_test.go` | — |
| `backend/internal/engine/nodes/connectors_ops.go` | PagerDuty, Zendesk, Monday.com, Mattermost, Twilio |
| `backend/internal/engine/nodes/connectors_ops_test.go` | — |
| `backend/internal/engine/nodes/connectors_db.go` | Postgres insert (revives the dead `db` template) |
| `backend/internal/engine/nodes/connectors_db_test.go` | — |
| `backend/internal/engine/nodes/connectors_feed.go` | RSS feed read, HackerNews, CoinGecko |
| `backend/internal/engine/nodes/connectors_feed_test.go` | — |
| `backend/internal/engine/nodes/connectors_graphql.go` | Generic GraphQL client |
| `backend/internal/engine/nodes/connectors_graphql_test.go` | — |

**Modified:**

| File | Change |
|---|---|
| `backend/internal/engine/context.go` | Ordered outputs; deterministic `Message()`; `OutputOrder()` |
| `backend/internal/engine/nodes/stubs.go` | Add `OutputOrder() []string` to `RunContexter` |
| `backend/internal/engine/nodes/tool.go:117` | Register Phase 2 templates in `ExecuteTool` |
| `backend/internal/engine/nodes/action.go:17` | Register Phase 3 templates in `ExecuteAction`; `replaceVar` → `resolveTemplate` |
| `backend/internal/engine/nodes/connector_helpers.go` | Add `getAndDecode` (read-capable counterpart to `doAndCheck`) |
| `frontend/src/lib/data.ts` | `TOOL_TEMPLATES` + `ACTION_TEMPLATES` entries |
| `frontend/src/components/canvas/Inspector.tsx` | `CONNECTOR_CONFIG` field schemas |

**Frozen — do not open for editing:** everything in C1.

---

# Phase 0 — Freeze the payment path

## Task 1: x402 freeze tripwire

**Files:**
- Create: `backend/internal/engine/x402_freeze_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: a failing `go test ./internal/engine/...` the moment any frozen file's bytes change. No exported symbols.

**Why first:** every later phase edits files adjacent to the payment path. This makes an accidental edit loud and immediate rather than something discovered after real USDC moves.

- [ ] **Step 1: Write the guard test**

Create `backend/internal/engine/x402_freeze_test.go`:

```go
package engine_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// frozenX402Files pins the byte content of the x402 / Tendril payment path.
//
// The wallet topology these files implement is:
//   Wallet 1 (PLATFORM_SPEND_WALLET) --USDC--> Wallet 2 (PLATFORM_WALLET)
//       settled via the GoPlausible facilitator   [inbound leg]
//   Wallet 2 --USDC--> the target endpoint's payTo [outbound leg]
//   Wallet 1 --USDC--> Wallet 2, flat platform markup [markup leg]
//
// Real mainnet USDC moves through this. A change here is never incidental.
//
// If this test fails and you DID intend the change: re-run the hash command in
// the failure message, paste the new digest below, and get explicit sign-off in
// the PR description explaining why the payment path moved. If you did NOT
// intend it, revert the file.
var frozenX402Files = map[string]string{
	"nodes/tool402.go":                    "",
	"nodes/runfund.go":                    "",
	"nodes/walletpay.go":                  "",
	"nodes/platformfee.go":                "",
	"nodes/tendril.go":                    "",
	"nodes/billing.go":                    "",
	"nodes/tier.go":                       "",
	"../api/handlers/x402relay.go":        "",
	"../x402/facilitator.go":              "",
}

func TestX402PaymentPathIsFrozen(t *testing.T) {
	for rel, want := range frozenX402Files {
		b, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("frozen file %s is unreadable — was it moved or deleted? %v", rel, err)
		}
		sum := sha256.Sum256(b)
		got := hex.EncodeToString(sum[:])
		if want == "" {
			t.Fatalf("no baseline digest recorded for %s.\n"+
				"Record it with:\n  shasum -a 256 backend/internal/engine/%s\n"+
				"then paste it into frozenX402Files.", rel, rel)
		}
		if got != want {
			t.Errorf("FROZEN FILE CHANGED: %s\n  want %s\n  got  %s\n\n"+
				"This file implements the Wallet 1 -> Wallet 2 -> provider payment path.\n"+
				"If this change is intentional, update the digest AND justify it in the PR.\n"+
				"If not, revert it.", rel, want, got)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails (empty baselines)**

```bash
cd backend && go test ./internal/engine/ -run TestX402PaymentPathIsFrozen -v
```

Expected: FAIL, nine times, each with "no baseline digest recorded".

- [ ] **Step 3: Record the real digests**

```bash
cd backend/internal/engine
for f in nodes/tool402.go nodes/runfund.go nodes/walletpay.go nodes/platformfee.go \
         nodes/tendril.go nodes/billing.go nodes/tier.go \
         ../api/handlers/x402relay.go ../x402/facilitator.go; do
  printf '\t"%s": "%s",\n' "$f" "$(shasum -a 256 "$f" | cut -d' ' -f1)"
done
```

Paste the emitted lines over the placeholder map entries, keeping the same keys.

- [ ] **Step 4: Run it to verify it passes**

```bash
cd backend && go test ./internal/engine/ -run TestX402PaymentPathIsFrozen -v
```

Expected: PASS.

- [ ] **Step 5: Prove the tripwire actually trips**

```bash
cd backend && printf '\n// tripwire check\n' >> internal/engine/nodes/tendril.go
go test ./internal/engine/ -run TestX402PaymentPathIsFrozen 2>&1 | head -20
git checkout internal/engine/nodes/tendril.go
go test ./internal/engine/ -run TestX402PaymentPathIsFrozen
```

Expected: FAIL with "FROZEN FILE CHANGED: nodes/tendril.go", then PASS after the revert. **Confirm the `git checkout` restored the file** (`git status` must show it clean) before moving on.

- [ ] **Step 6: Full suite + commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
git add internal/engine/x402_freeze_test.go
git commit -m "test: pin the x402 wallet1->wallet2->provider payment path against accidental edits"
```

---

# Phase 1 — Tier 1a: deterministic, addressable node output

## Task 2: Make `Message()` deterministic

**Files:**
- Modify: `backend/internal/engine/context.go:8-14` (struct), `:26-30` (`Set`), `:60-71` (`Message`)
- Test: `backend/internal/engine/context_order_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `RunContext.Set` now records insertion order. `Message() string` — same signature, now returns the output of the **most recently `Set`** node instead of a random one. New method `OutputOrder() []string` returning node IDs in insertion order (used by Task 3).

**The bug:** `context.go:66` does `for _, v := range rc.outputs { last = v }` over a Go map. Map iteration order is randomized by the runtime, so with two or more prior outputs `Message()` returns an arbitrary one — different per run. The doc comment claiming "most recent" is false.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/engine/context_order_test.go`:

```go
package engine_test

import (
	"testing"

	"github.com/agentmesh/backend/internal/engine"
)

func TestMessageReturnsMostRecentOutputDeterministically(t *testing.T) {
	// Repeat: a single pass can pass by luck under randomized map iteration.
	for i := 0; i < 200; i++ {
		rc := engine.NewRunContext("r1", []byte(`"trigger input"`))
		rc.Set("n1", "first")
		rc.Set("n2", "second")
		rc.Set("n3", "third")
		if got := rc.Message(); got != "third" {
			t.Fatalf("iteration %d: want most recent output %q, got %q", i, "third", got)
		}
	}
}

func TestMessageFallsBackToTriggerInputWhenNoOutputs(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"trigger input"`))
	if got := rc.Message(); got != "trigger input" {
		t.Errorf("want trigger input, got %q", got)
	}
}

// Re-Setting an existing node must not duplicate it in the order, and must
// move it to the end — a node that re-emits is the newest output.
func TestReSetMovesNodeToMostRecent(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "a")
	rc.Set("n2", "b")
	rc.Set("n1", "c")
	if got := rc.Message(); got != "c" {
		t.Errorf("want %q after re-set, got %q", "c", got)
	}
	order := rc.OutputOrder()
	if len(order) != 2 {
		t.Fatalf("want 2 entries in order, got %d: %v", len(order), order)
	}
	if order[len(order)-1] != "n1" {
		t.Errorf("want n1 last after re-set, got %v", order)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/ -run 'TestMessage|TestReSet' -v
```

Expected: FAIL — `rc.OutputOrder undefined`, and `TestMessageReturnsMostRecentOutputDeterministically` failing on some iteration.

- [ ] **Step 3: Implement ordered outputs**

In `backend/internal/engine/context.go`, replace the struct, `Set`, and `Message`:

```go
type RunContext struct {
	mu      sync.RWMutex
	outputs map[string]any
	// order records node IDs in the sequence they were Set. Message() reads
	// the tail of this rather than ranging over `outputs` — Go randomizes map
	// iteration order, so the old "most recent" was actually "an arbitrary
	// one", non-deterministic across runs of the same workflow.
	order []string
	input any
	runID string
}

func (rc *RunContext) Set(nodeID string, value any) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	// A re-Set moves the node to the tail: it just produced the newest output.
	for i, id := range rc.order {
		if id == nodeID {
			rc.order = append(rc.order[:i], rc.order[i+1:]...)
			break
		}
	}
	rc.order = append(rc.order, nodeID)
	rc.outputs[nodeID] = value
}

// OutputOrder returns node IDs in the order their outputs were Set, oldest
// first. The returned slice is a copy — callers may retain it safely.
func (rc *RunContext) OutputOrder() []string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	out := make([]string, len(rc.order))
	copy(out, rc.order)
	return out
}

// Message returns the most recent node output as a string, falling back to the
// trigger input when nothing has run yet.
//
// LOAD-BEARING FOR PAYMENTS: this value is the request body sent to paid x402
// endpoints (nodes/tool402.go) and the Python source sent to Tendril's metered
// /x402/run (nodes/tendril.go). Changing which output it selects changes what
// real money is spent on. Do not alter the selection rule.
func (rc *RunContext) Message() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	if len(rc.order) == 0 {
		return anyToString(rc.input)
	}
	return anyToString(rc.outputs[rc.order[len(rc.order)-1]])
}
```

Leave `NewRunContext`, `Get`, `UserInput`, `ToolOutputs`, and `anyToString` exactly as they are — `order` zero-values to a usable nil slice.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/ -run 'TestMessage|TestReSet' -v
```

Expected: PASS, all three.

- [ ] **Step 5: Prove the x402 / Tendril body selection is unchanged**

Add to `backend/internal/engine/context_order_test.go`:

```go
// The single-upstream shape every x402 and Tendril node actually runs in:
// one producer feeding the payment node. Message() must return that producer's
// output byte-for-byte, because it becomes the paid request body
// (nodes/tool402.go: payBody) / the metered Python source (nodes/tendril.go).
func TestMessageIsUnchangedForSingleUpstreamPaymentNodes(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"ignored trigger"`))
	rc.Set("agent1", "print(6*7)")
	if got := rc.Message(); got != "print(6*7)" {
		t.Errorf("x402/Tendril payload changed: want %q, got %q", "print(6*7)", got)
	}
}

// Structured (non-string) outputs still flatten via anyToString's json.Marshal
// fallback, same as before — tool402 sends this as the request body.
func TestMessageFlattensStructuredOutputAsBefore(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", map[string]any{"query": "weather in Kolkata"})
	if got := rc.Message(); got != `{"query":"weather in Kolkata"}` {
		t.Errorf("structured flattening changed: got %q", got)
	}
}
```

Run:

```bash
cd backend && go test ./internal/engine/ -run TestMessage -v
```

Expected: PASS.

- [ ] **Step 6: Full suite, including the freeze guard**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
```

Expected: PASS, including `TestX402PaymentPathIsFrozen` (this task touched no frozen file) and every existing `tool402`/`tendril`/`walletpay` test.

- [ ] **Step 7: Commit**

```bash
git add internal/engine/context.go internal/engine/context_order_test.go
git commit -m "fix(engine): make RunContext.Message deterministic

Message() ranged over a Go map to find the 'most recent' output. Map
iteration order is randomized, so with 2+ upstream nodes it returned an
arbitrary one, differing between runs of the same workflow. Track
insertion order explicitly and read the tail.

Single-upstream behaviour — the shape every x402/Tendril payment node
runs in — is byte-identical, covered by regression tests."
```

---

## Task 3: Expose ordered output to node implementations

**Files:**
- Modify: `backend/internal/engine/nodes/stubs.go:4-11`
- Test: `backend/internal/engine/nodes/resolve_test.go` (created in Task 4; this task only widens the interface)

**Interfaces:**
- Consumes: `RunContext.OutputOrder() []string` from Task 2.
- Produces: `RunContexter` gains `OutputOrder() []string`. Task 4's resolver depends on it.

- [ ] **Step 1: Widen the interface**

In `backend/internal/engine/nodes/stubs.go`, add one method — **add only, remove nothing** (C2):

```go
// RunContexter interface satisfied by engine.RunContext via duck typing.
// We use a local interface to avoid circular import engine → nodes → engine.
type RunContexter interface {
	Message() string
	UserInput() string
	ToolOutputs() map[string]any
	Set(string, any)
	Get(string) (any, bool)
	// OutputOrder returns node IDs oldest-first. Used by resolveTemplate to
	// resolve {{ node.<id> }} references deterministically.
	OutputOrder() []string
}
```

- [ ] **Step 2: Build to find every implementation that now breaks**

```bash
cd backend && go build ./... && go vet ./... && go test ./... 2>&1 | grep -E "does not implement|missing method" | sort -u
```

Expected: real `*engine.RunContext` satisfies it already (Task 2 added the method). Any **test fake** implementing `RunContexter` will fail here.

- [ ] **Step 3: Add the method to every fake the build named**

For each fake reported in Step 2, add:

```go
func (f *fakeRunContext) OutputOrder() []string { return nil }
```

Returning nil is correct for fakes: it means "no addressable upstream outputs", and `resolveTemplate` degrades to `{{ result }}`/`{{ input }}` only. If Step 2 reported no fakes, skip this step.

- [ ] **Step 4: Run the full suite**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A internal/engine
git commit -m "refactor(nodes): expose ordered node output through RunContexter"
```

---

## Task 4: The `{{ }}` reference resolver

**Files:**
- Create: `backend/internal/engine/nodes/resolve.go`, `backend/internal/engine/nodes/resolve_test.go`
- Modify: `backend/internal/engine/nodes/action.go:144-146` (`replaceVar`) and its call site at `:126`

**Interfaces:**
- Consumes: `RunContexter.Get`, `.Message`, `.UserInput`, `.OutputOrder`.
- Produces: `func resolveTemplate(s string, rc RunContexter) string` — package-internal, used by Phase 2 and Phase 3.

Today's `replaceVar` (`action.go:144`) is a literal `strings.ReplaceAll` bound to exactly one key, `result`, used only for the email body. This generalises it without breaking that.

Supported syntax:

| Reference | Resolves to |
|---|---|
| `{{ result }}` | `rc.Message()` — unchanged meaning, backwards compatible |
| `{{ input }}` | `rc.UserInput()` — the original trigger input |
| `{{ node.<id> }}` | that node's output, stringified |
| `{{ node.<id>.<field> }} ` | `<field>` of that node's output when it is a JSON object |

An unresolvable reference is left **verbatim** rather than replaced with empty string — a silently blank Slack message is worse than a visible `{{ node.n7 }}`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/engine/nodes/resolve_test.go`:

```go
package nodes_test

import (
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestResolveTemplate(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"original question"`))
	rc.Set("n1", "agent answer")
	rc.Set("n2", map[string]any{"city": "Kolkata", "temp": 31.5})

	cases := []struct{ name, in, want string }{
		{"result", "Answer: {{ result }}", `Answer: {"city":"Kolkata","temp":31.5}`},
		{"no spaces", "{{result}}", `{"city":"Kolkata","temp":31.5}`},
		{"input", "Q: {{ input }}", "Q: original question"},
		{"node by id", "got {{ node.n1 }}", "got agent answer"},
		{"node field", "in {{ node.n2.city }}", "in Kolkata"},
		{"numeric field", "temp {{ node.n2.temp }}", "temp 31.5"},
		{"two refs", "{{ node.n1 }} / {{ input }}", "agent answer / original question"},
		{"unknown node left verbatim", "x {{ node.nope }} y", "x {{ node.nope }} y"},
		{"unknown field left verbatim", "x {{ node.n2.nope }} y", "x {{ node.n2.nope }} y"},
		{"field on a string output left verbatim", "x {{ node.n1.city }} y", "x {{ node.n1.city }} y"},
		{"unknown keyword left verbatim", "x {{ bogus }} y", "x {{ bogus }} y"},
		{"no refs untouched", "plain text", "plain text"},
		{"empty string", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nodes.ResolveTemplateForTest(tc.in, rc); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// The email body's {{ result }} contract predates this resolver and must keep
// working exactly as before.
func TestEmailBodyStillResolvesResult(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "the answer")
	node := models.WorkflowNode{
		ID: "e1", Type: models.NodeTypeAction, Template: "email",
		EmailBody: "Result was: {{ result }}",
	}
	// No API key -> skipped before any network call; we are asserting the
	// template contract compiles and the skip sentinel is unchanged.
	got, err := nodes.ExecuteAction(t.Context(), node, rc)
	if got != "email_skipped_no_api_key" {
		t.Errorf("want skip sentinel, got %v (err %v)", got, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run 'TestResolveTemplate|TestEmailBody' -v
```

Expected: FAIL — `undefined: nodes.ResolveTemplateForTest`.

- [ ] **Step 3: Implement the resolver**

Create `backend/internal/engine/nodes/resolve.go`:

```go
package nodes

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// templateRef matches {{ ref }} with optional surrounding whitespace. The ref
// charset deliberately excludes braces and spaces so an unclosed or malformed
// reference simply fails to match and is left in the output verbatim.
var templateRef = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.\-]+)\s*\}\}`)

// resolveTemplate expands {{ }} references in s against the run context:
//
//	{{ result }}            the most recent node output (rc.Message)
//	{{ input }}             the original trigger input (rc.UserInput)
//	{{ node.<id> }}         that node's output, stringified
//	{{ node.<id>.<field> }} one field of that node's object output
//
// An unresolvable reference is left verbatim. Blanking it would produce a
// silently empty Slack message or email body, which is harder to diagnose than
// a literal "{{ node.n7 }}" showing up in the output.
func resolveTemplate(s string, rc RunContexter) string {
	if s == "" || !strings.Contains(s, "{{") {
		return s
	}
	return templateRef.ReplaceAllStringFunc(s, func(match string) string {
		m := templateRef.FindStringSubmatch(match)
		if len(m) != 2 {
			return match
		}
		val, ok := lookupRef(m[1], rc)
		if !ok {
			return match
		}
		return val
	})
}

func lookupRef(ref string, rc RunContexter) (string, bool) {
	switch ref {
	case "result":
		return rc.Message(), true
	case "input":
		return rc.UserInput(), true
	}
	rest, isNode := strings.CutPrefix(ref, "node.")
	if !isNode || rest == "" {
		return "", false
	}
	nodeID, field, hasField := strings.Cut(rest, ".")
	out, ok := rc.Get(nodeID)
	if !ok {
		return "", false
	}
	if !hasField {
		return stringifyRef(out), true
	}
	obj, ok := out.(map[string]any)
	if !ok {
		return "", false
	}
	fieldVal, ok := obj[field]
	if !ok {
		return "", false
	}
	return stringifyRef(fieldVal), true
}

// stringifyRef renders a resolved value for interpolation into text. Strings
// pass through unquoted; numbers render without json.Marshal's float
// formatting surprises; everything else falls back to compact JSON.
func stringifyRef(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
```

- [ ] **Step 4: Add the test-only export**

Create `backend/internal/engine/nodes/resolve_export_test.go`:

```go
package nodes

// ResolveTemplateForTest exposes the unexported resolver to the external
// nodes_test package. Test-only; no production caller.
func ResolveTemplateForTest(s string, rc RunContexter) string {
	return resolveTemplate(s, rc)
}
```

- [ ] **Step 5: Switch the email body to the resolver**

In `backend/internal/engine/nodes/action.go`, replace line 126:

```go
		bodyText = replaceVar(bodyText, "result", agentOutput)
```

with:

```go
		bodyText = resolveTemplate(bodyText, rc)
```

Then delete the now-unused `replaceVar` function at `action.go:144-146`. The local variable `agentOutput` is still used two lines above for the default body — leave it.

- [ ] **Step 6: Run tests to verify they pass**

```bash
cd backend && go test ./internal/engine/nodes/ -run 'TestResolveTemplate|TestEmailBody' -v
cd backend && go test ./internal/engine/... 
```

Expected: PASS. In particular every existing email test must still pass — `{{ result }}` resolves identically.

- [ ] **Step 7: Commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
git add internal/engine/nodes/resolve.go internal/engine/nodes/resolve_test.go \
        internal/engine/nodes/resolve_export_test.go internal/engine/nodes/action.go
git commit -m "feat(nodes): add {{ }} reference resolver for addressing upstream nodes

Generalises the single-key replaceVar into {{ result }}, {{ input }},
{{ node.<id> }} and {{ node.<id>.<field> }}. Unresolvable references are
left verbatim rather than blanked."
```

---

# Phase 2 — Tier 2: pure-computation nodes

All six call nothing external and add no dependency. They are `NodeTypeTool` templates dispatched from `ExecuteTool` (`tool.go:117`), reading settings from `node.Config` via the existing `configVal` helper.

## Task 5: `set` — Edit Fields

**Files:**
- Create: `backend/internal/engine/nodes/compute.go`, `backend/internal/engine/nodes/compute_test.go`
- Modify: `backend/internal/engine/nodes/tool.go:117-128`
- Modify: `frontend/src/lib/data.ts` (`TOOL_TEMPLATES`)

**Interfaces:**
- Consumes: `resolveTemplate` (Task 4), `configVal` (`connector_helpers.go:28`).
- Produces: `func executeSet(node models.WorkflowNode, rc RunContexter) (any, error)`. Emits a `map[string]any`, so downstream `{{ node.<id>.<field> }}` works against it.

Config key: `setFields` — a JSON object whose **values are templates**.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/engine/nodes/compute_test.go`:

```go
package nodes_test

import (
	"context"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestSetToolBuildsObjectFromTemplates(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"what is the weather"`))
	rc.Set("n1", map[string]any{"city": "Kolkata", "temp": 31.5})

	node := models.WorkflowNode{
		ID: "s1", Type: models.NodeTypeTool, Template: "set",
		Config: map[string]string{
			"setFields": `{"place":"{{ node.n1.city }}","reading":"{{ node.n1.temp }}C","asked":"{{ input }}"}`,
		},
	}
	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want map output so downstream {{ node.s1.field }} works, got %T", out)
	}
	if got["place"] != "Kolkata" {
		t.Errorf("place: got %v", got["place"])
	}
	if got["reading"] != "31.5C" {
		t.Errorf("reading: got %v", got["reading"])
	}
	if got["asked"] != "what is the weather" {
		t.Errorf("asked: got %v", got["asked"])
	}
}

func TestSetToolErrorsOnInvalidJSON(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{
		ID: "s1", Type: models.NodeTypeTool, Template: "set",
		Config: map[string]string{"setFields": `{"a": }`},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for malformed setFields, got nil")
	}
}

func TestSetToolErrorsWhenUnconfigured(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{ID: "s1", Type: models.NodeTypeTool, Template: "set"}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error when setFields is unset, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestSetTool -v
```

Expected: FAIL — `ExecuteTool` hits `default:` and returns `rc.Message()`, a string, so the `map[string]any` assertion fails; the two error tests fail with "want an error, got nil".

- [ ] **Step 3: Implement**

Create `backend/internal/engine/nodes/compute.go`:

```go
package nodes

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentmesh/backend/internal/models"
)

// executeSet builds an object from the node's `setFields` JSON, expanding
// {{ }} references in every string value. It returns a map rather than a
// string so downstream nodes can address individual fields with
// {{ node.<id>.<field> }}.
func executeSet(node models.WorkflowNode, rc RunContexter) (any, error) {
	raw := configVal(node, "setFields", "")
	if raw == "" {
		return nil, errors.New("set: no fields configured — set `setFields` to a JSON object")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("set: `setFields` is not a valid JSON object: %w", err)
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if s, ok := v.(string); ok {
			out[k] = resolveTemplate(s, rc)
			continue
		}
		out[k] = v
	}
	return out, nil
}
```

- [ ] **Step 4: Register the template**

In `backend/internal/engine/nodes/tool.go`, add to the `ExecuteTool` switch (after the `http` case):

```go
	case "set":
		return executeSet(node, rc)
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestSetTool -v
```

Expected: PASS, all three.

- [ ] **Step 6: Add the palette entry**

In `frontend/src/lib/data.ts`, add to `TOOL_TEMPLATES`:

```ts
  { id: "set", name: "Edit Fields", desc: "Build an object from refs", icon: "≔" },
```

- [ ] **Step 7: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes/compute.go \
        backend/internal/engine/nodes/compute_test.go \
        backend/internal/engine/nodes/tool.go frontend/src/lib/data.ts
git commit -m "feat(nodes): add Edit Fields (set) compute node"
```

---

## Task 6: `json_extract` — pull a value out of upstream JSON

**Files:**
- Modify: `backend/internal/engine/nodes/compute.go`, `compute_test.go`, `tool.go`, `frontend/src/lib/data.ts`

**Interfaces:**
- Consumes: `configVal`, `RunContexter.Message`.
- Produces: `func executeJSONExtract(node models.WorkflowNode, rc RunContexter) (any, error)`.

Config key: `jsonPath` — a dot path, e.g. `data.items.0.name`. Numeric segments index arrays.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/engine/nodes/compute_test.go`:

```go
func TestJSONExtractPullsNestedValue(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", `{"data":{"items":[{"name":"first"},{"name":"second"}]},"ok":true}`)

	cases := []struct {
		name, path string
		want       any
	}{
		{"nested object", "data.items.0.name", "first"},
		{"array index", "data.items.1.name", "second"},
		{"bool at root", "ok", true},
		{"whole subtree", "data", map[string]any{"items": []any{
			map[string]any{"name": "first"}, map[string]any{"name": "second"},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := models.WorkflowNode{
				ID: "j1", Type: models.NodeTypeTool, Template: "json_extract",
				Config: map[string]string{"jsonPath": tc.path},
			}
			got, err := nodes.ExecuteTool(context.Background(), node, rc)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "whole subtree" {
				m, ok := got.(map[string]any)
				if !ok || len(m) != 1 {
					t.Fatalf("want the data subtree, got %#v", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestJSONExtractErrorsOnMissingPath(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", `{"a":1}`)
	node := models.WorkflowNode{
		ID: "j1", Type: models.NodeTypeTool, Template: "json_extract",
		Config: map[string]string{"jsonPath": "a.b.c"},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for a path that does not exist, got nil")
	}
}

func TestJSONExtractErrorsOnNonJSONInput(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "this is not json")
	node := models.WorkflowNode{
		ID: "j1", Type: models.NodeTypeTool, Template: "json_extract",
		Config: map[string]string{"jsonPath": "a"},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for non-JSON input, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestJSONExtract -v
```

Expected: FAIL — falls through to `default`, returns the raw string.

- [ ] **Step 3: Implement**

Append to `backend/internal/engine/nodes/compute.go`:

```go
// executeJSONExtract parses the upstream output as JSON and walks a dot path
// into it. Numeric segments index arrays: "data.items.0.name".
func executeJSONExtract(node models.WorkflowNode, rc RunContexter) (any, error) {
	path := configVal(node, "jsonPath", "")
	if path == "" {
		return nil, errors.New("json_extract: no `jsonPath` configured")
	}
	var doc any
	if err := json.Unmarshal([]byte(rc.Message()), &doc); err != nil {
		return nil, fmt.Errorf("json_extract: upstream output is not valid JSON: %w", err)
	}
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return nil, fmt.Errorf("json_extract: no value at path %q (missing key %q)", path, seg)
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				return nil, fmt.Errorf("json_extract: path %q indexes an array with non-numeric segment %q", path, seg)
			}
			if i < 0 || i >= len(node) {
				return nil, fmt.Errorf("json_extract: index %d out of range at path %q (length %d)", i, path, len(node))
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("json_extract: path %q descends past a scalar at %q", path, seg)
		}
	}
	return cur, nil
}
```

Add `"strconv"` and `"strings"` to `compute.go`'s import block.

- [ ] **Step 4: Register**

In `tool.go`'s `ExecuteTool` switch:

```go
	case "json_extract":
		return executeJSONExtract(node, rc)
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestJSONExtract -v
```

Expected: PASS.

- [ ] **Step 6: Palette entry, verify, commit**

In `frontend/src/lib/data.ts` `TOOL_TEMPLATES`:

```ts
  { id: "json_extract", name: "JSON Extract", desc: "Pick a value by path", icon: "⌗" },
```

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts
git commit -m "feat(nodes): add JSON Extract compute node"
```

---

## Task 7: `crypto` — hash, HMAC, base64

**Files:**
- Modify: `compute.go`, `compute_test.go`, `tool.go`, `frontend/src/lib/data.ts`, `frontend/src/components/canvas/Inspector.tsx`

**Interfaces:**
- Produces: `func executeCrypto(node models.WorkflowNode, rc RunContexter) (any, error)`.

Config keys: `cryptoAction` — one of `sha256`, `sha512`, `sha1`, `md5`, `hmac-sha256`, `base64`, `base64decode`. `cryptoSecret` — HMAC key, required for `hmac-sha256` only.

- [ ] **Step 1: Write the failing test**

Append to `compute_test.go`:

```go
func TestCryptoActions(t *testing.T) {
	cases := []struct{ action, in, secret, want string }{
		// echo -n "hello" | shasum -a 256
		{"sha256", "hello", "", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		// echo -n "hello" | md5
		{"md5", "hello", "", "5d41402abc4b2a76b9719d911017c592"},
		// echo -n "hello" | shasum -a 1
		{"sha1", "hello", "", "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"},
		// echo -n "hello" | openssl dgst -sha256 -hmac "key"
		{"hmac-sha256", "hello", "key", "9307b3b915efb5171ff14d8cb55fbcc798c6c0ef1456d66ded1a6aa723a58b7b"},
		{"base64", "hello", "", "aGVsbG8="},
		{"base64decode", "aGVsbG8=", "", "hello"},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			rc := engine.NewRunContext("r1", nil)
			rc.Set("n1", tc.in)
			node := models.WorkflowNode{
				ID: "c1", Type: models.NodeTypeTool, Template: "crypto",
				Config:  map[string]string{"cryptoAction": tc.action},
				Secrets: map[string]string{"cryptoSecret": tc.secret},
			}
			got, err := nodes.ExecuteTool(context.Background(), node, rc)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%s: want %q, got %q", tc.action, tc.want, got)
			}
		})
	}
}

func TestCryptoRejectsUnknownAction(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "hello")
	node := models.WorkflowNode{
		ID: "c1", Type: models.NodeTypeTool, Template: "crypto",
		Config: map[string]string{"cryptoAction": "rot13"},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for an unsupported action, got nil")
	}
}

func TestCryptoHMACRequiresSecret(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "hello")
	node := models.WorkflowNode{
		ID: "c1", Type: models.NodeTypeTool, Template: "crypto",
		Config: map[string]string{"cryptoAction": "hmac-sha256"},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error when hmac has no secret, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestCrypto -v
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `compute.go`:

```go
// executeCrypto hashes / encodes the upstream output. Pure stdlib; no network.
func executeCrypto(node models.WorkflowNode, rc RunContexter) (any, error) {
	in := rc.Message()
	action := configVal(node, "cryptoAction", "sha256")

	var h hash.Hash
	switch action {
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	case "sha1":
		h = sha1.New()
	case "md5":
		h = md5.New()
	case "hmac-sha256":
		secret := secretVal(node, "cryptoSecret")
		if secret == "" {
			return nil, errors.New("crypto: hmac-sha256 needs `cryptoSecret` set")
		}
		h = hmac.New(sha256.New, []byte(secret))
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(in)), nil
	case "base64decode":
		b, err := base64.StdEncoding.DecodeString(in)
		if err != nil {
			return nil, fmt.Errorf("crypto: input is not valid base64: %w", err)
		}
		return string(b), nil
	default:
		return nil, fmt.Errorf("crypto: unsupported action %q "+
			"(want sha256, sha512, sha1, md5, hmac-sha256, base64, base64decode)", action)
	}
	h.Write([]byte(in))
	return hex.EncodeToString(h.Sum(nil)), nil
}
```

Add to `compute.go`'s imports: `crypto/hmac`, `crypto/md5`, `crypto/sha1`, `crypto/sha256`, `crypto/sha512`, `encoding/base64`, `encoding/hex`, `hash`.

> `md5` and `sha1` are included because real third-party APIs still require them for signature schemes (e.g. legacy webhook verification). They are not offered as a security recommendation.

- [ ] **Step 4: Register**

```go
	case "crypto":
		return executeCrypto(node, rc)
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestCrypto -v
```

Expected: PASS.

- [ ] **Step 6: Palette entry, verify, commit**

`frontend/src/lib/data.ts` `TOOL_TEMPLATES`:

```ts
  { id: "crypto", name: "Crypto", desc: "Hash / HMAC / base64", icon: "⚿" },
```

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts
git commit -m "feat(nodes): add Crypto compute node (hash, hmac, base64)"
```

---

## Task 8: real `datetime`

**Files:**
- Modify: `compute.go`, `compute_test.go`, `tool.go:121`, `frontend/src/lib/data.ts`

**Interfaces:**
- Produces: `func executeDateTime(node models.WorkflowNode) (any, error)`.

Today `tool.go:121-122` returns `time.Now().UTC().Format(time.RFC3339)` and ignores all config. This replaces it while keeping that exact output as the **default**, so existing workflows are unaffected.

Config keys: `dtFormat` (Go layout, or `rfc3339`/`unix`/`date`), `dtOffset` (a Go duration like `-24h`, `30m`), `dtZone` (IANA name, e.g. `Asia/Kolkata`).

- [ ] **Step 1: Write the failing test**

Append to `compute_test.go`:

```go
func TestDateTimeDefaultIsUnchangedRFC3339(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{ID: "d1", Type: models.NodeTypeTool, Template: "datetime"}
	got, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("want a string, got %T", got)
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		t.Errorf("default output must stay RFC3339 (existing workflows depend on it): %q", s)
	}
	if !strings.HasSuffix(s, "Z") {
		t.Errorf("default output must stay UTC, got %q", s)
	}
}

func TestDateTimeAppliesOffsetZoneAndFormat(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{
		ID: "d1", Type: models.NodeTypeTool, Template: "datetime",
		Config: map[string]string{"dtFormat": "date", "dtZone": "Asia/Kolkata", "dtOffset": "-24h"},
	}
	got, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Now().Add(-24 * time.Hour).In(mustZone(t, "Asia/Kolkata")).Format("2006-01-02")
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestDateTimeUnixFormat(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{
		ID: "d1", Type: models.NodeTypeTool, Template: "datetime",
		Config: map[string]string{"dtFormat": "unix"},
	}
	got, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.ParseInt(got.(string), 10, 64)
	if err != nil {
		t.Fatalf("want a unix timestamp, got %v", got)
	}
	if delta := time.Since(time.Unix(n, 0)); delta > time.Minute || delta < -time.Minute {
		t.Errorf("timestamp is not close to now: %v", delta)
	}
}

func TestDateTimeRejectsBadConfig(t *testing.T) {
	for _, cfg := range []map[string]string{
		{"dtZone": "Mars/Olympus"},
		{"dtOffset": "tomorrow"},
	} {
		rc := engine.NewRunContext("r1", nil)
		node := models.WorkflowNode{ID: "d1", Type: models.NodeTypeTool, Template: "datetime", Config: cfg}
		if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
			t.Errorf("want an error for config %v, got nil", cfg)
		}
	}
}

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tzdata unavailable for %s: %v", name, err)
	}
	return loc
}
```

Add `"strconv"`, `"strings"`, and `"time"` to `compute_test.go`'s imports.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestDateTime -v
```

Expected: the default test PASSES (current behaviour already matches); the offset/zone/format and bad-config tests FAIL.

- [ ] **Step 3: Implement**

Append to `compute.go`:

```go
// executeDateTime renders the current time. With no config it returns
// UTC RFC3339 — byte-identical to the previous hardcoded behaviour, so
// existing workflows using the datetime tool are unaffected.
func executeDateTime(node models.WorkflowNode) (any, error) {
	now := time.Now()

	if off := configVal(node, "dtOffset", ""); off != "" {
		d, err := time.ParseDuration(off)
		if err != nil {
			return nil, fmt.Errorf("datetime: `dtOffset` %q is not a duration (try -24h, 30m): %w", off, err)
		}
		now = now.Add(d)
	}

	loc := time.UTC
	if zone := configVal(node, "dtZone", ""); zone != "" {
		l, err := time.LoadLocation(zone)
		if err != nil {
			return nil, fmt.Errorf("datetime: unknown timezone %q (want an IANA name like Asia/Kolkata): %w", zone, err)
		}
		loc = l
	}
	now = now.In(loc)

	switch f := configVal(node, "dtFormat", "rfc3339"); f {
	case "rfc3339":
		return now.Format(time.RFC3339), nil
	case "unix":
		return strconv.FormatInt(now.Unix(), 10), nil
	case "date":
		return now.Format("2006-01-02"), nil
	case "time":
		return now.Format("15:04:05"), nil
	default:
		// Anything else is treated as a literal Go layout string.
		return now.Format(f), nil
	}
}
```

Add `"time"` to `compute.go`'s imports.

- [ ] **Step 4: Replace the inline implementation**

In `backend/internal/engine/nodes/tool.go`, replace:

```go
	case "datetime":
		return time.Now().UTC().Format(time.RFC3339), nil
```

with:

```go
	case "datetime":
		return executeDateTime(node)
```

If `time` becomes unused in `tool.go`, remove it from that file's imports (`go build` will say).

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestDateTime -v
```

Expected: PASS, all four.

- [ ] **Step 6: Palette entry, verify, commit**

`datetime` currently has **no** palette entry despite being implemented. Add it to `TOOL_TEMPLATES`:

```ts
  { id: "datetime", name: "Date & Time", desc: "Now, offset, timezone", icon: "◔" },
```

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts
git commit -m "feat(nodes): real Date & Time node with offset, timezone and format"
```

---

## Task 9: `xml` — XML to JSON

**Files:**
- Modify: `compute.go`, `compute_test.go`, `tool.go`, `frontend/src/lib/data.ts`

**Interfaces:**
- Produces: `func executeXMLToJSON(rc RunContexter) (any, error)`, plus `func decodeXMLNode(d *xml.Decoder, start xml.StartElement) (any, error)` — **also used by Task 19 (RSS)**, so keep it exported within the package and do not inline it.

Uses stdlib `encoding/xml` only.

- [ ] **Step 1: Write the failing test**

Append to `compute_test.go`:

```go
func TestXMLToJSONConvertsNestedElements(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", `<order id="7"><item>widget</item><item>gizmo</item><total>19.99</total></order>`)
	node := models.WorkflowNode{ID: "x1", Type: models.NodeTypeTool, Template: "xml"}

	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want a map, got %T", out)
	}
	if got["@id"] != "7" {
		t.Errorf("attributes should be prefixed with @: got %#v", got)
	}
	items, ok := got["item"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("repeated elements should collapse to a slice: got %#v", got["item"])
	}
	if items[0] != "widget" || items[1] != "gizmo" {
		t.Errorf("items: got %#v", items)
	}
	if got["total"] != "19.99" {
		t.Errorf("total: got %#v", got["total"])
	}
}

func TestXMLToJSONErrorsOnMalformedInput(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", `<order><item>unclosed`)
	node := models.WorkflowNode{ID: "x1", Type: models.NodeTypeTool, Template: "xml"}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for malformed XML, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestXMLToJSON -v
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `compute.go`:

```go
// executeXMLToJSON converts the upstream output from XML into a JSON-shaped
// map. Attributes become "@name" keys; repeated child elements collapse into a
// slice; leaf elements become their trimmed character data.
func executeXMLToJSON(rc RunContexter) (any, error) {
	dec := xml.NewDecoder(strings.NewReader(rc.Message()))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("xml: could not parse input: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue // skip prolog, comments, leading whitespace
		}
		val, err := decodeXMLNode(dec, start)
		if err != nil {
			return nil, fmt.Errorf("xml: could not parse input: %w", err)
		}
		return val, nil
	}
}

// decodeXMLNode reads one element's subtree. Shared with the RSS connector —
// keep it package-level rather than inlining into executeXMLToJSON.
func decodeXMLNode(d *xml.Decoder, start xml.StartElement) (any, error) {
	node := map[string]any{}
	for _, a := range start.Attr {
		node["@"+a.Name.Local] = a.Value
	}
	var text strings.Builder

	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := decodeXMLNode(d, t)
			if err != nil {
				return nil, err
			}
			key := t.Name.Local
			switch existing := node[key].(type) {
			case nil:
				node[key] = child
			case []any:
				node[key] = append(existing, child)
			default:
				node[key] = []any{existing, child}
			}
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			trimmed := strings.TrimSpace(text.String())
			// A leaf with no attributes and no children is just its text.
			if len(node) == 0 {
				return trimmed, nil
			}
			if trimmed != "" {
				node["#text"] = trimmed
			}
			return node, nil
		}
	}
}
```

Add `"encoding/xml"` to `compute.go`'s imports.

- [ ] **Step 4: Register**

```go
	case "xml":
		return executeXMLToJSON(rc)
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestXMLToJSON -v
```

Expected: PASS.

- [ ] **Step 6: Palette entry, verify, commit**

```ts
  { id: "xml", name: "XML → JSON", desc: "Parse XML payloads", icon: "⋔" },
```

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts
git commit -m "feat(nodes): add XML to JSON compute node"
```

---

## Task 10: `template` — free-text composition

**Files:**
- Modify: `compute.go`, `compute_test.go`, `tool.go`, `frontend/src/lib/data.ts`

**Interfaces:**
- Produces: `func executeTemplate(node models.WorkflowNode, rc RunContexter) (any, error)`.

Config key: `templateText`. This is the plain-text sibling of `set` — it makes the Task 4 resolver directly usable for message bodies.

- [ ] **Step 1: Write the failing test**

Append to `compute_test.go`:

```go
func TestTemplateNodeComposesText(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"the question"`))
	rc.Set("n1", map[string]any{"city": "Kolkata"})
	node := models.WorkflowNode{
		ID: "t1", Type: models.NodeTypeTool, Template: "template",
		Config: map[string]string{"templateText": "Asked {{ input }} about {{ node.n1.city }}."},
	}
	got, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Asked the question about Kolkata." {
		t.Errorf("got %q", got)
	}
}

func TestTemplateNodeErrorsWhenUnconfigured(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{ID: "t1", Type: models.NodeTypeTool, Template: "template"}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error when templateText is unset, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestTemplateNode -v
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `compute.go`:

```go
// executeTemplate renders free text with {{ }} references expanded.
func executeTemplate(node models.WorkflowNode, rc RunContexter) (any, error) {
	tpl := configVal(node, "templateText", "")
	if tpl == "" {
		return nil, errors.New("template: no `templateText` configured")
	}
	return resolveTemplate(tpl, rc), nil
}
```

- [ ] **Step 4: Register**

```go
	case "template":
		return executeTemplate(node, rc)
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestTemplateNode -v
```

Expected: PASS.

- [ ] **Step 6: Palette entry, verify, commit**

```ts
  { id: "template", name: "Text Template", desc: "Compose with {{ refs }}", icon: "¶" },
```

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts
git commit -m "feat(nodes): add Text Template compute node"
```

---

## Task 11: Inspector config fields for the Phase 2 nodes

**Files:**
- Modify: `frontend/src/components/canvas/Inspector.tsx`

**Interfaces:**
- Consumes: the config keys defined in Tasks 5-10 — `setFields`, `jsonPath`, `cryptoAction`, `cryptoSecret`, `dtFormat`, `dtOffset`, `dtZone`, `templateText`.
- Produces: user-editable fields for each. Without this task the Phase 2 nodes are configurable only by hand-editing workflow JSON.

- [ ] **Step 1: Read the existing schema shape**

```bash
cd frontend && sed -n '1980,2032p' src/components/canvas/Inspector.tsx
```

Note the shape: a record keyed by template id, each with `label` and a `fields` array of `{ kind: "secret" | "config", key, label, placeholder?, hint? }`.

- [ ] **Step 2: Add the six entries**

Add to the same record that holds `linear`/`todoist`:

```ts
  set: {
    label: "Edit Fields config",
    fields: [
      {
        kind: "config",
        key: "setFields",
        label: "Fields (JSON)",
        placeholder: '{"city":"{{ node.n1.city }}","asked":"{{ input }}"}',
        hint: "String values may use {{ result }}, {{ input }}, {{ node.<id>.<field> }}",
      },
    ],
  },
  json_extract: {
    label: "JSON Extract config",
    fields: [
      {
        kind: "config",
        key: "jsonPath",
        label: "Path",
        placeholder: "data.items.0.name",
        hint: "Dot path; numeric segments index arrays",
      },
    ],
  },
  crypto: {
    label: "Crypto config",
    fields: [
      {
        kind: "config",
        key: "cryptoAction",
        label: "Action",
        placeholder: "sha256",
        hint: "sha256 · sha512 · sha1 · md5 · hmac-sha256 · base64 · base64decode",
      },
      {
        kind: "secret",
        key: "cryptoSecret",
        label: "HMAC secret",
        hint: "only for hmac-sha256",
        placeholder: "shared secret",
      },
    ],
  },
  datetime: {
    label: "Date & Time config",
    fields: [
      {
        kind: "config",
        key: "dtFormat",
        label: "Format",
        placeholder: "rfc3339",
        hint: "rfc3339 · unix · date · time · or a Go layout",
      },
      {
        kind: "config",
        key: "dtOffset",
        label: "Offset",
        hint: "optional",
        placeholder: "-24h",
      },
      {
        kind: "config",
        key: "dtZone",
        label: "Timezone",
        hint: "optional, IANA name",
        placeholder: "Asia/Kolkata",
      },
    ],
  },
  template: {
    label: "Text Template config",
    fields: [
      {
        kind: "config",
        key: "templateText",
        label: "Template",
        placeholder: "Result: {{ result }}",
        hint: "Supports {{ result }}, {{ input }}, {{ node.<id>.<field> }}",
      },
    ],
  },
```

`xml` needs no configuration — it reads the upstream output and takes no options. Do not add an entry for it.

- [ ] **Step 3: Verify the config panel resolves for tool nodes**

```bash
cd frontend && grep -n "CONNECTOR_CONFIG\|node.type === \"action\"\|type === \"tool\"" src/components/canvas/Inspector.tsx | head -20
```

If the config-panel lookup is gated on `node.type === "action"`, widen that condition to also allow `"tool"`. If it already keys purely off `node.template`, no change is needed.

- [ ] **Step 4: Verify**

```bash
cd frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
```

Expected: clean.

- [ ] **Step 5: Manual check**

Start the frontend, open the canvas, drag each of the six new Tools onto it, and confirm each shows its config fields in the Inspector. Confirm the **x402 and Tendril tabs are visually unchanged** (C1).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(canvas): config fields for the compute nodes"
```

---

# Phase 3 — Tier 4: ten connectors

**The list is closed (C6):** Postgres, Stripe, Twilio, Shopify, Pipedrive, Zendesk, Monday.com, PagerDuty, Mattermost, RSS.

Every one follows the same five-point pattern, established by `sendTodoist` (`connectors_productivity.go:230-247`):

1. an overridable `<svc>APIBase` var + `Set<Svc>APIBaseForTest` (so tests can point at `httptest`)
2. a `send<Svc>(ctx, node, rc) (any, error)` reading credentials via `secretVal` and settings via `configVal`
3. a `case` in `ExecuteAction` (`action.go:17`)
4. a `TEMPLATE` entry in `frontend/src/lib/data.ts` with a `category`
5. a `CONNECTOR_CONFIG` entry in `Inspector.tsx`

…plus an `httptest` test asserting the sentinel, the auth header, and the request body.

## Task 12: Read-capable connector helper

**Files:**
- Modify: `backend/internal/engine/nodes/connector_helpers.go`
- Test: `backend/internal/engine/nodes/connector_helpers_test.go`

**Interfaces:**
- Consumes: `doValidatedRequest`, `readErrorBody` (existing).
- Produces: `func doAndDecode(req *http.Request, serviceName string) (any, error)`, `func getAndDecode(ctx context.Context, target string, extraHeaders map[string]string, serviceName string) (any, error)`, and `func getRaw(ctx context.Context, target string, extraHeaders map[string]string, serviceName string) ([]byte, error)`. **Task 19 (RSS) depends on `getRaw`.**

Today every connector is write-only: `doAndCheck` does `io.Copy(io.Discard, resp.Body)` and returns a sentinel string. Nothing can fetch.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/engine/nodes/connector_helpers_test.go`:

```go
func TestGetAndDecodeReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("want auth header forwarded, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"id":1}]}`))
	}))
	defer srv.Close()

	got, err := nodes.GetAndDecodeForTest(context.Background(), srv.URL,
		map[string]string{"Authorization": "Bearer tok"}, "Test")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("want a decoded map, got %T", got)
	}
	if _, ok := m["items"]; !ok {
		t.Errorf("want the decoded body, got %#v", m)
	}
}

func TestGetAndDecodeSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"nope"}`))
	}))
	defer srv.Close()

	if _, err := nodes.GetAndDecodeForTest(context.Background(), srv.URL, nil, "Test"); err == nil {
		t.Error("want an error for a 403, got nil")
	}
}

func TestGetAndDecodeRejectsSSRFTarget(t *testing.T) {
	if _, err := nodes.GetAndDecodeForTest(context.Background(), "http://169.254.169.254/latest/meta-data/", nil, "Test"); err == nil {
		t.Error("want the SSRF guard to reject link-local metadata, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestGetAndDecode -v
```

Expected: FAIL — `undefined: nodes.GetAndDecodeForTest`.

- [ ] **Step 3: Implement**

Append to `connector_helpers.go`:

```go
// doAndDecode runs req through the SSRF guard, then decodes a JSON response
// body instead of discarding it. This is the read-capable counterpart to
// doAndCheck — use it for connectors that fetch rather than post.
func doAndDecode(req *http.Request, serviceName string) (any, error) {
	resp, err := doValidatedRequest(req, serviceName)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		// readErrorBody returns a bounded body excerpt as a string, matching
		// doAndCheck's error shape exactly.
		return nil, fmt.Errorf("%s API %d: %s", serviceName, resp.StatusCode, readErrorBody(resp))
	}
	var out any
	if err := json.NewDecoder(io.LimitReader(resp.Body, httpResponseLimit)).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: response was not valid JSON: %w", serviceName, err)
	}
	return out, nil
}

// getAndDecode GETs target and returns the decoded JSON body.
func getAndDecode(ctx context.Context, target string, extraHeaders map[string]string, serviceName string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", serviceName, err)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	return doAndDecode(req, serviceName)
}

// getRaw GETs target and returns the raw body, bounded by httpResponseLimit.
// Used by connectors whose payload is not JSON (RSS/Atom feeds).
func getRaw(ctx context.Context, target string, extraHeaders map[string]string, serviceName string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", serviceName, err)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := doValidatedRequest(req, serviceName)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s API %d: %s", serviceName, resp.StatusCode, readErrorBody(resp))
	}
	return io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
}
```

These match the existing helpers exactly, verified against the source:
`readErrorBody(resp *http.Response) string` (`connector_helpers.go:83`) returns a bounded
excerpt as a **string**, and `doAndCheck` (`:91`) wraps it as
`fmt.Errorf("%s API %d: %s", serviceName, resp.StatusCode, readErrorBody(resp))`.
`httpResponseLimit` is `5 << 20` (`tool.go:46`). No import changes are needed —
`connector_helpers.go` already imports `context`, `encoding/json`, `fmt`, `io`, and `net/http`.

- [ ] **Step 4: Add the test-only export**

Create `backend/internal/engine/nodes/connector_helpers_export_test.go`:

```go
package nodes

import "context"

// GetAndDecodeForTest exposes getAndDecode to the external nodes_test package.
func GetAndDecodeForTest(ctx context.Context, target string, h map[string]string, svc string) (any, error) {
	return getAndDecode(ctx, target, h, svc)
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestGetAndDecode -v
```

Expected: PASS, all three.

- [ ] **Step 6: Commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
git add internal/engine/nodes/connector_helpers.go internal/engine/nodes/connector_helpers_test.go \
        internal/engine/nodes/connector_helpers_export_test.go
git commit -m "feat(nodes): add read-capable connector helpers (getAndDecode, getRaw)"
```

---

## Task 13: Stripe

**Files:**
- Create: `backend/internal/engine/nodes/connectors_commerce.go`, `connectors_commerce_test.go`
- Modify: `action.go:17` switch, `frontend/src/lib/data.ts`, `Inspector.tsx`

**Interfaces:**
- Consumes: `secretVal`, `configVal`, `doAndCheck`, `resolveTemplate`.
- Produces: `func sendStripe(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error)`, `func SetStripeAPIBaseForTest(base string)`.

Creates a Stripe **customer** with the run output as the description. API key auth (`Bearer sk_...`), no OAuth. Stripe's API is `application/x-www-form-urlencoded`, not JSON — this is the one connector in the set that does not use `postJSON`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/engine/nodes/connectors_commerce_test.go`:

```go
package nodes_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestStripeAction_CreatesCustomer(t *testing.T) {
	var gotAuth, gotCT, gotPath string
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"cus_123"}`))
	}))
	defer srv.Close()
	nodes.SetStripeAPIBaseForTest(srv.URL)
	defer nodes.SetStripeAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "st1", Type: models.NodeTypeAction, Template: "stripe",
		Secrets: map[string]string{"stripeAPIKey": "sk_test_xxx"},
		Config:  map[string]string{"stripeEmail": "buyer@example.com"},
	}
	rc := engine.NewRunContext("r1", []byte(`"new signup from the workflow"`))

	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "stripe_customer_created" {
		t.Errorf("want 'stripe_customer_created', got %v", result)
	}
	if gotAuth != "Bearer sk_test_xxx" {
		t.Errorf("want bearer auth, got %q", gotAuth)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("Stripe requires form encoding, got %q", gotCT)
	}
	if gotPath != "/v1/customers" {
		t.Errorf("want /v1/customers, got %q", gotPath)
	}
	if gotForm.Get("email") != "buyer@example.com" {
		t.Errorf("email: got %q", gotForm.Get("email"))
	}
	if gotForm.Get("description") != "new signup from the workflow" {
		t.Errorf("description should be the run output, got %q", gotForm.Get("description"))
	}
}

func TestStripeAction_SkipsWithoutAPIKey(t *testing.T) {
	node := models.WorkflowNode{ID: "st1", Type: models.NodeTypeAction, Template: "stripe"}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "stripe_skipped_no_api_key" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestStripeAction_SkipsWithoutEmail(t *testing.T) {
	node := models.WorkflowNode{
		ID: "st1", Type: models.NodeTypeAction, Template: "stripe",
		Secrets: map[string]string{"stripeAPIKey": "sk_test_xxx"},
	}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "stripe_skipped_no_email" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestStripeAction -v
```

Expected: FAIL — `undefined: nodes.SetStripeAPIBaseForTest`.

- [ ] **Step 3: Implement**

Create `backend/internal/engine/nodes/connectors_commerce.go`:

```go
package nodes

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// stripeAPIBase is overridden in tests via SetStripeAPIBaseForTest.
var stripeAPIBase = "https://api.stripe.com"

// SetStripeAPIBaseForTest overrides the Stripe API base URL. Call only from
// tests. Pass "" to reset to the real API.
func SetStripeAPIBaseForTest(base string) {
	if base == "" {
		stripeAPIBase = "https://api.stripe.com"
	} else {
		stripeAPIBase = base
	}
}

// sendStripe creates a Stripe customer, using the run output as the
// description. Stripe's API takes form encoding, not JSON, so this builds the
// request directly rather than going through postJSON.
func sendStripe(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "stripeAPIKey")
	if apiKey == "" {
		return "stripe_skipped_no_api_key", ErrActionSkipped
	}
	email := resolveTemplate(configVal(node, "stripeEmail", ""), rc)
	if email == "" {
		return "stripe_skipped_no_email", ErrActionSkipped
	}
	form := url.Values{}
	form.Set("email", email)
	form.Set("description", rc.Message())
	if name := resolveTemplate(configVal(node, "stripeName", ""), rc); name != "" {
		form.Set("name", name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		stripeAPIBase+"/v1/customers", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return doAndCheck(req, "stripe_customer_created", "Stripe")
}
```

`doAndCheck(req *http.Request, sentinel, serviceName string) (any, error)` is at `connector_helpers.go:91` — it runs the SSRF guard, turns any status ≥ 400 into an error carrying a body excerpt, and returns `sentinel` on success. The argument order above matches it.

- [ ] **Step 4: Register in the action switch**

In `backend/internal/engine/nodes/action.go`, add before `default:`:

```go
	case "stripe":
		return sendStripe(ctx, node, rc)
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestStripeAction -v
```

Expected: PASS, all three.

- [ ] **Step 6: Frontend wiring**

`frontend/src/lib/data.ts`, in `ACTION_TEMPLATES`:

```ts
  {
    id: "stripe",
    name: "Stripe Customer",
    desc: "Create a customer",
    icon: "st",
    category: "Commerce",
  },
```

If the surrounding entries carry a `category` field (added by the prior plan's A7 work), match it; if they do not, omit `category`.

`frontend/src/components/canvas/Inspector.tsx`:

```ts
  stripe: {
    label: "Stripe config",
    fields: [
      {
        kind: "secret",
        key: "stripeAPIKey",
        label: "Secret Key",
        placeholder: "sk_live_xxxxxxxxxxxx",
      },
      {
        kind: "config",
        key: "stripeEmail",
        label: "Customer email",
        placeholder: "buyer@example.com",
      },
      {
        kind: "config",
        key: "stripeName",
        label: "Customer name",
        hint: "optional",
        placeholder: "leave blank to omit",
      },
    ],
  },
```

- [ ] **Step 7: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(connectors): add Stripe customer connector"
```

---

## Task 14: Twilio SMS

**Files:**
- Create: `backend/internal/engine/nodes/connectors_ops.go`, `connectors_ops_test.go`
- Modify: `action.go`, `data.ts`, `Inspector.tsx`

**Interfaces:**
- Produces: `func sendTwilio(ctx, node, rc) (any, error)`, `func SetTwilioAPIBaseForTest(base string)`.

Twilio uses **HTTP Basic auth** (Account SID as username, Auth Token as password) and form encoding.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/engine/nodes/connectors_ops_test.go`:

```go
package nodes_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestTwilioAction_SendsSMS(t *testing.T) {
	var gotUser, gotPass, gotPath string
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"sid":"SM123"}`))
	}))
	defer srv.Close()
	nodes.SetTwilioAPIBaseForTest(srv.URL)
	defer nodes.SetTwilioAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "tw1", Type: models.NodeTypeAction, Template: "twilio",
		Secrets: map[string]string{"twilioAuthToken": "authtok"},
		Config: map[string]string{
			"twilioAccountSID": "AC123",
			"twilioFrom":       "+15550001111",
			"twilioTo":         "+15550002222",
		},
	}
	rc := engine.NewRunContext("r1", []byte(`"deploy finished"`))

	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "twilio_sms_sent" {
		t.Errorf("want 'twilio_sms_sent', got %v", result)
	}
	if gotUser != "AC123" || gotPass != "authtok" {
		t.Errorf("want basic auth SID/token, got %q/%q", gotUser, gotPass)
	}
	if !strings.HasSuffix(gotPath, "/Accounts/AC123/Messages.json") {
		t.Errorf("want the account-scoped Messages path, got %q", gotPath)
	}
	if gotForm.Get("To") != "+15550002222" || gotForm.Get("From") != "+15550001111" {
		t.Errorf("To/From: got %v", gotForm)
	}
	if gotForm.Get("Body") != "deploy finished" {
		t.Errorf("Body should be the run output, got %q", gotForm.Get("Body"))
	}
}

func TestTwilioAction_SkipsWhenUnconfigured(t *testing.T) {
	cases := []struct {
		name string
		node models.WorkflowNode
		want string
	}{
		{"no token", models.WorkflowNode{
			Template: "twilio",
			Config:   map[string]string{"twilioAccountSID": "AC1", "twilioFrom": "+1", "twilioTo": "+2"},
		}, "twilio_skipped_no_auth_token"},
		{"no sid", models.WorkflowNode{
			Template: "twilio",
			Secrets:  map[string]string{"twilioAuthToken": "t"},
			Config:   map[string]string{"twilioFrom": "+1", "twilioTo": "+2"},
		}, "twilio_skipped_no_account_sid"},
		{"no recipient", models.WorkflowNode{
			Template: "twilio",
			Secrets:  map[string]string{"twilioAuthToken": "t"},
			Config:   map[string]string{"twilioAccountSID": "AC1", "twilioFrom": "+1"},
		}, "twilio_skipped_no_recipient"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.node.Type = models.NodeTypeAction
			rc := engine.NewRunContext("r1", nil)
			got, _ := nodes.ExecuteAction(context.Background(), tc.node, rc)
			if got != tc.want {
				t.Errorf("want %q, got %v", tc.want, got)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestTwilioAction -v
```

Expected: FAIL — undefined symbol.

- [ ] **Step 3: Implement**

Create `backend/internal/engine/nodes/connectors_ops.go`:

```go
package nodes

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// twilioAPIBase is overridden in tests via SetTwilioAPIBaseForTest.
var twilioAPIBase = "https://api.twilio.com/2010-04-01"

// SetTwilioAPIBaseForTest overrides the Twilio API base URL. Call only from
// tests. Pass "" to reset to the real API.
func SetTwilioAPIBaseForTest(base string) {
	if base == "" {
		twilioAPIBase = "https://api.twilio.com/2010-04-01"
	} else {
		twilioAPIBase = base
	}
}

// sendTwilio sends the run output as an SMS. Twilio uses HTTP Basic auth
// (Account SID / Auth Token) and form encoding.
func sendTwilio(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "twilioAuthToken")
	if token == "" {
		return "twilio_skipped_no_auth_token", ErrActionSkipped
	}
	sid := configVal(node, "twilioAccountSID", "")
	if sid == "" {
		return "twilio_skipped_no_account_sid", ErrActionSkipped
	}
	to := resolveTemplate(configVal(node, "twilioTo", ""), rc)
	if to == "" {
		return "twilio_skipped_no_recipient", ErrActionSkipped
	}
	from := configVal(node, "twilioFrom", "")
	if from == "" {
		return "twilio_skipped_no_sender", ErrActionSkipped
	}
	form := url.Values{}
	form.Set("To", to)
	form.Set("From", from)
	form.Set("Body", rc.Message())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		twilioAPIBase+"/Accounts/"+url.PathEscape(sid)+"/Messages.json",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(sid, token)
	return doAndCheck(req, "twilio_sms_sent", "Twilio")
}
```

- [ ] **Step 4: Register**

```go
	case "twilio":
		return sendTwilio(ctx, node, rc)
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestTwilioAction -v
```

Expected: PASS.

- [ ] **Step 6: Frontend wiring**

`data.ts`:

```ts
  { id: "twilio", name: "Twilio SMS", desc: "Send an SMS", icon: "tw", category: "Messaging" },
```

`Inspector.tsx`:

```ts
  twilio: {
    label: "Twilio config",
    fields: [
      { kind: "secret", key: "twilioAuthToken", label: "Auth Token", placeholder: "your Twilio auth token" },
      { kind: "config", key: "twilioAccountSID", label: "Account SID", placeholder: "ACxxxxxxxxxxxxxxxx" },
      { kind: "config", key: "twilioFrom", label: "From number", placeholder: "+15550001111" },
      { kind: "config", key: "twilioTo", label: "To number", placeholder: "+15550002222" },
    ],
  },
```

- [ ] **Step 7: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(connectors): add Twilio SMS connector"
```

---

## Task 15: Mattermost, PagerDuty

Two connectors, one task — both are single-POST webhook/token services with no pagination or auth ceremony, and a reviewer would accept or reject them together.

**Files:**
- Modify: `connectors_ops.go`, `connectors_ops_test.go`, `action.go`, `data.ts`, `Inspector.tsx`

**Interfaces:**
- Produces: `sendMattermost`, `sendPagerDuty`, `SetPagerDutyAPIBaseForTest`. Mattermost needs no base override — its endpoint is the user's own webhook URL, read from `node.URL`.

- [ ] **Step 1: Write the failing tests**

Append to `connectors_ops_test.go`:

```go
func TestMattermostAction_PostsToWebhook(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = jsonDecode(r.Body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "mm1", Type: models.NodeTypeAction, Template: "mattermost",
		Secrets: map[string]string{"mattermostWebhookURL": srv.URL},
		Config:  map[string]string{"mattermostChannel": "town-square"},
	}
	rc := engine.NewRunContext("r1", []byte(`"build passed"`))

	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "mattermost_sent" {
		t.Errorf("want 'mattermost_sent', got %v", result)
	}
	if gotBody["text"] != "build passed" {
		t.Errorf("text: got %v", gotBody["text"])
	}
	if gotBody["channel"] != "town-square" {
		t.Errorf("channel: got %v", gotBody["channel"])
	}
}

func TestMattermostAction_SkipsWithoutWebhookURL(t *testing.T) {
	node := models.WorkflowNode{ID: "mm1", Type: models.NodeTypeAction, Template: "mattermost"}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "mattermost_skipped_no_webhook_url" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestPagerDutyAction_TriggersIncident(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = jsonDecode(r.Body, &gotBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	nodes.SetPagerDutyAPIBaseForTest(srv.URL)
	defer nodes.SetPagerDutyAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "pd1", Type: models.NodeTypeAction, Template: "pagerduty",
		Secrets: map[string]string{"pagerdutyRoutingKey": "routing_xxx"},
		Config:  map[string]string{"pagerdutySeverity": "warning"},
	}
	rc := engine.NewRunContext("r1", []byte(`"disk usage above 90%"`))

	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "pagerduty_event_triggered" {
		t.Errorf("want 'pagerduty_event_triggered', got %v", result)
	}
	if gotBody["routing_key"] != "routing_xxx" {
		t.Errorf("routing_key: got %v", gotBody["routing_key"])
	}
	if gotBody["event_action"] != "trigger" {
		t.Errorf("event_action: got %v", gotBody["event_action"])
	}
	payload, ok := gotBody["payload"].(map[string]any)
	if !ok {
		t.Fatalf("want a payload object, got %#v", gotBody["payload"])
	}
	if payload["summary"] != "disk usage above 90%" {
		t.Errorf("summary: got %v", payload["summary"])
	}
	if payload["severity"] != "warning" {
		t.Errorf("severity: got %v", payload["severity"])
	}
	if payload["source"] != "agentmesh" {
		t.Errorf("source: got %v", payload["source"])
	}
}

func TestPagerDutyAction_DefaultsSeverityToError(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = jsonDecode(r.Body, &gotBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	nodes.SetPagerDutyAPIBaseForTest(srv.URL)
	defer nodes.SetPagerDutyAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "pd1", Type: models.NodeTypeAction, Template: "pagerduty",
		Secrets: map[string]string{"pagerdutyRoutingKey": "routing_xxx"},
	}
	rc := engine.NewRunContext("r1", []byte(`"boom"`))
	if _, err := nodes.ExecuteAction(context.Background(), node, rc); err != nil {
		t.Fatal(err)
	}
	payload := gotBody["payload"].(map[string]any)
	if payload["severity"] != "error" {
		t.Errorf("want default severity 'error', got %v", payload["severity"])
	}
}

func TestPagerDutyAction_SkipsWithoutRoutingKey(t *testing.T) {
	node := models.WorkflowNode{ID: "pd1", Type: models.NodeTypeAction, Template: "pagerduty"}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "pagerduty_skipped_no_routing_key" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

// jsonDecode is a tiny helper so these tests read the same way as the
// existing connector tests without repeating the decoder boilerplate.
func jsonDecode(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
```

Add `"encoding/json"` to the test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/engine/nodes/ -run 'TestMattermost|TestPagerDuty' -v
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `connectors_ops.go`:

```go
// sendMattermost posts the run output to a Mattermost incoming webhook. The
// webhook URL is the credential — there is no separate token.
func sendMattermost(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	hookURL := secretVal(node, "mattermostWebhookURL")
	if hookURL == "" {
		return "mattermost_skipped_no_webhook_url", ErrActionSkipped
	}
	payload := map[string]any{"text": rc.Message()}
	if ch := configVal(node, "mattermostChannel", ""); ch != "" {
		payload["channel"] = ch
	}
	if user := configVal(node, "mattermostUsername", ""); user != "" {
		payload["username"] = user
	}
	return postJSON(ctx, hookURL, nil, payload, "mattermost_sent", "Mattermost")
}

// pagerdutyAPIBase is overridden in tests via SetPagerDutyAPIBaseForTest.
var pagerdutyAPIBase = "https://events.pagerduty.com"

// SetPagerDutyAPIBaseForTest overrides the PagerDuty Events API base URL.
// Call only from tests. Pass "" to reset to the real API.
func SetPagerDutyAPIBaseForTest(base string) {
	if base == "" {
		pagerdutyAPIBase = "https://events.pagerduty.com"
	} else {
		pagerdutyAPIBase = base
	}
}

// sendPagerDuty triggers a PagerDuty Events API v2 alert with the run output
// as the incident summary.
func sendPagerDuty(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	routingKey := secretVal(node, "pagerdutyRoutingKey")
	if routingKey == "" {
		return "pagerduty_skipped_no_routing_key", ErrActionSkipped
	}
	payload := map[string]any{
		"routing_key":  routingKey,
		"event_action": "trigger",
		"payload": map[string]any{
			"summary":  rc.Message(),
			"severity": configVal(node, "pagerdutySeverity", "error"),
			"source":   configVal(node, "pagerdutySource", "agentmesh"),
		},
	}
	return postJSON(ctx, pagerdutyAPIBase+"/v2/enqueue", nil, payload,
		"pagerduty_event_triggered", "PagerDuty")
}
```

- [ ] **Step 4: Register both**

```go
	case "mattermost":
		return sendMattermost(ctx, node, rc)
	case "pagerduty":
		return sendPagerDuty(ctx, node, rc)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd backend && go test ./internal/engine/nodes/ -run 'TestMattermost|TestPagerDuty' -v
```

Expected: PASS, all five.

- [ ] **Step 6: Frontend wiring**

`data.ts`:

```ts
  { id: "mattermost", name: "Mattermost Message", desc: "Webhook post", icon: "mm", category: "Messaging" },
  { id: "pagerduty", name: "PagerDuty Alert", desc: "Trigger an incident", icon: "pd", category: "DevTools" },
```

`Inspector.tsx`:

```ts
  mattermost: {
    label: "Mattermost config",
    fields: [
      { kind: "secret", key: "mattermostWebhookURL", label: "Incoming Webhook URL", placeholder: "https://mattermost.example.com/hooks/xxx" },
      { kind: "config", key: "mattermostChannel", label: "Channel", hint: "optional", placeholder: "town-square" },
      { kind: "config", key: "mattermostUsername", label: "Post as", hint: "optional", placeholder: "AgentMesh" },
    ],
  },
  pagerduty: {
    label: "PagerDuty config",
    fields: [
      { kind: "secret", key: "pagerdutyRoutingKey", label: "Integration Routing Key", placeholder: "Events API v2 routing key" },
      { kind: "config", key: "pagerdutySeverity", label: "Severity", placeholder: "error", hint: "critical · error · warning · info" },
      { kind: "config", key: "pagerdutySource", label: "Source", hint: "optional", placeholder: "agentmesh" },
    ],
  },
```

- [ ] **Step 7: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(connectors): add Mattermost and PagerDuty connectors"
```

---

## Task 16: Zendesk, Monday.com

**Files:**
- Modify: `connectors_ops.go`, `connectors_ops_test.go`, `action.go`, `data.ts`, `Inspector.tsx`

**Interfaces:**
- Produces: `sendZendesk`, `SetZendeskAPIBaseForTest`, `sendMonday`, `SetMondayAPIBaseForTest`.

Zendesk: per-subdomain host, Basic auth as `{email}/token:{apiToken}`. Monday.com: a single GraphQL endpoint with an `Authorization` token header.

- [ ] **Step 1: Write the failing tests**

Append to `connectors_ops_test.go`:

```go
func TestZendeskAction_CreatesTicket(t *testing.T) {
	var gotUser, gotPass string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		_ = jsonDecode(r.Body, &gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	nodes.SetZendeskAPIBaseForTest(srv.URL)
	defer nodes.SetZendeskAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "zd1", Type: models.NodeTypeAction, Template: "zendesk",
		Secrets: map[string]string{"zendeskAPIToken": "zdtok"},
		Config: map[string]string{
			"zendeskSubdomain": "acme",
			"zendeskEmail":     "agent@acme.com",
		},
	}
	rc := engine.NewRunContext("r1", []byte(`"customer cannot log in"`))

	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "zendesk_ticket_created" {
		t.Errorf("want 'zendesk_ticket_created', got %v", result)
	}
	if gotUser != "agent@acme.com/token" || gotPass != "zdtok" {
		t.Errorf("want Zendesk's email/token basic auth, got %q/%q", gotUser, gotPass)
	}
	ticket, ok := gotBody["ticket"].(map[string]any)
	if !ok {
		t.Fatalf("want a ticket envelope, got %#v", gotBody)
	}
	comment := ticket["comment"].(map[string]any)
	if comment["body"] != "customer cannot log in" {
		t.Errorf("comment body: got %v", comment["body"])
	}
	if ticket["subject"] == "" || ticket["subject"] == nil {
		t.Error("want a non-empty subject derived from the message")
	}
}

func TestZendeskAction_SkipsWhenUnconfigured(t *testing.T) {
	cases := []struct {
		name string
		node models.WorkflowNode
		want string
	}{
		{"no token", models.WorkflowNode{Template: "zendesk",
			Config: map[string]string{"zendeskSubdomain": "acme", "zendeskEmail": "a@b.com"}},
			"zendesk_skipped_no_api_token"},
		{"no subdomain", models.WorkflowNode{Template: "zendesk",
			Secrets: map[string]string{"zendeskAPIToken": "t"},
			Config:  map[string]string{"zendeskEmail": "a@b.com"}},
			"zendesk_skipped_missing_config"},
		{"no email", models.WorkflowNode{Template: "zendesk",
			Secrets: map[string]string{"zendeskAPIToken": "t"},
			Config:  map[string]string{"zendeskSubdomain": "acme"}},
			"zendesk_skipped_missing_config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.node.Type = models.NodeTypeAction
			rc := engine.NewRunContext("r1", nil)
			got, _ := nodes.ExecuteAction(context.Background(), tc.node, rc)
			if got != tc.want {
				t.Errorf("want %q, got %v", tc.want, got)
			}
		})
	}
}

func TestMondayAction_CreatesItem(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = jsonDecode(r.Body, &gotBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{"create_item":{"id":"1"}}}`))
	}))
	defer srv.Close()
	nodes.SetMondayAPIBaseForTest(srv.URL)
	defer nodes.SetMondayAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "mo1", Type: models.NodeTypeAction, Template: "monday",
		Secrets: map[string]string{"mondayAPIKey": "mtok"},
		Config:  map[string]string{"mondayBoardID": "12345"},
	}
	rc := engine.NewRunContext("r1", []byte(`"follow up with the lead"`))

	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "monday_item_created" {
		t.Errorf("want 'monday_item_created', got %v", result)
	}
	if gotAuth != "mtok" {
		t.Errorf("Monday sends the raw token, not a Bearer prefix; got %q", gotAuth)
	}
	if _, ok := gotBody["query"].(string); !ok {
		t.Errorf("want a GraphQL query field, got %#v", gotBody)
	}
	vars, ok := gotBody["variables"].(map[string]any)
	if !ok {
		t.Fatalf("want GraphQL variables, got %#v", gotBody["variables"])
	}
	if vars["boardId"] != "12345" {
		t.Errorf("boardId: got %v", vars["boardId"])
	}
	if vars["itemName"] != "follow up with the lead" {
		t.Errorf("itemName: got %v", vars["itemName"])
	}
}

func TestMondayAction_SkipsWithoutBoardID(t *testing.T) {
	node := models.WorkflowNode{
		ID: "mo1", Type: models.NodeTypeAction, Template: "monday",
		Secrets: map[string]string{"mondayAPIKey": "mtok"},
	}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "monday_skipped_no_board_id" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/engine/nodes/ -run 'TestZendesk|TestMonday' -v
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `connectors_ops.go`:

```go
// zendeskAPIBase is overridden in tests via SetZendeskAPIBaseForTest.
// Normally "https://{subdomain}.zendesk.com" is built per-node, so the test
// override replaces the whole scheme+host and sendZendesk skips that
// construction when it is set. Same shape as mailchimpAPIBase.
var zendeskAPIBase = ""

// SetZendeskAPIBaseForTest overrides the Zendesk API base URL entirely.
// Call only from tests. Pass "" to reset to the real per-subdomain host.
func SetZendeskAPIBaseForTest(base string) { zendeskAPIBase = base }

// sendZendesk opens a support ticket with the run output as the first comment.
// Zendesk authenticates with Basic auth where the username is
// "{email}/token" and the password is the API token.
func sendZendesk(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "zendeskAPIToken")
	if token == "" {
		return "zendesk_skipped_no_api_token", ErrActionSkipped
	}
	subdomain := configVal(node, "zendeskSubdomain", "")
	email := configVal(node, "zendeskEmail", "")
	if subdomain == "" || email == "" {
		return "zendesk_skipped_missing_config", ErrActionSkipped
	}
	base := zendeskAPIBase
	if base == "" {
		base = "https://" + url.PathEscape(subdomain) + ".zendesk.com"
	}
	msg := rc.Message()
	payload := map[string]any{"ticket": map[string]any{
		"subject": issueTitle(msg),
		"comment": map[string]any{"body": msg},
	}}
	req, err := newJSONRequest(ctx, http.MethodPost, base+"/api/v2/tickets.json", nil, payload)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(email+"/token", token)
	return doAndCheck(req, "zendesk_ticket_created", "Zendesk")
}

// mondayAPIBase is overridden in tests via SetMondayAPIBaseForTest.
var mondayAPIBase = "https://api.monday.com"

// SetMondayAPIBaseForTest overrides the Monday.com API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetMondayAPIBaseForTest(base string) {
	if base == "" {
		mondayAPIBase = "https://api.monday.com"
	} else {
		mondayAPIBase = base
	}
}

// mondayCreateItem is the GraphQL mutation Monday.com's v2 API takes. Board
// IDs are ID! and item names String! — passed as variables rather than
// interpolated, so a message containing quotes cannot break the query.
const mondayCreateItem = `mutation ($boardId: ID!, $itemName: String!) {
  create_item(board_id: $boardId, item_name: $itemName) { id }
}`

// sendMonday creates a Monday.com board item named after the run output.
func sendMonday(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "mondayAPIKey")
	if apiKey == "" {
		return "monday_skipped_no_api_key", ErrActionSkipped
	}
	boardID := configVal(node, "mondayBoardID", "")
	if boardID == "" {
		return "monday_skipped_no_board_id", ErrActionSkipped
	}
	payload := map[string]any{
		"query": mondayCreateItem,
		"variables": map[string]any{
			"boardId":  boardID,
			"itemName": rc.Message(),
		},
	}
	// Monday.com expects the bare token, with no "Bearer " prefix.
	headers := map[string]string{"Authorization": apiKey, "API-Version": "2023-10"}
	return postJSON(ctx, mondayAPIBase+"/v2", headers, payload, "monday_item_created", "Monday.com")
}
```

Add `"net/url"` to `connectors_ops.go`'s imports if not already present. `issueTitle(message string) string` already exists in the package at `connector_helpers.go:168` (used by `sendTodoist`) — do not redefine it.

- [ ] **Step 4: Register both**

```go
	case "zendesk":
		return sendZendesk(ctx, node, rc)
	case "monday":
		return sendMonday(ctx, node, rc)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd backend && go test ./internal/engine/nodes/ -run 'TestZendesk|TestMonday' -v
```

Expected: PASS.

- [ ] **Step 6: Frontend wiring**

`data.ts`:

```ts
  { id: "zendesk", name: "Zendesk Ticket", desc: "Open a ticket", icon: "zd", category: "Productivity" },
  { id: "monday", name: "Monday.com Item", desc: "Create a board item", icon: "mo", category: "Productivity" },
```

`Inspector.tsx`:

```ts
  zendesk: {
    label: "Zendesk config",
    fields: [
      { kind: "secret", key: "zendeskAPIToken", label: "API Token", placeholder: "your Zendesk API token" },
      { kind: "config", key: "zendeskSubdomain", label: "Subdomain", placeholder: "acme (from acme.zendesk.com)" },
      { kind: "config", key: "zendeskEmail", label: "Agent email", placeholder: "agent@acme.com" },
    ],
  },
  monday: {
    label: "Monday.com config",
    fields: [
      { kind: "secret", key: "mondayAPIKey", label: "API Token", placeholder: "your Monday.com v2 token" },
      { kind: "config", key: "mondayBoardID", label: "Board ID", placeholder: "123456789" },
    ],
  },
```

- [ ] **Step 7: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(connectors): add Zendesk and Monday.com connectors"
```

---

## Task 17: Shopify, Pipedrive

**Files:**
- Modify: `connectors_commerce.go`, `connectors_commerce_test.go`, `action.go`, `data.ts`, `Inspector.tsx`

**Interfaces:**
- Produces: `sendShopify`, `SetShopifyAPIBaseForTest`, `sendPipedrive`, `SetPipedriveAPIBaseForTest`.

Shopify: per-store host, `X-Shopify-Access-Token` header. Pipedrive: per-company host, API token as a **query parameter** (not a header).

- [ ] **Step 1: Write the failing tests**

Append to `connectors_commerce_test.go`:

```go
func TestShopifyAction_CreatesCustomerNote(t *testing.T) {
	var gotToken, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Shopify-Access-Token")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	nodes.SetShopifyAPIBaseForTest(srv.URL)
	defer nodes.SetShopifyAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "sh1", Type: models.NodeTypeAction, Template: "shopify",
		Secrets: map[string]string{"shopifyAccessToken": "shpat_xxx"},
		Config:  map[string]string{"shopifyStore": "acme-store", "shopifyEmail": "buyer@example.com"},
	}
	rc := engine.NewRunContext("r1", []byte(`"VIP customer from the workflow"`))

	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "shopify_customer_created" {
		t.Errorf("want 'shopify_customer_created', got %v", result)
	}
	if gotToken != "shpat_xxx" {
		t.Errorf("want the Shopify access-token header, got %q", gotToken)
	}
	if gotPath != "/admin/api/2024-10/customers.json" {
		t.Errorf("want the versioned admin path, got %q", gotPath)
	}
	cust := gotBody["customer"].(map[string]any)
	if cust["email"] != "buyer@example.com" {
		t.Errorf("email: got %v", cust["email"])
	}
	if cust["note"] != "VIP customer from the workflow" {
		t.Errorf("note: got %v", cust["note"])
	}
}

func TestShopifyAction_SkipsWhenUnconfigured(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	noToken := models.WorkflowNode{Type: models.NodeTypeAction, Template: "shopify",
		Config: map[string]string{"shopifyStore": "s", "shopifyEmail": "a@b.com"}}
	if got, _ := nodes.ExecuteAction(context.Background(), noToken, rc); got != "shopify_skipped_no_access_token" {
		t.Errorf("want skip sentinel, got %v", got)
	}
	noStore := models.WorkflowNode{Type: models.NodeTypeAction, Template: "shopify",
		Secrets: map[string]string{"shopifyAccessToken": "t"},
		Config:  map[string]string{"shopifyEmail": "a@b.com"}}
	if got, _ := nodes.ExecuteAction(context.Background(), noStore, rc); got != "shopify_skipped_missing_config" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestPipedriveAction_CreatesNote(t *testing.T) {
	var gotQueryToken string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQueryToken = r.URL.Query().Get("api_token")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	nodes.SetPipedriveAPIBaseForTest(srv.URL)
	defer nodes.SetPipedriveAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "pi1", Type: models.NodeTypeAction, Template: "pipedrive",
		Secrets: map[string]string{"pipedriveAPIToken": "pdtok"},
		Config:  map[string]string{"pipedriveCompanyDomain": "acme", "pipedriveDealID": "77"},
	}
	rc := engine.NewRunContext("r1", []byte(`"call scheduled for Tuesday"`))

	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "pipedrive_note_created" {
		t.Errorf("want 'pipedrive_note_created', got %v", result)
	}
	if gotQueryToken != "pdtok" {
		t.Errorf("Pipedrive takes the token as a query param; got %q", gotQueryToken)
	}
	if gotBody["content"] != "call scheduled for Tuesday" {
		t.Errorf("content: got %v", gotBody["content"])
	}
	if gotBody["deal_id"] != "77" {
		t.Errorf("deal_id: got %v", gotBody["deal_id"])
	}
}

func TestPipedriveAction_SkipsWithoutToken(t *testing.T) {
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "pipedrive"}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "pipedrive_skipped_no_api_token" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}
```

Add `"encoding/json"` to `connectors_commerce_test.go`'s imports.

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/engine/nodes/ -run 'TestShopify|TestPipedrive' -v
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `connectors_commerce.go`:

```go
// shopifyAPIBase is overridden in tests via SetShopifyAPIBaseForTest.
// Normally "https://{store}.myshopify.com" is built per-node, so the test
// override replaces the whole scheme+host.
var shopifyAPIBase = ""

// SetShopifyAPIBaseForTest overrides the Shopify API base URL entirely.
// Call only from tests. Pass "" to reset to the real per-store host.
func SetShopifyAPIBaseForTest(base string) { shopifyAPIBase = base }

// shopifyAPIVersion pins the Admin API version. Shopify dates its API and
// removes versions after ~12 months — bumping this is a deliberate, tested
// change, not something to leave floating.
const shopifyAPIVersion = "2024-10"

// sendShopify creates a Shopify customer with the run output as the note.
func sendShopify(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "shopifyAccessToken")
	if token == "" {
		return "shopify_skipped_no_access_token", ErrActionSkipped
	}
	store := configVal(node, "shopifyStore", "")
	email := resolveTemplate(configVal(node, "shopifyEmail", ""), rc)
	if store == "" || email == "" {
		return "shopify_skipped_missing_config", ErrActionSkipped
	}
	base := shopifyAPIBase
	if base == "" {
		base = "https://" + url.PathEscape(store) + ".myshopify.com"
	}
	payload := map[string]any{"customer": map[string]any{
		"email": email,
		"note":  rc.Message(),
	}}
	headers := map[string]string{"X-Shopify-Access-Token": token}
	return postJSON(ctx, base+"/admin/api/"+shopifyAPIVersion+"/customers.json",
		headers, payload, "shopify_customer_created", "Shopify")
}

// pipedriveAPIBase is overridden in tests via SetPipedriveAPIBaseForTest.
var pipedriveAPIBase = ""

// SetPipedriveAPIBaseForTest overrides the Pipedrive API base URL entirely.
// Call only from tests. Pass "" to reset to the real per-company host.
func SetPipedriveAPIBaseForTest(base string) { pipedriveAPIBase = base }

// sendPipedrive logs a CRM note with the run output. Pipedrive takes its API
// token as a query parameter rather than a header.
func sendPipedrive(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "pipedriveAPIToken")
	if token == "" {
		return "pipedrive_skipped_no_api_token", ErrActionSkipped
	}
	base := pipedriveAPIBase
	if base == "" {
		domain := configVal(node, "pipedriveCompanyDomain", "")
		if domain == "" {
			return "pipedrive_skipped_missing_config", ErrActionSkipped
		}
		base = "https://" + url.PathEscape(domain) + ".pipedrive.com"
	}
	payload := map[string]any{"content": rc.Message()}
	if dealID := configVal(node, "pipedriveDealID", ""); dealID != "" {
		payload["deal_id"] = dealID
	}
	if personID := configVal(node, "pipedrivePersonID", ""); personID != "" {
		payload["person_id"] = personID
	}
	target := base + "/api/v1/notes?api_token=" + url.QueryEscape(token)
	return postJSON(ctx, target, nil, payload, "pipedrive_note_created", "Pipedrive")
}
```

`connectors_commerce.go` already imports `net/url` (from Task 13's Stripe form encoding).

- [ ] **Step 4: Register both**

```go
	case "shopify":
		return sendShopify(ctx, node, rc)
	case "pipedrive":
		return sendPipedrive(ctx, node, rc)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd backend && go test ./internal/engine/nodes/ -run 'TestShopify|TestPipedrive' -v
```

Expected: PASS.

- [ ] **Step 6: Frontend wiring**

`data.ts`:

```ts
  { id: "shopify", name: "Shopify Customer", desc: "Create a customer", icon: "sp", category: "Commerce" },
  { id: "pipedrive", name: "Pipedrive Note", desc: "Log a CRM note", icon: "pi", category: "Commerce" },
```

`Inspector.tsx`:

```ts
  shopify: {
    label: "Shopify config",
    fields: [
      { kind: "secret", key: "shopifyAccessToken", label: "Admin API Access Token", placeholder: "shpat_xxxxxxxxxxxx" },
      { kind: "config", key: "shopifyStore", label: "Store handle", placeholder: "acme-store (from acme-store.myshopify.com)" },
      { kind: "config", key: "shopifyEmail", label: "Customer email", placeholder: "buyer@example.com" },
    ],
  },
  pipedrive: {
    label: "Pipedrive config",
    fields: [
      { kind: "secret", key: "pipedriveAPIToken", label: "API Token", placeholder: "your Pipedrive API token" },
      { kind: "config", key: "pipedriveCompanyDomain", label: "Company domain", placeholder: "acme (from acme.pipedrive.com)" },
      { kind: "config", key: "pipedriveDealID", label: "Deal ID", hint: "optional", placeholder: "attach the note to a deal" },
      { kind: "config", key: "pipedrivePersonID", label: "Person ID", hint: "optional", placeholder: "attach the note to a person" },
    ],
  },
```

- [ ] **Step 7: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(connectors): add Shopify and Pipedrive connectors"
```

---

## Task 18: Postgres — revive the dead `db` node

**Files:**
- Create: `backend/internal/engine/nodes/connectors_db.go`, `connectors_db_test.go`
- Modify: `action.go`, `data.ts`, `Inspector.tsx`

**Interfaces:**
- Consumes: `github.com/jackc/pgx/v5` — **already in `go.mod:12`**, no new dependency.
- Produces: `func sendPostgres(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error)`.

The `db` template currently renders a green successful node that writes to nothing (`action.go` has no `case "db"`, so it hits `default: return "logged", nil`). This makes it real.

Config: `pgTable`, `pgColumn` (column receiving the run output), `pgExtraColumns` (optional JSON object of literal column→value pairs, templates resolved). Secret: `pgConnString`.

**Safety:** table and column names cannot be parameterised in SQL. They are validated against a strict identifier pattern and quoted — never interpolated raw.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/engine/nodes/connectors_db_test.go`:

```go
package nodes_test

import (
	"context"
	"os"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestPostgresAction_SkipsWithoutConnString(t *testing.T) {
	node := models.WorkflowNode{
		ID: "db1", Type: models.NodeTypeAction, Template: "db",
		Config: map[string]string{"pgTable": "events", "pgColumn": "payload"},
	}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "db_skipped_no_conn_string" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestPostgresAction_SkipsWithoutTableOrColumn(t *testing.T) {
	node := models.WorkflowNode{
		ID: "db1", Type: models.NodeTypeAction, Template: "db",
		Secrets: map[string]string{"pgConnString": "postgres://localhost/x"},
	}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "db_skipped_missing_config" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

// Identifiers reach SQL as text and cannot be bound as parameters, so they
// must be rejected rather than escaped-and-hoped.
func TestPostgresAction_RejectsUnsafeIdentifiers(t *testing.T) {
	for _, bad := range []string{
		`events; DROP TABLE users--`,
		`events"`,
		`ev ents`,
		`"events"`,
		``,
	} {
		node := models.WorkflowNode{
			ID: "db1", Type: models.NodeTypeAction, Template: "db",
			Secrets: map[string]string{"pgConnString": "postgres://localhost/x"},
			Config:  map[string]string{"pgTable": bad, "pgColumn": "payload"},
		}
		rc := engine.NewRunContext("r1", nil)
		got, err := nodes.ExecuteAction(context.Background(), node, rc)
		if err == nil && got != "db_skipped_missing_config" {
			t.Errorf("table %q should be rejected, got %v (err %v)", bad, got, err)
		}
	}
}

// Real insert against a live Postgres. Skipped unless TEST_POSTGRES_URL is set,
// so the default `go test ./...` needs no database.
func TestPostgresAction_InsertsRow(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_URL to run the live Postgres insert test")
	}
	node := models.WorkflowNode{
		ID: "db1", Type: models.NodeTypeAction, Template: "db",
		Secrets: map[string]string{"pgConnString": dsn},
		Config: map[string]string{
			"pgTable":  "agentmesh_test_events",
			"pgColumn": "payload",
		},
	}
	rc := engine.NewRunContext("r1", []byte(`"row from the workflow"`))
	got, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatalf("insert failed — create the table first:\n"+
			"  CREATE TABLE agentmesh_test_events (payload text);\n%v", err)
	}
	if got != "db_row_inserted" {
		t.Errorf("want 'db_row_inserted', got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestPostgresAction -v
```

Expected: FAIL — the skip tests return `"logged"` (the `default:` branch), not the sentinels.

- [ ] **Step 3: Implement**

Create `backend/internal/engine/nodes/connectors_db.go`:

```go
package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/agentmesh/backend/internal/models"
	"github.com/jackc/pgx/v5"
)

// pgIdentifier matches a safe unquoted SQL identifier. Table and column names
// cannot be sent as bind parameters — they are part of the statement text — so
// anything not matching this is rejected outright rather than escaped. An
// optional schema qualifier is allowed: "public.events".
var pgIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// quotePGIdentifier wraps each dot-separated part in double quotes. Only ever
// called on strings already validated by pgIdentifier.
func quotePGIdentifier(ident string) string {
	parts := strings.Split(ident, ".")
	for i, p := range parts {
		parts[i] = `"` + p + `"`
	}
	return strings.Join(parts, ".")
}

// sendPostgres inserts one row containing the run output. Values are always
// bound as parameters; only the table and column names are interpolated, and
// only after passing pgIdentifier.
func sendPostgres(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	connString := secretVal(node, "pgConnString")
	if connString == "" {
		return "db_skipped_no_conn_string", ErrActionSkipped
	}
	table := configVal(node, "pgTable", "")
	column := configVal(node, "pgColumn", "")
	if table == "" || column == "" {
		return "db_skipped_missing_config", ErrActionSkipped
	}
	if !pgIdentifier.MatchString(table) {
		return nil, fmt.Errorf("db: table name %q is not a valid SQL identifier", table)
	}
	if !pgIdentifier.MatchString(column) {
		return nil, fmt.Errorf("db: column name %q is not a valid SQL identifier", column)
	}

	cols := []string{quotePGIdentifier(column)}
	vals := []any{rc.Message()}

	if extra := configVal(node, "pgExtraColumns", ""); extra != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(extra), &m); err != nil {
			return nil, fmt.Errorf("db: `pgExtraColumns` is not a valid JSON object: %w", err)
		}
		for k, v := range m {
			if !pgIdentifier.MatchString(k) {
				return nil, fmt.Errorf("db: extra column name %q is not a valid SQL identifier", k)
			}
			cols = append(cols, quotePGIdentifier(k))
			if s, ok := v.(string); ok {
				vals = append(vals, resolveTemplate(s, rc))
				continue
			}
			vals = append(vals, v)
		}
	}

	placeholders := make([]string, len(vals))
	for i := range vals {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quotePGIdentifier(table), strings.Join(cols, ", "), strings.Join(placeholders, ", "))

	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("db: could not connect: %w", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, stmt, vals...); err != nil {
		return nil, fmt.Errorf("db: insert failed: %w", err)
	}
	return "db_row_inserted", nil
}
```

> **Note on `extra` column ordering:** Go map iteration is randomized, so `cols`/`vals` pairs are appended in an arbitrary but *consistent-with-each-other* order — column `i` always pairs with placeholder `$i+1`. That is correct. Do not "fix" it by sorting only one of the two slices.

- [ ] **Step 4: Register**

In `action.go`:

```go
	case "db":
		return sendPostgres(ctx, node, rc)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestPostgresAction -v
```

Expected: PASS — three run, the live one skips.

- [ ] **Step 6: Optionally run the live test**

```bash
psql "$TEST_POSTGRES_URL" -c 'CREATE TABLE IF NOT EXISTS agentmesh_test_events (payload text);'
cd backend && TEST_POSTGRES_URL="$TEST_POSTGRES_URL" go test ./internal/engine/nodes/ -run TestPostgresAction_InsertsRow -v
```

Expected: PASS. Then verify the row landed: `psql "$TEST_POSTGRES_URL" -c 'SELECT * FROM agentmesh_test_events;'`.

- [ ] **Step 7: Frontend wiring**

`data.ts` — the `db` entry already exists (`"Database Insert" / "Postgres / Neon"`). Keep it; only update the description if it now overpromises. Add the config schema in `Inspector.tsx`:

```ts
  db: {
    label: "Postgres config",
    fields: [
      { kind: "secret", key: "pgConnString", label: "Connection string", placeholder: "postgres://user:pass@host:5432/dbname" },
      { kind: "config", key: "pgTable", label: "Table", placeholder: "events" },
      { kind: "config", key: "pgColumn", label: "Output column", placeholder: "payload", hint: "receives the run output" },
      {
        kind: "config",
        key: "pgExtraColumns",
        label: "Extra columns (JSON)",
        hint: "optional",
        placeholder: '{"source":"agentmesh","city":"{{ node.n1.city }}"}',
      },
    ],
  },
```

- [ ] **Step 8: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/components/canvas/Inspector.tsx frontend/src/lib/data.ts
git commit -m "feat(connectors): implement the Postgres insert node

The db template rendered a green successful node with no backend case,
falling through to default and writing nothing. Backed by pgx (already a
dependency). Identifiers validated and quoted; values always bound."
```

---

## Task 19: RSS Feed Read — the first read-capable node

**Files:**
- Create: `backend/internal/engine/nodes/connectors_feed.go`, `connectors_feed_test.go`
- Modify: `action.go`, `data.ts`, `Inspector.tsx`

**Interfaces:**
- Consumes: `getRaw` (Task 12), `resolveTemplate` (Task 4). Does **not** depend on Task 9 — see the note in Step 3.
- Produces: `func fetchRSS(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error)`.

Zero credentials, zero cost, and the first node in the codebase that **returns data** rather than a sentinel. Config: `rssURL`, `rssLimit` (default 10).

- [ ] **Step 1: Write the failing test**

Create `backend/internal/engine/nodes/connectors_feed_test.go`:

```go
package nodes_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

const sampleRSS = `<?xml version="1.0"?>
<rss version="2.0"><channel>
  <title>Example Feed</title>
  <item><title>First post</title><link>https://example.com/1</link><pubDate>Mon, 04 Aug 2026 10:00:00 GMT</pubDate></item>
  <item><title>Second post</title><link>https://example.com/2</link><pubDate>Tue, 05 Aug 2026 10:00:00 GMT</pubDate></item>
  <item><title>Third post</title><link>https://example.com/3</link><pubDate>Wed, 06 Aug 2026 10:00:00 GMT</pubDate></item>
</channel></rss>`

func TestRSSAction_ReturnsItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "rss1", Type: models.NodeTypeAction, Template: "rss",
		Config: map[string]string{"rssURL": srv.URL},
	}
	rc := engine.NewRunContext("r1", nil)

	out, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want a structured result, got %T", out)
	}
	if got["title"] != "Example Feed" {
		t.Errorf("feed title: got %v", got["title"])
	}
	items, ok := got["items"].([]map[string]any)
	if !ok {
		t.Fatalf("want an items slice, got %T", got["items"])
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	if items[0]["title"] != "First post" {
		t.Errorf("first item title: got %v", items[0]["title"])
	}
	if items[0]["link"] != "https://example.com/1" {
		t.Errorf("first item link: got %v", items[0]["link"])
	}
}

func TestRSSAction_RespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "rss1", Type: models.NodeTypeAction, Template: "rss",
		Config: map[string]string{"rssURL": srv.URL, "rssLimit": "2"},
	}
	rc := engine.NewRunContext("r1", nil)
	out, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	items := out.(map[string]any)["items"].([]map[string]any)
	if len(items) != 2 {
		t.Errorf("want 2 items with rssLimit=2, got %d", len(items))
	}
}

func TestRSSAction_SkipsWithoutURL(t *testing.T) {
	node := models.WorkflowNode{ID: "rss1", Type: models.NodeTypeAction, Template: "rss"}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "rss_skipped_no_url" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestRSSAction_ErrorsOnMalformedFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not a feed at all"))
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "rss1", Type: models.NodeTypeAction, Template: "rss",
		Config: map[string]string{"rssURL": srv.URL},
	}
	rc := engine.NewRunContext("r1", nil)
	if _, err := nodes.ExecuteAction(context.Background(), node, rc); err == nil {
		t.Error("want an error for a non-feed response, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestRSSAction -v
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Create `backend/internal/engine/nodes/connectors_feed.go`:

```go
package nodes

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"

	"github.com/agentmesh/backend/internal/models"
)

// rssFeed is the subset of RSS 2.0 this node reads. Atom is not handled — a
// separate element tree — and is left for a follow-up rather than
// half-supported here.
type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			PubDate     string `xml:"pubDate"`
			Description string `xml:"description"`
			GUID        string `xml:"guid"`
		} `xml:"item"`
	} `xml:"channel"`
}

const rssDefaultLimit = 10

// fetchRSS reads an RSS 2.0 feed and returns its items as structured data.
// Unlike every other connector this returns a value rather than a sentinel —
// downstream nodes address it with {{ node.<id>.title }}.
func fetchRSS(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	feedURL := resolveTemplate(configVal(node, "rssURL", ""), rc)
	if feedURL == "" {
		return "rss_skipped_no_url", ErrActionSkipped
	}
	limit := rssDefaultLimit
	if raw := configVal(node, "rssLimit", ""); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("rss: `rssLimit` %q is not a positive number", raw)
		}
		limit = n
	}

	body, err := getRaw(ctx, feedURL, map[string]string{"Accept": "application/rss+xml, application/xml, text/xml"}, "RSS")
	if err != nil {
		return nil, err
	}

	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("rss: could not parse the feed: %w", err)
	}
	if feed.Channel.Title == "" && len(feed.Channel.Items) == 0 {
		return nil, errors.New("rss: the response is not an RSS 2.0 feed (no channel title or items found)")
	}

	items := make([]map[string]any, 0, limit)
	for i, it := range feed.Channel.Items {
		if i >= limit {
			break
		}
		items = append(items, map[string]any{
			"title":       it.Title,
			"link":        it.Link,
			"pubDate":     it.PubDate,
			"description": it.Description,
			"guid":        it.GUID,
		})
	}
	return map[string]any{
		"title": feed.Channel.Title,
		"count": len(items),
		"items": items,
	}, nil
}
```

> `decodeXMLNode` from Task 9 is not needed here — `encoding/xml`'s struct unmarshalling is a better fit for a known schema. Task 9's helper stays available for the generic `xml` node. If Task 9 has not landed yet, this task is still unblocked.

- [ ] **Step 4: Register**

```go
	case "rss":
		return fetchRSS(ctx, node, rc)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestRSSAction -v
```

Expected: PASS, all four.

- [ ] **Step 6: Frontend wiring**

`data.ts`:

```ts
  { id: "rss", name: "RSS Feed", desc: "Read a feed (no key)", icon: "rs", category: "Data" },
```

`Inspector.tsx`:

```ts
  rss: {
    label: "RSS config",
    fields: [
      { kind: "config", key: "rssURL", label: "Feed URL", placeholder: "https://example.com/feed.xml" },
      { kind: "config", key: "rssLimit", label: "Max items", hint: "optional, default 10", placeholder: "10" },
    ],
  },
```

- [ ] **Step 7: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(connectors): add RSS feed reader, the first data-returning node"
```

---

# Phase 2b — compute nodes that need a dependency

The only two tasks in this plan that touch `go.mod`. Kept in their own phase so the
dependency-free property of everything else stays legible, and so a reviewer can reject
these two without rejecting Phase 2.

## Task 20: `html_extract` — CSS-selector extraction

**Files:**
- Modify: `backend/internal/engine/nodes/compute.go`, `compute_test.go`, `tool.go`, `frontend/src/lib/data.ts`, `Inspector.tsx`
- Modify: `backend/go.mod`, `backend/go.sum`

**Interfaces:**
- Consumes: `configVal`, `RunContexter.Message`, `github.com/PuerkitoBio/goquery`.
- Produces: `func executeHTMLExtract(node models.WorkflowNode, rc RunContexter) (any, error)`.

Config keys: `htmlSelector` (CSS selector, required), `htmlAttr` (attribute to read; empty means text content), `htmlMode` — `first` (default, returns a string) or `all` (returns `[]string`).

This is a **pure computation** node: it parses HTML already in the run context — it does not fetch a URL. Pair it with the existing `http` tool to scrape.

- [ ] **Step 1: Add the dependency**

```bash
cd backend && go get github.com/PuerkitoBio/goquery@latest && go mod tidy
git diff --stat go.mod go.sum
```

Expected: `go.mod` gains `github.com/PuerkitoBio/goquery` and the indirect `golang.org/x/net`. Confirm nothing else appeared.

- [ ] **Step 2: Write the failing test**

Append to `backend/internal/engine/nodes/compute_test.go`:

```go
const sampleHTML = `<html><body>
  <h1 class="title">Main Heading</h1>
  <ul id="links">
    <li><a href="https://example.com/1">First</a></li>
    <li><a href="https://example.com/2">Second</a></li>
  </ul>
  <p class="empty"></p>
</body></html>`

func TestHTMLExtractFirstText(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", sampleHTML)
	node := models.WorkflowNode{
		ID: "h1", Type: models.NodeTypeTool, Template: "html_extract",
		Config: map[string]string{"htmlSelector": "h1.title"},
	}
	got, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Main Heading" {
		t.Errorf("want %q, got %q", "Main Heading", got)
	}
}

func TestHTMLExtractAllMode(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", sampleHTML)
	node := models.WorkflowNode{
		ID: "h1", Type: models.NodeTypeTool, Template: "html_extract",
		Config: map[string]string{"htmlSelector": "#links a", "htmlMode": "all"},
	}
	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.([]string)
	if !ok {
		t.Fatalf("want a []string in all mode, got %T", out)
	}
	if len(got) != 2 || got[0] != "First" || got[1] != "Second" {
		t.Errorf("got %#v", got)
	}
}

func TestHTMLExtractAttribute(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", sampleHTML)
	node := models.WorkflowNode{
		ID: "h1", Type: models.NodeTypeTool, Template: "html_extract",
		Config: map[string]string{"htmlSelector": "#links a", "htmlAttr": "href", "htmlMode": "all"},
	}
	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	got := out.([]string)
	if got[0] != "https://example.com/1" || got[1] != "https://example.com/2" {
		t.Errorf("got %#v", got)
	}
}

func TestHTMLExtractNoMatchIsEmptyNotError(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", sampleHTML)
	// first mode: no match -> empty string, not an error. A missing element on
	// a page is normal, not a failure worth halting the run for.
	first := models.WorkflowNode{
		ID: "h1", Type: models.NodeTypeTool, Template: "html_extract",
		Config: map[string]string{"htmlSelector": ".nope"},
	}
	got, err := nodes.ExecuteTool(context.Background(), first, rc)
	if err != nil {
		t.Fatalf("no match should not error: %v", err)
	}
	if got != "" {
		t.Errorf("want empty string, got %q", got)
	}
	// all mode: no match -> empty slice, never nil, so range works downstream.
	all := models.WorkflowNode{
		ID: "h1", Type: models.NodeTypeTool, Template: "html_extract",
		Config: map[string]string{"htmlSelector": ".nope", "htmlMode": "all"},
	}
	out, err := nodes.ExecuteTool(context.Background(), all, rc)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := out.([]string)
	if !ok || s == nil {
		t.Errorf("want a non-nil empty slice, got %#v", out)
	}
	if len(s) != 0 {
		t.Errorf("want length 0, got %d", len(s))
	}
}

func TestHTMLExtractRejectsBadSelector(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", sampleHTML)
	for _, sel := range []string{"", "a[["} {
		node := models.WorkflowNode{
			ID: "h1", Type: models.NodeTypeTool, Template: "html_extract",
			Config: map[string]string{"htmlSelector": sel},
		}
		if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
			t.Errorf("selector %q should error, got nil", sel)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestHTMLExtract -v
```

Expected: FAIL — falls through `ExecuteTool`'s `default:` and returns the raw HTML.

- [ ] **Step 4: Implement**

Append to `backend/internal/engine/nodes/compute.go`:

```go
// executeHTMLExtract runs a CSS selector over the upstream output. This parses
// HTML that is already in the run context — it does not fetch a URL. Chain it
// after the `http` tool to scrape a page.
//
// goquery panics on a malformed selector rather than returning an error, so the
// selector is compiled up front with cascadia (goquery's own selector engine)
// where failure is an ordinary error.
func executeHTMLExtract(node models.WorkflowNode, rc RunContexter) (any, error) {
	selector := configVal(node, "htmlSelector", "")
	if selector == "" {
		return nil, errors.New("html_extract: no `htmlSelector` configured")
	}
	sel, err := cascadia.Compile(selector)
	if err != nil {
		return nil, fmt.Errorf("html_extract: %q is not a valid CSS selector: %w", selector, err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rc.Message()))
	if err != nil {
		return nil, fmt.Errorf("html_extract: could not parse the HTML: %w", err)
	}
	attr := configVal(node, "htmlAttr", "")

	read := func(s *goquery.Selection) string {
		if attr == "" {
			return strings.TrimSpace(s.Text())
		}
		v, _ := s.Attr(attr)
		return v
	}

	matches := doc.FindMatcher(sel)
	if configVal(node, "htmlMode", "first") == "all" {
		// Non-nil even when empty, so downstream range/len never hit a nil.
		out := make([]string, 0, matches.Length())
		matches.Each(func(_ int, s *goquery.Selection) {
			out = append(out, read(s))
		})
		return out, nil
	}
	if matches.Length() == 0 {
		// A missing element is normal on a real page, not a run-halting error.
		return "", nil
	}
	return read(matches.First()), nil
}
```

Add to `compute.go`'s imports:

```go
	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
```

`cascadia` arrives transitively with goquery; `go mod tidy` in Step 1 promotes it to a direct requirement once imported. Re-run `go mod tidy` after this step.

- [ ] **Step 5: Register**

In `tool.go`'s `ExecuteTool` switch:

```go
	case "html_extract":
		return executeHTMLExtract(node, rc)
```

- [ ] **Step 6: Run test to verify it passes**

```bash
cd backend && go mod tidy && go test ./internal/engine/nodes/ -run TestHTMLExtract -v
```

Expected: PASS, all five.

- [ ] **Step 7: Frontend wiring**

`data.ts` `TOOL_TEMPLATES`:

```ts
  { id: "html_extract", name: "HTML Extract", desc: "CSS selector → text", icon: "⌸" },
```

`Inspector.tsx`:

```ts
  html_extract: {
    label: "HTML Extract config",
    fields: [
      { kind: "config", key: "htmlSelector", label: "CSS selector", placeholder: "h1.title" },
      { kind: "config", key: "htmlAttr", label: "Attribute", hint: "optional, blank = text", placeholder: "href" },
      { kind: "config", key: "htmlMode", label: "Mode", placeholder: "first", hint: "first · all" },
    ],
  },
```

- [ ] **Step 8: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/go.mod backend/go.sum backend/internal/engine/nodes \
        frontend/src/lib/data.ts frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(nodes): add HTML Extract compute node (goquery)"
```

---

## Task 21: `markdown` — Markdown to HTML

**Files:**
- Modify: `compute.go`, `compute_test.go`, `tool.go`, `data.ts`, `Inspector.tsx`, `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `github.com/yuin/goldmark`.
- Produces: `func executeMarkdown(node models.WorkflowNode, rc RunContexter) (any, error)`.

Config key: `mdGFM` — `"true"` (default) enables GitHub Flavored Markdown (tables, strikethrough, autolinks).

Direction is **Markdown → HTML** only. That is the direction that matters here: agents emit Markdown, and email/HTML destinations need HTML. HTML → Markdown would need a second dependency and is deferred (noted in D4).

- [ ] **Step 1: Add the dependency**

```bash
cd backend && go get github.com/yuin/goldmark@latest && go mod tidy
git diff --stat go.mod go.sum
```

Expected: `go.mod` gains `github.com/yuin/goldmark` and nothing else — goldmark has no dependencies of its own.

- [ ] **Step 2: Write the failing test**

Append to `compute_test.go`:

```go
func TestMarkdownRendersHTML(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "# Heading\n\nSome **bold** text and a [link](https://example.com).")
	node := models.WorkflowNode{ID: "m1", Type: models.NodeTypeTool, Template: "markdown"}

	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.(string)
	if !ok {
		t.Fatalf("want a string, got %T", out)
	}
	for _, want := range []string{"<h1>Heading</h1>", "<strong>bold</strong>", `href="https://example.com"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMarkdownGFMTablesOnByDefault(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "| A | B |\n|---|---|\n| 1 | 2 |")
	node := models.WorkflowNode{ID: "m1", Type: models.NodeTypeTool, Template: "markdown"}
	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.(string), "<table>") {
		t.Errorf("GFM tables should render by default, got:\n%s", out)
	}
}

func TestMarkdownGFMCanBeDisabled(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "| A | B |\n|---|---|\n| 1 | 2 |")
	node := models.WorkflowNode{
		ID: "m1", Type: models.NodeTypeTool, Template: "markdown",
		Config: map[string]string{"mdGFM": "false"},
	}
	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.(string), "<table>") {
		t.Errorf("tables should not render with mdGFM=false, got:\n%s", out)
	}
}

func TestMarkdownEmptyInputIsEmptyOutput(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "")
	node := models.WorkflowNode{ID: "m1", Type: models.NodeTypeTool, Template: "markdown"}
	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.(string)) != "" {
		t.Errorf("want empty output, got %q", out)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestMarkdown -v
```

Expected: FAIL — returns the raw Markdown unchanged.

- [ ] **Step 4: Implement**

Append to `compute.go`:

```go
// executeMarkdown renders the upstream output from Markdown into HTML.
// GitHub Flavored Markdown (tables, strikethrough, autolinks) is on by default
// because that is what LLM output actually looks like.
//
// Raw HTML embedded in the source is NOT passed through — goldmark escapes it
// unless WithUnsafe is set, and it is deliberately not set here: this node's
// input is frequently model output or third-party text, which is exactly the
// content you do not want emitting live HTML into an email.
func executeMarkdown(node models.WorkflowNode, rc RunContexter) (any, error) {
	opts := []goldmark.Option{}
	if configVal(node, "mdGFM", "true") != "false" {
		opts = append(opts, goldmark.WithExtensions(extension.GFM))
	}
	md := goldmark.New(opts...)

	var buf bytes.Buffer
	if err := md.Convert([]byte(rc.Message()), &buf); err != nil {
		return nil, fmt.Errorf("markdown: render failed: %w", err)
	}
	return buf.String(), nil
}
```

Add to `compute.go`'s imports:

```go
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
```

- [ ] **Step 5: Register**

```go
	case "markdown":
		return executeMarkdown(node, rc)
```

- [ ] **Step 6: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestMarkdown -v
```

Expected: PASS, all four.

- [ ] **Step 7: Frontend wiring**

`data.ts`:

```ts
  { id: "markdown", name: "Markdown → HTML", desc: "Render agent output", icon: "⌘" },
```

`Inspector.tsx`:

```ts
  markdown: {
    label: "Markdown config",
    fields: [
      {
        kind: "config",
        key: "mdGFM",
        label: "GitHub Flavored",
        placeholder: "true",
        hint: "true · false — tables, strikethrough, autolinks",
      },
    ],
  },
```

- [ ] **Step 8: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/go.mod backend/go.sum backend/internal/engine/nodes \
        frontend/src/lib/data.ts frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(nodes): add Markdown to HTML compute node (goldmark)"
```

---

# Phase 3b — generic GraphQL and free-API connectors

## Task 22: Generic GraphQL node

**Files:**
- Create: `backend/internal/engine/nodes/connectors_graphql.go`, `connectors_graphql_test.go`
- Modify: `action.go`, `data.ts`, `Inspector.tsx`

**Interfaces:**
- Consumes: `doAndDecode` (Task 12), `resolveTemplate` (Task 4), `newJSONRequest`.
- Produces: `func sendGraphQL(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error)`.

The highest-leverage node in this phase. Linear, GitHub v4, Shopify Storefront and Monday.com are GraphQL-only or GraphQL-first; one generic node reaches all of them without a bespoke connector each. Task 16's Monday.com connector is this exact shape hand-rolled — this generalises it.

Config: `graphqlEndpoint`, `graphqlQuery`, `graphqlVariables` (JSON object, templates resolved). Secret: `graphqlAuthHeader` (sent verbatim as `Authorization`, so the user controls whether it carries a `Bearer ` prefix — Monday.com wants a bare token, GitHub wants `Bearer`).

**Returns the decoded response**, not a sentinel — like RSS, this is a read node.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/engine/nodes/connectors_graphql_test.go`:

```go
package nodes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestGraphQLAction_PostsQueryAndReturnsData(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"viewer":{"login":"octocat"}}}`))
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "gq1", Type: models.NodeTypeAction, Template: "graphql",
		Secrets: map[string]string{"graphqlAuthHeader": "Bearer ghp_xxx"},
		Config: map[string]string{
			"graphqlEndpoint":  srv.URL,
			"graphqlQuery":     "query { viewer { login } }",
			"graphqlVariables": `{"first":10,"search":"{{ input }}"}`,
		},
	}
	rc := engine.NewRunContext("r1", []byte(`"octo"`))

	out, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer ghp_xxx" {
		t.Errorf("auth header should be sent verbatim, got %q", gotAuth)
	}
	if gotBody["query"] != "query { viewer { login } }" {
		t.Errorf("query: got %v", gotBody["query"])
	}
	vars := gotBody["variables"].(map[string]any)
	if vars["first"] != float64(10) {
		t.Errorf("non-string variables must survive unchanged, got %#v", vars["first"])
	}
	if vars["search"] != "octo" {
		t.Errorf("string variables should resolve templates, got %v", vars["search"])
	}
	// The decoded response is the node's output, not a sentinel.
	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want the decoded response, got %T", out)
	}
	data := res["data"].(map[string]any)
	if data["viewer"].(map[string]any)["login"] != "octocat" {
		t.Errorf("got %#v", res)
	}
}

// A GraphQL server returns 200 with an "errors" array rather than an HTTP
// error status. Silently succeeding on that would be the db-node bug again.
func TestGraphQLAction_SurfacesGraphQLErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errors":[{"message":"Field 'nope' doesn't exist"}]}`))
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "gq1", Type: models.NodeTypeAction, Template: "graphql",
		Config: map[string]string{"graphqlEndpoint": srv.URL, "graphqlQuery": "query { nope }"},
	}
	rc := engine.NewRunContext("r1", nil)
	_, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err == nil {
		t.Fatal("want an error when the response carries a GraphQL errors array, got nil")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should quote the server's message, got %v", err)
	}
}

func TestGraphQLAction_SkipsWhenUnconfigured(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	noEndpoint := models.WorkflowNode{Type: models.NodeTypeAction, Template: "graphql",
		Config: map[string]string{"graphqlQuery": "query { x }"}}
	if got, _ := nodes.ExecuteAction(context.Background(), noEndpoint, rc); got != "graphql_skipped_no_endpoint" {
		t.Errorf("want skip sentinel, got %v", got)
	}
	noQuery := models.WorkflowNode{Type: models.NodeTypeAction, Template: "graphql",
		Config: map[string]string{"graphqlEndpoint": "https://api.example.com/graphql"}}
	if got, _ := nodes.ExecuteAction(context.Background(), noQuery, rc); got != "graphql_skipped_no_query" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestGraphQLAction_RejectsMalformedVariables(t *testing.T) {
	node := models.WorkflowNode{
		Type: models.NodeTypeAction, Template: "graphql",
		Config: map[string]string{
			"graphqlEndpoint":  "https://api.example.com/graphql",
			"graphqlQuery":     "query { x }",
			"graphqlVariables": `{"broken": }`,
		},
	}
	rc := engine.NewRunContext("r1", nil)
	if _, err := nodes.ExecuteAction(context.Background(), node, rc); err == nil {
		t.Error("want an error for malformed graphqlVariables, got nil")
	}
}
```

Add `"strings"` to this test file's imports.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestGraphQLAction -v
```

Expected: FAIL — falls through to `default:` and returns `"logged"`.

- [ ] **Step 3: Implement**

Create `backend/internal/engine/nodes/connectors_graphql.go`:

```go
package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// sendGraphQL POSTs a GraphQL query to any endpoint and returns the decoded
// response. Generic on purpose: Linear, GitHub v4, Shopify Storefront and
// Monday.com are all reachable through this without a bespoke connector.
func sendGraphQL(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	endpoint := resolveTemplate(configVal(node, "graphqlEndpoint", ""), rc)
	if endpoint == "" {
		return "graphql_skipped_no_endpoint", ErrActionSkipped
	}
	query := configVal(node, "graphqlQuery", "")
	if query == "" {
		return "graphql_skipped_no_query", ErrActionSkipped
	}

	payload := map[string]any{"query": query}
	if raw := configVal(node, "graphqlVariables", ""); raw != "" {
		var vars map[string]any
		if err := json.Unmarshal([]byte(raw), &vars); err != nil {
			return nil, fmt.Errorf("graphql: `graphqlVariables` is not a valid JSON object: %w", err)
		}
		// Only string values are template-expanded; numbers, booleans and
		// nested objects pass through untouched so their JSON types survive.
		for k, v := range vars {
			if s, ok := v.(string); ok {
				vars[k] = resolveTemplate(s, rc)
			}
		}
		payload["variables"] = vars
	}

	headers := map[string]string{}
	// Sent verbatim: Monday.com wants a bare token, GitHub wants "Bearer ...".
	// Prefixing here would break one of them.
	if auth := secretVal(node, "graphqlAuthHeader"); auth != "" {
		headers["Authorization"] = auth
	}

	req, err := newJSONRequest(ctx, http.MethodPost, endpoint, headers, payload)
	if err != nil {
		return nil, fmt.Errorf("graphql: %w", err)
	}
	out, err := doAndDecode(req, "GraphQL")
	if err != nil {
		return nil, err
	}

	// GraphQL reports failures as a 200 carrying an "errors" array. Returning
	// that as success would render a green node for a query that did nothing.
	if body, ok := out.(map[string]any); ok {
		if errs, ok := body["errors"].([]any); ok && len(errs) > 0 {
			return nil, fmt.Errorf("graphql: server returned errors: %s", graphQLErrorText(errs))
		}
	}
	return out, nil
}

// graphQLErrorText joins the "message" field of each GraphQL error for the
// wrapped error string, falling back to the raw JSON for non-standard shapes.
func graphQLErrorText(errs []any) string {
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		if m, ok := e.(map[string]any); ok {
			if s, ok := m["message"].(string); ok {
				msgs = append(msgs, s)
				continue
			}
		}
		b, _ := json.Marshal(e)
		msgs = append(msgs, string(b))
	}
	return strings.Join(msgs, "; ")
}
```

- [ ] **Step 4: Register**

```go
	case "graphql":
		return sendGraphQL(ctx, node, rc)
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestGraphQLAction -v
```

Expected: PASS, all four.

- [ ] **Step 6: Frontend wiring**

`data.ts`:

```ts
  { id: "graphql", name: "GraphQL Query", desc: "Any GraphQL endpoint", icon: "gq", category: "DevTools" },
```

`Inspector.tsx`:

```ts
  graphql: {
    label: "GraphQL config",
    fields: [
      { kind: "config", key: "graphqlEndpoint", label: "Endpoint", placeholder: "https://api.github.com/graphql" },
      { kind: "config", key: "graphqlQuery", label: "Query", placeholder: "query { viewer { login } }" },
      {
        kind: "config",
        key: "graphqlVariables",
        label: "Variables (JSON)",
        hint: "optional",
        placeholder: '{"first":10,"search":"{{ result }}"}',
      },
      {
        kind: "secret",
        key: "graphqlAuthHeader",
        label: "Authorization header",
        hint: "sent verbatim — include Bearer if the API wants it",
        placeholder: "Bearer ghp_xxxxxxxx",
      },
    ],
  },
```

- [ ] **Step 7: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(connectors): add generic GraphQL node"
```

---

## Task 23: HackerNews, CoinGecko, QuickChart

**Files:**
- Modify: `connectors_feed.go`, `connectors_feed_test.go`, `action.go`, `compute.go`, `tool.go`, `data.ts`, `Inspector.tsx`

**Interfaces:**
- Consumes: `getAndDecode` (Task 12), `configVal`, `resolveTemplate`.
- Produces: `func fetchHackerNews(ctx, node, rc) (any, error)`, `func fetchCoinGecko(ctx, node, rc) (any, error)`, `func executeQuickChart(node models.WorkflowNode, rc RunContexter) (any, error)`.

All three need **no credential of any kind**.

> **Node-type split, and why it matters for billing (C2b):** HackerNews and CoinGecko make network calls, so they are `NodeTypeAction` and are charged the flat action fee. QuickChart only *builds a URL* — it makes no request at all — so it is a `NodeTypeTool`, which `BillableFlatFee` leaves free. Do not "fix" this inconsistency by making them uniform; the split is the billing rule working correctly.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/engine/nodes/connectors_feed_test.go`:

```go
func TestHackerNewsAction_SearchesStories(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hits":[
			{"title":"Show HN: a thing","url":"https://example.com/a","points":120,"objectID":"1"},
			{"title":"Ask HN: another","url":"https://example.com/b","points":80,"objectID":"2"}
		]}`))
	}))
	defer srv.Close()
	nodes.SetHackerNewsAPIBaseForTest(srv.URL)
	defer nodes.SetHackerNewsAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "hn1", Type: models.NodeTypeAction, Template: "hackernews",
		Config: map[string]string{"hnQuery": "{{ input }}"},
	}
	rc := engine.NewRunContext("r1", []byte(`"golang"`))

	out, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "golang" {
		t.Errorf("query should resolve templates, got %q", gotQuery)
	}
	res := out.(map[string]any)
	items := res["items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if items[0]["title"] != "Show HN: a thing" {
		t.Errorf("got %#v", items[0])
	}
	if items[0]["points"] != float64(120) {
		t.Errorf("points: got %#v", items[0]["points"])
	}
}

func TestHackerNewsAction_SkipsWithoutQuery(t *testing.T) {
	node := models.WorkflowNode{ID: "hn1", Type: models.NodeTypeAction, Template: "hackernews"}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "hackernews_skipped_no_query" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestCoinGeckoAction_ReturnsPrices(t *testing.T) {
	var gotIDs, gotVs string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIDs = r.URL.Query().Get("ids")
		gotVs = r.URL.Query().Get("vs_currencies")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"bitcoin":{"usd":64000.5},"ethereum":{"usd":3100}}`))
	}))
	defer srv.Close()
	nodes.SetCoinGeckoAPIBaseForTest(srv.URL)
	defer nodes.SetCoinGeckoAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "cg1", Type: models.NodeTypeAction, Template: "coingecko",
		Config: map[string]string{"cgIDs": "bitcoin,ethereum"},
	}
	rc := engine.NewRunContext("r1", nil)

	out, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if gotIDs != "bitcoin,ethereum" {
		t.Errorf("ids: got %q", gotIDs)
	}
	if gotVs != "usd" {
		t.Errorf("want the default vs_currency usd, got %q", gotVs)
	}
	res := out.(map[string]any)
	btc := res["bitcoin"].(map[string]any)
	if btc["usd"] != float64(64000.5) {
		t.Errorf("got %#v", res)
	}
}

func TestCoinGeckoAction_SkipsWithoutIDs(t *testing.T) {
	node := models.WorkflowNode{ID: "cg1", Type: models.NodeTypeAction, Template: "coingecko"}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "coingecko_skipped_no_ids" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestQuickChartToolBuildsURL(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{
		ID: "qc1", Type: models.NodeTypeTool, Template: "quickchart",
		Config: map[string]string{
			"qcConfig": `{"type":"bar","data":{"labels":["a","b"],"datasets":[{"data":[1,2]}]}}`,
			"qcWidth":  "600",
		},
	}
	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(string)
	if !strings.HasPrefix(got, "https://quickchart.io/chart?") {
		t.Fatalf("want a quickchart URL, got %q", got)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("w") != "600" {
		t.Errorf("width: got %q", u.Query().Get("w"))
	}
	// The chart config must be URL-encoded, not spliced in raw.
	if u.Query().Get("c") != `{"type":"bar","data":{"labels":["a","b"],"datasets":[{"data":[1,2]}]}}` {
		t.Errorf("chart config: got %q", u.Query().Get("c"))
	}
}

func TestQuickChartToolRejectsInvalidConfig(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	for _, cfg := range []string{"", `{"type": }`} {
		node := models.WorkflowNode{
			ID: "qc1", Type: models.NodeTypeTool, Template: "quickchart",
			Config: map[string]string{"qcConfig": cfg},
		}
		if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
			t.Errorf("config %q should error, got nil", cfg)
		}
	}
}
```

Add `"net/url"` and `"strings"` to `connectors_feed_test.go`'s imports.

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && go test ./internal/engine/nodes/ -run 'TestHackerNews|TestCoinGecko|TestQuickChart' -v
```

Expected: FAIL — undefined test helpers, and both tools/actions fall through to their `default:` branches.

- [ ] **Step 3: Implement the two fetchers**

Append to `backend/internal/engine/nodes/connectors_feed.go`:

```go
// hackerNewsAPIBase is overridden in tests via SetHackerNewsAPIBaseForTest.
// This is Algolia's HN search API — one request returns matching stories,
// unlike the Firebase API which needs an N+1 fetch per item id. No auth.
var hackerNewsAPIBase = "https://hn.algolia.com/api/v1"

// SetHackerNewsAPIBaseForTest overrides the HN search API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetHackerNewsAPIBaseForTest(base string) {
	if base == "" {
		hackerNewsAPIBase = "https://hn.algolia.com/api/v1"
	} else {
		hackerNewsAPIBase = base
	}
}

// fetchHackerNews searches Hacker News. No credential of any kind.
func fetchHackerNews(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	query := resolveTemplate(configVal(node, "hnQuery", ""), rc)
	if query == "" {
		return "hackernews_skipped_no_query", ErrActionSkipped
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("tags", configVal(node, "hnTags", "story"))
	target := hackerNewsAPIBase + "/search?" + q.Encode()

	raw, err := getAndDecode(ctx, target, nil, "HackerNews")
	if err != nil {
		return nil, err
	}
	body, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hackernews: unexpected response shape %T", raw)
	}
	hits, _ := body["hits"].([]any)

	limit := rssDefaultLimit
	if s := configVal(node, "hnLimit", ""); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("hackernews: `hnLimit` %q is not a positive number", s)
		}
		limit = n
	}

	items := make([]map[string]any, 0, limit)
	for i, h := range hits {
		if i >= limit {
			break
		}
		hit, ok := h.(map[string]any)
		if !ok {
			continue
		}
		id, _ := hit["objectID"].(string)
		items = append(items, map[string]any{
			"title":   hit["title"],
			"url":     hit["url"],
			"points":  hit["points"],
			"author":  hit["author"],
			"hnURL":   "https://news.ycombinator.com/item?id=" + id,
			"created": hit["created_at"],
		})
	}
	return map[string]any{"count": len(items), "items": items}, nil
}

// coinGeckoAPIBase is overridden in tests via SetCoinGeckoAPIBaseForTest.
// CoinGecko's /simple/price endpoint is usable without an API key.
var coinGeckoAPIBase = "https://api.coingecko.com/api/v3"

// SetCoinGeckoAPIBaseForTest overrides the CoinGecko API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetCoinGeckoAPIBaseForTest(base string) {
	if base == "" {
		coinGeckoAPIBase = "https://api.coingecko.com/api/v3"
	} else {
		coinGeckoAPIBase = base
	}
}

// fetchCoinGecko returns spot prices for the configured coin ids. No key.
func fetchCoinGecko(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	ids := resolveTemplate(configVal(node, "cgIDs", ""), rc)
	if ids == "" {
		return "coingecko_skipped_no_ids", ErrActionSkipped
	}
	q := url.Values{}
	q.Set("ids", ids)
	q.Set("vs_currencies", configVal(node, "cgCurrencies", "usd"))
	return getAndDecode(ctx, coinGeckoAPIBase+"/simple/price?"+q.Encode(), nil, "CoinGecko")
}
```

Add `"net/url"` to `connectors_feed.go`'s imports (`context`, `fmt`, `strconv` are already there from Task 19).

- [ ] **Step 4: Implement QuickChart as a tool**

Append to `backend/internal/engine/nodes/compute.go`:

```go
// quickChartBase is the public QuickChart renderer. This node builds a URL and
// makes NO request — the image is fetched by whoever renders the URL (Slack,
// an email client, a browser). That is why it is a NodeTypeTool (free under
// BillableFlatFee) rather than an action.
const quickChartBase = "https://quickchart.io/chart"

// executeQuickChart returns a QuickChart image URL for a Chart.js config.
func executeQuickChart(node models.WorkflowNode, rc RunContexter) (any, error) {
	cfg := resolveTemplate(configVal(node, "qcConfig", ""), rc)
	if cfg == "" {
		return nil, errors.New("quickchart: no `qcConfig` configured — set a Chart.js config object")
	}
	// Validate before handing the user a URL that renders an error image.
	var probe any
	if err := json.Unmarshal([]byte(cfg), &probe); err != nil {
		return nil, fmt.Errorf("quickchart: `qcConfig` is not valid JSON: %w", err)
	}
	q := url.Values{}
	q.Set("c", cfg)
	if w := configVal(node, "qcWidth", ""); w != "" {
		q.Set("w", w)
	}
	if h := configVal(node, "qcHeight", ""); h != "" {
		q.Set("h", h)
	}
	return quickChartBase + "?" + q.Encode(), nil
}
```

Add `"net/url"` to `compute.go`'s imports.

- [ ] **Step 5: Register all three**

In `action.go`'s `ExecuteAction` switch:

```go
	case "hackernews":
		return fetchHackerNews(ctx, node, rc)
	case "coingecko":
		return fetchCoinGecko(ctx, node, rc)
```

In `tool.go`'s `ExecuteTool` switch:

```go
	case "quickchart":
		return executeQuickChart(node, rc)
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
cd backend && go test ./internal/engine/nodes/ -run 'TestHackerNews|TestCoinGecko|TestQuickChart' -v
```

Expected: PASS, all six.

- [ ] **Step 7: Frontend wiring**

`data.ts` `ACTION_TEMPLATES`:

```ts
  { id: "hackernews", name: "Hacker News", desc: "Search stories (no key)", icon: "hn", category: "Data" },
  { id: "coingecko", name: "CoinGecko Price", desc: "Spot prices (no key)", icon: "cg", category: "Data" },
```

`data.ts` `TOOL_TEMPLATES`:

```ts
  { id: "quickchart", name: "QuickChart", desc: "Chart image URL", icon: "▦" },
```

`Inspector.tsx`:

```ts
  hackernews: {
    label: "Hacker News config",
    fields: [
      { kind: "config", key: "hnQuery", label: "Search query", placeholder: "{{ result }}" },
      { kind: "config", key: "hnTags", label: "Tags", hint: "optional", placeholder: "story · comment · show_hn · ask_hn" },
      { kind: "config", key: "hnLimit", label: "Max items", hint: "optional, default 10", placeholder: "10" },
    ],
  },
  coingecko: {
    label: "CoinGecko config",
    fields: [
      { kind: "config", key: "cgIDs", label: "Coin IDs", placeholder: "bitcoin,ethereum" },
      { kind: "config", key: "cgCurrencies", label: "Currencies", hint: "optional, default usd", placeholder: "usd,eur" },
    ],
  },
  quickchart: {
    label: "QuickChart config",
    fields: [
      {
        kind: "config",
        key: "qcConfig",
        label: "Chart.js config (JSON)",
        placeholder: '{"type":"bar","data":{"labels":["a","b"],"datasets":[{"data":[1,2]}]}}',
      },
      { kind: "config", key: "qcWidth", label: "Width", hint: "optional", placeholder: "600" },
      { kind: "config", key: "qcHeight", label: "Height", hint: "optional", placeholder: "400" },
    ],
  },
```

- [ ] **Step 8: Verify and commit**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd .. && git add backend/internal/engine/nodes frontend/src/lib/data.ts frontend/src/components/canvas/Inspector.tsx
git commit -m "feat(connectors): add HackerNews, CoinGecko and QuickChart (no credentials)"
```

---

## Task 24: Phase 3 integration check

**Files:**
- Test: `backend/internal/engine/nodes/action_test.go` (modify)

**Interfaces:**
- Consumes: every connector registered in Tasks 13-19 and 22-23.
- Produces: a table test asserting each new template is dispatched rather than silently swallowed by `default:`.

The `default: return "logged", nil` branch is what let the `db` node lie for so long. This makes that failure mode impossible to reintroduce silently.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/engine/nodes/action_test.go`:

```go
// Every template offered in the palette must reach a real implementation.
// A template that falls through to ExecuteAction's `default:` returns
// "logged" and renders a green, successful node that did nothing — exactly
// the bug the db template shipped with.
func TestEveryNewActionTemplateIsDispatched(t *testing.T) {
	templates := []string{
		"stripe", "twilio", "mattermost", "pagerduty",
		"zendesk", "monday", "shopify", "pipedrive", "db", "rss",
		"graphql", "hackernews", "coingecko",
	}
	for _, tpl := range templates {
		t.Run(tpl, func(t *testing.T) {
			node := models.WorkflowNode{ID: "n1", Type: models.NodeTypeAction, Template: tpl}
			rc := engine.NewRunContext("r1", nil)
			got, _ := nodes.ExecuteAction(context.Background(), node, rc)
			if got == "logged" {
				t.Errorf("template %q fell through to ExecuteAction's default branch — "+
					"it renders as a successful node but does nothing", tpl)
			}
		})
	}
}
```

Every listed template is unconfigured here, so each must return its own `*_skipped_*` sentinel — never `"logged"`.

- [ ] **Step 2: Run test to verify it passes**

```bash
cd backend && go test ./internal/engine/nodes/ -run TestEveryNewActionTemplateIsDispatched -v
```

Expected: PASS (all thirteen registered by Tasks 13-19 and 22-23). If any subtest fails, that connector's `case` is missing from `action.go`.

> `quickchart` is **not** in this list — it is a `NodeTypeTool`, not an action, so it never reaches `ExecuteAction`. Task 23 covers it separately.

- [ ] **Step 3: Confirm the test can actually fail**

```bash
cd backend
# Temporarily comment out the `case "rss":` line in action.go, then:
go test ./internal/engine/nodes/ -run TestEveryNewActionTemplateIsDispatched 2>&1 | head
# Restore it.
git diff internal/engine/nodes/action.go   # must be empty after restoring
```

Expected: FAIL for `rss` while commented out, PASS after restoring, and a clean `git diff`.

- [ ] **Step 4: Full verification**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
```

Expected: everything green, **including `TestX402PaymentPathIsFrozen`** — no task in Phases 1-3b touched a frozen file.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/nodes/action_test.go
git commit -m "test(nodes): assert every action template is dispatched, not defaulted"
```

---

# Phase 4 — Manual end-to-end verification

## Task 25: Click-through in the real app

**Files:** none — this is verification, not code.

- [ ] **Step 1: Start the stack**

```bash
cd backend && go run ./cmd/server
# separate terminal:
cd frontend && pnpm dev
```

- [ ] **Step 2: Build a workflow that exercises the new plumbing**

On the canvas, wire: **Chat trigger → AI Agent → Edit Fields → Text Template → Slack**.

- Configure Edit Fields with `setFields` = `{"answer":"{{ result }}","asked":"{{ input }}"}`.
- Configure Text Template with `templateText` = `Q: {{ node.<editFieldsNodeID> .asked }} / A: {{ node.<editFieldsNodeID>.answer }}` (substituting the real node ID shown in the Inspector).

- [ ] **Step 3: Run it and confirm the plumbing**

Run the workflow. In the run console, confirm the Slack node received the **composed template text**, not a random upstream output. Re-run three times and confirm the output is **identical each time** — this is the Task 2 determinism fix observed end-to-end.

- [ ] **Step 4: Confirm the x402 and Tendril tabs are untouched**

Open the palette's **x402** and **Tendril** tabs. Confirm every entry is present and unchanged, and that dragging an existing x402 node onto the canvas still shows its Discover / pricing UI exactly as before.

- [ ] **Step 5: Run one real x402 workflow**

Run an existing, known-good x402 workflow end to end. Confirm from the run console and the settlement receipt that both legs still settle: Wallet 1 → Wallet 2 (inbound) and Wallet 2 → provider (outbound). **This is the real acceptance test for C1** — the freeze guard proves the source didn't change, this proves the behaviour didn't either.

> If anything about the payment path looks different, stop and report it before proceeding. Do not attempt a fix in this plan's scope.

- [ ] **Step 6: Record the result**

Note in the PR description: which workflows were run, the settlement txids observed, and confirmation that the run console showed the correct node count (not "0/0").

---

# Phase 5 — Deferred, specified but NOT implemented here

Do **not** start these as part of this plan. They are recorded so the sequencing decision is explicit rather than forgotten.

### D1. Tier 1b — control flow (`If`, `Switch`, `Filter`, `Merge`, `Loop`)

`TopologicalSort` (`graph.go:11-66`) groups nodes into levels and `runner.go` executes **every node in every level, unconditionally**. There is no skip mechanism, so conditional branching is not merely unimplemented — it is unrepresentable.

Required first:
- An edge-level "this branch was not taken" signal, and a `runner.go` pass that transitively marks the downstream of an untaken branch as skipped (not failed — the run console must distinguish them).
- A decision on item-array semantics (n8n runs downstream nodes once per item; AgentMesh passes one value). `Loop`/`Filter` are meaningless without it, `If`/`Switch` are not.
- Billing implications: N iterations of a loop containing a paid node is N charges. `BillableFlatFee` (`runner.go:848`) currently assumes one call per node.

**This is where the x402 risk actually lives.** A loop containing a `tool402` node changes how many times real USDC moves. Any control-flow work must treat the payment path as read-only and get separate sign-off.

### D2. OAuth refresh, then Google Sheets and Gmail

A generic OAuth2 link flow already exists — `backend/internal/api/handlers/connector_oauth.go` covers 12 providers (Slack, GitHub, Notion, Airtable, HubSpot, Asana, ClickUp, Jira, Linear, Mailchimp, GitLab, Todoist). The prior plan's claim that there is "no OAuth2" is **out of date**.

What is genuinely missing:
- **Token refresh.** The file documents this gap itself at `:146-151`, `:170-175`, `:188-193`, `:234-240`. Tokens are exchanged once and written onto the node's secrets map; nothing refreshes them. HubSpot/Asana/Airtable access tokens expire in about an hour.
- **Atlassian rotates refresh tokens on every use** (`:234-240`) — a refresh implementation must persist the new refresh token each time or the next refresh fails.
- **Credential reuse.** Tokens live per-node, so the same account pasted into five nodes expires five times. A shared `oauth_credentials` table (next migration number: `000020`) is the fix.

Google Sheets and Gmail are the two highest-usage n8n nodes and both need this. Sequence: refresh + shared credentials → add Google as a provider → Sheets → Gmail. Start Google's OAuth verification paperwork early; restricted Gmail scopes are reviewed by a human and approval is not instant.

### D3. Scheduler (`cron` trigger)

The `cron` trigger is in the palette with no backend at all. A real implementation needs a `scheduled_workflows` table, an advisory-lock-guarded ticker safe under multiple backend replicas, and a run-enqueue path. A naive in-process `time.Ticker` double-fires on every deploy with more than one instance.

### D4. Nodes deliberately excluded

- **`code`** — needs a real sandbox. Note that metered Python execution already exists via the Tendril "Run a Job" node.
- **`vector`, `memory`** — need a vector store and cross-run persistence respectively; both carry recurring cost.
- **Binary/file nodes** (PDF read, image edit, spreadsheet parse) — need blob storage *and* item-array semantics (D1) first.
- **HTML → Markdown** (the reverse of Task 21) — needs a second Markdown dependency. Task 21 ships Markdown → HTML only, which is the direction that matters when an agent emits Markdown and the destination wants HTML.
- **`code`, once more, for the record** — the temptation will recur every time someone wants "just a little transformation". Reach for `set`, `json_extract`, `template` or Tendril first.

---

## Sequencing summary

| Phase | Tasks | Gate |
|---|---|---|
| 0 — Freeze | 1 | Tripwire proven to trip and reset |
| 1 — Data plumbing | 2, 3, 4 | x402/Tendril body selection provably unchanged |
| 2 — Compute nodes | 5-11 | Six nodes, zero new dependencies |
| 3 — Connectors | 12-19 | Ten connectors on the existing pattern |
| 2b — Dep'd compute | 20, 21 | Exactly two new deps: goquery, goldmark |
| 3b — GraphQL + free APIs | 22, 23 | Three more connectors, no credentials |
| 4 — Integration + E2E | 24, 25 | Nothing defaults; a real x402 run still settles both legs |
| 5 — Deferred | D1-D4 | Not started; separate plan and sign-off |

**Parallelisable:** Tasks 13-19 and 22-23 are mutually independent once Task 12 lands. Tasks 20 and 21 are independent of everything except Phase 2's `compute.go` existing.

**Strictly sequential:** Phase 1 (2 → 3 → 4), then Task 5 before the rest of Phase 2. Task 24 must run after every connector task, and Task 25 last.

Phases 2b and 3b are placed after Phase 3 deliberately: they carry the plan's only `go.mod` changes and its least-critical connectors, so if the plan is cut short, what gets dropped is the least valuable and the most dependency-heavy work.

## Open questions for the plan owner

1. **Item arrays (D1)** — RSS returns a list today and downstream nodes can only address it by index (`{{ node.rss1.items.0.title }}` is not even supported by the Task 4 resolver, which stops at one level). Confirm that is acceptable for now, or promote item semantics ahead of more connectors.
2. **Connector billing** — action nodes are currently free. RSS is the first that could be called in a tight loop against a third party. Confirm no rate limiting is needed before shipping it.
3. **Shopify API version** — pinned to `2024-10`. Confirm that is current at implementation time; Shopify removes versions after roughly twelve months.
4. **The two new dependencies** — `goquery` (which pulls `golang.org/x/net`) and `goldmark`. Both are widely used and actively maintained, and Phase 2b is isolated so they can be dropped without touching anything else. Ratify before Task 20, since backing them out later means deleting two shipped nodes.
5. **QuickChart sends chart data to a third party.** The node builds a URL containing the full Chart.js config, and whoever renders that URL (Slack, an email client) fetches it from `quickchart.io`. Anything plotted is therefore disclosed to an external service in the URL itself. Confirm that is acceptable, or plan a self-hosted QuickChart instance — it is open-source and the base URL is a one-line change.
