# Tendril VPS Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a first-class, officially-collaborated "Tendril" workflow in AgentMesh where a user rents a real Linux machine by the hour, pays for it with x402 USDC over the existing Wallet 1 → Wallet 2 → target relay, and gets an SSH session they can drive from the canvas console.

**Architecture:** A new `tendril` node type sits alongside `tool402`. It reuses the *existing, unmodified* relay payment path (`executeTool402V2Relay`) for every paid Tendril call, so no payment plumbing changes. Two things are new. **State:** a Tendril lease outlives the workflow run that opened it (a run finishes in seconds, a lease meters for hours), so leases become a persisted, first-class AgentMesh resource with their own table, generated SSH keypair, release/reaper lifecycle, and a WebSocket→SSH bridge feeding an xterm.js terminal tab in the canvas console. **A second balance:** every topup accumulates in the one shared Wallet 2 Tendril pool, but what any given user may *spend* is their own **Tendril credit** balance — a separate currency from AgentMesh credits, bought with AgentMesh credits inside the workflow, and displayed in the workflow itself.

**Tech Stack:** Go 1.25 (chi, pgx, `golang.org/x/crypto/ssh`, `github.com/coder/websocket`), Next.js 16 / React 19 (`@xterm/xterm`, `@xterm/addon-fit`), Postgres, Algorand mainnet USDC via GoPlausible facilitator.

---

## Global Constraints

- **This is mainnet, and it is real money.** Tendril's live registry quotes `algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=` (mainnet) and asset `31566704` (mainnet USDC). The only online machine today costs **$6.00/hour**. Every guard in this plan exists because a bug here spends real USDC.
- **Do not change the payment path.** Wallet 1 (`PLATFORM_SPEND_WALLET_*`) pays through `/x402/relay` to Wallet 2 (`PLATFORM_WALLET_*`), which pays Tendril. Tendril keys its credit balance to **the address that paid it**, which is therefore **Wallet 2's address** — one shared platform pool, not a per-user account.
- **Two balances, never conflated.** AgentMesh credits (the existing `users.credit_balance_usd_micros`) buy Tendril credits; Tendril credits (new, `users.tendril_credit_usd_micros`) buy machine time. Renting and running draw on **Tendril credits only** — never directly on AgentMesh credits. See "The Accounting Model" below; getting this wrong either double-charges users or lets one user spend another's hours.
- **Registry base URL:** `https://tendrilregister.007575.xyz`, configured as `TENDRIL_REGISTRY_URL`, never hardcoded outside the default. (Note: the 402 challenges self-report `resource.url` as `https://tendril.007575.xyz/...` — that is Tendril's public branding host, not the API host. Call the register host.)
- **`MAX_RELAY_OUTBOUND_USD_MICROS` defaults to `5_000_000` ($5.00) and will reject a $6.00 topup.** Task 7 raises it deliberately; do not silently work around it by splitting topups.
- **Never log, return, or persist an unencrypted `leaseToken` or SSH private key.** Both use `wallet.Encrypt`/`wallet.Decrypt` with `ENCRYPTION_KEY`, exactly like `AgentWallet.EncryptedMnemonic`.
- **Every new node type must keep working as an agent-attached tool.** Ports are `["in","out","top"]` — `in`/`out` for the standalone Tendril-only workflow the user asked for, `top` so a future agent can attach a Tendril node and drive the box itself. Do not omit `top` because Phase 1 does not use it.
- Go tests: `go test ./...` from `backend/`. DB-backed tests skip without `TEST_DATABASE_URL`. Frontend: `npm test` (vitest) and `npm run typecheck` from `frontend/`.
- Commit after every task. Never `git push` without explicit approval.

---

## Verified Tendril API Reference

Everything below was confirmed live against `https://tendrilregister.007575.xyz` on 2026-08-04. Treat this as the contract; do not re-derive it from the client repo, which points at a testnet default.

### Free endpoints (no payment, no auth)

**`GET /platform`**
```json
{"payTo":"ZIK7QQE7ZX446TW3PN7PQ5UDZNTY7JI5RYNTIU3LPEYBOSTVWI6PTNSWKI",
 "network":"algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=",
 "asset":{"id":"31566704","decimals":6,"symbol":"USDC"},
 "facilitatorUrl":"https://facilitator.goplausible.xyz",
 "minTopUpAtomic":100000,"maxTopUpAtomic":1000000000,"minWithdrawAtomic":5000000}
```

**`GET /explorer`** — the machine market.
```json
{"nodes":[{"id":"I8zY887UpE","ownerAddr":"EN6U…","payToAddr":"EN6U…","payoutBlocked":false,
           "label":"my-laptop","cpuCores":4,"ramMb":23989,"gpu":null,
           "pricePerHourUsd":6,"status":"online"}]}
```
Filter to `status === "online"`. `pricePerHourUsd` is a **plain USD number, not atomic** — `6` means $6.00/hr.

**`GET /lease/{leaseId}`** — `Authorization: Bearer <leaseToken>`. Returns
`{"lease":{"status":…,"rateAtomicPerHour":…,"expiresAt":…}}`.

**`DELETE /x402/leases/{leaseId}`** — `Authorization: Bearer <leaseToken>`. **Free, despite the `/x402/` prefix.** This is where compute is actually billed. Returns
`{"usedSeconds":…,"chargedAtomic":…,"balance":…}`.

### Wallet session (free, off-chain)

`GET /auth/wallet-nonce?address=<addr>` → `{"nonce":"…"}`, then
`POST /auth/wallet-login` with `{address, nonce, payment}` where `payment` is a base64 **signed 0-amount self-payment** carrying the nonce in its note field. It is verified and discarded, never broadcast — it costs nothing and needs no balance. Returns `{"token":…,"balanceAtomic":…}` (7-day token).

`GET /wallet` with `Authorization: Bearer <token>` → `{address, balanceAtomic, stats:{…}, topups:[], charges:[], payouts:[]}`.

**AgentMesh must sign this as Wallet 2** (`PLATFORM_WALLET_ENC_MNEMONIC`), because Wallet 2 is the address that pays Tendril and therefore the address the credit balance is keyed to.

### Paid endpoints (x402, v2 dialect)

All three answer `402` with an identical `accepts[0]` shape:
```json
{"scheme":"exact","network":"algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=",
 "amount":"<atomic>","asset":"31566704",
 "payTo":"ZIK7QQE7ZX446TW3PN7PQ5UDZNTY7JI5RYNTIU3LPEYBOSTVWI6PTNSWKI",
 "maxTimeoutSeconds":60,
 "extra":{"decimals":6,"name":"USDC",
          "feePayer":"ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA",
          "tag":"x402-global-challenge"}}
```
`extra.feePayer` is present, so `SignUSDCPaymentGroup` (the 2-txn atomic group where the facilitator sponsors the network fee) is the correct signer — which `tool402.go` already selects automatically off that field. The challenge is served both in the JSON body and base64 in the `PAYMENT-REQUIRED` header; `ChallengeFromHeader` already handles both.

| Endpoint | Charge | Notes |
|---|---|---|
| `POST /topup?amount=<atomic>` | **the `amount` you ask for** | Empty body. Credits the paying address. Response `{address, credited, balance, asset, payment:{txid,network}}`. Bounded by `minTopUpAtomic`/`maxTopUpAtomic`. Also routable as `/x402/topup`. |
| `POST /x402/rent?nodeId=<id>` | **flat 0.01 USDC gate fee** | Body `{"sshPubKey":"ssh-ed25519 AAAA…"}` (optional but required for SSH). Hourly time is **not** charged here — it meters against credit at the node's rate until release. Response `{leaseId, leaseToken, ssh:{host,port,username,command}, startedAt, fundedUntil}`. The client also handles `ssh.authMethod === "password"` with `ssh.password`. |
| `POST /x402/run` | **flat 0.01 USDC per job** | Body `{"payload":"<python>"}`. With `Authorization: Bearer <leaseToken>` it runs inside your held machine; without one Tendril picks an idle machine, runs it in a throwaway sandbox, and bills the seconds from credit. Response `{jobId, ok, result, execution?:{nodeId,seconds,costAtomic,balance}}`. |

**The billing model, stated plainly:** renting is cheap (1¢) and *does not* buy time. Time is bought by having credit on the paying address, and `fundedUntil` is derived from `balance ÷ rate`. So "buy 2 hours on a $6/hr box" means **top the shared pool up so it holds ≥ $12, then rent**. Release is where the meter stops and the real charge lands.

---

## The Accounting Model

Tendril keys credit to the paying address, and the paying address is always Wallet 2. So on Tendril's side there is exactly **one balance for all of AgentMesh**. AgentMesh therefore keeps its own per-user sub-ledger on top of that shared pool, and a user's spending power is their row in that sub-ledger — not the pool.

**Three balances, in the order money moves through them:**

| Balance | Where it lives | Bought with | Spends on |
|---|---|---|---|
| AgentMesh credits | `users.credit_balance_usd_micros` (existing) | fiat / crypto top-ups | everything on the platform, incl. Tendril credits |
| **Tendril credits** | `users.tendril_credit_usd_micros` (**new**) | AgentMesh credits, via a Topup node | machine hours and jobs |
| Tendril pool | Wallet 2's balance at Tendril | real mainnet USDC via the relay | nothing directly — it is the custodial float |

**The flow the user runs:**

1. **Run the workflow.** The Topup node fires.
2. **Wallet 1 → Wallet 2 → Tendril** settles a real on-chain USDC topup for the requested amount. That amount is added to the **shared pool**, where every user's topups accumulate together.
3. **The same amount is deducted from that user's AgentMesh credits** and **credited to that user's Tendril credits**. One transfer, two ledger rows, one transaction.
4. **Rent and Run draw down the user's Tendril credits.** The pool is untouched by the accounting — it only has to be *large enough*, which step 2 guarantees.
5. **Release settles the difference.** Tendril reports what it actually charged; the unused remainder of the reservation goes back to the user's Tendril credits, not to AgentMesh credits. Hours a user bought stay theirs.

**The invariant, which every task here must preserve:**

```
SUM(users.tendril_credit_usd_micros) + (hours currently metering) <= Tendril pool balance at Wallet 2
```

The pool is a float the platform custodies on users' behalf. It only ever grows by a topup that simultaneously credits exactly one user, and only ever shrinks by compute that was reserved against exactly one user's balance. A user can never spend another user's hours, because the check is always against their own row and never against the pool.

**What the user sees in the workflow:** their Tendril credit balance, live, on the Tendril node and in the Inspector — clearly labelled as distinct from AgentMesh credits. "$14.00 Tendril credit — about 2.3 hours on this machine" is the reading that matters, not the pool's size, which is an implementation detail they should never see.

---

## File Structure

**New — backend**
- `backend/internal/tendril/client.go` — typed HTTP client for the free + session endpoints. No payment logic, no DB. One responsibility: speak Tendril.
- `backend/internal/tendril/client_test.go` — table tests against `httptest` fakes.
- `backend/internal/tendril/session.go` — Wallet 2 nonce-signing and the cached 7-day token.
- `backend/internal/tendril/session_test.go`
- `backend/internal/engine/nodes/tendril.go` — the node executors (`rent`, `run`, `release`), built on the existing relay helper.
- `backend/internal/engine/nodes/tendril_test.go`
- `backend/internal/sshkeys/sshkeys.go` — ed25519 keypair generation + OpenSSH encoding. Tiny and dependency-light so it is trivially testable.
- `backend/internal/sshkeys/sshkeys_test.go`
- `backend/internal/api/handlers/leases.go` — lease REST (list, get, key download, release) + the terminal WebSocket.
- `backend/internal/api/handlers/leases_test.go`
- `backend/internal/db/migrations/000014_tendril_leases.{up,down}.sql`
- `backend/internal/db/migrations/000015_debit_kind_tendril.{up,down}.sql`
- `backend/internal/db/migrations/000016_tendril_credits.{up,down}.sql` — the per-user sub-ledger.
- `backend/internal/db/tendril_credit.go` — balance + ledger store methods, kept out of the already-large `store.go`.
- `backend/internal/db/tendril_credit_test.go`

**New — frontend**
- `frontend/src/components/canvas/TerminalTab.tsx` — xterm.js pane, one responsibility: bytes in, bytes out.
- `frontend/src/components/leases/LeasesPanel.tsx` — active leases with release buttons.
- `frontend/src/lib/tendril.ts` — Tendril-specific client calls + hour/cost math.
- `frontend/src/lib/tendril.test.ts`

**Modified**
- `backend/internal/models/types.go` — `NodeTypeTendril`, `TendrilLease`, `DebitKindTendrilLease`, node fields.
- `backend/internal/db/store.go` — lease CRUD + pool serialization.
- `backend/internal/engine/runner.go:647` — dispatch `NodeTypeTendril`.
- `backend/internal/api/router.go` — mount lease routes.
- `backend/cmd/server/main.go` — `TENDRIL_REGISTRY_URL`, raised relay cap, reaper goroutine.
- `backend/internal/bazaar/curated.go` — add the rent entry beside the existing `curated:tendril-run`.
- `frontend/src/lib/types.ts`, `frontend/src/lib/data.ts` — node type, meta, templates.
- `frontend/src/components/canvas/{PalettePanel,Inspector,LogDrawer}.tsx`, `nodes/index.tsx`.
- `frontend/src/lib/demoWorkflow.ts`, `frontend/src/components/workflows/WorkflowsPage.tsx` — the official Tendril workflow.

---

## Phase 1 — Speak Tendril

### Task 1: Tendril HTTP client (free endpoints)

**Files:**
- Create: `backend/internal/tendril/client.go`
- Test: `backend/internal/tendril/client_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `tendril.Client` with `NewClient(baseURL string) *Client`, `Platform(ctx) (Platform, error)`, `OnlineNodes(ctx) ([]Node, error)`, `Lease(ctx, leaseID, leaseToken string) (LeaseStatus, error)`, `Release(ctx, leaseID, leaseToken string) (ReleaseResult, error)`. Types `Platform`, `Node`, `LeaseStatus`, `ReleaseResult` as defined below.

- [ ] **Step 1: Write the failing test**

```go
package tendril

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/platform":
			w.Write([]byte(`{"payTo":"PAYTO","network":"algorand:wGHE2","asset":{"id":"31566704","decimals":6,"symbol":"USDC"},"minTopUpAtomic":100000,"maxTopUpAtomic":1000000000}`))
		case "/explorer":
			w.Write([]byte(`{"nodes":[
				{"id":"cheap","cpuCores":2,"ramMb":4096,"pricePerHourUsd":1.5,"status":"online"},
				{"id":"dear","cpuCores":8,"ramMb":32768,"pricePerHourUsd":6,"status":"online"},
				{"id":"gone","cpuCores":4,"ramMb":8192,"pricePerHourUsd":0.5,"status":"offline"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestPlatformParsesAssetAndBounds(t *testing.T) {
	srv := fakeRegistry(t)
	defer srv.Close()

	p, err := NewClient(srv.URL).Platform(context.Background())
	if err != nil {
		t.Fatalf("Platform: %v", err)
	}
	if p.Asset.ID != "31566704" || p.Asset.Decimals != 6 {
		t.Errorf("asset = %+v, want id 31566704 decimals 6", p.Asset)
	}
	if p.MinTopUpAtomic != 100000 || p.MaxTopUpAtomic != 1000000000 {
		t.Errorf("topup bounds = %d..%d", p.MinTopUpAtomic, p.MaxTopUpAtomic)
	}
}

// Offline machines are unrentable, and the market must be cheapest-first so the
// canvas's default pick is the cheapest box rather than whatever Tendril
// happened to list first.
func TestOnlineNodesFiltersOfflineAndSortsByPrice(t *testing.T) {
	srv := fakeRegistry(t)
	defer srv.Close()

	nodes, err := NewClient(srv.URL).OnlineNodes(context.Background())
	if err != nil {
		t.Fatalf("OnlineNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2 (offline filtered)", len(nodes))
	}
	if nodes[0].ID != "cheap" || nodes[1].ID != "dear" {
		t.Errorf("order = %s,%s; want cheap,dear", nodes[0].ID, nodes[1].ID)
	}
	if nodes[1].RateUSDMicrosPerHour() != 6_000_000 {
		t.Errorf("rate = %d, want 6000000", nodes[1].RateUSDMicrosPerHour())
	}
}

// A trailing slash on the configured base URL must not produce "//platform".
func TestBaseURLTrailingSlashTrimmed(t *testing.T) {
	srv := fakeRegistry(t)
	defer srv.Close()

	if _, err := NewClient(srv.URL + "/").Platform(context.Background()); err != nil {
		t.Fatalf("Platform with trailing slash: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/tendril/ -run TestPlatform -v`
Expected: FAIL — build error, `undefined: NewClient`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package tendril is a typed client for the Tendril compute registry
// (https://tendrilregister.007575.xyz). It speaks only Tendril's own HTTP
// surface: the free market/lease endpoints and the wallet session. It never
// makes an x402 payment — paid routes go through the engine's existing relay
// path, which already knows how to sign and settle.
package tendril

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Asset struct {
	ID       string `json:"id"`
	Decimals int    `json:"decimals"`
	Symbol   string `json:"symbol"`
}

type Platform struct {
	PayTo          string `json:"payTo"`
	Network        string `json:"network"`
	Asset          Asset  `json:"asset"`
	FacilitatorURL string `json:"facilitatorUrl"`
	MinTopUpAtomic int64  `json:"minTopUpAtomic"`
	MaxTopUpAtomic int64  `json:"maxTopUpAtomic"`
}

type Node struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	CPUCores        int     `json:"cpuCores"`
	RAMMb           int     `json:"ramMb"`
	GPU             *string `json:"gpu"`
	PricePerHourUSD float64 `json:"pricePerHourUsd"`
	Status          string  `json:"status"`
	PayoutBlocked   bool    `json:"payoutBlocked"`
}

// RateUSDMicrosPerHour converts Tendril's plain-dollar hourly price into the
// USD-micros unit every ledger in this codebase uses. Rounded to nearest so a
// float like 1.5 never lands a micro short.
func (n Node) RateUSDMicrosPerHour() int64 {
	return int64(n.PricePerHourUSD*1e6 + 0.5)
}

type LeaseStatus struct {
	Status            string `json:"status"`
	RateAtomicPerHour int64  `json:"rateAtomicPerHour"`
	ExpiresAt         string `json:"expiresAt"`
}

type ReleaseResult struct {
	UsedSeconds   int64 `json:"usedSeconds"`
	ChargedAtomic int64 `json:"chargedAtomic"`
	Balance       int64 `json:"balance"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path, bearer string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("tendril %s %s: %d %s", method, path, resp.StatusCode, truncate(body))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func truncate(b []byte) string {
	if len(b) > 300 {
		return string(b[:300])
	}
	return string(b)
}

func (c *Client) Platform(ctx context.Context) (Platform, error) {
	var p Platform
	err := c.do(ctx, http.MethodGet, "/platform", "", &p)
	return p, err
}

func (c *Client) OnlineNodes(ctx context.Context) ([]Node, error) {
	var wrapper struct {
		Nodes []Node `json:"nodes"`
	}
	if err := c.do(ctx, http.MethodGet, "/explorer", "", &wrapper); err != nil {
		return nil, err
	}
	online := wrapper.Nodes[:0]
	for _, n := range wrapper.Nodes {
		if n.Status == "online" {
			online = append(online, n)
		}
	}
	sort.SliceStable(online, func(i, j int) bool {
		return online[i].PricePerHourUSD < online[j].PricePerHourUSD
	})
	return online, nil
}

func (c *Client) Lease(ctx context.Context, leaseID, leaseToken string) (LeaseStatus, error) {
	var wrapper struct {
		Lease LeaseStatus `json:"lease"`
	}
	err := c.do(ctx, http.MethodGet, "/lease/"+leaseID, leaseToken, &wrapper)
	return wrapper.Lease, err
}

// Release stops the meter and is where Tendril actually bills the elapsed
// compute. Free despite the /x402/ prefix — no payment is attached.
func (c *Client) Release(ctx context.Context, leaseID, leaseToken string) (ReleaseResult, error) {
	var r ReleaseResult
	err := c.do(ctx, http.MethodDelete, "/x402/leases/"+leaseID, leaseToken, &r)
	return r, err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/tendril/ -v`
Expected: PASS — all three tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/tendril/
git commit -m "tendril: typed client for the free registry endpoints"
```

---

### Task 2: Wallet 2 session (signed-nonce login + balance)

**Files:**
- Create: `backend/internal/tendril/session.go`
- Test: `backend/internal/tendril/session_test.go`

**Interfaces:**
- Consumes: `tendril.Client` from Task 1.
- Produces: `type NonceSigner interface { SignZeroSelfPayment(ctx context.Context, encMnemonic, note, genesisHashB64, genesisID string) (addressB64Txn string, address string, err error) }` and `(*Client).Session(ctx, signer NonceSigner, encMnemonic string) (*Session, error)`, `(*Session).Balance(ctx) (int64, error)`. `Session` caches its token and re-logs-in when it expires.

- [ ] **Step 1: Write the failing test**

```go
package tendril

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type fakeSigner struct{ notes []string }

func (f *fakeSigner) SignZeroSelfPayment(_ context.Context, _, note, _, _ string) (string, string, error) {
	f.notes = append(f.notes, note)
	return "c2lnbmVk", "WALLET2ADDR", nil
}

func sessionServer(t *testing.T, logins *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/platform":
			w.Write([]byte(`{"network":"algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=","asset":{"id":"31566704","decimals":6,"symbol":"USDC"}}`))
		case "/auth/wallet-nonce":
			if got := r.URL.Query().Get("address"); got != "WALLET2ADDR" {
				t.Errorf("nonce requested for %q, want WALLET2ADDR", got)
			}
			w.Write([]byte(`{"nonce":"NONCE-1"}`))
		case "/auth/wallet-login":
			atomic.AddInt32(logins, 1)
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["nonce"] != "NONCE-1" || body["payment"] != "c2lnbmVk" {
				t.Errorf("login body = %v", body)
			}
			w.Write([]byte(`{"token":"TOK","balanceAtomic":12500000}`))
		case "/wallet":
			if r.Header.Get("Authorization") != "Bearer TOK" {
				t.Errorf("auth = %q", r.Header.Get("Authorization"))
			}
			w.Write([]byte(`{"address":"WALLET2ADDR","balanceAtomic":12500000}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestSessionSignsNonceAndReadsBalance(t *testing.T) {
	var logins int32
	srv := sessionServer(t, &logins)
	defer srv.Close()

	signer := &fakeSigner{}
	sess, err := NewClient(srv.URL).Session(context.Background(), signer, "enc-mnemonic")
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	bal, err := sess.Balance(context.Background())
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if bal != 12_500_000 {
		t.Errorf("balance = %d, want 12500000", bal)
	}
	if len(signer.notes) != 1 || signer.notes[0] != "NONCE-1" {
		t.Errorf("signed notes = %v, want [NONCE-1]", signer.notes)
	}
}

// A session is a 7-day token; re-reading the balance must not re-login.
func TestSessionReusesToken(t *testing.T) {
	var logins int32
	srv := sessionServer(t, &logins)
	defer srv.Close()

	sess, err := NewClient(srv.URL).Session(context.Background(), &fakeSigner{}, "enc")
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := sess.Balance(context.Background()); err != nil {
			t.Fatalf("Balance %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&logins); got != 1 {
		t.Errorf("logins = %d, want 1", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/tendril/ -run TestSession -v`
Expected: FAIL — `sess.Session undefined`.

- [ ] **Step 3: Write minimal implementation**

```go
package tendril

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// NonceSigner produces the 0-amount self-payment Tendril's wallet-login
// expects. The transaction is verified and discarded, never broadcast, so it
// costs nothing and needs no balance — it exists purely to prove control of
// the address whose Tendril credit balance we are about to read.
//
// wallet.Service implements this (Task 3).
type NonceSigner interface {
	SignZeroSelfPayment(ctx context.Context, encMnemonic, note, genesisHashB64, genesisID string) (signedTxnB64 string, address string, err error)
}

type Session struct {
	client      *Client
	signer      NonceSigner
	encMnemonic string
	network     string

	mu    sync.Mutex
	token string
}

// genesisParts splits a CAIP-2 id like
// "algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=" into the base64
// genesis hash and the matching genesis id string algod expects.
func genesisParts(caip2 string) (hashB64, genesisID string) {
	_, hashB64, _ = strings.Cut(caip2, ":")
	switch hashB64 {
	case "wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=":
		return hashB64, "mainnet-v1.0"
	default:
		return hashB64, "testnet-v1.0"
	}
}

func (c *Client) Session(ctx context.Context, signer NonceSigner, encMnemonic string) (*Session, error) {
	p, err := c.Platform(ctx)
	if err != nil {
		return nil, fmt.Errorf("tendril session: platform: %w", err)
	}
	s := &Session{client: c, signer: signer, encMnemonic: encMnemonic, network: p.Network}
	if _, err := s.login(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Session) login(ctx context.Context) (string, error) {
	hashB64, genesisID := genesisParts(s.network)

	// The address is whatever the signer derives from the mnemonic; ask it
	// first with an empty note so the nonce is requested for the right address.
	_, address, err := s.signer.SignZeroSelfPayment(ctx, s.encMnemonic, "", hashB64, genesisID)
	if err != nil {
		return "", fmt.Errorf("tendril login: derive address: %w", err)
	}

	var nonceResp struct {
		Nonce string `json:"nonce"`
	}
	if err := s.client.do(ctx, http.MethodGet, "/auth/wallet-nonce?address="+url.QueryEscape(address), "", &nonceResp); err != nil {
		return "", fmt.Errorf("tendril login: nonce: %w", err)
	}

	signed, _, err := s.signer.SignZeroSelfPayment(ctx, s.encMnemonic, nonceResp.Nonce, hashB64, genesisID)
	if err != nil {
		return "", fmt.Errorf("tendril login: sign: %w", err)
	}

	body, _ := json.Marshal(map[string]string{
		"address": address, "nonce": nonceResp.Nonce, "payment": signed,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.client.baseURL+"/auth/wallet-login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("tendril login: %d %s", resp.StatusCode, truncate(raw))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.token = out.Token
	s.mu.Unlock()
	return out.Token, nil
}

// Balance is the shared platform pool's Tendril credit, in atomic USDC units.
func (s *Session) Balance(ctx context.Context) (int64, error) {
	s.mu.Lock()
	token := s.token
	s.mu.Unlock()

	var w struct {
		BalanceAtomic int64 `json:"balanceAtomic"`
	}
	err := s.client.do(ctx, http.MethodGet, "/wallet", token, &w)
	if err == nil {
		return w.BalanceAtomic, nil
	}
	// A 7-day token can expire mid-life; one silent re-login beats surfacing
	// an auth error to a user who is trying to rent a machine.
	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "403") {
		return 0, err
	}
	fresh, lerr := s.login(ctx)
	if lerr != nil {
		return 0, lerr
	}
	if err := s.client.do(ctx, http.MethodGet, "/wallet", fresh, &w); err != nil {
		return 0, err
	}
	return w.BalanceAtomic, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/tendril/ -v`
Expected: PASS — five tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/tendril/
git commit -m "tendril: Wallet 2 signed-nonce session and pool balance"
```

---

### Task 3: `SignZeroSelfPayment` on the wallet service

**Files:**
- Modify: `backend/internal/wallet/algorand.go` (append a method after `SignUSDCPaymentSingle`)
- Test: `backend/internal/wallet/algorand_test.go` (append)

**Interfaces:**
- Consumes: `tendril.NonceSigner` (Task 2).
- Produces: `func (s *Service) SignZeroSelfPayment(ctx context.Context, encMnemonic, note, genesisHashB64, genesisID string) (string, string, error)` — satisfies `tendril.NonceSigner`.

- [ ] **Step 1: Write the failing test**

```go
// Tendril's wallet-login proves address control with a 0-amount self-payment
// that is verified and discarded, never broadcast. It must therefore be
// signable with no algod round trip and no balance whatsoever.
func TestSignZeroSelfPaymentIsOfflineAndSelfAddressed(t *testing.T) {
	svc := NewService(testEncKey, "http://unreachable.invalid", "", "mainnet")
	addr, encMnemonic, err := svc.GenerateWallet()
	if err != nil {
		t.Fatalf("GenerateWallet: %v", err)
	}

	signed, gotAddr, err := svc.SignZeroSelfPayment(
		context.Background(), encMnemonic, "NONCE-1",
		"wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=", "mainnet-v1.0",
	)
	if err != nil {
		t.Fatalf("SignZeroSelfPayment: %v", err)
	}
	if gotAddr != addr {
		t.Errorf("address = %q, want %q", gotAddr, addr)
	}
	if signed == "" {
		t.Error("signed txn is empty")
	}
	if _, err := base64.StdEncoding.DecodeString(signed); err != nil {
		t.Errorf("signed txn is not base64: %v", err)
	}
}

// An empty note is the "just tell me the address" probe the session's login
// makes before it knows which nonce to sign.
func TestSignZeroSelfPaymentEmptyNoteStillDerivesAddress(t *testing.T) {
	svc := NewService(testEncKey, "http://unreachable.invalid", "", "mainnet")
	addr, encMnemonic, err := svc.GenerateWallet()
	if err != nil {
		t.Fatalf("GenerateWallet: %v", err)
	}
	_, gotAddr, err := svc.SignZeroSelfPayment(
		context.Background(), encMnemonic, "",
		"wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=", "mainnet-v1.0",
	)
	if err != nil {
		t.Fatalf("SignZeroSelfPayment: %v", err)
	}
	if gotAddr != addr {
		t.Errorf("address = %q, want %q", gotAddr, addr)
	}
}
```

If `testEncKey` does not already exist in `algorand_test.go`, add
`const testEncKey = "0123456789abcdef0123456789abcdef"` near the top, and add
`"context"` / `"encoding/base64"` to that file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/wallet/ -run TestSignZeroSelfPayment -v`
Expected: FAIL — `svc.SignZeroSelfPayment undefined`.

- [ ] **Step 3: Write minimal implementation**

```go
// SignZeroSelfPayment signs a 0-amount payment from an address to itself,
// carrying note in the note field, using hardcoded suggested params rather
// than any algod round trip.
//
// Tendril's /auth/wallet-login verifies this signature and then discards the
// transaction — it is never broadcast. That is why the params below are
// invented rather than fetched: a transaction nobody submits has no real
// validity window to respect, and requiring algod here would make logging in
// to read a balance fail whenever the node is slow. It also costs nothing and
// requires no balance, which matters because Wallet 2's ALGO is not this
// feature's concern.
//
// Returns the base64 signed transaction and the signing address.
func (s *Service) SignZeroSelfPayment(ctx context.Context, encMnemonic, note, genesisHashB64, genesisID string) (string, string, error) {
	mn, err := s.DecryptMnemonic(encMnemonic)
	if err != nil {
		return "", "", err
	}
	privateKey, err := mnemonic.ToPrivateKey(mn)
	if err != nil {
		return "", "", err
	}
	acct, err := crypto.AccountFromPrivateKey(privateKey)
	if err != nil {
		return "", "", err
	}
	addr := acct.Address.String()

	genesisHash, err := base64.StdEncoding.DecodeString(genesisHashB64)
	if err != nil {
		return "", "", fmt.Errorf("genesis hash: %w", err)
	}
	params := types.SuggestedParams{
		Fee:             1000,
		MinFee:          1000,
		FirstRoundValid: 1,
		LastRoundValid:  1000,
		GenesisID:       genesisID,
		GenesisHash:     genesisHash,
		FlatFee:         true,
	}
	txn, err := transaction.MakePaymentTxn(addr, addr, 0, []byte(note), "", params)
	if err != nil {
		return "", "", err
	}
	_, signed, err := crypto.SignTransaction(privateKey, txn)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(signed), addr, nil
}
```

Add `"encoding/base64"` and `"fmt"` to `algorand.go`'s imports if absent; `crypto`, `mnemonic`, `transaction`, and `types` are already imported by the existing signing methods.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/wallet/ -v && go build ./...`
Expected: PASS, clean build.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/wallet/
git commit -m "wallet: sign the 0-amount self-payment Tendril's wallet-login verifies"
```

---

## Phase 2 — Rent a machine

### Task 4: SSH keypair generation

**Files:**
- Create: `backend/internal/sshkeys/sshkeys.go`
- Test: `backend/internal/sshkeys/sshkeys_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func Generate() (publicKey string, privateKeyPEM string, err error)` — `publicKey` in OpenSSH `authorized_keys` form (`ssh-ed25519 AAAA… agentmesh`), `privateKeyPEM` in OpenSSH PEM form suitable for both `ssh.ParsePrivateKey` and a downloaded `id_ed25519` file.

- [ ] **Step 1: Write the failing test**

```go
package sshkeys

import (
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateProducesUsableEd25519Pair(t *testing.T) {
	pub, privPEM, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Errorf("public key = %q, want an ssh-ed25519 prefix", pub)
	}
	if !strings.Contains(privPEM, "OPENSSH PRIVATE KEY") {
		t.Errorf("private key is not an OpenSSH PEM:\n%s", privPEM)
	}

	signer, err := ssh.ParsePrivateKey([]byte(privPEM))
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	// The authorized_keys line we hand Tendril must be the same key the
	// terminal bridge later authenticates with, or the box accepts a key we
	// cannot use.
	want := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if got := strings.TrimSpace(strings.TrimSuffix(pub, " agentmesh")); got != want {
		t.Errorf("public key mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestGenerateIsUnique(t *testing.T) {
	a, _, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, _, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if a == b {
		t.Error("two Generate calls produced the same public key")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/sshkeys/ -v`
Expected: FAIL — `undefined: Generate`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package sshkeys generates the per-lease ed25519 keypair AgentMesh authorizes
// on a rented Tendril machine.
//
// A fresh key per lease rather than one platform key: the private key is
// downloadable by the lease's owner, so a shared key would hand every renter
// access to every machine AgentMesh has ever rented.
package sshkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"

	"golang.org/x/crypto/ssh"
)

func Generate() (publicKey string, privateKeyPEM string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	return line + " agentmesh", string(pem.EncodeToMemory(block)), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/sshkeys/ -v`
Expected: PASS — both tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/sshkeys/
git commit -m "sshkeys: per-lease ed25519 keypair generation"
```

---

### Task 5: Lease schema, model, and store

**Files:**
- Create: `backend/internal/db/migrations/000014_tendril_leases.up.sql`
- Create: `backend/internal/db/migrations/000014_tendril_leases.down.sql`
- Create: `backend/internal/db/migrations/000015_debit_kind_tendril.up.sql`
- Create: `backend/internal/db/migrations/000015_debit_kind_tendril.down.sql`
- Modify: `backend/internal/models/types.go`
- Modify: `backend/internal/db/store.go` (append)
- Test: `backend/internal/db/tendril_lease_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `models.TendrilLease`, `models.DebitKindTendrilLease = "tendril_lease"`, and store methods
  `InsertTendrilLease(ctx, models.TendrilLease) (models.TendrilLease, error)`,
  `GetTendrilLease(ctx, id string) (models.TendrilLease, error)`,
  `ListActiveTendrilLeases(ctx, userID string) ([]models.TendrilLease, error)`,
  `ListExpiredTendrilLeases(ctx, now time.Time) ([]models.TendrilLease, error)`,
  `MarkTendrilLeaseReleased(ctx, id string, usedSeconds, chargedUSDMicros int64) error`.

- [ ] **Step 1: Write the migrations and the model**

`000014_tendril_leases.up.sql`:
```sql
CREATE TABLE IF NOT EXISTS tendril_leases (
    id                        TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id                   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workflow_id               TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    run_id                    TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    node_id                   TEXT NOT NULL,
    lease_id                  TEXT NOT NULL UNIQUE,
    lease_token_enc           TEXT NOT NULL,
    tendril_node_id           TEXT NOT NULL,
    tendril_node_label        TEXT NOT NULL DEFAULT '',
    ssh_host                  TEXT NOT NULL DEFAULT '',
    ssh_port                  INTEGER NOT NULL DEFAULT 22,
    ssh_username              TEXT NOT NULL DEFAULT 'root',
    ssh_command               TEXT NOT NULL DEFAULT '',
    ssh_public_key            TEXT NOT NULL DEFAULT '',
    ssh_private_key_enc       TEXT NOT NULL DEFAULT '',
    ssh_password_enc          TEXT NOT NULL DEFAULT '',
    rate_usd_micros_per_hour  BIGINT NOT NULL,
    hours_purchased           NUMERIC NOT NULL,
    reserved_usd_micros       BIGINT NOT NULL,
    charged_usd_micros        BIGINT,
    used_seconds              BIGINT,
    status                    TEXT NOT NULL DEFAULT 'active',
    started_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    funded_until              TIMESTAMPTZ NOT NULL,
    released_at               TIMESTAMPTZ,
    CONSTRAINT tendril_lease_status_valid CHECK (status IN ('active', 'released', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_tendril_leases_user   ON tendril_leases(user_id);
CREATE INDEX IF NOT EXISTS idx_tendril_leases_active ON tendril_leases(status, funded_until);
```

`000014_tendril_leases.down.sql`:
```sql
DROP TABLE IF EXISTS tendril_leases;
```

`000015_debit_kind_tendril.up.sql` — `debit_ledger` has a `CHECK (kind IN (…))` constraint that will reject the new kind outright:
```sql
ALTER TABLE debit_ledger DROP CONSTRAINT IF EXISTS debit_ledger_kind_valid;
ALTER TABLE debit_ledger ADD CONSTRAINT debit_ledger_kind_valid
    CHECK (kind IN ('byok_flat_fee', 'x402_platform_fee', 'x402_relay_cost',
                    'platform_key_llm_fee', 'tendril_lease'));
```

`000015_debit_kind_tendril.down.sql`:
```sql
ALTER TABLE debit_ledger DROP CONSTRAINT IF EXISTS debit_ledger_kind_valid;
ALTER TABLE debit_ledger ADD CONSTRAINT debit_ledger_kind_valid
    CHECK (kind IN ('byok_flat_fee', 'x402_platform_fee', 'x402_relay_cost',
                    'platform_key_llm_fee'));
```

Before writing 000015, run `grep -rn "debit_ledger_kind_valid" backend/internal/db/migrations/` and copy the **current** full kind list from the latest migration that redefines it — the list above must be a superset of what is live, or existing rows fail the new constraint.

In `backend/internal/models/types.go`, add to the `DebitKind*` const block:
```go
	DebitKindTendrilLease = "tendril_lease"
```
and add the type:
```go
// TendrilLease is one rented Tendril machine. A lease deliberately outlives
// the run that opened it: a workflow run finishes in seconds while the machine
// meters for hours, so this is a first-class AgentMesh resource with its own
// release lifecycle rather than run-scoped state.
type TendrilLease struct {
	ID         string `json:"id"`
	UserID     string `json:"userId"`
	WorkflowID string `json:"workflowId"`
	RunID      string `json:"runId"`
	NodeID     string `json:"nodeId"`

	LeaseID          string `json:"leaseId"`
	LeaseTokenEnc    string `json:"-"`
	TendrilNodeID    string `json:"tendrilNodeId"`
	TendrilNodeLabel string `json:"tendrilNodeLabel"`

	SSHHost          string `json:"sshHost"`
	SSHPort          int    `json:"sshPort"`
	SSHUsername      string `json:"sshUsername"`
	SSHCommand       string `json:"sshCommand"`
	SSHPublicKey     string `json:"sshPublicKey"`
	SSHPrivateKeyEnc string `json:"-"`
	SSHPasswordEnc   string `json:"-"`

	RateUSDMicrosPerHour int64      `json:"rateUsdMicrosPerHour"`
	HoursPurchased       float64    `json:"hoursPurchased"`
	ReservedUSDMicros    int64      `json:"reservedUsdMicros"`
	ChargedUSDMicros     *int64     `json:"chargedUsdMicros,omitempty"`
	UsedSeconds          *int64     `json:"usedSeconds,omitempty"`
	Status               string     `json:"status"`
	StartedAt            time.Time  `json:"startedAt"`
	FundedUntil          time.Time  `json:"fundedUntil"`
	ReleasedAt           *time.Time `json:"releasedAt,omitempty"`
}
```

- [ ] **Step 2: Write the failing store test**

```go
package db

import (
	"context"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

func TestTendrilLeaseRoundTripAndRelease(t *testing.T) {
	store, userID, wfID, runID := setupRunFixture(t) // existing helper in this package
	ctx := context.Background()

	in := models.TendrilLease{
		UserID: userID, WorkflowID: wfID, RunID: runID, NodeID: "n2",
		LeaseID: "lease_9k2m", LeaseTokenEnc: "enc-token",
		TendrilNodeID: "I8zY887UpE", TendrilNodeLabel: "my-laptop",
		SSHHost: "bore.pub", SSHPort: 41823, SSHUsername: "root",
		SSHCommand: "ssh root@bore.pub -p 41823",
		SSHPublicKey: "ssh-ed25519 AAAA agentmesh", SSHPrivateKeyEnc: "enc-key",
		RateUSDMicrosPerHour: 6_000_000, HoursPurchased: 2,
		ReservedUSDMicros: 12_010_000,
		FundedUntil:       time.Now().Add(2 * time.Hour),
	}
	saved, err := store.InsertTendrilLease(ctx, in)
	if err != nil {
		t.Fatalf("InsertTendrilLease: %v", err)
	}
	if saved.ID == "" || saved.Status != "active" {
		t.Fatalf("saved = %+v, want an id and status active", saved)
	}

	active, err := store.ListActiveTendrilLeases(ctx, userID)
	if err != nil {
		t.Fatalf("ListActiveTendrilLeases: %v", err)
	}
	if len(active) != 1 || active[0].LeaseID != "lease_9k2m" {
		t.Fatalf("active = %+v", active)
	}
	if active[0].LeaseTokenEnc != "enc-token" {
		t.Error("lease token did not round-trip")
	}

	if err := store.MarkTendrilLeaseReleased(ctx, saved.ID, 3600, 6_000_000); err != nil {
		t.Fatalf("MarkTendrilLeaseReleased: %v", err)
	}
	after, err := store.ListActiveTendrilLeases(ctx, userID)
	if err != nil {
		t.Fatalf("ListActiveTendrilLeases after release: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("released lease still listed as active: %+v", after)
	}

	got, err := store.GetTendrilLease(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetTendrilLease: %v", err)
	}
	if got.Status != "released" || got.ChargedUSDMicros == nil || *got.ChargedUSDMicros != 6_000_000 {
		t.Errorf("released lease = %+v", got)
	}
}

// The reaper must find leases whose funded window has closed, and must not
// find ones already released.
func TestListExpiredTendrilLeases(t *testing.T) {
	store, userID, wfID, runID := setupRunFixture(t)
	ctx := context.Background()

	past, err := store.InsertTendrilLease(ctx, models.TendrilLease{
		UserID: userID, WorkflowID: wfID, RunID: runID, NodeID: "n2",
		LeaseID: "lease_old", LeaseTokenEnc: "e", TendrilNodeID: "x",
		RateUSDMicrosPerHour: 1, HoursPurchased: 1, ReservedUSDMicros: 1,
		FundedUntil: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("insert past: %v", err)
	}
	if _, err := store.InsertTendrilLease(ctx, models.TendrilLease{
		UserID: userID, WorkflowID: wfID, RunID: runID, NodeID: "n3",
		LeaseID: "lease_future", LeaseTokenEnc: "e", TendrilNodeID: "x",
		RateUSDMicrosPerHour: 1, HoursPurchased: 1, ReservedUSDMicros: 1,
		FundedUntil: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("insert future: %v", err)
	}

	expired, err := store.ListExpiredTendrilLeases(ctx, time.Now())
	if err != nil {
		t.Fatalf("ListExpiredTendrilLeases: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != past.ID {
		t.Fatalf("expired = %+v, want only lease_old", expired)
	}
}
```

If `setupRunFixture` does not exist in `internal/db`, add it to this new test file — it must `t.Skip()` when `TEST_DATABASE_URL` is unset, mirroring the existing DB tests (read `backend/internal/db/debit_test.go` for the exact skip + schema-setup helper this package already uses, and reuse that helper rather than writing a second one).

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/agentmesh_test go test ./internal/db/ -run TendrilLease -v`
Expected: FAIL — `store.InsertTendrilLease undefined`.

- [ ] **Step 4: Write the store methods**

```go
const tendrilLeaseCols = `id, user_id, workflow_id, run_id, node_id, lease_id,
	lease_token_enc, tendril_node_id, tendril_node_label, ssh_host, ssh_port,
	ssh_username, ssh_command, ssh_public_key, ssh_private_key_enc,
	ssh_password_enc, rate_usd_micros_per_hour, hours_purchased,
	reserved_usd_micros, charged_usd_micros, used_seconds, status, started_at,
	funded_until, released_at`

func scanTendrilLease(row pgx.Row) (models.TendrilLease, error) {
	var l models.TendrilLease
	err := row.Scan(&l.ID, &l.UserID, &l.WorkflowID, &l.RunID, &l.NodeID, &l.LeaseID,
		&l.LeaseTokenEnc, &l.TendrilNodeID, &l.TendrilNodeLabel, &l.SSHHost, &l.SSHPort,
		&l.SSHUsername, &l.SSHCommand, &l.SSHPublicKey, &l.SSHPrivateKeyEnc,
		&l.SSHPasswordEnc, &l.RateUSDMicrosPerHour, &l.HoursPurchased,
		&l.ReservedUSDMicros, &l.ChargedUSDMicros, &l.UsedSeconds, &l.Status,
		&l.StartedAt, &l.FundedUntil, &l.ReleasedAt)
	return l, err
}

func (s *Store) InsertTendrilLease(ctx context.Context, l models.TendrilLease) (models.TendrilLease, error) {
	return scanTendrilLease(s.pool.QueryRow(ctx, `
		INSERT INTO tendril_leases (user_id, workflow_id, run_id, node_id, lease_id,
			lease_token_enc, tendril_node_id, tendril_node_label, ssh_host, ssh_port,
			ssh_username, ssh_command, ssh_public_key, ssh_private_key_enc,
			ssh_password_enc, rate_usd_micros_per_hour, hours_purchased,
			reserved_usd_micros, funded_until)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING `+tendrilLeaseCols,
		l.UserID, l.WorkflowID, l.RunID, l.NodeID, l.LeaseID, l.LeaseTokenEnc,
		l.TendrilNodeID, l.TendrilNodeLabel, l.SSHHost, l.SSHPort, l.SSHUsername,
		l.SSHCommand, l.SSHPublicKey, l.SSHPrivateKeyEnc, l.SSHPasswordEnc,
		l.RateUSDMicrosPerHour, l.HoursPurchased, l.ReservedUSDMicros, l.FundedUntil))
}

func (s *Store) GetTendrilLease(ctx context.Context, id string) (models.TendrilLease, error) {
	return scanTendrilLease(s.pool.QueryRow(ctx,
		`SELECT `+tendrilLeaseCols+` FROM tendril_leases WHERE id = $1`, id))
}

func (s *Store) ListActiveTendrilLeases(ctx context.Context, userID string) ([]models.TendrilLease, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+tendrilLeaseCols+` FROM tendril_leases
		 WHERE user_id = $1 AND status = 'active' ORDER BY started_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.TendrilLease
	for rows.Next() {
		l, err := scanTendrilLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListExpiredTendrilLeases feeds the reaper. An active lease past its funded
// window is a meter still running against the shared pool with nobody watching.
func (s *Store) ListExpiredTendrilLeases(ctx context.Context, now time.Time) ([]models.TendrilLease, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+tendrilLeaseCols+` FROM tendril_leases
		 WHERE status = 'active' AND funded_until <= $1 ORDER BY funded_until`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.TendrilLease
	for rows.Next() {
		l, err := scanTendrilLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) MarkTendrilLeaseReleased(ctx context.Context, id string, usedSeconds, chargedUSDMicros int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE tendril_leases
		   SET status = 'released', released_at = NOW(),
		       used_seconds = $2, charged_usd_micros = $3
		 WHERE id = $1 AND status = 'active'`, id, usedSeconds, chargedUSDMicros)
	return err
}

```

**No pool-wide lock is needed, and adding one would be misleading.** Concurrency safety here comes from the per-user `CHECK (tendril_credit_usd_micros >= 0)` in Task 6: two concurrent rents by the *same* user serialize on that user's row and the second one aborts if it would overdraw, while two rents by *different* users cannot interfere at all because neither consults the shared pool. A global lock would suggest the pool is the contended resource. It is not — it is a float that only ever grows by topups, each of which credits exactly one user.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/agentmesh_test go test ./internal/db/ -run TendrilLease -v && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/db/ backend/internal/models/types.go
git commit -m "db: tendril_leases table, model, and store"
```

---

### Task 6: Per-user Tendril credit ledger and the Topup node

Wallet 2's Tendril balance is universal — every user's topups pile into it. A user may only ever spend **what they themselves bought**, so spending authority lives in a per-user sub-ledger and is never checked against the pool.

**Files:**
- Create: `backend/internal/db/migrations/000016_tendril_credits.up.sql`
- Create: `backend/internal/db/migrations/000016_tendril_credits.down.sql`
- Create: `backend/internal/db/tendril_credit.go`
- Create: `backend/internal/db/tendril_credit_test.go`
- Modify: `backend/internal/models/types.go`
- Modify: `backend/internal/engine/nodes/tendril.go`

**Interfaces:**
- Consumes: `payTendril` (Task 7 defines it; write it here and Task 7 reuses it — if implementing in order, define `payTendril` in this task).
- Produces:
  ```go
  // models
  type TendrilCreditEntry struct {
      ID, UserID, Kind string
      AmountUSDMicros  int64   // always positive; Kind says which direction
      LeaseID          *string
      TxID             *string
      CreatedAt        time.Time
  }
  const (
      TendrilCreditKindTopup   = "topup"    // AgentMesh credits -> Tendril credits
      TendrilCreditKindCharge  = "charge"   // Tendril credits -> compute
      TendrilCreditKindRefund  = "refund"   // unused reservation returned
  )

  // store
  func (s *Store) TendrilCreditBalance(ctx context.Context, userID string) (int64, error)
  func (s *Store) ConvertCreditsToTendril(ctx context.Context, userID string, amountUSDMicros int64, txID string) (newTendrilBalance int64, err error)
  func (s *Store) ChargeTendrilCredit(ctx context.Context, userID, leaseID, kind string, amountUSDMicros int64) error
  ```

- [ ] **Step 1: Write the migration and model**

`000016_tendril_credits.up.sql`:
```sql
-- A user's spendable Tendril balance. Distinct from credit_balance_usd_micros:
-- AgentMesh credits buy Tendril credits, Tendril credits buy machine time.
-- The CHECK is the whole safety property — it makes overspending a database
-- error rather than something application code has to remember to prevent.
ALTER TABLE users ADD COLUMN IF NOT EXISTS tendril_credit_usd_micros BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD CONSTRAINT tendril_credit_non_negative
    CHECK (tendril_credit_usd_micros >= 0);

CREATE TABLE IF NOT EXISTS tendril_credit_ledger (
    id                TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id           TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind              TEXT NOT NULL,
    amount_usd_micros BIGINT NOT NULL,
    lease_id          TEXT,
    tx_id             TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tendril_credit_kind_valid CHECK (kind IN ('topup', 'charge', 'refund')),
    CONSTRAINT tendril_credit_amount_positive CHECK (amount_usd_micros > 0)
);

CREATE INDEX IF NOT EXISTS idx_tendril_credit_ledger_user ON tendril_credit_ledger(user_id, created_at DESC);
```

`000016_tendril_credits.down.sql`:
```sql
DROP TABLE IF EXISTS tendril_credit_ledger;
ALTER TABLE users DROP CONSTRAINT IF EXISTS tendril_credit_non_negative;
ALTER TABLE users DROP COLUMN IF EXISTS tendril_credit_usd_micros;
```

Add to `models/types.go` the `TendrilCreditEntry` struct and the three `TendrilCreditKind*` constants exactly as given in the Interfaces block above.

- [ ] **Step 2: Write the failing test**

```go
package db

import (
	"context"
	"testing"
)

// The topup is a transfer, not a grant: AgentMesh credits go down by exactly
// what Tendril credits go up by, in one transaction.
func TestConvertCreditsToTendrilIsAtomicTransfer(t *testing.T) {
	store, userID := setupUserWithCredits(t, 20_000_000) // $20 AgentMesh credits
	ctx := context.Background()

	newBal, err := store.ConvertCreditsToTendril(ctx, userID, 12_000_000, "TXID1")
	if err != nil {
		t.Fatalf("ConvertCreditsToTendril: %v", err)
	}
	if newBal != 12_000_000 {
		t.Errorf("tendril balance = %d, want 12000000", newBal)
	}
	agentMesh, err := store.GetCreditBalance(ctx, userID)
	if err != nil {
		t.Fatalf("GetCreditBalance: %v", err)
	}
	if agentMesh != 8_000_000 {
		t.Errorf("agentmesh balance = %d, want 8000000", agentMesh)
	}
}

// A user cannot buy Tendril credit they cannot afford, and a failed conversion
// must leave BOTH balances untouched.
func TestConvertCreditsToTendrilRejectsOverdraftAtomically(t *testing.T) {
	store, userID := setupUserWithCredits(t, 5_000_000)
	ctx := context.Background()

	if _, err := store.ConvertCreditsToTendril(ctx, userID, 12_000_000, "TXID1"); err == nil {
		t.Fatal("expected an error converting more than the AgentMesh balance")
	}
	agentMesh, _ := store.GetCreditBalance(ctx, userID)
	tendril, _ := store.TendrilCreditBalance(ctx, userID)
	if agentMesh != 5_000_000 || tendril != 0 {
		t.Errorf("balances moved on a failed conversion: agentmesh=%d tendril=%d", agentMesh, tendril)
	}
}

// This is the property that keeps one user off another user's hours: the check
// is against this user's own row, never against the shared pool.
func TestChargeTendrilCreditCannotOverspendOneUsersBalance(t *testing.T) {
	store, userID := setupUserWithCredits(t, 20_000_000)
	ctx := context.Background()
	if _, err := store.ConvertCreditsToTendril(ctx, userID, 6_000_000, "TXID1"); err != nil {
		t.Fatalf("convert: %v", err)
	}

	if err := store.ChargeTendrilCredit(ctx, userID, "lease1", "charge", 6_000_001); err == nil {
		t.Fatal("expected an error charging more than this user's tendril credit")
	}
	if err := store.ChargeTendrilCredit(ctx, userID, "lease1", "charge", 6_000_000); err != nil {
		t.Fatalf("charging the exact balance should succeed: %v", err)
	}
	bal, _ := store.TendrilCreditBalance(ctx, userID)
	if bal != 0 {
		t.Errorf("balance = %d, want 0", bal)
	}
}

// Releasing early returns the unused reservation as Tendril credit — hours a
// user bought stay theirs rather than evaporating into the pool.
func TestRefundReturnsCreditToTheSameUser(t *testing.T) {
	store, userID := setupUserWithCredits(t, 20_000_000)
	ctx := context.Background()
	if _, err := store.ConvertCreditsToTendril(ctx, userID, 12_000_000, "TXID1"); err != nil {
		t.Fatalf("convert: %v", err)
	}
	if err := store.ChargeTendrilCredit(ctx, userID, "lease1", "charge", 12_000_000); err != nil {
		t.Fatalf("charge: %v", err)
	}
	if err := store.ChargeTendrilCredit(ctx, userID, "lease1", "refund", 9_000_000); err != nil {
		t.Fatalf("refund: %v", err)
	}
	bal, _ := store.TendrilCreditBalance(ctx, userID)
	if bal != 9_000_000 {
		t.Errorf("balance after refund = %d, want 9000000", bal)
	}
	// The refund must not touch AgentMesh credits — the user still holds
	// Tendril hours, they did not get their money back.
	agentMesh, _ := store.GetCreditBalance(ctx, userID)
	if agentMesh != 8_000_000 {
		t.Errorf("agentmesh balance = %d, want 8000000 (untouched by a tendril refund)", agentMesh)
	}
}
```

`setupUserWithCredits(t, micros)` creates a user with that AgentMesh credit balance and skips without `TEST_DATABASE_URL` — follow `backend/internal/db/credit_test.go`'s existing helper and reuse it rather than writing a second one.

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/agentmesh_test go test ./internal/db/ -run Tendril -v`
Expected: FAIL — `store.ConvertCreditsToTendril undefined`.

- [ ] **Step 4: Write the store methods**

```go
package db

import (
	"context"
	"fmt"
)

func (s *Store) TendrilCreditBalance(ctx context.Context, userID string) (int64, error) {
	var bal int64
	err := s.pool.QueryRow(ctx,
		`SELECT tendril_credit_usd_micros FROM users WHERE id = $1`, userID).Scan(&bal)
	return bal, err
}

// ConvertCreditsToTendril moves value between the user's two balances in one
// transaction. Both the debit and the credit are guarded by CHECK constraints,
// so a concurrent spend that would overdraw either side aborts the whole
// transfer rather than leaving the user credited on one side and not debited
// on the other.
//
// txID is the on-chain id of the topup settlement that put the matching USDC
// into the shared Wallet 2 pool — recorded so a user's Tendril credit is
// always traceable to the real payment that backs it.
func (s *Store) ConvertCreditsToTendril(ctx context.Context, userID string, amountUSDMicros int64, txID string) (int64, error) {
	if amountUSDMicros <= 0 {
		return 0, fmt.Errorf("tendril topup amount must be positive, got %d", amountUSDMicros)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var agentMeshBalance int64
	if err := tx.QueryRow(ctx, `
		UPDATE users SET credit_balance_usd_micros = credit_balance_usd_micros - $2
		 WHERE id = $1 RETURNING credit_balance_usd_micros`,
		userID, amountUSDMicros).Scan(&agentMeshBalance); err != nil {
		return 0, fmt.Errorf("insufficient AgentMesh credits for a %d micro Tendril topup: %w", amountUSDMicros, err)
	}

	var tendrilBalance int64
	if err := tx.QueryRow(ctx, `
		UPDATE users SET tendril_credit_usd_micros = tendril_credit_usd_micros + $2
		 WHERE id = $1 RETURNING tendril_credit_usd_micros`,
		userID, amountUSDMicros).Scan(&tendrilBalance); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO tendril_credit_ledger (user_id, kind, amount_usd_micros, tx_id)
		VALUES ($1, 'topup', $2, $3)`, userID, amountUSDMicros, nullIfEmpty(txID)); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tendrilBalance, nil
}

// ChargeTendrilCredit debits (kind "charge") or credits back (kind "refund")
// one user's Tendril balance. The non-negative CHECK is what stops a user
// spending hours they did not buy — the shared pool is never consulted.
func (s *Store) ChargeTendrilCredit(ctx context.Context, userID, leaseID, kind string, amountUSDMicros int64) error {
	if amountUSDMicros <= 0 {
		return fmt.Errorf("tendril charge amount must be positive, got %d", amountUSDMicros)
	}
	delta := -amountUSDMicros
	if kind == "refund" {
		delta = amountUSDMicros
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE users SET tendril_credit_usd_micros = tendril_credit_usd_micros + $2
		 WHERE id = $1`, userID, delta); err != nil {
		return fmt.Errorf("insufficient Tendril credit for %d micros: %w", amountUSDMicros, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tendril_credit_ledger (user_id, kind, amount_usd_micros, lease_id)
		VALUES ($1, $2, $3, $4)`, userID, kind, amountUSDMicros, nullIfEmpty(leaseID)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

If `nullIfEmpty` already exists in package `db`, use the existing one instead of declaring a second.

- [ ] **Step 5: Write the topup executor**

Append to `backend/internal/engine/nodes/tendril.go`:

```go
// executeTendrilTopup buys Tendril credit for THIS user. It settles a real
// USDC payment into the shared Wallet 2 pool and, in the same breath, moves
// the same value from the user's AgentMesh credits to their Tendril credits.
//
// The pool is universal — every user's topups accumulate in it — but what a
// user may spend is only ever their own converted balance. That is why the
// conversion is not optional bookkeeping: it IS the spending authority.
func executeTendrilTopup(ctx context.Context, node models.WorkflowNode, cfg TendrilConfig) (any, error) {
	amountUSD, err := parseTopupUSD(node.TendrilAmount)
	if err != nil {
		return nil, err
	}
	atomic := int64(amountUSD*1e6 + 0.5)

	// Tendril's own bounds, read live rather than hardcoded.
	platform, err := cfg.Client.Platform(ctx)
	if err != nil {
		return nil, fmt.Errorf("tendril: platform: %w", err)
	}
	if platform.MinTopUpAtomic > 0 && atomic < platform.MinTopUpAtomic {
		return nil, fmt.Errorf("tendril: minimum topup is %s", formatUSDCAmount(platform.MinTopUpAtomic))
	}
	if platform.MaxTopUpAtomic > 0 && atomic > platform.MaxTopUpAtomic {
		return nil, fmt.Errorf("tendril: maximum topup is %s", formatUSDCAmount(platform.MaxTopUpAtomic))
	}

	// Refuse before paying if the user cannot afford it. Settling first and
	// discovering the shortfall afterwards would put real USDC in the pool
	// with no user entitled to spend it.
	balance, err := cfg.Store.TendrilCreditBalance(ctx, cfg.UserID)
	if err != nil {
		return nil, err
	}
	agentMeshBalance, err := cfg.Store.CreditBalance(ctx, cfg.UserID)
	if err != nil {
		return nil, err
	}
	if agentMeshBalance < atomic {
		return nil, fmt.Errorf("tendril: topup of %s needs %s in AgentMesh credits, you have %s",
			formatUSDCAmount(atomic), formatUSDCAmount(atomic), formatUSDCAmount(agentMeshBalance))
	}

	receipt, err := payTendril(ctx, cfg, fmt.Sprintf("/topup?amount=%d", atomic), nil, "")
	if err != nil {
		return nil, err
	}

	txID := ""
	if m, ok := receipt.(map[string]any); ok {
		txID, _ = m["txId"].(string)
	}
	newBalance, err := cfg.Store.ConvertCreditsToTendril(ctx, cfg.UserID, atomic, txID)
	if err != nil {
		// The USDC really moved into the pool. Surface that loudly: the pool
		// is now larger than the sum of user entitlements, which is the one
		// direction of drift that is safe but must still be reconciled.
		return nil, fmt.Errorf("tendril: topup settled on-chain (tx %s) but crediting your balance failed — contact support with that tx id: %w", txID, err)
	}

	out := map[string]any{
		"toppedUp":             formatUSDCAmount(atomic),
		"tendrilCreditBalance": formatUSDCAmount(newBalance),
		"previousBalance":      formatUSDCAmount(balance),
		"note":                 "Tendril credit is separate from your AgentMesh credits and can only be spent on Tendril machine time.",
	}
	if m, ok := receipt.(map[string]any); ok {
		for _, k := range []string{"txId", "explorerURL", "outboundTxId", "outboundExplorerURL"} {
			if v, ok := m[k]; ok {
				out[k] = v
			}
		}
	}
	return out, nil
}

func parseTopupUSD(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("tendril: set a topup amount in USD")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("tendril: topup amount %q is not a number", raw)
	}
	if v <= 0 {
		return 0, fmt.Errorf("tendril: topup amount must be positive, got %v", v)
	}
	return v, nil
}
```

Add `TendrilAmount string \`json:"tendrilAmount,omitempty"\`` to `models.WorkflowNode` beside the other Tendril fields, add `"topup"` to `ExecuteTendril`'s switch dispatching to `executeTendrilTopup`, and extend `TendrilStore` with:
```go
	TendrilCreditBalance(ctx context.Context, userID string) (int64, error)
	CreditBalance(ctx context.Context, userID string) (int64, error)
	ConvertCreditsToTendril(ctx context.Context, userID string, amountUSDMicros int64, txID string) (int64, error)
	ChargeTendrilCredit(ctx context.Context, userID, leaseID, kind string, amountUSDMicros int64) error
```
`db.Store` already has `GetCreditBalance`; either rename the interface method to match it or add a thin `CreditBalance` alias — do **not** add a second implementation of the same query.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd backend && TEST_DATABASE_URL=… go test ./internal/db/ -run Tendril -v && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/db/ backend/internal/models/types.go backend/internal/engine/nodes/tendril.go
git commit -m "tendril: per-user credit sub-ledger over the shared Wallet 2 pool"
```

---

### Task 7: The rent executor

**Files:**
- Create: `backend/internal/engine/nodes/tendril.go`
- Test: `backend/internal/engine/nodes/tendril_test.go`
- Modify: `backend/internal/models/types.go` (node fields)

**Interfaces:**
- Consumes: `tendril.Client`/`Session` (Tasks 1–2), `sshkeys.Generate` (Task 4), store methods (Task 5), and the **existing** `executeTool402V2Relay` path via `ExecuteTool402V2`.
- Produces:
  ```go
  type TendrilConfig struct {
      Client     *tendril.Client
      Session    *tendril.Session
      Store      TendrilStore     // interface, defined below
      EncryptKey string
      Relay      X402RelayConfig  // reused verbatim, not modified
      UserID     string
      WorkflowID string
      RunID      string
  }
  func ExecuteTendril(ctx context.Context, node models.WorkflowNode, rc RunContexter, cfg TendrilConfig) (any, error)
  func RequiredCreditAtomic(rateUSDMicrosPerHour int64, hours float64) int64
  ```
  Node fields added to `models.WorkflowNode`: `TendrilAction string`, `TendrilNodeID string`, `TendrilHours string`.

- [ ] **Step 1: Write the failing test**

```go
package nodes

import "testing"

// Renting is a flat 1¢ gate fee; hours are bought by holding credit. So "2
// hours on a $6/hr box" costs the user $12.00 of their own Tendril credit,
// plus the 1¢ gate fee for the rent call itself.
func TestRequiredCreditAtomic(t *testing.T) {
	cases := []struct {
		name  string
		rate  int64
		hours float64
		want  int64
	}{
		{"two hours at six dollars", 6_000_000, 2, 12_010_000},
		{"one hour at six dollars", 6_000_000, 1, 6_010_000},
		{"half hour at one fifty", 1_500_000, 0.5, 760_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RequiredCreditAtomic(tc.rate, tc.hours); got != tc.want {
				t.Errorf("RequiredCreditAtomic(%d, %v) = %d, want %d", tc.rate, tc.hours, got, tc.want)
			}
		})
	}
}

// Hours come off a canvas text field, so every rejection here is a rejection
// of real money being spent on a nonsense duration.
func TestParseHours(t *testing.T) {
	ok := map[string]float64{"1": 1, "2": 2, "0.5": 0.5, " 3 ": 3, "": 1}
	for in, want := range ok {
		got, err := parseHours(in)
		if err != nil {
			t.Errorf("parseHours(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseHours(%q) = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"0", "-1", "abc", "1e9", "25"} {
		if _, err := parseHours(bad); err == nil {
			t.Errorf("parseHours(%q) should have errored", bad)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/nodes/ -run 'TestRequiredCreditAtomic|TestParseHours' -v`
Expected: FAIL — `undefined: RequiredCreditAtomic`.

- [ ] **Step 3: Write the pure helpers plus the executor**

Add to `models.WorkflowNode` in `types.go`:
```go
	// Tendril node fields. TendrilAction is "rent" | "run" | "release";
	// TendrilHours is how many hours of credit to guarantee before renting,
	// as a decimal string ("1", "2", "0.5") — a string, like every other
	// canvas-entered value on this struct.
	TendrilAction string `json:"tendrilAction,omitempty"`
	TendrilNodeID string `json:"tendrilNodeId,omitempty"`
	TendrilHours  string `json:"tendrilHours,omitempty"`
```

`backend/internal/engine/nodes/tendril.go`:
```go
package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sshkeys"
	"github.com/agentmesh/backend/internal/tendril"
	"github.com/agentmesh/backend/internal/wallet"
)

// tendrilRentGateFeeAtomic is Tendril's flat charge to open a lease, confirmed
// live 2026-08-04 in the /x402/rent challenge (amount "10000", 6 decimals).
// Renting does NOT buy time — time meters from the paying address's credit
// balance, which is why RequiredCreditAtomic adds hours on top.
const tendrilRentGateFeeAtomic int64 = 10_000

// maxTendrilHours caps a single rent. At $6/hr a fat-fingered "100" would
// commit $600 of real mainnet USDC in one click.
const maxTendrilHours = 24.0

// RequiredCreditAtomic is how much of THIS USER's Tendril credit a rent
// reserves. Not the pool's balance — the pool is a shared custodial float that
// holds every user's topups at once, so it can never be the thing a rent is
// authorized against.
func RequiredCreditAtomic(rateUSDMicrosPerHour int64, hours float64) int64 {
	return int64(float64(rateUSDMicrosPerHour)*hours+0.5) + tendrilRentGateFeeAtomic
}

func parseHours(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 1, nil
	}
	h, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("tendril: hours %q is not a number", raw)
	}
	if h <= 0 {
		return 0, fmt.Errorf("tendril: hours must be positive, got %v", h)
	}
	if h > maxTendrilHours {
		return 0, fmt.Errorf("tendril: hours must be at most %v, got %v", maxTendrilHours, h)
	}
	return h, nil
}

// TendrilStore is the slice of *db.Store this package needs, as an interface
// so tests can drive the executor without a database.
type TendrilStore interface {
	InsertTendrilLease(ctx context.Context, l models.TendrilLease) (models.TendrilLease, error)
	GetTendrilLease(ctx context.Context, id string) (models.TendrilLease, error)
	MarkTendrilLeaseReleased(ctx context.Context, id string, usedSeconds, chargedUSDMicros int64) error
	LatestActiveLeaseForRun(ctx context.Context, runID string) (models.TendrilLease, error)
	// Credit sub-ledger (Task 6) — the authority on what THIS user may spend.
	TendrilCreditBalance(ctx context.Context, userID string) (int64, error)
	CreditBalance(ctx context.Context, userID string) (int64, error)
	ConvertCreditsToTendril(ctx context.Context, userID string, amountUSDMicros int64, txID string) (int64, error)
	ChargeTendrilCredit(ctx context.Context, userID, leaseID, kind string, amountUSDMicros int64) error
}

type TendrilConfig struct {
	Client     *tendril.Client
	Session    *tendril.Session
	Store      TendrilStore
	EncryptKey string
	Relay      X402RelayConfig
	UserID     string
	WorkflowID string
	RunID      string
}

func ExecuteTendril(ctx context.Context, node models.WorkflowNode, rc RunContexter, cfg TendrilConfig) (any, error) {
	switch node.TendrilAction {
	case "", "rent":
		return executeTendrilRent(ctx, node, cfg)
	case "topup":
		return executeTendrilTopup(ctx, node, cfg)
	case "run":
		return executeTendrilRun(ctx, node, rc, cfg)
	case "release":
		return executeTendrilRelease(ctx, node, cfg)
	default:
		return nil, fmt.Errorf("tendril: unknown action %q", node.TendrilAction)
	}
}

func executeTendrilRent(ctx context.Context, node models.WorkflowNode, cfg TendrilConfig) (any, error) {
	hours, err := parseHours(node.TendrilHours)
	if err != nil {
		return nil, err
	}

	machines, err := cfg.Client.OnlineNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("tendril: market: %w", err)
	}
	if len(machines) == 0 {
		return nil, fmt.Errorf("tendril: no machines are online right now")
	}
	machine := machines[0] // cheapest, per OnlineNodes' ordering
	if node.TendrilNodeID != "" {
		found := false
		for _, m := range machines {
			if m.ID == node.TendrilNodeID {
				machine, found = m, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("tendril: machine %q is not online", node.TendrilNodeID)
		}
	}

	// Reserve the hours against THIS user's Tendril credit. The shared Wallet 2
	// pool is deliberately not consulted: it holds every user's topups at once,
	// so checking it would let one user rent on hours somebody else bought.
	// Their own balance is the only authority.
	need := RequiredCreditAtomic(machine.RateUSDMicrosPerHour(), hours)
	userCredit, err := cfg.Store.TendrilCreditBalance(ctx, cfg.UserID)
	if err != nil {
		return nil, err
	}
	if userCredit < need {
		return nil, fmt.Errorf(
			"tendril: %v hour(s) on %s costs %s but your Tendril credit is %s — add a Topup node, or raise its amount, before this Rent node",
			hours, machine.ID, formatUSDCAmount(need), formatUSDCAmount(userCredit))
	}
	if err := cfg.Store.ChargeTendrilCredit(ctx, cfg.UserID, "", "charge", need); err != nil {
		return nil, fmt.Errorf("tendril: reserve credit: %w", err)
	}
	// From here on the user has paid; any failure below must hand the
	// reservation back rather than silently keeping it.
	reserved := true
	defer func() {
		if reserved {
			if err := cfg.Store.ChargeTendrilCredit(context.Background(), cfg.UserID, "", "refund", need); err != nil {
				log.Printf("tendril: FAILED to refund %d micros to user %s after a failed rent: %v", need, cfg.UserID, err)
			}
		}
	}()

	// A sanity check on the custodial float, not an authorization check. If the
	// pool cannot cover what users have collectively bought, the invariant has
	// been violated upstream and renting would silently fail at Tendril's end.
	if poolBalance, perr := cfg.Session.Balance(ctx); perr == nil && poolBalance < need {
		return nil, fmt.Errorf("tendril: the platform pool is short (%s available, %s needed) — this is a platform-side problem, not yours; no credit was spent",
			formatUSDCAmount(poolBalance), formatUSDCAmount(need))
	}

	sshPub, sshPriv, err := sshkeys.Generate()
	if err != nil {
		return nil, fmt.Errorf("tendril: ssh keygen: %w", err)
	}
	body, _ := json.Marshal(map[string]string{"sshPubKey": sshPub})
	raw, err := payTendril(ctx, cfg, "/x402/rent?nodeId="+machine.ID, body, "")
	if err != nil {
		return nil, err
	}

	lease, err := decodeRentResponse(raw)
	if err != nil {
		return nil, err
	}

	tokenEnc, err := wallet.Encrypt(lease.LeaseToken, cfg.EncryptKey)
	if err != nil {
		return nil, fmt.Errorf("tendril: encrypt lease token: %w", err)
	}
	keyEnc, err := wallet.Encrypt(sshPriv, cfg.EncryptKey)
	if err != nil {
		return nil, fmt.Errorf("tendril: encrypt ssh key: %w", err)
	}
	passwordEnc := ""
	if lease.SSH.Password != "" {
		if passwordEnc, err = wallet.Encrypt(lease.SSH.Password, cfg.EncryptKey); err != nil {
			return nil, fmt.Errorf("tendril: encrypt ssh password: %w", err)
		}
	}

	fundedUntil, err := time.Parse(time.RFC3339, lease.FundedUntil)
	if err != nil {
		fundedUntil = time.Now().Add(time.Duration(hours * float64(time.Hour)))
	}

	saved, err := cfg.Store.InsertTendrilLease(ctx, models.TendrilLease{
		UserID: cfg.UserID, WorkflowID: cfg.WorkflowID, RunID: cfg.RunID, NodeID: node.ID,
		LeaseID: lease.LeaseID, LeaseTokenEnc: tokenEnc,
		TendrilNodeID: machine.ID, TendrilNodeLabel: machine.Label,
		SSHHost: lease.SSH.Host, SSHPort: lease.SSH.Port, SSHUsername: lease.SSH.Username,
		SSHCommand: lease.SSH.Command, SSHPublicKey: sshPub,
		SSHPrivateKeyEnc: keyEnc, SSHPasswordEnc: passwordEnc,
		RateUSDMicrosPerHour: machine.RateUSDMicrosPerHour(),
		HoursPurchased:       hours,
		ReservedUSDMicros:    need,
		FundedUntil:          fundedUntil,
	})
	if err != nil {
		return nil, fmt.Errorf("tendril: persist lease: %w", err)
	}
	// The lease exists and is metering — the reservation is now legitimately
	// spent, so stop the deferred refund from clawing it back.
	reserved = false

	remaining, _ := cfg.Store.TendrilCreditBalance(ctx, cfg.UserID)

	// The lease token never leaves the server. Everything here is safe to show
	// in the console and to cache in localStorage with the run transcript.
	out := map[string]any{
		"agentMeshLeaseId": saved.ID,
		"leaseId":          lease.LeaseID,
		"machine":          map[string]any{"id": machine.ID, "label": machine.Label, "cpuCores": machine.CPUCores, "ramMb": machine.RAMMb, "pricePerHourUsd": machine.PricePerHourUSD},
		"hours":            hours,
		"ssh":              map[string]any{"host": lease.SSH.Host, "port": lease.SSH.Port, "username": lease.SSH.Username, "command": lease.SSH.Command},
		"fundedUntil":      fundedUntil.Format(time.RFC3339),
		"reservedUsd":      formatUSDCAmount(need),
		// What the user has left to spend on Tendril — the number the canvas
		// shows them, and the only balance that governs what they may rent.
		"tendrilCreditBalance": formatUSDCAmount(remaining),
	}
	if m, ok := raw.(map[string]any); ok {
		for _, k := range []string{"txId", "explorerURL", "outboundTxId", "outboundExplorerURL"} {
			if v, ok := m[k]; ok {
				out[k] = v
			}
		}
	}
	return out, nil
}

type rentResponse struct {
	LeaseID     string `json:"leaseId"`
	LeaseToken  string `json:"leaseToken"`
	FundedUntil string `json:"fundedUntil"`
	SSH         struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Command  string `json:"command"`
		Password string `json:"password"`
	} `json:"ssh"`
}

func decodeRentResponse(raw any) (rentResponse, error) {
	var lease rentResponse
	blob, err := json.Marshal(raw)
	if err != nil {
		return lease, fmt.Errorf("tendril: rent response: %w", err)
	}
	if err := json.Unmarshal(blob, &lease); err != nil {
		return lease, fmt.Errorf("tendril: rent response: %w", err)
	}
	if lease.LeaseID == "" || lease.LeaseToken == "" {
		// Money has already moved at this point, so say so loudly rather than
		// returning a lease-shaped zero value.
		return lease, fmt.Errorf("tendril: rent settled but returned no lease: %s", truncateJSON(blob))
	}
	return lease, nil
}

func truncateJSON(b []byte) string {
	if len(b) > 400 {
		return string(b[:400])
	}
	return string(b)
}

// payTendril runs one paid Tendril call through the EXISTING relay path by
// synthesizing a tool402 node for it. Nothing about payment is reimplemented
// here: ExecuteTool402V2 probes the 402, picks the group-vs-single signer off
// extra.feePayer, settles through Wallet 1 -> Wallet 2 -> Tendril, and bills
// the user's credit balance through cfg.Relay's ledger exactly as a normal
// x402 tool call does.
//
// bearer, when set, is the Tendril lease token the TARGET needs (for /x402/run
// against a machine the user already holds). It is not auth for our own relay
// — see the X-Relay-Auth passthrough added in Task 10.
func payTendril(ctx context.Context, cfg TendrilConfig, path string, body []byte, bearer string) (any, error) {
	node := models.WorkflowNode{
		ID:       "tendril:" + path,
		Type:     models.NodeTypeTool402,
		Endpoint: strings.TrimRight(cfg.Client.BaseURL(), "/") + path,
		Method:   http.MethodPost,
	}
	if len(body) > 0 {
		node.BodyMode = models.BodyModeJSON
		node.BodyTemplate = string(body)
	}
	if bearer != "" {
		node.TendrilLeaseToken = bearer
	}
	res, err := ExecuteTool402V2(ctx, node, emptyRunContext{}, models.AgentWallet{}, nil, cfg.Relay)
	if err != nil {
		return nil, fmt.Errorf("tendril %s: %w", path, err)
	}
	return res.Response, nil
}

// emptyRunContext satisfies RunContexter for the synthesized nodes above,
// whose bodies are fully specified by BodyTemplate and must never pick up the
// run's free-text trigger message.
type emptyRunContext struct{}

func (emptyRunContext) Message() string { return "" }
```

Add a `BaseURL()` accessor to `tendril.Client`:
```go
func (c *Client) BaseURL() string { return c.baseURL }
```

Check `RunContexter`'s full method set in `backend/internal/engine/nodes/tool.go` and implement every method on `emptyRunContext` — if it has more than `Message()`, add the rest returning zero values.

Raise the relay ceiling in `backend/cmd/server/main.go:85`:
```go
	// $20.00 default, up from $5.00: Tendril's cheapest online machine is
	// $6.00/hour and a 2-hour rent tops the shared pool up by $12.00 in one
	// call, which the old ceiling rejected outright.
	maxRelayOutboundUSDMicros := envInt64Or("MAX_RELAY_OUTBOUND_USD_MICROS", 20_000_000)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/engine/nodes/ -run 'TestRequiredCreditAtomic|TestParseHours' -v && go build ./...`
Expected: PASS, clean build.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/nodes/tendril.go backend/internal/engine/nodes/tendril_test.go backend/internal/models/types.go backend/internal/tendril/client.go backend/cmd/server/main.go
git commit -m "tendril: rent executor over the existing relay payment path"
```

---

### Task 8: Wire the node type into the runner

**Files:**
- Modify: `backend/internal/models/types.go` (add `NodeTypeTendril`)
- Modify: `backend/internal/engine/runner.go` (`executeNode` switch at :647, `NewRunner` signature/fields)
- Modify: `backend/cmd/server/main.go` (construct the client + session, pass to `NewRunner`)
- Test: `backend/internal/engine/runner_tendril_test.go`

**Interfaces:**
- Consumes: `ExecuteTendril`, `TendrilConfig` (Tasks 6–7).
- Produces: `models.NodeTypeTendril NodeType = "tendril"`; `Runner.SetTendril(client *tendril.Client, session *tendril.Session)`.

- [ ] **Step 1: Write the failing test**

```go
package engine

import (
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// A tendril node is a flow step in its own right, not an agent resource: the
// user's Tendril-only workflow is trigger -> rent -> end with no agent at all.
// It must therefore survive topological sort as a normal node.
func TestTendrilNodeIsATopologicalStep(t *testing.T) {
	nodes := []models.WorkflowNode{
		{ID: "n1", Type: models.NodeTypeTrigger},
		{ID: "n2", Type: models.NodeTypeTendril, TendrilAction: "rent"},
		{ID: "n3", Type: models.NodeTypeEnd},
	}
	edges := []models.WorkflowEdge{
		{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
		{ID: "e2", From: "n2", To: "n3", Kind: models.EdgeKindFlow},
	}
	levels, err := TopologicalSort(nodes, edges)
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if len(levels) != 3 {
		t.Fatalf("got %d levels, want 3", len(levels))
	}
	if levels[1][0].ID != "n2" {
		t.Errorf("level 1 = %s, want n2", levels[1][0].ID)
	}
}

// Without a configured registry the node must refuse before any money moves,
// rather than nil-panicking inside the executor.
func TestTendrilNodeWithoutConfigErrors(t *testing.T) {
	r := &Runner{}
	_, err := r.executeNode(t.Context(),
		models.WorkflowNode{ID: "n2", Type: models.NodeTypeTendril, TendrilAction: "rent"},
		nil, nil, NewRunContext("run1", nil), models.Run{ID: "run1"}, models.Workflow{ID: "wf1"})
	if err == nil {
		t.Fatal("expected an error when Tendril is not configured")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/ -run TestTendrilNode -v`
Expected: FAIL — `models.NodeTypeTendril undefined`.

- [ ] **Step 3: Write the implementation**

In `types.go`'s NodeType const block:
```go
	NodeTypeTendril  NodeType = "tendril"
```

In `runner.go`, add fields to `Runner` and a setter:
```go
// SetTendril supplies the Tendril registry client and the Wallet 2 session
// that reads the shared credit pool. Left nil when TENDRIL_REGISTRY_URL is
// unset, in which case tendril nodes fail closed.
func (r *Runner) SetTendril(client *tendril.Client, session *tendril.Session) {
	r.tendrilClient = client
	r.tendrilSession = session
}
```

Add the `executeNode` case, placed immediately after `case models.NodeTypeTool402:`:
```go
	case models.NodeTypeTendril:
		if r.tendrilClient == nil || r.tendrilSession == nil {
			return nil, fmt.Errorf("tendril: TENDRIL_REGISTRY_URL is not configured on this server")
		}
		// Same conservative pre-flight as tool402: one cheap balance check
		// before any network call that could spend money.
		if err := r.preflightCheck(ctx, wf, models.X402PlatformFeeUSDMicros); err != nil {
			return nil, err
		}
		usdcSigner, _ := r.walletSvc.(nodes.USDCGroupSigner)
		ledger := r.newPaymentLedger(wf, run)
		return nodes.ExecuteTendril(ctx, node, rc, nodes.TendrilConfig{
			Client:     r.tendrilClient,
			Session:    r.tendrilSession,
			Store:      r.store,
			EncryptKey: r.encryptionKey,
			UserID:     wf.UserID,
			WorkflowID: wf.ID,
			RunID:      run.ID,
			Relay: nodes.X402RelayConfig{
				USDCSigner:               usdcSigner,
				PlatformSpendEncMnemonic: r.platformSpendEncMnemonic,
				ExpectedAssetID:          r.x402.USDCAssetID,
				RelayBaseURL:             r.relayBaseURL,
				Ledger:                   nodes.RunLedger(ledger),
				LegacyLedger:             nodes.CallLedger(ledger),
			},
		})
```

`Runner` needs `tendrilClient *tendril.Client`, `tendrilSession *tendril.Session`, and `encryptionKey string` fields. Read `NewRunner` at `runner.go:54` and thread `encryptionKey` in the same way `platformSpendEncMnemonic` already is; update every `NewRunner` call site (`cmd/server/main.go` and any test doubles found by `grep -rn "NewRunner(" backend/`).

In `cmd/server/main.go`, after the runner is constructed:
```go
	if registryURL := envOr("TENDRIL_REGISTRY_URL", "https://tendrilregister.007575.xyz"); registryURL != "" {
		tc := tendril.NewClient(registryURL)
		// Wallet 2 is what pays Tendril through the relay, so Wallet 2's
		// address is the one Tendril keys the shared credit pool to — sign the
		// session with its mnemonic, not Wallet 1's.
		sess, err := tc.Session(ctx, walletSvc, platformWalletEncMnemonic)
		if err != nil {
			log.Printf("tendril: registry session unavailable (%v) — tendril nodes will fail closed", err)
		} else {
			runner.SetTendril(tc, sess)
			log.Printf("tendril: registry %s, pool wallet %s", registryURL, platformWalletAddr)
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/engine/... -v && go build ./...`
Expected: PASS, clean build.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/ backend/internal/models/types.go backend/cmd/server/main.go
git commit -m "engine: dispatch the tendril node type"
```

---

### Task 9: Canvas support for the Tendril node

**Files:**
- Modify: `frontend/src/lib/types.ts`, `frontend/src/lib/data.ts`
- Modify: `frontend/src/components/canvas/PalettePanel.tsx`, `Inspector.tsx`, `nodes/index.tsx`
- Create: `frontend/src/lib/tendril.ts`
- Test: `frontend/src/lib/tendril.test.ts`

**Interfaces:**
- Consumes: node fields from Tasks 6–7.
- Produces: `TENDRIL_TEMPLATES` in `data.ts`; `estimateLeaseCostUSD(pricePerHourUsd: number, hours: number): number` and `tendril.machines(): Promise<TendrilMachine[]>` in `lib/tendril.ts`.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { estimateLeaseCostUSD } from "./tendril";

describe("estimateLeaseCostUSD", () => {
  // Mirrors the backend's RequiredCreditAtomic: hourly rate x hours, plus the
  // flat $0.01 gate fee Tendril charges to open a lease.
  it("adds the flat rent gate fee to the metered hours", () => {
    expect(estimateLeaseCostUSD(6, 2)).toBeCloseTo(12.01, 6);
    expect(estimateLeaseCostUSD(6, 1)).toBeCloseTo(6.01, 6);
    expect(estimateLeaseCostUSD(1.5, 0.5)).toBeCloseTo(0.76, 6);
  });

  it("returns the bare gate fee for zero hours rather than NaN", () => {
    expect(estimateLeaseCostUSD(6, 0)).toBeCloseTo(0.01, 6);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npm test -- tendril`
Expected: FAIL — cannot resolve `./tendril`.

- [ ] **Step 3: Write the implementation**

`frontend/src/lib/tendril.ts`:
```ts
import { BASE } from "@/lib/api";

// Tendril charges a flat 0.01 USDC to open a lease; the hours themselves meter
// against credit at the machine's hourly rate. Kept in sync with the backend's
// tendrilRentGateFeeAtomic.
export const TENDRIL_RENT_GATE_FEE_USD = 0.01;

export interface TendrilMachine {
  id: string;
  label: string;
  cpuCores: number;
  ramMb: number;
  gpu: string | null;
  pricePerHourUsd: number;
}

export function estimateLeaseCostUSD(
  pricePerHourUsd: number,
  hours: number,
): number {
  return pricePerHourUsd * hours + TENDRIL_RENT_GATE_FEE_USD;
}

export const tendril = {
  async machines(): Promise<TendrilMachine[]> {
    const res = await fetch(`${BASE}/tendril/machines`, {
      credentials: "include",
    });
    if (!res.ok) throw new Error(`machines: ${res.status}`);
    const body = (await res.json()) as { machines: TendrilMachine[] };
    return body.machines ?? [];
  },
};
```

If `lib/api.ts` does not export `BASE`, export it there, or import the existing request helper that file already uses and follow its pattern exactly rather than hand-rolling a second fetch convention.

In `lib/types.ts`: add `"tendril"` to the `NodeType` union, and add the fields
```ts
  // tendril-specific
  tendrilAction?: "topup" | "rent" | "run" | "release";
  tendrilNodeId?: string;
  tendrilHours?: string;
  // USD of AgentMesh credit to convert into Tendril credit, on a topup node.
  tendrilAmount?: string;
```

In `lib/data.ts`, add the meta entry (note all three ports — `top` is what lets a future agent attach a Tendril node and drive the machine itself):
```ts
  tendril: { w: 240, h: 96, ports: ["in", "out", "top"] },
```
and the templates:
```ts
export const TENDRIL_TEMPLATES = [
  {
    id: "tendril_topup",
    name: "Buy Tendril Credit",
    desc: "AgentMesh credits → Tendril credit",
    action: "topup" as const,
    icon: "＄",
  },
  {
    id: "tendril_rent",
    name: "Rent a Machine",
    desc: "Open a metered SSH session",
    action: "rent" as const,
    icon: "▣",
  },
  {
    id: "tendril_run",
    name: "Run a Job",
    desc: "Execute Python on the machine",
    action: "run" as const,
    icon: "▶",
  },
  {
    id: "tendril_release",
    name: "Release",
    desc: "Stop the meter and bill",
    action: "release" as const,
    icon: "■",
  },
];
```

In `PalettePanel.tsx`, add a tab following the exact shape of the existing `TOOL402_TEMPLATES` tab:
```tsx
  {
    key: "tendril",
    label: "Tendril",
    items: () => TENDRIL_TEMPLATES,
    map: (it: (typeof TENDRIL_TEMPLATES)[0]): Partial<WorkflowNode> => ({
      type: "tendril",
      template: it.id,
      name: it.name,
      icon: it.icon,
      sub: it.desc,
      tendrilAction: it.action,
      tendrilHours: "1",
      tendrilAmount: "10",
    }),
  },
```

In `nodes/index.tsx`, add `case "tendril":` to the renderer switch at line 30, reusing the `tool402` card renderer and showing `sub` plus, when `tendrilAction === "rent"`, `${tendrilHours}h`.

In `Inspector.tsx`, add a `{selected.type === "tendril" && (…)}` block alongside the existing per-type blocks (line ~127). It always renders the **Tendril credit balance** at the top, fetched from `tendril.credit()` — this is the number that decides what the user can rent, and it is not the same thing as their AgentMesh credits:

```tsx
<div style={{ fontSize: 12, opacity: 0.85 }}>
  Tendril credit: <strong>${credit.toFixed(2)}</strong>
  {machine && (
    <> — about {(credit / machine.pricePerHourUsd).toFixed(1)} h on {machine.label || machine.id}</>
  )}
  <div style={{ opacity: 0.6, marginTop: 2 }}>
    Separate from your AgentMesh credits. Buy more with a Topup node.
  </div>
</div>
```

Then, per action:
- **`topup`** — an amount `<input type="number" min="0.1" step="0.5">` bound to `tendrilAmount`, plus the line `Converts $X.XX of your AgentMesh credits into Tendril credit.`
- **`rent`** — a machine `<select>` populated from `tendril.machines()`, each option labelled `` `${label || id} — ${cpuCores} vCPU, ${Math.round(ramMb/1024)} GB — $${pricePerHourUsd}/hr` ``, bound to `tendrilNodeId`, with a leading "Cheapest online" option whose value is `""`; an hours `<input type="number" min="0.5" step="0.5" max="24">` bound to `tendrilHours`; and a cost line `Costs $${estimateLeaseCostUSD(rate, hours).toFixed(2)} of Tendril credit`, rendered in a warning colour when it exceeds the balance shown above, with the text `— not enough Tendril credit, add a Topup node`.
- **`run`** — a `<textarea>` bound to the `customParams` entry named `payload`, matching how the tool402 block already edits custom params.

Add to `lib/tendril.ts`:
```ts
export async function credit(): Promise<number> {
  const res = await fetch(`${BASE}/tendril/credits`, { credentials: "include" });
  if (!res.ok) throw new Error(`credits: ${res.status}`);
  const body = (await res.json()) as { tendrilCreditUsdMicros: number };
  return body.tendrilCreditUsdMicros / 1e6;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npm test -- tendril && npm run typecheck`
Expected: PASS, no type errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/ frontend/src/components/canvas/
git commit -m "canvas: tendril node type, palette tab, and machine picker"
```

---

## Phase 3 — Run, release, and don't leak a meter

### Task 10: Run and release executors

**Files:**
- Modify: `backend/internal/engine/nodes/tendril.go`
- Test: `backend/internal/engine/nodes/tendril_test.go` (append)

**Interfaces:**
- Consumes: Task 7's `TendrilConfig`, Tasks 5–6's store methods.
- Produces: `executeTendrilRun`, `executeTendrilRelease`, and `ReleaseLease(ctx context.Context, cfg TendrilConfig, lease models.TendrilLease) (tendril.ReleaseResult, error)` — exported so the reaper (Task 10) and the REST handler (Task 11) share one release implementation.

- [ ] **Step 1: Write the failing test**

```go
// Release is the only place compute is actually billed, so it must persist
// what Tendril reported rather than what AgentMesh predicted.
func TestReleaseLeasePersistsTendrilsOwnCharge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/x402/leases/lease_9k2m" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer plain-token" {
			t.Errorf("auth = %q, want the decrypted lease token", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"usedSeconds":1800,"chargedAtomic":3000000,"balance":9000000}`))
	}))
	defer srv.Close()

	enc, err := wallet.Encrypt("plain-token", testEncKey)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	store := &fakeTendrilStore{}
	res, err := ReleaseLease(context.Background(), TendrilConfig{
		Client: tendril.NewClient(srv.URL), Store: store, EncryptKey: testEncKey,
	}, models.TendrilLease{ID: "row1", LeaseID: "lease_9k2m", LeaseTokenEnc: enc})
	if err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}
	if res.ChargedAtomic != 3_000_000 || res.UsedSeconds != 1800 {
		t.Errorf("result = %+v", res)
	}
	if store.releasedID != "row1" || store.releasedCharged != 3_000_000 || store.releasedSeconds != 1800 {
		t.Errorf("store got id=%q seconds=%d charged=%d",
			store.releasedID, store.releasedSeconds, store.releasedCharged)
	}
}

// Releasing twice must be harmless — the reaper and a user clicking Release can
// race, and a double DELETE against Tendril would surface as a run failure.
func TestReleaseLeaseIsIdempotentOnAlreadyReleased(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"lease not found"}`))
	}))
	defer srv.Close()

	enc, _ := wallet.Encrypt("plain-token", testEncKey)
	store := &fakeTendrilStore{}
	if _, err := ReleaseLease(context.Background(), TendrilConfig{
		Client: tendril.NewClient(srv.URL), Store: store, EncryptKey: testEncKey,
	}, models.TendrilLease{ID: "row1", LeaseID: "gone", LeaseTokenEnc: enc}); err != nil {
		t.Fatalf("ReleaseLease on a missing lease should not error: %v", err)
	}
	if store.releasedID != "row1" {
		t.Error("a lease Tendril no longer knows about must still be marked released locally")
	}
}

// Releasing early must hand the unused reservation back as TENDRIL credit —
// the hours stay the user's. Refunding to AgentMesh credit instead would let a
// user cycle rent/release to convert Tendril credit into general platform
// credit the pool cannot honour.
func TestReleaseRefundsUnusedReservationAsTendrilCredit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Reserved 2h at $6 (+1c gate) = 12.01; actually used 30 min = $3.00.
		w.Write([]byte(`{"usedSeconds":1800,"chargedAtomic":3000000,"balance":0}`))
	}))
	defer srv.Close()

	enc, _ := wallet.Encrypt("plain-token", testEncKey)
	store := &fakeTendrilStore{}
	if _, err := ReleaseLease(context.Background(), TendrilConfig{
		Client: tendril.NewClient(srv.URL), Store: store, EncryptKey: testEncKey,
	}, models.TendrilLease{
		ID: "row1", UserID: "user1", LeaseID: "lease_9k2m", LeaseTokenEnc: enc,
		ReservedUSDMicros: 12_010_000,
	}); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}
	if store.refunded != 9_010_000 {
		t.Errorf("refunded = %d, want 9010000", store.refunded)
	}
}

type fakeTendrilStore struct {
	tendrilCredit   int64
	agentMeshCredit int64
	refunded        int64
	releasedID      string
	releasedSeconds int64
	releasedCharged int64
	inserted        models.TendrilLease
	byID            map[string]models.TendrilLease
}

func (f *fakeTendrilStore) InsertTendrilLease(_ context.Context, l models.TendrilLease) (models.TendrilLease, error) {
	l.ID = "row1"
	f.inserted = l
	return l, nil
}
func (f *fakeTendrilStore) GetTendrilLease(_ context.Context, id string) (models.TendrilLease, error) {
	return f.byID[id], nil
}
func (f *fakeTendrilStore) MarkTendrilLeaseReleased(_ context.Context, id string, used, charged int64) error {
	f.releasedID, f.releasedSeconds, f.releasedCharged = id, used, charged
	return nil
}
func (f *fakeTendrilStore) LatestActiveLeaseForRun(_ context.Context, _ string) (models.TendrilLease, error) {
	return models.TendrilLease{}, nil
}
func (f *fakeTendrilStore) TendrilCreditBalance(_ context.Context, _ string) (int64, error) {
	return f.tendrilCredit, nil
}
func (f *fakeTendrilStore) CreditBalance(_ context.Context, _ string) (int64, error) {
	return f.agentMeshCredit, nil
}
func (f *fakeTendrilStore) ConvertCreditsToTendril(_ context.Context, _ string, amount int64, _ string) (int64, error) {
	f.agentMeshCredit -= amount
	f.tendrilCredit += amount
	return f.tendrilCredit, nil
}
func (f *fakeTendrilStore) ChargeTendrilCredit(_ context.Context, _, leaseID, kind string, amount int64) error {
	if kind == "refund" {
		f.tendrilCredit += amount
		f.refunded += amount
		return nil
	}
	f.tendrilCredit -= amount
	return nil
}
```

Add `const testEncKey = "0123456789abcdef0123456789abcdef"` to this test file if the package does not already define one.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/nodes/ -run TestReleaseLease -v`
Expected: FAIL — `undefined: ReleaseLease`.

- [ ] **Step 3: Write the implementation**

```go
// ReleaseLease stops the meter and records what Tendril actually charged.
// Shared by the release node, the REST endpoint, and the reaper so all three
// bill identically.
func ReleaseLease(ctx context.Context, cfg TendrilConfig, lease models.TendrilLease) (tendril.ReleaseResult, error) {
	token, err := wallet.Decrypt(lease.LeaseTokenEnc, cfg.EncryptKey)
	if err != nil {
		return tendril.ReleaseResult{}, fmt.Errorf("tendril: decrypt lease token: %w", err)
	}
	res, err := cfg.Client.Release(ctx, lease.LeaseID, token)
	if err != nil {
		// Tendril's own watchdog reaps abandoned leases, so a lease it no
		// longer knows about is already stopped and already billed. Treating
		// that as a failure would leave our row 'active' forever and have the
		// reaper retry it on every tick.
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
			if merr := cfg.Store.MarkTendrilLeaseReleased(ctx, lease.ID, 0, 0); merr != nil {
				return tendril.ReleaseResult{}, merr
			}
			return tendril.ReleaseResult{}, nil
		}
		return tendril.ReleaseResult{}, fmt.Errorf("tendril: release: %w", err)
	}
	if err := cfg.Store.MarkTendrilLeaseReleased(ctx, lease.ID, res.UsedSeconds, res.ChargedAtomic); err != nil {
		return res, err
	}

	// Return the unused part of the reservation as Tendril credit — not as
	// AgentMesh credit. The user bought hours; releasing early means they
	// still hold those hours, just not on this machine. Refunding to AgentMesh
	// credits instead would let a user cycle rent/release to convert Tendril
	// credit back into general platform credit, which the pool cannot honour
	// (the USDC is already sitting at Tendril).
	if unused := lease.ReservedUSDMicros - res.ChargedAtomic; unused > 0 {
		if err := cfg.Store.ChargeTendrilCredit(ctx, lease.UserID, lease.ID, "refund", unused); err != nil {
			// The lease is already closed and billed; a failed refund is a
			// reconciliation problem, not a reason to report the release as
			// failed and have the reaper retry a DELETE that already ran.
			log.Printf("tendril: lease %s released but refunding %d micros to user %s failed: %v",
				lease.LeaseID, unused, lease.UserID, err)
		}
	}
	return res, nil
}

func executeTendrilRelease(ctx context.Context, node models.WorkflowNode, cfg TendrilConfig) (any, error) {
	lease, err := resolveLease(ctx, node, cfg)
	if err != nil {
		return nil, err
	}
	res, err := ReleaseLease(ctx, cfg, lease)
	if err != nil {
		return nil, err
	}
	remaining, _ := cfg.Store.TendrilCreditBalance(ctx, lease.UserID)
	return map[string]any{
		"agentMeshLeaseId": lease.ID,
		"leaseId":          lease.LeaseID,
		"usedSeconds":      res.UsedSeconds,
		"charged":          formatUSDCAmount(res.ChargedAtomic),
		"refunded":         formatUSDCAmount(max64(0, lease.ReservedUSDMicros-res.ChargedAtomic)),
		// Deliberately NOT res.Balance: that is the shared pool, which is
		// every user's money and must never be shown to one of them.
		"tendrilCreditBalance": formatUSDCAmount(remaining),
	}, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// resolveLease finds which lease a run/release node acts on. A node may name
// one explicitly via TendrilNodeID; otherwise it takes the lease this run
// opened, which is what the trigger -> rent -> run -> release workflow wants.
func resolveLease(ctx context.Context, node models.WorkflowNode, cfg TendrilConfig) (models.TendrilLease, error) {
	if node.TendrilNodeID != "" {
		return cfg.Store.GetTendrilLease(ctx, node.TendrilNodeID)
	}
	lease, err := cfg.Store.LatestActiveLeaseForRun(ctx, cfg.RunID)
	if err != nil {
		return models.TendrilLease{}, fmt.Errorf("tendril: no lease to act on — put a Rent node before this one: %w", err)
	}
	return lease, nil
}

func executeTendrilRun(ctx context.Context, node models.WorkflowNode, rc RunContexter, cfg TendrilConfig) (any, error) {
	payload := strings.TrimSpace(rc.Message())
	for _, p := range node.CustomParams {
		if p.Name == "payload" && strings.TrimSpace(p.Value) != "" {
			payload = p.Value
		}
	}
	if payload == "" {
		return nil, fmt.Errorf("tendril: run needs a payload — set one on the node or pass it as the run's input")
	}

	// A lease is optional: with one, the job runs inside the machine the user
	// is already paying for; without one, Tendril picks an idle machine and
	// bills the seconds. Both are the same paid endpoint.
	body, _ := json.Marshal(map[string]string{"payload": payload})
	var leaseToken string
	if lease, err := resolveLease(ctx, node, cfg); err == nil && lease.LeaseTokenEnc != "" {
		if tok, derr := wallet.Decrypt(lease.LeaseTokenEnc, cfg.EncryptKey); derr == nil {
			leaseToken = tok
		}
	}
	return payTendril(ctx, cfg, "/x402/run", body, leaseToken)
}
```

**The relay has no auth passthrough today — add one.** `setRelayTargetHeaders` (`tool402.go:1110`) forwards exactly three things to the relay: `X-Relay-Method`, `X-Relay-Body`, `X-Relay-Content-Type`. A lease token bound for the target has nowhere to ride, so add a fourth in the same style.

Add the field to `models.WorkflowNode` beside the other Tendril fields:
```go
	// TendrilLeaseToken is a bearer the TARGET needs, carried to the relay
	// out of band. Never persisted on a saved workflow — it is only ever set
	// on the synthesized nodes payTendril builds at call time.
	TendrilLeaseToken string `json:"-"`
```

In `tool402.go`, change `setRelayTargetHeaders` to take the token and set it:
```go
func setRelayTargetHeaders(req *http.Request, method string, body []byte, contentType, targetAuth string) {
	// … existing X-Relay-Method / X-Relay-Body / X-Relay-Content-Type logic …

	// A bearer the TARGET requires (Tendril's lease token). Named X-Relay-Auth
	// rather than Authorization so it can never be confused with auth for the
	// relay itself, which is a different trust boundary entirely.
	if targetAuth != "" {
		req.Header.Set("X-Relay-Auth", targetAuth)
	}
}
```
Thread it through `newRelayRequest` and `executeTool402V2Relay` (both take it as a new trailing parameter and pass `node.TendrilLeaseToken`), and update every existing call site to pass `""`. Find them with `grep -rn "setRelayTargetHeaders\|newRelayRequest\|executeTool402V2Relay" backend/`.

In `handlers/x402relay.go`, read it beside the existing header reads (~line 122) and set it on the outbound target request built at line 423:
```go
	targetAuth := r.Header.Get("X-Relay-Auth")
	// … later, on the request to target:
	if targetAuth != "" {
		req.Header.Set("Authorization", "Bearer "+targetAuth)
	}
```

Add a test in `backend/internal/api/handlers/x402relay_test.go` asserting that a request carrying `X-Relay-Auth: tok` produces `Authorization: Bearer tok` on the target request, and that one without it sets no `Authorization` header at all.

Add to `TendrilStore` and `db.Store`:
```go
// LatestActiveLeaseForRun is how a run/release node finds the lease its own
// run opened, without the canvas having to thread an id between nodes.
func (s *Store) LatestActiveLeaseForRun(ctx context.Context, runID string) (models.TendrilLease, error) {
	return scanTendrilLease(s.pool.QueryRow(ctx,
		`SELECT `+tendrilLeaseCols+` FROM tendril_leases
		 WHERE run_id = $1 AND status = 'active'
		 ORDER BY started_at DESC LIMIT 1`, runID))
}
```
and the matching interface method + `fakeTendrilStore` implementation.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/engine/nodes/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/nodes/ backend/internal/db/store.go
git commit -m "tendril: run and release executors, shared release path"
```

---

### Task 11: The lease reaper

**Files:**
- Create: `backend/internal/engine/tendril_reaper.go`
- Test: `backend/internal/engine/tendril_reaper_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Consumes: `ListExpiredTendrilLeases` (Task 5), `ReleaseLease` (Task 10).
- Produces: `func (r *Runner) ReapExpiredLeases(ctx context.Context) (released int, err error)` and `func (r *Runner) StartLeaseReaper(ctx context.Context, every time.Duration)`.

- [ ] **Step 1: Write the failing test**

```go
package engine

import (
	"context"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

type reaperStore struct {
	expired  []models.TendrilLease
	released []string
}

func (s *reaperStore) ListExpiredTendrilLeases(_ context.Context, _ time.Time) ([]models.TendrilLease, error) {
	return s.expired, nil
}
func (s *reaperStore) MarkTendrilLeaseReleased(_ context.Context, id string, _, _ int64) error {
	s.released = append(s.released, id)
	return nil
}

// An expired lease is a meter still running against the shared pool. One
// failing release must not stop the others from being reaped.
func TestReapExpiredLeasesContinuesPastFailures(t *testing.T) {
	store := &reaperStore{expired: []models.TendrilLease{
		{ID: "bad", LeaseID: "l1", LeaseTokenEnc: "not-decryptable"},
		{ID: "good", LeaseID: "l2", LeaseTokenEnc: "not-decryptable"},
	}}
	r := &Runner{}
	released, err := r.reapWith(context.Background(), store, func(_ context.Context, l models.TendrilLease) error {
		if l.ID == "bad" {
			return context.DeadlineExceeded
		}
		store.released = append(store.released, l.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("reapWith: %v", err)
	}
	if released != 1 {
		t.Errorf("released = %d, want 1", released)
	}
	if len(store.released) != 1 || store.released[0] != "good" {
		t.Errorf("released ids = %v, want [good]", store.released)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/engine/ -run TestReapExpiredLeases -v`
Expected: FAIL — `r.reapWith undefined`.

- [ ] **Step 3: Write the implementation**

```go
package engine

import (
	"context"
	"log"
	"time"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

type expiredLeaseLister interface {
	ListExpiredTendrilLeases(ctx context.Context, now time.Time) ([]models.TendrilLease, error)
}

// reapWith is the testable core: list what has expired, release each one, and
// keep going past individual failures. A single unreachable machine must not
// leave every other meter running.
func (r *Runner) reapWith(ctx context.Context, lister expiredLeaseLister, release func(context.Context, models.TendrilLease) error) (int, error) {
	expired, err := lister.ListExpiredTendrilLeases(ctx, time.Now())
	if err != nil {
		return 0, err
	}
	released := 0
	for _, lease := range expired {
		if err := release(ctx, lease); err != nil {
			log.Printf("tendril reaper: lease %s: %v", lease.LeaseID, err)
			continue
		}
		released++
	}
	return released, nil
}

// ReapExpiredLeases releases every lease whose funded window has closed.
func (r *Runner) ReapExpiredLeases(ctx context.Context) (int, error) {
	if r.tendrilClient == nil {
		return 0, nil
	}
	return r.reapWith(ctx, r.store, func(ctx context.Context, lease models.TendrilLease) error {
		_, err := nodes.ReleaseLease(ctx, nodes.TendrilConfig{
			Client: r.tendrilClient, Store: r.store, EncryptKey: r.encryptionKey,
		}, lease)
		return err
	})
}

// StartLeaseReaper runs ReapExpiredLeases forever. Tendril has its own
// watchdog, but relying on it would mean AgentMesh's own rows drift out of
// sync with what the user is actually being charged for.
func (r *Runner) StartLeaseReaper(ctx context.Context, every time.Duration) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n, err := r.ReapExpiredLeases(ctx); err != nil {
					log.Printf("tendril reaper: %v", err)
				} else if n > 0 {
					log.Printf("tendril reaper: released %d expired lease(s)", n)
				}
			}
		}
	}()
}
```

In `cmd/server/main.go`, beside `go expireStalePendingTransactionsLoop(ctx, store)`:
```go
	runner.StartLeaseReaper(ctx, time.Minute)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/engine/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/ backend/cmd/server/main.go
git commit -m "tendril: reap leases whose funded window has closed"
```

---

### Task 12: Lease REST endpoints and the active-leases panel

**Files:**
- Create: `backend/internal/api/handlers/leases.go`
- Test: `backend/internal/api/handlers/leases_test.go`
- Modify: `backend/internal/api/router.go`
- Create: `frontend/src/components/leases/LeasesPanel.tsx`
- Modify: `frontend/src/components/canvas/CanvasPage.tsx`

**Interfaces:**
- Consumes: store methods (Tasks 5–6), `ReleaseLease` (Task 10).
- Produces: authenticated routes
  `GET /tendril/machines` → `{machines:[…]}`,
  `GET /leases` → `{leases:[…]}` (active, current user),
  `POST /leases/{id}/release` → `{usedSeconds, charged, poolBalance}`,
  `GET /leases/{id}/key` → `text/plain` OpenSSH private key, `Content-Disposition: attachment; filename="agentmesh-<leaseId>"`,
  `GET /tendril/credits` → `{tendrilCreditUsdMicros, recent:[…last 20 ledger entries…]}` for the **current user only**.

- [ ] **Step 1: Write the failing test**

```go
// A lease's private key is SSH access to a running machine. It must be
// downloadable only by the user who rented it.
func TestLeaseKeyDeniedToOtherUsers(t *testing.T) {
	d, owner, other := leaseFixture(t) // inserts one lease owned by `owner`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/leases/row1/key", nil)
	d.DownloadLeaseKey(rec, withUser(req, other))
	if rec.Code != http.StatusNotFound {
		t.Errorf("other user got %d, want 404", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	d.DownloadLeaseKey(rec2, withUser(httptest.NewRequest(http.MethodGet, "/leases/row1/key", nil), owner))
	if rec2.Code != http.StatusOK {
		t.Fatalf("owner got %d, want 200", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "OPENSSH PRIVATE KEY") {
		t.Errorf("body is not a private key:\n%s", rec2.Body.String())
	}
	if cd := rec2.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", cd)
	}
}

func TestListLeasesOnlyReturnsOwnActiveLeases(t *testing.T) {
	d, owner, other := leaseFixture(t)

	rec := httptest.NewRecorder()
	d.ListLeases(rec, withUser(httptest.NewRequest(http.MethodGet, "/leases", nil), owner))
	var got struct {
		Leases []models.TendrilLease `json:"leases"`
	}
	json.NewDecoder(rec.Body).Decode(&got)
	if len(got.Leases) != 1 {
		t.Fatalf("owner sees %d leases, want 1", len(got.Leases))
	}
	// The encrypted token and key must never be serialized to a client.
	if strings.Contains(rec.Body.String(), "lease_token_enc") ||
		strings.Contains(rec.Body.String(), "PRIVATE KEY") {
		t.Error("lease JSON leaked a secret")
	}

	rec2 := httptest.NewRecorder()
	d.ListLeases(rec2, withUser(httptest.NewRequest(http.MethodGet, "/leases", nil), other))
	json.NewDecoder(rec2.Body).Decode(&got)
	if len(got.Leases) != 0 {
		t.Errorf("other user sees %d leases, want 0", len(got.Leases))
	}
}
```

`leaseFixture` and `withUser` follow this package's existing DB-test conventions — read `backend/internal/api/handlers/deploy_test.go` for how it skips without `TEST_DATABASE_URL` and how it injects an authenticated user into the request context, and reuse those helpers.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/agentmesh_test go test ./internal/api/handlers/ -run Lease -v`
Expected: FAIL — `d.DownloadLeaseKey undefined`.

- [ ] **Step 3: Write the implementation**

`handlers/leases.go` exposes `ListLeases`, `ReleaseLease`, `DownloadLeaseKey`, `TendrilMachines`, `TendrilCredits`. Every handler resolves the user from the request context exactly as the existing workflow handlers do, and every lease lookup filters on `user_id` — an id-only lookup that then compares ownership in Go is fine, but it **must** return `404`, not `403`, so lease ids are not enumerable.

`TendrilCredits` returns **only the requesting user's** balance and ledger. It must never expose the shared Wallet 2 pool balance: that is the sum of every user's money, and showing it to one user both leaks aggregate business data and invites them to believe they can spend it.

`DownloadLeaseKey`:
```go
func (d *Deps) DownloadLeaseKey(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	lease, err := d.Store.GetTendrilLease(r.Context(), chi.URLParam(r, "id"))
	if err != nil || lease.UserID != userID {
		respond.Error(w, http.StatusNotFound, "lease not found")
		return
	}
	key, err := wallet.Decrypt(lease.SSHPrivateKeyEnc, d.EncryptionKey)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "key unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", "agentmesh-"+lease.LeaseID))
	w.Write([]byte(key))
}
```

In `router.go`, inside the authenticated `r.Group`:
```go
		r.Get("/tendril/machines", d.TendrilMachines)
		r.Get("/tendril/credits", d.TendrilCredits)
		r.Get("/leases", d.ListLeases)
		r.Post("/leases/{id}/release", d.ReleaseLease)
		r.Get("/leases/{id}/key", d.DownloadLeaseKey)
```

`LeasesPanel.tsx` renders one row per active lease: machine label, `$X.XX/hr`, a live countdown to `fundedUntil`, the `ssh …` command with a copy button, a "Download key" link to `/leases/{id}/key`, and a "Release" button that POSTs and refreshes. Mount it in `CanvasPage.tsx` above the `LogDrawer`, rendered only when the list is non-empty — an idle canvas should not grow a panel about machines nobody rented.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && TEST_DATABASE_URL=… go test ./internal/api/... -v` and `cd frontend && npm run typecheck`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/ frontend/src/components/leases/ frontend/src/components/canvas/CanvasPage.tsx
git commit -m "leases: REST endpoints, key download, and the active-leases panel"
```

---

## Phase 4 — The terminal in the console

### Task 13: WebSocket → SSH bridge

**Files:**
- Modify: `backend/internal/api/handlers/leases.go`
- Modify: `backend/go.mod` (add `github.com/coder/websocket`)
- Test: `backend/internal/api/handlers/terminal_test.go`

**Interfaces:**
- Consumes: lease rows + decrypted key (Task 12).
- Produces: `GET /leases/{id}/terminal` (WebSocket upgrade). Client→server frames are either raw keystroke bytes (binary) or a JSON text frame `{"type":"resize","cols":N,"rows":N}`. Server→client frames are raw PTY output (binary).

- [ ] **Step 1: Add the dependency and write the failing test**

```bash
cd backend && go get github.com/coder/websocket@latest
```

```go
// The terminal is a live root shell on a rented machine. Authentication is
// the whole security boundary, so it is tested before anything else about it.
func TestTerminalRejectsNonOwner(t *testing.T) {
	d, _, other := leaseFixture(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.LeaseTerminal(w, withUser(r, other))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/leases/row1/terminal", nil)
	if err == nil {
		t.Fatal("a non-owner completed the WebSocket handshake")
	}
}

func TestTerminalRejectsReleasedLease(t *testing.T) {
	d, owner, _ := leaseFixture(t)
	if err := d.Store.MarkTendrilLeaseReleased(context.Background(), "row1", 60, 100); err != nil {
		t.Fatalf("MarkTendrilLeaseReleased: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.LeaseTerminal(w, withUser(r, owner))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/leases/row1/terminal", nil); err == nil {
		t.Fatal("a released lease accepted a terminal connection")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/handlers/ -run TestTerminal -v`
Expected: FAIL — `d.LeaseTerminal undefined`.

- [ ] **Step 3: Write the implementation**

```go
// LeaseTerminal bridges a browser WebSocket to an SSH shell on the rented
// machine, authenticating with the per-lease key AgentMesh generated and
// authorized at rent time.
//
// HostKeyCallback is InsecureIgnoreHostKey deliberately: Tendril hands out a
// fresh ephemeral host (bore.pub tunnels on rotating ports) whose host key we
// have never seen and have no channel to pin. The connection is still
// encrypted; what is not proven is the far end's identity. Revisit if Tendril
// ever publishes host keys in the rent response.
func (d *Deps) LeaseTerminal(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	lease, err := d.Store.GetTendrilLease(r.Context(), chi.URLParam(r, "id"))
	if err != nil || lease.UserID != userID || lease.Status != "active" {
		respond.Error(w, http.StatusNotFound, "lease not found")
		return
	}
	keyPEM, err := wallet.Decrypt(lease.SSHPrivateKeyEnc, d.EncryptionKey)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "key unavailable")
		return
	}
	signer, err := ssh.ParsePrivateKey([]byte(keyPEM))
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "key unusable")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{originHost(d.FrontendURL)},
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	auth := []ssh.AuthMethod{ssh.PublicKeys(signer)}
	if lease.SSHPasswordEnc != "" {
		if pw, derr := wallet.Decrypt(lease.SSHPasswordEnc, d.EncryptionKey); derr == nil {
			auth = append(auth, ssh.Password(pw))
		}
	}
	client, err := ssh.Dial("tcp",
		fmt.Sprintf("%s:%d", lease.SSHHost, lease.SSHPort),
		&ssh.ClientConfig{
			User:            lease.SSHUsername,
			Auth:            auth,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         20 * time.Second,
		})
	if err != nil {
		conn.Close(websocket.StatusInternalError, "ssh dial failed")
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		conn.Close(websocket.StatusInternalError, "ssh session failed")
		return
	}
	defer session.Close()

	stdin, _ := session.StdinPipe()
	stdout, _ := session.StdoutPipe()
	stderr, _ := session.StderrPipe()
	if err := session.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{
		ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		conn.Close(websocket.StatusInternalError, "pty request failed")
		return
	}
	if err := session.Shell(); err != nil {
		conn.Close(websocket.StatusInternalError, "shell failed")
		return
	}

	ctx := r.Context()
	pump := func(src io.Reader) {
		buf := make([]byte, 4096)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	go pump(stdout)
	go pump(stderr)

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageText {
			var msg struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
				session.WindowChange(msg.Rows, msg.Cols)
				continue
			}
		}
		if _, err := stdin.Write(data); err != nil {
			return
		}
	}
}

// originHost extracts the host:port a browser will send as Origin, so the
// WebSocket accept is not left origin-wildcarded.
func originHost(frontendURL string) string {
	if u, err := url.Parse(frontendURL); err == nil && u.Host != "" {
		return u.Host
	}
	return "localhost:3000"
}
```

Register the route beside the other lease routes:
```go
		r.Get("/leases/{id}/terminal", d.LeaseTerminal)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/api/handlers/ -run TestTerminal -v && go build ./... && go mod tidy`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/api/handlers/
git commit -m "leases: WebSocket to SSH terminal bridge"
```

---

### Task 14: xterm.js terminal tab in the console

**Files:**
- Create: `frontend/src/components/canvas/TerminalTab.tsx`
- Modify: `frontend/src/components/canvas/LogDrawer.tsx`
- Modify: `frontend/package.json`

**Interfaces:**
- Consumes: `GET /leases/{id}/terminal` (Task 13), lease list (Task 12).
- Produces: `<TerminalTab leaseId={string} onClose={() => void} />`.

- [ ] **Step 1: Add dependencies**

```bash
cd frontend && npm install @xterm/xterm @xterm/addon-fit
```

- [ ] **Step 2: Write the component**

```tsx
"use client";
import { useEffect, useRef } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";

// The SSE stream already bypasses Next's /api rewrite because that proxy does
// not hold long-lived connections open (see LogDrawer's SSE_BASE comment). A
// WebSocket has exactly the same problem, so it dials the backend directly for
// exactly the same reason.
const WS_BASE = process.env.NEXT_PUBLIC_API_URL ?? "";

export function TerminalTab({
  leaseId,
  onClose,
}: {
  leaseId: string;
  onClose: () => void;
}) {
  const hostRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    const term = new Terminal({
      convertEol: true,
      fontSize: 12,
      fontFamily:
        "var(--font-geist-mono), ui-monospace, SFMono-Regular, Menlo, monospace",
      theme: { background: "#0b0b0d" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();

    const base = WS_BASE.replace(/^http/, "ws");
    const ws = new WebSocket(`${base}/leases/${leaseId}/terminal`);
    ws.binaryType = "arraybuffer";

    const sendResize = () => {
      fit.fit();
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(
          JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }),
        );
      }
    };

    ws.onopen = () => {
      term.writeln("\x1b[2m connected — this is a real machine you are paying for \x1b[0m");
      sendResize();
    };
    ws.onmessage = (ev) => {
      term.write(
        typeof ev.data === "string"
          ? ev.data
          : new Uint8Array(ev.data as ArrayBuffer),
      );
    };
    ws.onclose = () => {
      term.writeln("\r\n\x1b[2m disconnected \x1b[0m");
    };

    const keys = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(data);
    });

    const observer = new ResizeObserver(sendResize);
    observer.observe(host);

    return () => {
      observer.disconnect();
      keys.dispose();
      ws.close();
      term.dispose();
    };
  }, [leaseId]);

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <div
        style={{
          display: "flex",
          justifyContent: "flex-end",
          padding: "4px 8px",
        }}
      >
        <button onClick={onClose} style={{ fontSize: 11 }}>
          close terminal
        </button>
      </div>
      <div ref={hostRef} style={{ flex: 1, minHeight: 0 }} />
    </div>
  );
}
```

- [ ] **Step 3: Wire it into the console**

In `LogDrawer.tsx`, add a tab strip above the log list with `Logs` and, when the run produced a lease, `Terminal`. Detect the lease from the log events already streaming in: a `tendril` log entry whose `output` carries `agentMeshLeaseId`. Store it in state and render `<TerminalTab leaseId={id} …/>` in place of the log list when the Terminal tab is active. Keep the log list mounted (hidden via CSS) rather than unmounted, so switching tabs does not lose the transcript.

- [ ] **Step 4: Verify manually**

Run `npm run dev` in `frontend/` and the backend with `TENDRIL_REGISTRY_URL` set. Load the Tendril workflow, run it, and confirm: the rent step logs an `ssh` command, the Terminal tab appears, and typing `uname -a` in it returns output from the rented machine.

Run: `cd frontend && npm run typecheck && npm run lint`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/src/components/canvas/
git commit -m "console: xterm.js terminal tab wired to the lease SSH bridge"
```

---

## Phase 5 — Ship it as an official Tendril workflow

### Task 15: The prebuilt Tendril workflow and the Bazaar rent entry

**Files:**
- Modify: `frontend/src/lib/demoWorkflow.ts`
- Modify: `frontend/src/components/workflows/WorkflowsPage.tsx`
- Modify: `backend/internal/bazaar/curated.go`
- Test: `backend/internal/bazaar/curated_test.go` (append)

**Interfaces:**
- Consumes: node type and templates (Task 9).
- Produces: `buildTendrilWorkflow(): Pick<Workflow, "nodes" | "edges">`; a `curated:tendril-rent` Bazaar resource.

- [ ] **Step 1: Write the failing test**

```go
// Tendril is an official collaboration, so both of its paid routes must be in
// the supported tier — a user should never have to hand-write JSON for either.
func TestCuratedIncludesBothTendrilRoutes(t *testing.T) {
	byID := map[string]Resource{}
	for _, r := range Curated() {
		byID[r.ID] = r
	}
	rent, ok := byID["curated:tendril-rent"]
	if !ok {
		t.Fatal("curated:tendril-rent is missing")
	}
	if !rent.Supported {
		t.Error("the rent entry must be in the supported tier")
	}
	if rent.URL != "https://tendrilregister.007575.xyz/x402/rent" {
		t.Errorf("rent URL = %q", rent.URL)
	}
	// Confirmed live 2026-08-04: renting is a flat 0.01 USDC gate fee.
	if rent.AmountMicros != 10000 {
		t.Errorf("rent amount = %d micros, want 10000", rent.AmountMicros)
	}
	if _, ok := byID["curated:tendril-run"]; !ok {
		t.Error("curated:tendril-run disappeared")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/bazaar/ -run TestCuratedIncludesBothTendril -v`
Expected: FAIL — `curated:tendril-rent is missing`.

- [ ] **Step 3: Write the implementation**

Add to `Curated()`, immediately after the existing `curated:tendril-run` entry:
```go
		{
			ID:          "curated:tendril-rent",
			URL:         "https://tendrilregister.007575.xyz/x402/rent",
			Method:      http.MethodPost,
			Provider:    "Tendril",
			Host:        "tendrilregister.007575.xyz",
			Description: "Rent a real Linux machine by the hour and get SSH into it. Flat 0.01 USDC to open the lease; the hours themselves meter from the Tendril credit balance keyed to the paying wallet at the machine's own rate. Prefer the Tendril workflow template, which buys the hours and manages the lease for you.",
			Network:     AlgorandMainnet,
			Asset:       "31566704",
			PayTo:       "ZIK7QQE7ZX446TW3PN7PQ5UDZNTY7JI5RYNTIU3LPEYBOSTVWI6PTNSWKI",
			AmountMicros: 10000,
			Supported:    true,
			Params: []Param{
				{
					Name:        "nodeId",
					Type:        "string",
					Required:    true,
					Description: "Which machine to rent. Pick one from the market listing.",
				},
				{
					Name:        "sshPubKey",
					Type:        "string",
					Required:    false,
					Description: "OpenSSH public key to authorize on the machine. Without one you can still run jobs, but you cannot SSH in.",
				},
			},
		},
```

In `demoWorkflow.ts`:
```ts
// Tendril — an official AgentMesh collaboration. This workflow has no agent
// and no LLM in it at all. It lays out the money flow left to right so the two
// balances are visible as separate steps rather than hidden inside one node:
//
//   trigger -> Topup -> Rent -> Run -> end
//              ^^^^^    ^^^^
//              buys     spends that credit on hours,
//              Tendril  and hands back an SSH command the
//              credit   console turns into a live terminal
//
// Topup is its own node precisely because it is a currency conversion, not a
// purchase: AgentMesh credits become Tendril credits, which only ever buy
// machine time. Delete the Topup node once you already hold enough credit.
export function buildTendrilWorkflow(
  hours: string = "1",
  topupUsd: string = "10",
): Pick<Workflow, "nodes" | "edges"> {
  const nodes: WorkflowNode[] = [
    {
      id: "n1",
      type: "trigger",
      template: "manual",
      x: 60,
      y: 240,
      label: "Run to rent a machine",
    },
    {
      id: "n2",
      type: "tendril",
      template: "tendril_topup",
      x: 340,
      y: 220,
      name: "Buy Tendril Credit",
      icon: "＄",
      tendrilAction: "topup",
      tendrilAmount: topupUsd,
      description:
        "Settles a real mainnet USDC payment into AgentMesh's Tendril pool and converts the same amount of your AgentMesh credits into Tendril credit. Tendril credit is yours alone and can only be spent on machine time.",
    },
    {
      id: "n3",
      type: "tendril",
      template: "tendril_rent",
      x: 640,
      y: 220,
      name: "Rent a Machine",
      icon: "▣",
      tendrilAction: "rent",
      tendrilHours: hours,
      description:
        "Reserves the hours from your Tendril credit, opens a metered lease on the cheapest online machine, and authorizes a freshly generated SSH key. The machine listed today is $6.00/hour. Release early and the unused hours return to your Tendril credit.",
    },
    {
      id: "n4",
      type: "tendril",
      template: "tendril_run",
      x: 940,
      y: 220,
      name: "Run a Job",
      icon: "▶",
      tendrilAction: "run",
      customParams: [
        { name: "payload", kind: "text", value: "print(sum(range(100)))" },
      ],
      description:
        "Executes Python inside the machine the Rent node just opened and returns its stdout. Flat 0.01 USDC per job.",
    },
    { id: "n5", type: "end", template: "done", x: 1240, y: 240 },
  ];
  const edges: WorkflowEdge[] = [
    { id: "e1", from: "n1", to: "n2", kind: "flow", toPort: "in" },
    { id: "e2", from: "n2", to: "n3", kind: "flow", toPort: "in" },
    { id: "e3", from: "n3", to: "n4", kind: "flow", toPort: "in" },
    { id: "e4", from: "n4", to: "n5", kind: "flow", toPort: "in" },
  ];
  return { nodes, edges };
}
```

In `WorkflowsPage.tsx`, add a handler mirroring `handleLoadCanix402Workflow`:
```tsx
  const handleLoadTendrilWorkflow = useCallback(async () => {
    if (creatingTendril) return;
    setCreatingTendril(true);
    try {
      const wf = await workflowsApi.create("Tendril — Rent a Machine");
      const { nodes, edges } = buildTendrilWorkflow();
      await workflowsApi.update(wf.id, { name: wf.name, nodes, edges });
      router.push(`/workflows/${wf.id}`);
    } catch {
      setCreatingTendril(false);
    }
  }, [creatingTendril, router]);
```
and render its card beside the existing template cards, with an "Official — built with Tendril" badge (reuse the `bz-supported-card` styling from `BazaarPage.tsx` so the "verified by AgentMesh" visual language is consistent) and the subtitle "Rent a real Linux machine by the hour. SSH from the console."

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/bazaar/ -v` and `cd frontend && npm run typecheck && npm run lint`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/bazaar/ frontend/src/lib/demoWorkflow.ts frontend/src/components/workflows/WorkflowsPage.tsx
git commit -m "tendril: official prebuilt workflow and Bazaar rent entry"
```

---

### Task 16: Documentation and full-suite verification

**Files:**
- Modify: `CONTRIBUTING.md`
- Modify: `README.md`

- [ ] **Step 1: Document the environment and the flow**

In `CONTRIBUTING.md`'s environment table, add:

| Var | Default | Meaning |
|---|---|---|
| `TENDRIL_REGISTRY_URL` | `https://tendrilregister.007575.xyz` | Tendril compute registry. Unset disables tendril nodes (they fail closed). |
| `MAX_RELAY_OUTBOUND_USD_MICROS` | `20000000` | Ceiling on one relayed x402 payment. Raised from $5 because a 2-hour rent on a $6/hr machine tops the pool up by $12 in a single call. |

Add a section titled **"The two Tendril balances"** explaining, in this order: Tendril keys credit to the paying address and the payer is always Wallet 2, so on Tendril's side there is exactly one balance for all of AgentMesh; AgentMesh therefore keeps a per-user sub-ledger (`users.tendril_credit_usd_micros` + `tendril_credit_ledger`) and a user may only ever spend what they themselves converted; a Topup node settles USDC into the shared pool and moves the same value from that user's AgentMesh credits to their Tendril credits in one transaction; renting is a flat 1¢ that does *not* buy time, and hours come out of the user's Tendril credit; release is where compute is actually billed and the unused reservation returns to their Tendril credit; and the reaper exists because an unreleased lease keeps metering against the pool. State the invariant explicitly: `SUM(users.tendril_credit_usd_micros) + metering <= pool balance`.

In the README's "What's live today" table, add `| Tendril compute — rent a machine by the hour, SSH from the console | ✅ |`, and remove any now-stale "coming" bullet it duplicates.

- [ ] **Step 2: Run the full suite**

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && npm run typecheck && npm run lint && npm test
```
Expected: all green. DB-backed tests skip without `TEST_DATABASE_URL`; run them once with it set before declaring the phase done.

- [ ] **Step 3: End-to-end check against the live registry**

With a real `.env` (mainnet, funded Wallet 1 and Wallet 2), load the Tendril workflow and run it. Confirm in order:

1. The Topup step shows a settlement with a Lora explorer link, AgentMesh credits drop by the topup amount, and `GET /tendril/credits` rises by exactly the same amount.
2. The Rent step shows a settlement, an `ssh …` command, and a reduced Tendril credit balance. AgentMesh credits are **unchanged** by this step — renting spends Tendril credit only.
3. `GET /leases` lists the lease; `GET /leases/{id}/key` downloads a usable private key; the Terminal tab opens a shell and `uname -a` returns output from the machine.
4. Release reports `usedSeconds`, a charge, and a `refunded` amount; Tendril credit goes back up by the unused remainder; the row's status is `released`.
5. Sign in as a **second** user with zero Tendril credit and run the same workflow's Rent node alone. It must refuse with the "your Tendril credit is $0.00" error even though the shared pool is flush from user one's topup. This is the invariant under test — do not skip it.

**This spends real USDC.** Rent for the minimum duration and release immediately.

- [ ] **Step 4: Commit**

```bash
git add CONTRIBUTING.md README.md
git commit -m "docs: Tendril compute — pool model, env vars, and the rent flow"
```

---

## Deliberately Out of Scope

Flagged rather than silently dropped:

- **Withdrawing Tendril credit back to AgentMesh credit.** Tendril credit is one-way: AgentMesh credits buy it, and it only ever spends on machine time. The USDC backing it already sits at Tendril, so a reverse conversion needs a real pool withdrawal (`/platform` reports `minWithdrawAtomic: 5000000`, so Tendril supports one) plus a policy on who absorbs the fees. Users must be told this before their first topup — the Inspector copy in Task 9 says so.
- **Pool reconciliation reporting.** Nothing yet asserts `SUM(users.tendril_credit_usd_micros) <= pool balance` on a schedule. Task 6 logs loudly on the one known drift path (a topup that settles on-chain but fails to convert, which drifts *safe* — pool larger than entitlements), but a periodic check with an alert would catch anything unforeseen. Small, and worth doing before this carries real volume.
- **Agent-driven SSH.** Node ports include `top` and the executors are already parameterized by action, so attaching a Tendril node to an agent and exposing `tendril.run` as a callable tool is a small addition to `provider.go`'s tool-schema builder. Not wired in this plan.
- **Host key verification** for the SSH bridge — see the comment in Task 13; Tendril publishes no host keys today.
- **Multiple concurrent leases per run.** `LatestActiveLeaseForRun` takes the most recent; a workflow renting two machines in one run would need explicit lease-id wiring between nodes.
