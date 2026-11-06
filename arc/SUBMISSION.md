# Submission — Arc Studio Continuity Track: Bring Your Project to Arc

**Track:** Arc Studio Continuity Track — *Bring Your Project to Arc* ($3,000).

**Fit:** the track's third bullet — *nanopayment infrastructure shipped into an existing AI agent or
API monetization tool.* AgentMesh is that tool, and was live before the event.

**Project:** [AgentMesh](https://www.agent-mesh.app) — a no-code platform for autonomous AI agent
workflows, where agents pay per call for the APIs they use over x402.

**What was brought to Arc:** AgentMesh's x402 nanopayment settlement layer. The platform already
monetises agent API calls per call; every payment settled on another chain's rail. It now settles
**USDC on Arc Testnet** through Arc's x402 facilitator, with the chain chosen by one environment
variable and no change to the signing, relay, catalog or agent-loop code.

No contract was deployed for this. x402's `exact` scheme settles against USDC's own EIP-3009
interface, so the integration is a chain profile — which is the whole point: a payment rail that
drops into an existing product in a day is one a product will actually adopt.

---

## Qualification requirements

### 1. Built in Arc Studio

**Arc Studio project: https://studio.arc.io/app/11ddfe5a-fa3b-4239-bf10-dd8bca5c40df**

What Arc Studio did, in that project:

- Built the **Arc rail console** — the demo app, in [`console/`](console/) in this repo and live in
  the Studio preview panel. React + viem/wagmi, driving the full x402 exchange against the live
  facilitator.
- **Settled real payments on Arc Testnet** from that code while building it, including finding two
  things the spec handed to it got wrong: the resource server reads the v2 `Payment-Signature`
  header (not `X-Payment` alone) and answers with `Payment-Response`; and the `fetch` endpoint is
  `POST` at 0.015 USDC, not `GET` at 0.01. The console reads method, price and input schema from
  the live catalog as a result.
- Earlier in the same project: wrote, reviewed (twice) and tested
  [`contracts/AgentRunEscrow.sol`](contracts/AgentRunEscrow.sol) — 7 Foundry suites, 94 tests,
  Slither. **Not deployed, not part of this submission's working path**; see the README.

### 2. Functional MVP, frontend + backend + architecture diagram

- **Backend** — AgentMesh's Go backend, in this repo. The Arc rail is
  `backend/internal/chain/{profile,arc}.go` plus `X402_CHAIN=arc`; the x402 client, EIP-3009
  signing, relay and Bazaar catalog it feeds were already there and are unchanged.
- **Frontend** — two, both working: AgentMesh's own Next.js app (its copy, settlement-token labels
  and demo workflow follow the rail via `NEXT_PUBLIC_X402_CHAIN`), and the Arc rail console in
  [`console/`](console/).
- **Architecture diagrams** — in [`README.md`](README.md): a sequence diagram of one paid call end
  to end, and the one-package/two-rails profile switch.
- **How to run both** — [README → Run the demo](README.md#run-the-demo).

### 3. Video demonstration and presentation

Script outline, 3½ minutes — *still to record:*

| Time | Beat |
|---|---|
| 0:00–0:45 | The Arc Studio project: the console being built there, the chat history, the preview panel. State plainly that the payment code in the repo came out of this workspace. |
| 0:45–1:15 | The console's rail header: Arc Testnet 5042002, USDC `0x3600…0000`, and the facilitator's advertised domain separator next to the one read from the token by RPC — matching. Then the tool list, read live from `arc.007575.xyz/discovery/resources`. |
| 1:15–2:15 | "Pay & call" on the search endpoint: the 402 and its decoded `Payment-Required` challenge, the EIP-3009 typed data, the wallet signature, the retry. |
| 2:15–2:45 | The settlement: tx hash, opened on `explorer.testnet.arc.io` — `AuthorizationUsed` + `Transfer`, submitted by the facilitator's relayer, so the payer spent no gas and held only USDC. |
| 2:45–3:30 | The same payment inside AgentMesh with `X402_CHAIN=arc`: the startup line naming Arc, the Bazaar filled from Arc's catalog, a workflow run paying `…/api/arc/v1/search`, the run receipt linking to Arc's explorer. Close on the one-asset argument: on Arc the asset an agent spends is the asset fees are paid in, so a funded agent cannot be stranded. |

### 4. Repository

https://github.com/agen-mesh/wailist-agentmesh — this work is the `feat/arc-run-escrow` branch,
pull request #290.

---

## Verification

| | |
|---|---|
| Arc profile values | Read live on 2026-11-07 from `rpc.testnet.arc.io` and `arc.007575.xyz`; the token's `DOMAIN_SEPARATOR()` matches the facilitator's `/assets`. Table in [README](README.md#live-verification) |
| Payments on Arc Testnet | 2, both confirmed by `eth_getTransactionReceipt`: `0xe022ec29…80cf16` and `0xffef67af…ccc185` — status `0x1`, relayer-submitted, `AuthorizationUsed` + `Transfer` |
| Go | `go build ./...` clean; `go test ./...` passes every package, including `TestX402PaymentPathIsFrozen`, the digest test that pins the payment-path files — this work leaves all of them byte-identical |
| Frontend | `pnpm typecheck` clean; `pnpm test` 1123 tests in 122 files, all passing |
| Console | `npm install`, `npm run typecheck` and `npm run build` all clean from this repo's copy, plus the live settlements above |
| Contract | Written and reviewed in Arc Studio, 94 Foundry tests **reported by Arc Studio** and not reproduced here (no Foundry on the machine this was assembled on) |

**Not claimed:** no contract deployed; no mainnet; AgentMesh's production deployment still settles on
its existing rail (Arc is opt-in by `X402_CHAIN`, deliberately, so merging this changes nothing
until it is set); no audit beyond Arc Studio's own review of the unused escrow.
