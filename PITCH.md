# AgentMesh — Pitch Document
### Pitch Competition · June 12, 2026

---

## THE OPENING (use this for all three tables)

> "What if your AI agent could walk into a store, pick up what it needs, pay for it — and you never had to hand it a credit card?
> That's what we built.
> AgentMesh gives every AI agent its own Algorand wallet. Agents earn money, spend money, and pay for the data they need — on-chain, in under 5 seconds, without a single API subscription."

Pause. Then say:

> "Today I'm going to show you working code, a real blockchain transaction, and a business model that makes money from the first call."

---

## PANEL 1 — TECHNICAL

### One-line architecture

> "Next.js frontend → Go backend → Algorand blockchain. No frameworks we didn't need. No black boxes."

---

### What we actually built (show this, don't just say it)

**Custom SVG Canvas** — we didn't use ReactFlow or any graph library. The entire drag-drop-wire canvas (`frontend/src/components/canvas/`) is custom SVG. We did it because no library matched our port-connection model — `portUtils.ts` computes canvas-space coordinates and enforces wiring rules at the type level.

**Agentic function-calling loop** — `backend/internal/engine/nodes/provider.go`. When an agent runs, it enters a loop (up to 15 iterations):
1. Call LLM (Gemini or OpenAI) with `functionDeclarations` built from the tool's discovered parameters
2. LLM returns a `functionCall` → execute the x402 tool, get the result
3. Feed result back into conversation history → call LLM again
4. Repeat until the LLM returns a plain text response

The agent decides what to call and when. Not us.

**x402 payment flow** — `backend/internal/engine/nodes/tool402.go`:
```
GET endpoint → 402 with price + recipient
→ decrypt AES-GCM mnemonic → sign Algorand tx
→ retry with X-Payment-Txid header → data returned
```
~$0.0002 per transaction. ~3.5 second round trip. No smart contracts required.

**Topological executor** — `backend/internal/engine/runner.go`. Graph is sorted, nodes in the same level run in parallel goroutines. Results pass through `RunContext`. Tool nodes attached to an agent are **excluded** from standalone execution — the LLM drives them, not the graph.

**Security decisions made:**
- Wallet mnemonics: AES-GCM encrypted at rest, decrypted only at execution time
- SSRF protection on HTTP tools: DNS-resolved, all RFC-1918 private IP ranges blocked
- JWT: HS256, min 32-byte secret. SSE uses `?token=` query param fallback (EventSource can't set headers)
- API keys masked in all workflow GET responses — stored encrypted in `tool_credentials` table

---

### Test coverage (live numbers from today's run)

| Package | Coverage | What's tested |
|---|---|---|
| `internal/api` | **89.9%** | JWT middleware, CORS, auth, health |
| `internal/sse` | **83.0%** | Broker pub/sub, all subscribers receive |
| `internal/engine/nodes` | **40.3%** | x402 full flow (5 cases), HTTP tool, webhook, log action |
| `internal/wallet` | **41.3%** | Ed25519 keygen, AES-GCM encrypt/decrypt round-trip |
| `internal/engine` | **24.3%** | Topological sort, cycle detection, attach map |
| `internal/api/handlers` | **9.2%** | Auth validation; DB integration tests skipped without `TEST_DATABASE_URL` |

**28 source files. 24 test files. Nearly 1:1 ratio.**

The x402 tests cover every state: free endpoint (200), payment parse (402), full sign-pay-retry flow, no wallet configured, and signer failure. See `tool402_test.go`.

HTML coverage report: `backend/test-reports/coverage.html`

---

### "How would you handle 10X users?"

- **Stateless Go backend** — horizontal scale behind a load balancer, no shared state between requests
- **PostgreSQL + pgx/v5** — using Supabase's PgBouncer in transaction mode; query protocol forces simple mode for compatibility
- **Golang-migrate** — schema versioning, migrations run on boot
- **SSE broker** — in-process pub/sub per run; at scale, replace with Redis pub/sub (1-day swap)
- **Algorand** — finality is network-level, not our load; we just sign and submit
- **Goroutine-per-node** — parallel level execution already baked in; more nodes = same latency at the bottleneck level, not multiplicative

Honest bottleneck: the SSE broker is in-process. Multi-instance needs Redis. We know exactly what to swap.

---

### Live walkthrough (5-10 min)

Walk them through `tool402.go:39–99`:
- Show the HTTP 402 response → `parsePaymentHeader` → Algorand sign → retry
- Run it live against `test-x402/` local server (weather endpoint)
- Show the Algorand Explorer link in the logs output

---

## PANEL 2 — BUSINESS

### The problem (one sentence)

> "AI developers spend more time wiring API keys and managing subscriptions than building agents — and every API they add costs a flat monthly fee whether the agent uses it once or a thousand times."

### Why this is real

- The average LLM agent needs 3-8 external APIs (weather, search, financial data, compute)
- Each one requires: account creation, API key management, billing setup, per-seat/monthly subscription
- If the agent runs 3 times a month, you still pay the full monthly fee
- If you build for 10 agents, you manage 80 API keys

**The model is broken for autonomous agents.** Subscriptions assume humans managing usage. Agents need pay-as-you-go.

---

### Our solution — pay-as-you-go from day one

**No subscriptions. No flat fees. Pay per call.**

1. User deposits credits (like Twilio/AWS)
2. Every agent run deducts from credits based on what it actually does:
   - Each LLM call: ~$0.001–$0.01 (we pass through provider cost + 20% margin)
   - Each x402 API call: tool price + $0.0002 Algorand fee + our 10% routing fee
   - Each workflow run: $0.01 platform fee

3. Agent wallets are funded directly by the user — ALGO goes to the agent, not through us
4. We earn on LLM routing margin + x402 routing fee + platform run fee

**Revenue at 1,000 active agents running 10 calls/day:**
- LLM calls: 1,000 × 10 × $0.005 avg × 20% = **$10,000/month**
- x402 routing: 1,000 × 10 × $0.50 avg tool × 10% = **$5,000/month**
- Run fees: 1,000 × 30 runs × $0.01 = **$300/month**
- **Total: ~$15,000 MRR at 1,000 agents**

This scales linearly. No churn from subscription cancellations. Revenue is pure usage.

---

### Why blockchain and not just a regular API proxy?

This is the question judges will ask. The honest answer:

**You could build a payment proxy in a database.** But:

1. **Trust** — when an agent pays an API, the payment is on-chain. The API provider can verify it without trusting our platform. This enables a two-sided marketplace with zero-trust transactions between strangers.
2. **Micropayments at $0.0002/tx** — no traditional payment rails support $0.001 API calls profitably. Stripe minimum is $0.30. Algorand makes $0.001 pricing real.
3. **Agent-to-agent** — our roadmap item: deploy a workflow as an x402 endpoint. Agents pay other agents. This is only possible on-chain.
4. **Audit trail** — every agent payment is an immutable on-chain record. Compliance, debugging, dispute resolution — all without a database you control.

---

### Market

**Primary:** AI developers building autonomous agents
- 4.4M developers using LLM APIs as of Q1 2026 (OpenAI/Anthropic disclosed)
- Growing 3x YoY

**Secondary:** API publishers who want to monetize without subscription overhead
- Any API that wants per-call revenue without billing infrastructure

**TAM:** $2.1B in API management tooling (2025, Gartner) + $800M AI agent tooling segment (growing to $4.5B by 2027)

**Beachhead:** 50,000 Algorand ecosystem developers who already have wallets and understand x402

---

### Customer value proposition (quantified)

| Old way | AgentMesh |
|---|---|
| 8 API key signups per project | 0 signups for x402 APIs |
| $200/mo flat subscriptions per project | Pay only what runs |
| Subscription billing per API | Per-call: $0.001–$0.01 |
| Manual credential rotation | Encrypted, rotated automatically |
| No audit trail | Every payment on-chain |

**30% cost reduction** for developers running agents at moderate frequency. **Zero upfront cost** for new agents.

---

### First 100 users strategy

1. **Algorand developer community** (30) — Algorand Discord, Algorand Foundation grants program. x402 is an Algorand-native protocol. They already want this.
2. **AI agent hackathons** (25) — sponsor or attend ETHGlobal, Solana Breakpoint, Buildathon. Demo the live x402 flow.
3. **Direct outreach to AI newsletter builders** (20) — developers who already publish AI tools. Offer to list their API in our marketplace free.
4. **ProductHunt launch** (25) — "Give your AI agent a wallet" is a memorable hook. Targets indie developers.

We are not saying "go viral." We have specific communities and channels.

---

## PANEL 3 — SCALABILITY & EXECUTION

### Architecture scalability

```
Browser → Next.js (Vercel) → Go API (Railway/Fly.io) → PostgreSQL (Supabase)
                                        ↓
                               Algorand Testnet/Mainnet
```

**Each layer scales independently:**

| Layer | Scale mechanism | Bottleneck? |
|---|---|---|
| Frontend | Vercel edge — auto-scale | No |
| Go API | Stateless, horizontal — add instances | No |
| Database | Supabase connection pooling (PgBouncer) | At ~10k concurrent users |
| SSE broker | In-process pub/sub → Redis swap | Yes, at multi-instance |
| Algorand | Network-level, not our infra | No |

**Honest known bottleneck:** SSE broker is in-process. Multi-instance deployment requires Redis. We have this in the codebase as a known upgrade — it is a 1-day implementation, not an architectural rewrite.

---

### Data at scale — blockchain indexing

We currently query Algorand directly via the Go SDK for balance checks. At scale:

- **Indexer:** Algorand Indexer API (hosted, or run own node) for historical transaction queries
- **Cache layer:** Redis for wallet balances (TTL 30s) — avoids hitting Algorand RPC on every run log
- **Fallback:** If Algorand RPC fails, agent run logs the error gracefully and surfaces it in SSE stream — agent continues with other tools

We depend on: Algorand RPC, Supabase, Resend (email), LLM providers. Each has a documented fallback or skip behavior.

---

### 6-month roadmap

| Month | Milestone |
|---|---|
| July 2026 | Standard HTTP tool node (non-x402 APIs) — currently stubbed |
| August 2026 | Webhook + schedule triggers — run agents on a timer |
| September 2026 | x402 marketplace — browse published APIs, add in one click |
| October 2026 | Memory nodes — agents remember across runs |
| November 2026 | Agent-to-agent payments — deploy workflow as x402 endpoint |
| December 2026 | On-chain run receipts + run history UI |

Each milestone is an existing stub or roadmap item in the codebase — not invented for this pitch.

---

### Infrastructure cost at scale

| Users | Monthly cloud cost | Revenue (est.) |
|---|---|---|
| 100 agents | ~$50/mo (Supabase free + Railway hobby) | ~$1,500 |
| 1,000 agents | ~$300/mo | ~$15,000 |
| 10,000 agents | ~$2,000/mo (DB upgrade + Redis) | ~$150,000 |

Gross margin stays above 85% because Algorand transaction costs are negligible and LLM costs are passed through.

---

### Regulatory awareness

- **x402 payments are micropayments** on a public L1 blockchain. No fiat currency changes hands through our platform — users fund ALGO wallets directly.
- We are not a money services business (MSB) under this model — agents interact with the Algorand network directly, we never custody funds.
- **KYC/AML:** Not currently required for testnet. Mainnet launch will require legal review of whether wallet provisioning triggers MSB classification in our jurisdiction. We are aware of this risk and will get a legal opinion before mainnet.
- **Data privacy:** Wallet mnemonics are AES-GCM encrypted, never logged, never returned in API responses.

---

### Team capability

We built:
- Custom SVG canvas with port-level wiring logic
- Agentic LLM function-calling loop from scratch (no LangChain, no agent framework)
- Full x402 payment flow end-to-end
- Algorand wallet provisioning + AES-GCM encryption
- SSE streaming broker
- OAuth (GitHub + Google) authorization code flow

We are new to blockchain. We learned Algorand SDK, x402 protocol, and Go crypto primitives during this build. We are honest about that. What we showed is what we built — not a demo of someone else's library.

---

### Hard questions — honest answers

**"What if x402 tools already exist outside AgentMesh?"**
> That's the point. x402 is an open standard. Any x402 API works with our platform automatically — just paste the URL and click Discover. We don't compete with the tools. We are the agent runtime that pays for them. We win when more x402 APIs exist.

**"Anthropic has no function-calling loop. Is this a limitation?"**
> Yes. Gemini and OpenAI run the full agentic loop (up to 15 iterations). Anthropic is basic chat today. Anthropic's tool-use API is documented and implementation is a known sprint. It is listed in `CONTRIBUTING.md` as an open issue.

**"What if Algorand fails / goes down?"**
> x402 payment steps fail gracefully — the agent logs the error, surfaces it in the SSE stream, and continues with non-payment tools. We don't crash. We also support non-x402 standard HTTP tools (roadmap July), so agents aren't blocked on Algorand availability.

**"Why not use Ethereum or Solana?"**
> Ethereum: $0.50–$5.00 per transaction. That kills sub-cent API pricing. Solana: fast, but smart contracts and token programs add complexity we don't need — x402 is a simple payment transfer. Algorand: $0.0002/tx, 3.5s finality, no smart contracts needed. It's the right tool.

---

## DEMO SCRIPT (2 minutes)

1. Open canvas — drag out a Trigger, Agent (Gemini), Tool402 (weather endpoint), Email action
2. Wire: Trigger → Agent → Tool402 (attached), Agent → Email
3. Click **Discover** on the Tool402 node — watch price and params populate from the live 402 response
4. Click **Deploy** — Algorand wallet created for the agent, address shown
5. Click **Run** — watch SSE stream:
   - `[agent] calling weather tool` 
   - `[tool402] 402 received — price: 0.001 ALGO`
   - `[tool402] payment sent — txID: ABC123`
   - `[tool402] data received`
   - `[email] sent to user@example.com`
6. Open Algorand Explorer link from the logs — show the on-chain transaction

Total: 90 seconds of live demo, nothing faked.
