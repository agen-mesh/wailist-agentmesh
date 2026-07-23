# x402 Orchestrator Relay — Design Spec

## Why

The Algorand x402 Global Challenge scores three entry shapes. AgentMesh's
current x402 support (agent wallet pays an external endpoint directly,
`backend/internal/engine/nodes/tool402.go`) only ever plays the *client*
role. That satisfies Standard/Composite entries but not Orchestrator: per
the challenge rules, "if you're never actually in the payment flow,
there's nothing on-chain to attribute to you." Orchestrator credit
requires we ourselves expose a paid endpoint that settles the inbound
payment first, then pay the downstream endpoint from our own wallet —
"both sides of a real transaction are real activity" (blog:
algorand.co/blog/the-x402-global-challenge-is-live-how-to-build-submit-your-entry).

Reverified against that post directly:
- Both legs (client→us, us→downstream) must be real payments settled
  through the **GoPlausible facilitator specifically** — no other
  facilitator counts.
- **Mainnet only counts toward the leaderboard.** Testnet is validation-only.
- The `payTo` address must stay fixed for the whole competition — it's the
  leaderboard's attribution key.
- Downstream endpoints do **not** need the `x402-global-challenge` tag
  themselves — any real GoPlausible-settled endpoint counts, tagged or not.
  (Confirmed live: `facilitator.goplausible.xyz/discovery/resources` shows
  662 real registered endpoints today, most untagged, some tagged.)
- The tag lives at `accepts[].extra.tag` in our own 402 response — confirmed
  from live catalog data, not just docs.

## What changes vs. what doesn't

**Unchanged:** the tool402 canvas node, its UI, `WorkflowNode.Endpoint`
field, the discover/inspector flow. Users keep pointing a tool402 node at
any x402 URL exactly like today.

**Changed:** what happens at runtime when that node executes. Today,
`ExecuteTool402` pays `node.Endpoint` directly from the agent's own wallet
(bare ALGO transfer, custom `X-Payment-Txid` header, direct algod submit —
our own legacy dialect, not the real x402/GoPlausible protocol). Going
forward, for any endpoint that speaks real x402 v2 (has `accepts[]`), the
agent instead pays **our own relay endpoint**, which:
1. mirrors the target's real price back as our own v2/USDC 402 challenge
   (tagged, payTo = platform wallet)
2. settles the agent's inbound payment via the GoPlausible facilitator
   (real settle, credited to us)
3. pays the actual target from the platform wallet, also via facilitator
   (real settle, credited to them)
4. returns the target's paid response back through to the agent

Endpoints still using our old legacy dialect (flat `price`/`recipient`,
no `accepts[]`) keep working exactly as today, direct pay, no relay, no
leaderboard credit — that path was never challenge-compliant and isn't
becoming so; it's kept only for backward compatibility with existing
non-GoPlausible demo endpoints (e.g. the `x402-weather-example`).

## Architecture

```
tool402 node (Endpoint = any x402 URL) → agent's wallet
        │
        ▼ (v2/accepts[] targets only; legacy targets bypass relay entirely)
  AgentMesh X402 Relay  (new: GET/POST /x402/relay?target=<url>)
        │  no X-PAYMENT  → fetch target's real price, mirror as our own
        │                  402 (payTo=platform wallet, USDC, tagged
        │                  x402-global-challenge)
        │  X-PAYMENT     → facilitator verify + settle           ┐
        │                  (real settle #1 — credited to US)     │
        ▼                                                        │
  platform (treasury) wallet builds new USDC atomic-group payment │──▶ facilitator.goplausible.xyz
  to target's own payTo, sends X-PAYMENT to target                │
        │  target verifies+settles itself                        │
        │  (real settle #2 — credited to THEM)                   ┘
        ▼
  target's paid response relayed back to agent
```

## New components

1. **Facilitator HTTP client** (`backend/internal/x402/facilitator.go`) —
   thin wrapper over `POST /verify`, `POST /settle`, `GET /supported`
   against `facilitator.goplausible.xyz` (URL env-configurable, matching
   existing `ALGOD_URL`/`ALGORAND_NETWORK` env pattern in
   `backend/cmd/server/main.go`).

2. **USDC atomic-group signer** — new method alongside
   `wallet.Service.SignAndSendPayment` (`backend/internal/wallet/algorand.go`).
   Builds a 2-txn atomic group (signed USDC axfer + unsigned fee-payer
   stub for the facilitator to cosign, per the documented gasless pattern),
   `AssignGroupID`s it, signs only the caller's own leg, returns the
   base64/msgpack-encoded group for the `X-PAYMENT` header. Does not
   submit to chain itself — the facilitator's `/settle` call does that.
   Used by both the agent wallet (paying the relay) and the platform
   wallet (paying downstream targets).

3. **Platform (treasury) wallet** — new, singular, env-configured mnemonic
   (like the existing per-agent wallets but not tied to any workflow).
   Needs USDC ASA opt-in (testnet `10458941` / mainnet `31566704`) and a
   small ALGO balance for opt-in MBR. No standing USDC float required —
   payment is pass-through (agent-funded), sequencing (inbound settle
   fully lands before outbound fires) is what matters, not balance.

4. **Relay resource-server handler** (`backend/internal/api/handlers/x402relay.go`
   + route in `router.go`) — the new `/x402/relay` endpoint described
   above. Public route (like `/run/{workflowId}`), not JWT-gated, since
   it's called by arbitrary x402 clients, not our own frontend.

5. **Relay settlement ledger** — new small table (new migration,
   `00XX_x402_relay_settlements`) tracking each inbound settle (txid,
   target URL, amount, status) and its paired outbound settle attempt.
   Separate from the existing credits/debits ledger
   (`backend/internal/db/migrations/000005_debit_ledger.up.sql`) — that
   ledger is internal platform-currency accounting; this is on-chain
   settlement bookkeeping and replay/idempotency protection (reject a
   reused `X-PAYMENT` payload/txid).

6. **`tool402.go` rewrite** — `ExecuteTool402` inspects the target's 402
   quote shape: `accepts[]` present → route through
   `/x402/relay?target=...` using the new USDC group-signer; legacy flat
   shape → unchanged existing direct-pay path.

## Error handling (explicit acceptance, not deferred)

- Inbound settle succeeds, outbound payment to target fails (target down,
  bad response, timeout): retry once, then surface an error result to the
  workflow same as any other tool402 failure today. **No refund path** —
  x402/GoPlausible has no chargeback mechanism; this mirrors real-world
  risk of the protocol itself. We already earned inbound attribution
  regardless of outbound outcome.
- Replay of a captured `X-PAYMENT` payload: rejected via the relay
  settlement ledger (txid/payload hash uniqueness check) before calling
  facilitator verify.
- Facilitator itself down/timeout: relay returns 502, same posture as
  existing `QuoteX402`'s upstream-fetch-failed handling
  (`backend/internal/api/handlers/tools.go`).

## Testing

- Unit: USDC atomic-group construction/signing (deterministic, no network)
  mirroring `backend/internal/wallet/algorand_test.go` conventions.
- Unit: relay handler against a fake facilitator (httptest server), mirroring
  `backend/internal/engine/nodes/tool402_test.go`'s existing httptest pattern.
- Integration: one real testnet round-trip (validation only, per the
  blog's own testnet/mainnet distinction) before flipping `ALGORAND_NETWORK`
  to mainnet for the actual leaderboard-counted deployment.

## Explicitly out of scope

- Any canvas/UI change — confirmed with user, this is backend-only.
- Refund/escrow mechanics.
- Restricting downstream targets to challenge-tagged endpoints only —
  confirmed any real GoPlausible-settled endpoint counts for the
  downstream side's own credit.
