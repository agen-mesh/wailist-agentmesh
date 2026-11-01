# Continue PR #285 on another device

Updated 2026-11-02. [PR #285: Add guarded service purchase checkout](https://github.com/agen-mesh/wailist-agentmesh/pull/285) is **draft, open and unmerged**. Keep it that way. The source branch is `feat/mumbai-services`; the PR base is `master`.

This document describes the published code baseline `3ef39fa4c6c00933fd23cb39798285e1d28b167c`, root tree `5d33d97a4114ef153884dd82ea4eb84e668b1c0b`. The handoff commit adds this document and an inert historical patch, without changing application or test source. The last verified base was `b419419bd9dc536f6257fde033fc70d66359ad7c`. Check the actual branch head before continuing; later commits may supersede this snapshot. The results below belong to the application baseline; fresh CI for the handoff commit is separate and pending at writing.

## Start here

Authorize repository access on the new device first. Use a registered SSH key for the commands below, or clone with `https://github.com/agen-mesh/wailist-agentmesh.git` and the normal credential manager if SSH is not ready. Never embed credentials or tokens in a URL or command.

For a fresh clone, these commands work in fish:

```fish
git clone git@github.com:agen-mesh/wailist-agentmesh.git
cd wailist-agentmesh
git fetch origin feat/mumbai-services
git switch --track -c feat/mumbai-services origin/feat/mumbai-services
git merge-base --is-ancestor 3ef39fa4c6c00933fd23cb39798285e1d28b167c HEAD
git log -6 --oneline
git status --short
```

Read this file, [purchase behavior](docs/arc-purchases.md), [World integration](docs/world-agent-pass.md), and [current Arkiv schema](arkiv/schema.md). Current code and the corrections below take precedence over older setup examples. Continue with small commits. Announce the exact proposed push or PR change and obtain explicit approval before publishing another head. Do not merge, close [issue #286](https://github.com/agen-mesh/wailist-agentmesh/issues/286), or resolve historical review threads as part of setup.

Keep private configuration, keys, database dumps, signed envelopes, test drivers, dependency directories and build outputs outside Git. Obtain secrets through the owner's password manager and staging service variables. Do not paste them into chat, this document, PR text, screenshots or shell history. A fresh clone contains source and public evidence links, not the previous machine's private test state.

### Preserved draft inventory

The scoped worktree inventory found no missing current PR #285 source, documentation or tests. Two unique staged additions from older PR #202 are preserved in [handoff/PR202-staged-draft.patch](handoff/PR202-staged-draft.patch). This is an **inert, untested historical draft**, not an application change. Do not apply it blindly to the current branch.

Original HEAD/index base: `a91f7527bf47a2d39706b39060da6d30d592e669`. The full-index patch contains only these two text additions:

| Historical path | Exact staged Git blob |
| --- | --- |
| `frontend/src/components/ui/foundation.test.tsx` | `9d64f1d2c570bab86b847c715031282c179bc0c7` |
| `frontend/src/components/ui/select.tsx` | `0dacfa17a081a7df167c1f93072baea706a7b710` |

Archive size is 9,616 bytes; SHA256 is `d396679aec8eefcafebd634f25c4a830e42dd8aa66fb0d2e77ed9ad689af329e`. Both complete files are added from `/dev/null`. Reconstruction in an empty temporary index produced both exact staged blobs without using the original commit or any preimage. The original HEAD above records provenance only. The original worktree/index was not changed, and the patch was not applied to current source. No code or tests from the archive were executed. Bounded content checks found no checked credential, private-path or attribution markers; no sanitization was needed. Any future use needs a separate review and relevant tests.

All 46 staged blobs from older PR #259 are recoverable in [published commit 968a8579994adbf06dd25a5212d9835b3dc59f58](https://github.com/agen-mesh/wailist-agentmesh/commit/968a8579994adbf06dd25a5212d9835b3dc59f58), so they are referenced rather than duplicated. Historical branches were not merged into this handoff. The existing `x402/example-server` gitlink remains at `6730478126f4fdc67e2cd84ad5e376db81690321`; its contents are not a new handoff change.

## What is implemented

| Component | Current role and boundary |
| --- | --- |
| Algorand | Still used by agent wallets, legacy USDC/x402 payments and workflows. Normal server startup still requires its two provisioned platform wallets. It has not been detached. |
| Arc checkout | Customer signs an exact EIP-3009 USDC authorization; a separate server relayer pays gas. Immutable order terms, allowance reservation, durable signed envelope, payment-event uniqueness, merchant acceptance/result lookup and reconciliation are implemented. |
| Arc shared spending | Raw spending and workflow broadcasts use `ARC_SPEND_PRIVATE_KEY`. Only explicitly configured operators may spend it. Workflow ownership, scheduled owner context, World verification and a per-call cap do not grant authority. Read-only quotes remain available. |
| Arc treasury | A registry exposes network-specific chain metadata and treasury operations. Explicit transfer/bridge operations are separate from checkout. `purchase.Rail` abstracts the purchase service's rail; it is not a shared Algorand/Arc router. There is no automatic cheapest-chain routing or unified cross-chain allowance/balance. |
| Arkiv | Provider-owned public offer discovery and wallet publishing on Rabdi, chain `7794095`, using SDK `1.0.0`. It stores offer terms, not customer requests, identity evidence or a public payment receipt in this slice. |
| World | Fresh identity evidence bound to the initiating browser, account and order. It does not approve a purchase or authorize shared spending. |

The checkout enables **Arc testnet `5042002` only**, using USDC `0x3600000000000000000000000000000000000000` and integer amounts with six decimals. For example, `10000` micros is `0.01 USDC`. The customer principal, relayer EVM nonce, authorization nonce and allowance reservation have separate identities. The random order salt is not the derived payment nonce.

`ARC_NETWORK` defaults to `testnet`; set it explicitly for staging. `mainnet` selects other legacy registry behavior, but checkout rejects it as unconfigured. [Migration 43](backend/internal/db/migrations/000043_purchase_chain_lease.up.sql) removes old hard-coded order constraints and adds a reconciliation lease. It does not enable mainnet or rewrite stored chain, asset, quote or intent commitments. Future mainnet support needs explicit domain/token/network validation and tests.

New Arkiv snapshots bind Rabdi's chain ID and the version 2 digest. Tiramisu entities do not carry over. Old unsigned orders must be cancelled/recreated from a current offer; already released Arc payments retain their original order for reconciliation. Never reinterpret an old entity key on the new chain or rewrite committed hashes.

Ambiguous Arc workflow attempts, unfinished durable logs and older unclassified attempts remain blocked even with forced resume. Only proven safe pre-send failures or confirmed reverts permit a new attempt. Purchase recovery uses the exact committed envelope; an authoritative external payment or confirmed unused expiry can still settle the customer hold while the original relayer nonce remains unresolved. Manual reconciliation can be necessary. There is no exactly-once guarantee.

The included merchant receives USDC directly. A paid tool failure does not refund it; the approval screen discloses this before signing. The checkout does not use escrow or bridge funds. Public `/x402/arc` signed-customer settlement remains a separate gas-sponsored path; its existing signed-authorization checks and per-call cap are not a general customer spending-grant system.

Key source: [startup](backend/cmd/server/main.go), [chain registry](backend/internal/arc/chains.go), [operator policy](backend/internal/arc/access.go), [purchase chain gate](backend/internal/purchase/chain.go), [journal service](backend/internal/purchase/service.go), [Arc rail](backend/internal/purchase/rail.go), [router](backend/internal/api/router.go).

## Toolchain and verification

Use Go `1.26.0`, Node `22.22.2`, pnpm `10.18.0`, Java 21, Android SDK platform 36 and the committed Gradle `8.14.3` wrapper. Hosted Android used Node `22.23.4` and Temurin `21.0.13+9`. Frontend manifests pin Next `16.3.4`, React `19.3.0`, Arkiv SDK `1.0.0` and viem `2.57.13`; resolve the rest from the committed locks. Do not upgrade dependencies just to resume work.

For a new development checkout, use the committed npm lock for the main frontend toolchain. The separate pnpm compatibility gate requires an empty frontend dependency directory; run it in another clean checkout rather than mixing installers:

```fish
cd frontend
npm ci
npm run lint
npm run typecheck
npm test -- --maxWorkers=2
npm run build
```

The independent install-only command is `CI=1 pnpm install --frozen-lockfile` inside `frontend/`, with normal lifecycle scripts and no restored `node_modules`. Confirm no manifest or lock drift afterward. The exact hosted checks are in [ci.yml](.github/workflows/ci.yml).

Backend checks require a fresh disposable PostgreSQL database in `TEST_DATABASE_URL`. The three Postgres connector tests additionally need a separate `TEST_POSTGRES_URL` database and their documented fixture in [connectors_db_test.go](backend/internal/engine/nodes/connectors_db_test.go). Never point either setting at an application or production database. `TEST_ALGOD_URL` enables the two optional Algod-dependent wallet tests; absent infrastructure means skips, not live proof.

```fish
cd backend
env GOMAXPROCS=2 go test -p 1 -count=1 -json ./...
env GOMAXPROCS=2 go build -p 1 ./...
env GOMAXPROCS=2 go vet -p 1 ./...
```

To run the ordinary server after securely provisioning the settings below, use `go run ./cmd/server` from `backend/`. It applies migrations automatically. For controlled local protocol simulations, the separate `cmd/purchase-sim` command uses the real router/journal with local external-service fixtures. It requires a loopback `PURCHASE_SIM_DATABASE_URL`, a database name starting with `ao`, and an owned private state directory. Inspect its flags with `go run ./cmd/purchase-sim -h`; the relevant flags are `-listen`, `-frontend`, `-world-callback`, `-state-dir` and optional `-quote-ttl`. Its default quote lifetime is five minutes; permitted test values are whole seconds from 31 seconds through 15 minutes. Preserve the database and private chain context together across restart. This command is not a production bypass or a live sponsor test.

For Android, install locked dependencies in `mobile/` with `npm ci`, set a public HTTPS `NEXT_PUBLIC_API_URL` for the native build, then run `npm run sync`. From `mobile/android/`, use `./gradlew --no-daemon --max-workers=2 assembleDebug testDebugUnitTest`. Static export has no Next proxy; `/api` is not a usable native API origin. Use a separate output checkout when switching between web and mobile build settings. Do not run the signed release workflow or provision release keys for a debug check.

### Observed evidence for `3ef`

These are retained results, not checks run by reading this document. Counts from different runs are not combined.

| Scope | Result and limit |
| --- | --- |
| Owner full Go | 2,474 test/subtest passes, 31 passing packages, 4 without tests; build/vet passed. Two wallet tests skipped because `TEST_ALGOD_URL` was unset. |
| Owner focused Go and race | 1,508 passes in each separate run across Arc, engine, nodes and handlers; no skips or race report. |
| Backend proof transfer | Produced on the backend correction commit, then transferred to `3ef` only after matching all 501 backend file hashes and subtree `3da4b369a1e265a9b2c4e4a7441143712d126591`. |
| Independent selected Go | 95 passes across 10 top-level controls, no failures/skips; separate from the full and race totals. |
| Owner frontend | 1,301 tests in 133 files; lint/types and default/configured production builds passed. One existing `ConnectorOAuthButton.tsx:49` lint warning remains. |
| Owner Android | Static export, sync and debug assembly passed. Local JVM XML reports 15 cases, 11 GeofenceStore and 4 GeofenceRegistrar, with no failures/skips. Ten plugin tasks were `NO-SOURCE`. No physical-device proof. |
| Current hosted checks | All four GitHub jobs passed on merge checkout `b209731a8299487760a0b69214d291ea89b2e275`, whose parents are master `b419` and `3ef` and whose tree matches `3ef`. |

Public job links: [frozen pnpm install](https://github.com/agen-mesh/wailist-agentmesh/actions/runs/41347836355/job/124971370963), [frontend](https://github.com/agen-mesh/wailist-agentmesh/actions/runs/41347836355/job/124971371007), [backend](https://github.com/agen-mesh/wailist-agentmesh/actions/runs/41347836355/job/124971370768), [Android](https://github.com/agen-mesh/wailist-agentmesh/actions/runs/41347836467/job/124971370953).

Hosted frontend reported 1,301 tests in 133 files. Hosted Go reported 31 passing packages, 17 cached, and 4 without tests, plus build/vet; it did not emit individual test or skip totals. Hosted Android tasks passed but did not expose XML or a case count. The 15-case count above is local only. [The exact-head Vercel deployment](https://vercel.com/soubhagya-sadhukhans-projects/agentmesh-main/XdnU61kg5S1VWHdBTV9oug43YDdZ) succeeded; deployment status is not page-health, identity or payment evidence.

[Review 5730190007](https://github.com/agen-mesh/wailist-agentmesh/pull/285#pullrequestreview-5730190007) and [review 5730218632](https://github.com/agen-mesh/wailist-agentmesh/pull/285#pullrequestreview-5730218632) say Ready with no required findings on `3ef`. Both are `COMMENTED`, based on static review, not formal approval or test execution. Both native review runs ended without a submitted verdict. Do not convert those comments into an approval claim or automatically resolve old threads.

The PR retains 22 historical media assets with their original source labels. Earlier controlled app simulations demonstrated purchase/recovery, browser/account binding, allowance refresh, expired checkout and Rabdi UI behavior on their labelled snapshots. They do not establish live World identity, a live Rabdi write, a real Arc payment, genuine Arc Studio origin or hackathon qualification. All owned local application, browser, database and build processes were released after verification.

## Secure configuration and startup

Let **F** be the stable public HTTPS frontend origin and **B** the public HTTPS backend origin, both without a trailing slash. Their real values still need to be chosen/confirmed by the owner. Do not register the letters F or B or a guessed preview URL. Use a staging database and test wallets; do not reuse production funds or keys.

Store backend secrets in the staging service's secret variables, with a password-manager recovery copy controlled by the owner. Public frontend settings are shipped to browsers. No private RPC access key belongs in a `NEXT_PUBLIC_*` setting. For local development, use ignored private environment files with restricted permissions. Never print the whole environment to check readiness.

### Ordinary server prerequisites

These apply even if only Arc/World is being tested. There is no Arc-only production startup switch.

| Setting | Required behavior or default |
| --- | --- |
| `DATABASE_URL` | Required PostgreSQL connection, 14+; validation used 16. Startup runs migrations. Use staging credentials and suitable TLS/pooler settings. |
| `ENCRYPTION_KEY` | Required, exactly 32 raw bytes. Use 32 ASCII characters from secure random material, not 64 hexadecimal characters. The same value must decrypt existing data; changing it without migration breaks access. |
| `JWT_SECRET` | Required backend signing secret; keep private. |
| `PLATFORM_WALLET_ADDRESS`, `PLATFORM_WALLET_ENC_MNEMONIC` | Required matching Algorand address and base64 AES-GCM encrypted mnemonic under the same encryption key. Startup decrypts and checks the address. |
| `PLATFORM_SPEND_WALLET_ADDRESS`, `PLATFORM_SPEND_WALLET_ENC_MNEMONIC` | A second required matching Algorand pair. Do not substitute an EVM key. |
| `CASHFREE_APP_ID`, `CASHFREE_SECRET_KEY` | Both required at startup. `CASHFREE_SANDBOX` defaults false; set it true for intended sandbox credentials. |
| `NOWPAYMENTS_API_KEY`, `NOWPAYMENTS_IPN_SECRET` | Both required at startup. `NOWPAYMENTS_SANDBOX` defaults false; use matching sandbox configuration. |
| `BASE_URL`, `FRONTEND_URL`, `CORS_ORIGIN` | Set to B, F, F for the web topology. Local base/frontend defaults are ports 8080/3000. CORS accepts an explicit comma-separated origin list. `PORT` defaults 8080. |
| `ALGORAND_NETWORK`, `ALGOD_URL`, `ALGOD_TOKEN`, `ALGORAND_INDEXER_URL` | Testnet default. Algod URL defaults to the public testnet endpoint; token is optional there. Choose matching endpoints explicitly when changing network. The indexer default follows network. |
| `RELAY_BASE_URL` | Optional, defaults to `BASE_URL`. Legacy outbound relay calls require a publicly resolvable allowed destination, not a private/loopback workaround. |

The Algorand USDC asset IDs are 10458941 on testnet and 31566704 on mainnet. These legacy funds are separate from the Arc customer allowance. Additional optional integrations are documented in [backend/.env.example](backend/.env.example); do not invent placeholder credentials to satisfy startup and then claim readiness.

### Arc, Arkiv and merchant settings

Source of truth: [purchase configuration](backend/internal/config/purchase.go) and [startup wiring](backend/internal/api/handlers/purchases.go).

| Setting | Value or format |
| --- | --- |
| `ARC_NETWORK` | Explicit `testnet` for this checkout. Default is testnet; mainnet checkout is disabled. |
| `ARC_PURCHASE_ENABLED` | Literal `true` enables initialization if all required configuration is present; otherwise default-off. |
| `ARC_PURCHASE_RPC_URL` | Required HTTPS Arc testnet RPC. Current shared registry uses `https://rpc.testnet.arc.io` and explorer `https://explorer.testnet.arc.io/tx/`. Chain checks must still return 5042002. |
| `ARC_PURCHASE_RELAYER_PRIVATE_KEY` | Required private EVM key, 32 bytes encoded as 64 hex characters, optional `0x`. Use a dedicated staging relayer, not the customer or provider key. |
| `ARC_PURCHASE_MAX_GAS_WEI` | Required positive decimal integer total gas-cost ceiling in native base units. Choose an explicit approved budget; this is not customer principal or a USDC-micros value. |
| `ARC_PURCHASE_CONFIRMATIONS` | Optional, default 2, accepted range 2..100. |
| `ARKIV_RPC_URL` | Backend HTTPS Rabdi RPC, normally `https://rpc.rabdi.db-chain.testnet.arkiv.network`. Expected chain 7794095 is fixed; an RPC cannot select a new chain. |
| `PURCHASE_MERCHANT_RESOURCE` | Exact public HTTPS resource, for the included service use `B/merchant/fulfill`. This exact string must match the provider offer and quote. No credentials in the URL. |
| `PURCHASE_MERCHANT_BEARER` | Required private internal bearer; included handler requires 32..256 visible ASCII characters without spaces. Never give it to the browser. |
| `PURCHASE_MERCHANT_PUBLIC_KEY` | Required Ed25519 verification key, 32 bytes as 64 hex characters, optional `0x`. This is distinct from the EVM payee. |
| `PURCHASE_MERCHANT_SIGNING_KEY` | Required to mount the included merchant; 64-byte Ed25519 private key as 128 hex characters, optional `0x`, matching the public key. Omit only when an independently deployed compatible merchant provides the routes. A 32-byte seed alone is not this field's format. |
| `PURCHASE_MERCHANT_PAYEE` | Required EVM address; it must equal the Arkiv entity owner and payload payee. |
| `PURCHASE_MERCHANT_PRICE_MICROS` | Required positive decimal integer, exactly matching the listing price. No floating-point amounts. |
| `ARC_SPEND_PRIVATE_KEY`, `ARC_OPERATOR_USER_IDS` | Optional legacy shared-spending key and comma-separated trusted account IDs. They are not required for customer checkout. An empty operator list denies shared-key spending and emits a warning when a key is configured. |
| `ARC_STUDIO_URL` | Optional legacy metadata only. Setting it does not prove creation in Studio. |

The included useful service has capability `text.digest.v1` and accepts an object with one required string field `text`, with additional properties disabled. Its bound offer method is `POST`. Publish only this public schema, not sample customer text or secrets.

The merchant client uses the resource's origin plus `/merchant`: `POST /merchant/quote`, `POST /merchant/accept`, `POST /merchant/fulfill` and `GET /merchant/results/{orderID}?intentHash=...`. These routes use the private bearer and durable store. The resource path is an exact commitment, not permission to call any arbitrary tool. Endpoint validation rejects unsafe destinations. The backend must reach its configured merchant origin without an interactive hosting login wall.

### Frontend and callback topology

| Setting | Where and effect |
| --- | --- |
| `NEXT_PUBLIC_API_URL` | Frontend build setting: `/api` for web. All normal browser authentication and purchase requests use this origin. |
| `BACKEND_URL` | Server-side Next configuration: B, without `/api`. [The rewrite](frontend/next.config.ts) forwards `/api/:path*` to `B/:path*`. Treat a change as requiring a new frontend build/deployment. |
| `NEXT_PUBLIC_ARKIV_RPC_URL` | Optional public frontend build setting for provider wallet publication; use a browser-safe Rabdi endpoint. If omitted the helper uses its public Rabdi default. Never put a backend private access key here. |
| `MOBILE_BUILD`, `NEXT_PUBLIC_NATIVE_CLIENT` | Set by the mobile export script. Native builds require an absolute public HTTPS API URL and have no Next rewrite server. |

Rebuild the frontend after changing public build variables. Restart the backend after configuration changes; a dashboard save does not prove the running process loaded them. Keep all replicas consistent. Do not assume legacy Arc configuration reads provide hot reload for the initialized purchase service.

## World registration, then an identity-only checkpoint

1. Open the [sandbox portal](https://sandbox.auth.world.org/portal) first and follow the [application registration guide](https://auth.world.org/docs/guides/app-registration). Production has a [separate portal](https://auth.world.org/portal). Confirm access/eligibility and the current [sandbox testing procedure](https://auth.world.org/docs/guides/testing) there. Sandbox proof is not production identity proof.
2. Register a confidential web/backend client. Prefer **HTTP Basic** (`client_secret_basic`) when creating it. Use authorization code, query response mode, scope `openid`, and the exact web callback **`F/api/purchases/world/callback`**. The application generates PKCE S256, state and nonce. Keep the registered authentication method consistent; do not change environment variables to pretend a differently registered client uses Basic.
3. Copy the client secret directly into the password manager and backend staging variables. Do not capture it in media or this repository. Use the variables below, then restart the backend.

| Backend variable | Exact meaning |
| --- | --- |
| `WORLD_ENVIRONMENT` | `sandbox` for sandbox registration, later `production` only with the corresponding real client. Issuers are derived and pinned by the application. |
| `WORLD_CLIENT_ID` | The registered client identifier. |
| `WORLD_CLIENT_AUTH_METHOD` | `client_secret_basic`, also the default. |
| `WORLD_CLIENT_SECRET` | The private secret for that registered client. |
| `WORLD_REDIRECT_URI` | Exact registered `F/api/purchases/world/callback`, with no extra query, fragment or trailing slash. |
| `WORLD_REQUIRED_ACR` | Optional; defaults to `https://world.org/oidc/acr/orb`. Keep that value for the Orb flow. |
| `WORLD_PRIVATE_KEY_PEM`, `WORLD_PRIVATE_KEY_ID` | Both absent for Basic. Only use for a client actually registered for `private_key_jwt`; that alternative requires a valid RSA private PEM and key ID, with the client secret absent. |

The validator accepts the documented Orb method set `pop` plus `iris`, in either order, while checking issuer, audience, signature, nonce, time, fresh `auth_time` and the configured assurance level. It does not accept arbitrary AMR supersets. Default identity/attempt freshness is five minutes and clock skew is bounded at 30 seconds. Keep host clocks synchronized.

Use the same browser and signed-in account throughout. `POST F/api/purchases/{id}/identity` sets the HttpOnly `purchase_world_state` cookie on F with Path `/` and SameSite `Lax`. With HTTPS backend configuration it is Secure. The callback needs both that cookie and the same account's `agentmesh_token`; a Bearer-only API session does not automatically accompany browser navigation. Do not start through F and return directly to B. The callback returns 303 to **`F/purchases?order=<order-id>`**. Query values must stay out of logs and screenshots. If Vercel preview protection redirects to hosting login, finish owner authentication or configure an appropriate stable staging route before testing; a login wall is not an application health result.

World client settings alone are insufficient for the current app flow. Before `Verify with Agent Pass` can start, the full purchase service must initialize, Arc RPC reads must support wallet linking, a real Rabdi offer must be discoverable, and the configured merchant must quote and durably accept a linked-wallet order. Provider publication needs Rabdi gas. Customer principal and relayer gas funding can wait until the payment phase.

For an identity-only checkpoint: sign in, link an unfunded customer wallet, select the matching offer, enter valid text, create the exact order, and click **Verify with Agent Pass**. A successful return reaches `awaiting_approval`. Stop there and cancel the unsigned order if done. Do not click approval or payment signing merely to verify login.

After a backend restart, sign in through F and inspect the authenticated `GET F/api/purchases/config` response or the page's readiness notices. `enabled`/`worldAvailable` indicate configured initialization; `arkivAvailable` indicates catalog construction and can be true with checkout disabled. They do not prove a valid code exchange, RPC reachability, a funded wallet or a successful merchant call. Confirm actual discovery and the browser callback separately. Check only variable names, readiness booleans and sanitized errors, never stored secret values.

## Remaining live sequence

1. **Choose stable F/B and provision staging.** Transfer existing required credentials securely, verify the environment and restart/build boundaries, and keep production untouched. The normal server's Algorand/payment-provider startup requirements still apply.
2. **Publish and discover a real Rabdi offer.** Consult [Rabdi network documentation](https://docs.arkiv.network/networks/rabdi/), [replaced networks](https://docs.arkiv.network/networks/older-networks/) and the [Arkiv faucet](https://hub.arkiv.network/faucet). Fund the provider wallet with test tGLM on 7794095. In `/purchases`, open **Publish a service offer**, connect the provider wallet and use **Publish with provider wallet**. Match owner/payee, `text.digest.v1`, resource and integer price. Observe a genuine entity/transaction and read it back into discovery. The controlled simulator does not publish Arkiv entities.
3. **Complete the World checkpoint above.** Keep registration, sandbox access and a real callback as separate observed gates. No genuine World login has been established by the retained local tests.
4. **Fund only test wallets for payment.** Use the [Circle faucet](https://faucet.circle.com/) and [Arc connection documentation](https://docs.arc.io/arc/references/connect-to-arc). Fund the customer with test USDC principal and the dedicated relayer with native USDC gas. Native gas amounts use 18 decimals; ERC-20 purchase amounts use 6. Provider Rabdi tGLM is unrelated. Verify balances on the intended networks before signing.
5. **Run one small exact purchase.** Link the customer wallet, save a bounded spending limit, inspect a fresh offer/order, verify identity, approve the displayed direct-payment terms, then sign the exact USDC authorization. Record only public order/transaction/entity identifiers and sanitized state. Observe one payment event, merchant result and correct available/reserved/spent ledger. If pending or unknown, reconcile the same order; do not create a replacement payment or force a replay. Signature release is the cancellation boundary; a released authorization may remain usable until expiry.
6. **Exercise recovery after the first live success.** Verify duplicate result lookup without a second fulfillment, restart with preserved journal, and authoritative unused expiry. These were tested with controlled fixtures, not with live sponsor systems. Do not manufacture failures with real funds.
7. **Handle Studio provenance honestly.** [Arc Studio](https://studio.arc.io/) project creation/access remains owner setup. A URL or a later import cannot retroactively establish where earlier work originated. Record only observed project history and dates. Keep hackathon timing, public-license eligibility and SDK-license provenance unresolved until documented.

## Backlog and handoff limits

- Audit the recorded dependency findings: 32 frontend advisories, including 1 critical, and 7 mobile advisories. These are unassessed package-manager summaries, not confirmed reachable vulnerabilities or completed remediation. Inspect locked versions and affected paths before making targeted updates.
- Observe a complete live World/Rabdi/Arc/merchant flow and then test operational recovery, alerting and manual reconciliation. The existing lease serializes the background reconciler; recovery must continue using only committed bytes across replicas.
- Keep the shared-key boundary explicit. A future customer gas policy, spending grant or user-specific signer needs its own durable authorization and budget design. Do not restore workflow-owner authority over the platform key.
- Mainnet enablement, broader merchant support, public receipt publication, escrow/refunds and automatic cross-chain routing are future design work. They are not unlocked by credentials or by changing a URL.
- Purchase history intentionally blocks account deletion with a conflict. A fuller retention/unlink policy remains a product decision; never delete financial history to bypass a foreign-key failure.
- Retain the original 22 media labels and old review evidence. New visible behavior needs fresh actual-application evidence. Hosted green checks and static Ready comments do not establish live integrations, device behavior or formal approval.

No previous-machine cache, fixture or secret is needed to read this handoff. Recreate private test infrastructure deliberately; do not copy an arbitrary old worktree or reset a chain fixture behind a surviving journal.

Fun fact: EIP-3009 authorization nonces are independent of the relayer account's sequential transaction nonce.
