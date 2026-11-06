# Submission — Arc Studio Continuity Track: Bring Your Project to Arc

**Bounty:** Arc Studio Continuity Track — *Bring Your Project to Arc* ($3,000).

**Project:** [AgentMesh](https://www.agent-mesh.app) — a live no-code platform for
autonomous AI agent workflows, where agents pay per call for the APIs they use over the
x402 protocol.

**What was brought to Arc:** AgentMesh's nanopayment settlement layer. The platform
already monetises agent API calls, but every settlement landed on Algorand, and the
escrow contract its backend was written against had never existed as anything but an
unused 64-line prototype. Arc Studio reviewed that prototype, found why it could not be
shipped, and produced and deployed the contract that replaces it.

This matches the track's third bullet — *nanopayment infrastructure shipped into an
existing AI agent or API monetization tool* — on a project that was already in
production before the event.

---

## Qualification requirements

### 1. Built in Arc Studio

Arc Studio project: **https://studio.arc.io/app/11ddfe5a-fa3b-4239-bf10-dd8bca5c40df**

Arc Studio did the onchain work: wrote `AgentRunEscrow.sol`, reviewed it over two rounds,
authored 7 Foundry test suites against adversarial token and Permit2 mocks, ran Slither,
and is deploying to Arc Testnet.

> **Pending at time of writing:** the live Arc Testnet deployment and the console below.
> Both are in flight in the Studio project; this file is updated with the deployed
> address and transaction hashes as soon as they exist. Nothing in the table at the
> bottom marked *pending* should be read as done.

### 2. Functional MVP and architecture diagram

- **Backend** — `backend/internal/arc/` carries AgentMesh's Arc treasury: chain and CCTP
  configuration, the 6-vs-18-decimal money layer, EVM signing, submission and recovery,
  and operator access control. Its `Config.Escrow` field reads `ARC_ESCROW`, which is the
  slot this contract fills.
- **Onchain** — [`arc/contracts/AgentRunEscrow.sol`](contracts/AgentRunEscrow.sol).
  Written, reviewed and tested; Arc Testnet deployment *pending*.
- **Frontend** — an Arc rail console in the Arc Studio project walking a run's whole
  lifecycle against the live contract: open a budget, pay simulated API tools, watch the
  budget bar move, read the `CallSettled` receipts back from chain, close and refund.
  *Pending* — built once the contract has a deployed address to point at.
- **Diagram** — in [`README.md`](README.md), showing which parts already existed and
  which were built here.

### 3. Video demonstration, presentation and documentation

- Documentation: [`README.md`](README.md) for the architecture, the economics argument
  and the review findings; [`contracts/README.md`](contracts/README.md) for the contract
  and how to reproduce the test run.
- **Video and presentation: still to record.** The demo should show the Arc Studio
  workspace itself, then drive the console through open → settle → settle → close, with
  the Arc Testnet explorer open on the receipts.

### 4. Repository

https://github.com/agen-mesh/wailist-agentmesh

---

## The argument in one table

AgentMesh's whole model depends on a payment small enough to put in front of a single
API call. Arc changes three things that decide whether that works.

| | Algorand rail (existing) | Arc rail (this work) |
|---|---|---|
| Assets the payer must hold | Two — a volatile gas token *and* USDC, plus a one-time opt-in before it can receive USDC at all | One — USDC **is** the native gas token |
| Can a funded payer be stranded? | Yes. A wallet out of gas token cannot move the USDC it holds. | No. The asset it spends is the asset it pays fees in. |
| Gasless payment | A 2-transaction fee-pooled group whose unsigned stub must be cosigned by a shared facilitator; group order is load-bearing | Witness-bound Permit2 authorization, relayed by anyone |
| Per-run spend ceiling | Application code only | Enforced onchain — a runaway agent loop cannot exceed its run budget |
| Receipt trail | Database rows | `CallSettled` events, one per paid API call |

---

## Verification

| | |
|---|---|
| Foundry tests | 94 tests, 7 suites, **94 passing / 0 failing** |
| Static analysis | Slither |
| Contract review | Arc Studio review, two rounds, clean on the second |
| Contract compiled locally | No — this machine has no Go or Foundry toolchain; the 94-test run is Arc Studio's, reproducible with `forge test` per `contracts/README.md` |
| Live on Arc Testnet | *pending* — open → settle ×2 → close, to be exercised against the deployed contract |

What is **not** claimed: no mainnet deployment, no live production cutover of AgentMesh's
settlement path to Arc, and no audit beyond Arc Studio's own review. The Go treasury in
`backend/internal/arc/` predates this work and is reviewed separately in its own pull
request.
