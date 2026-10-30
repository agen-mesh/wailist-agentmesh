# Service purchases

The `/purchases` page discovers provider-owned Arkiv offers and supports one configured `text.digest.v1` merchant. Checkout uses the customer's Arc testnet USDC authorization, a separate server gas relayer, and fresh World verification. World verification never grants access to the legacy shared signer. Missing checkout configuration leaves payments off; configured Arkiv discovery and browser offer creation remain independent.

This implementation was started before the November 5 to 7 event. Local simulations, public capability reads, and actual authenticated or signed network actions are separate evidence categories. No Studio origin, event eligibility, live identity, live payment, or public offer publication is established by this document. Project publication rights and the SDK license provenance still require confirmation.

## Flow and durable boundaries

1. Link an EOA using an expiring account-bound wallet challenge. Set an allowance in integer USDC micros. One USDC is `1000000` micros.
2. Select an Arkiv offer. The server validates typed attributes, native block expiry, UTC validity, owner/payee equality, endpoint policy, and the entire normalized snapshot. It obtains a signed merchant quote and persists the accepted immutable request.
3. Complete World authorization for that order. The callback first requires the initiating browser state cookie and the same authenticated account, then verifies the issuer, audience, signature, nonce, PKCE, fresh authentication time and required verification level. Stored identity state binds the account, order and intent. It does not approve payment.
4. Approve the exact order. Requesting signable data revalidates the offer and chain, atomically reserves allowance, records an event-search anchor and marks signature release before returning typed data.
5. Sign the stored EIP-3009 transfer. Its nonce is the intent hash. The random order salt is a separate input to the versioned canonical intent. The server stores encrypted signed transaction bytes and the transaction hash before its first broadcast. Reconciliation recovers only those identical bytes if the RPC cannot find the hash and the sender nonce is next. Known pending transactions are not resent. New orders cannot allocate across a missing relayer nonce.
6. Reconcile a confirmed `AuthorizationUsed` and matching `Transfer` event, including transactions submitted by another relayer. A consumed nonce without the exact event remains unknown. Event identity can fund only one order. The merchant durably claims execution and exposes result lookup by order and intent.

After signature release, cancellation cannot revoke the authorization. Expiry can release an unused allowance hold only after confirmed chain evidence. A paid execution failure does not imply a refund. Unknown payment or merchant results require reconciliation; force resume and process restart never create a replacement payment. Relay recovery remains eligible after completion or unused expiry until a confirmed sender nonce proves that transaction slot consumed. An expired authorization can therefore consume its original relay nonce through a reverted transaction, without a replacement payment. No gas-price replacement is automatic; an underpriced or unavailable relay still needs operator reconciliation. A merchant crash after its durable execution claim can require manual reconciliation. This is not an exactly-once execution guarantee.

The purchase workflow node waits durably and blocks downstream execution. Its configuration contains `offerKey`, `maxMicros` and optional `request` JSON. An empty request uses the preceding text. Resume retrieves the same logical order. Stopping the workflow prevents continuation without discarding its payment journal. Dry run creates no order, allowance hold, authorization or payment.

## Configuration

See [the environment example](../backend/.env.example). Configuration is loaded at startup. Secrets stay server-side and are not returned by the configuration endpoint.

| Variable | Purpose |
| --- | --- |
| `ARKIV_RPC_URL` | Trusted HTTPS Arkiv Tiramisu RPC, chain `7738577`. Enables discovery independently. |
| `ARC_PURCHASE_ENABLED` | Must be `true` to enable checkout. Defaults off. |
| `ARC_PURCHASE_RPC_URL` | Trusted HTTPS Arc testnet RPC, chain `5042002`. |
| `ARC_PURCHASE_RELAYER_PRIVATE_KEY` | Dedicated gas relayer key. Separate from `ARC_SPEND_PRIVATE_KEY`. |
| `ARC_PURCHASE_MAX_GAS_WEI` | Positive integer maximum transaction gas budget. |
| `ARC_PURCHASE_CONFIRMATIONS` | Required confirmations, default `2`, permitted range `2` to `100`. |
| `PURCHASE_MERCHANT_RESOURCE` | Exact public HTTPS service resource, port 443, without query or fragment. |
| `PURCHASE_MERCHANT_BEARER` | Internal merchant credential, at least 32 ASCII bytes. |
| `PURCHASE_MERCHANT_PUBLIC_KEY` | Ed25519 quote verification key as 32-byte hex. |
| `PURCHASE_MERCHANT_SIGNING_KEY` | Optional 64-byte Ed25519 private-key hex to mount the included merchant. Must match its public key. |
| `PURCHASE_MERCHANT_PAYEE` | Merchant Arc address, also the Arkiv offer owner. |
| `PURCHASE_MERCHANT_PRICE_MICROS` | Exact positive integer price for the configured merchant. |
| `WORLD_ENVIRONMENT` | `sandbox` or `production`, never selected by a request. |
| `WORLD_CLIENT_ID` | Registered World client identifier. |
| `WORLD_REDIRECT_URI` | Exact registered public callback: frontend `/api/purchases/world/callback` when using the proxy, or direct API `/purchases/world/callback`. |
| `WORLD_CLIENT_AUTH_METHOD` | Default `client_secret_basic`, registered `client_secret_post`, or registered `private_key_jwt`. |
| `WORLD_CLIENT_SECRET` | Required with the default client authentication method. |
| `WORLD_PRIVATE_KEY_PEM`, `WORLD_PRIVATE_KEY_ID` | Registered client signing key and key ID for `private_key_jwt`. |
| `WORLD_REQUIRED_ACR` | Optional explicit required verification level; client defaults remain enforced. |
| `FRONTEND_URL` | Browser origin used for wallet challenge binding and return navigation. |
| `ENCRYPTION_KEY` | Existing application encryption key, exactly 32 characters as consumed by the current wallet codec. |

The controlled merchant serves `/merchant/quote`, `/merchant/accept`, `/merchant/fulfill` and `/merchant/results/{orderId}` at the resource's origin. Merchant requests use the internal bearer. Public endpoints undergo DNS validation and address-pinned dialing; redirects and private destinations are rejected. Callback logging removes query values before the request logger creates its entry.

Frontend builds read `NEXT_PUBLIC_API_URL=/api`, server-only `BACKEND_URL`, and public `NEXT_PUBLIC_ARKIV_RPC_URL` for provider wallet writes. The browser RPC must be suitable for public inclusion and point to Tiramisu. It is separate from the backend's `ARKIV_RPC_URL`. A missing browser RPC leaves provider publication unavailable.

Register the callback on the same browser host that receives identity-start requests and the authentication cookie. With `NEXT_PUBLIC_API_URL=/api`, use `https://<frontend-host>/api/purchases/world/callback`. Direct API clients use `https://<backend-host>/purchases/world/callback` and must keep that host's authenticated browser session. The short-lived `purchase_world_state` cookie is host-only, HttpOnly, SameSite=Lax, and scoped to `/`; HTTPS startup configuration marks it Secure. A callback without either cookie cannot complete enrollment.

Migration `000042` runs through normal application startup. It adds order, allowance, wallet, identity attempt, merchant claim and payment-event constraints. Back up any non-scratch database before a separately approved deployment. No deployment is part of local verification.

## Controlled local simulation

The separate `cmd/purchase-sim` executable uses the real router, database, identity validator, payment parser, purchase journal and merchant executor with loopback external services. It never substitutes a success flag for production validation. Use an empty owned Postgres database whose name begins with `ao`, a numeric loopback connection, and a new private directory with mode `0700`:

```fish
set -x PURCHASE_SIM_DATABASE_URL '<owned loopback scratch URL>'
go run ./cmd/purchase-sim --listen 127.0.0.1:8104 --frontend http://127.0.0.1:3104 --state-dir '<private directory>'
```

Build the frontend with `NEXT_PUBLIC_API_URL=/api` and `BACKEND_URL=http://127.0.0.1:8104`, then serve it on `127.0.0.1:3104`. For the frontend proxy flow, also pass `--world-callback http://127.0.0.1:3104/api/purchases/world/callback` to the simulator. The page visibly identifies simulation mode. The private `/__verify__/capabilities` endpoint lists supported faults. Controls and ephemeral wallets are available only in this separate executable. No production route exposes them.

Preserve the database, private directory, merchant resource and provider address together across restart. Scenario controls reset counters and faults while preserving transaction nonces, receipts and consumed authorizations. Private serialized chain state includes signed transaction bytes and must never be published. Unsupported controls remain blocked. The local Arkiv fixture does not implement SDK offer writes; successful wallet-unit simulations are not live publication evidence.

For controlled unused-expiry checks, `--quote-ttl 45s` shortens new quotes only. The default remains five minutes; permitted values are whole seconds from 31 seconds through 15 minutes. Wait past the stored deadline and confirm new chain blocks before reconciliation. This flag does not advance the clock or rewrite an existing order. The `before_accept` RPC fault rejects a new transaction before ledger mutation, enabling exact-envelope recovery checks after the fault is cleared.

## External setup still required

Register a World client and exact callback, configure its permitted environment and client authentication, then observe a genuine sign-in without recording codes, state, tokens or secrets. Create an Arc Studio project if available and record its actual creation date and URL. Provision only an approved disposable testnet relayer and customer wallet for later network checks. Publish and read a real provider-owned Arkiv offer, then separately verify payment and merchant result with actual receipts. None of these steps is established by a local passing test.

Optional public Arkiv receipt publication, escrow and bridge checkout are outside this slice. The private verified Arc event and durable merchant result remain the payment record.
