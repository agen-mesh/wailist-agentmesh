# World Agent Pass

Implemented locally on October 30, 2026, Asia/Kolkata, from base `b5e46422b2e8e62b56607a2966a0449c94843ac6`. This is pre-event work. The isolated backend module is `backend/internal/world`. It does not import the purchase package, choose an account, approve an order, issue application credentials, grant signer access, or dispatch funds.

Local protocol simulations pass against an independently controlled TLS issuer with generated RSA keys. They do not demonstrate a real World authentication, a production personhood check, or an integrated protected purchase. No privately configured registered World client was supplied to this leaf. No registration, real authentication, production setting change, payment, or Studio provenance is claimed.

## Private setup contract

Startup owns environment loading and injects `world.Config`. This module does not read environment variables. The names below are the agreed startup mapping; this document does not establish that a running deployment has loaded them.

| Setting | Contract |
| --- | --- |
| `ARC_PURCHASE_ENABLED` | Defaults to `false`. Central startup keeps checkout disabled until all purchase prerequisites exist. |
| `WORLD_ENVIRONMENT` | Required when enabled: `sandbox` or `production`. Startup derives the exact official issuer from this value. |
| `WORLD_CLIENT_ID` | The client registered in the selected environment. |
| `WORLD_REDIRECT_URI` | The exact registered public callback, including scheme, host, port, case and path. Use `https://<frontend-host>/api/purchases/world/callback` for frontend proxy requests or `https://<backend-host>/purchases/world/callback` for direct API requests, matching the initiating browser cookie host. No query or fragment. HTTPS is required except registered sandbox loopback HTTP callbacks. |
| `WORLD_CLIENT_AUTH_METHOD` | Defaults to `client_secret_basic`. Also supports `client_secret_post` and `private_key_jwt`; must match registration and discovery. Public clients are rejected. |
| `WORLD_CLIENT_SECRET` | Required only for Basic or POST authentication. Private backend configuration. |
| `WORLD_PRIVATE_KEY_PEM` | Required only for private-key JWT authentication. RSA PKCS#1 or PKCS#8 PEM, 2048 to 8192 bits. Register only its public JWKS. |
| `WORLD_PRIVATE_KEY_ID` | Required with private-key JWT; selects the registered key. |
| `WORLD_REQUIRED_ACR` | Defaults to `https://world.org/oidc/acr/orb`. `https://world.org/oidc/acr/orb-face` is accepted only if discovery advertises it. |

The integration uses fixed module defaults of 300 seconds for attempt lifetime and maximum authentication age, and 30 seconds for clock skew. It does not load separate environment variables for these limits or a custom issuer. Token expiration itself has no grace period.

Keep registration credentials and identity mappings separate by environment. Never expose these private settings through frontend environment variables, API configuration responses, request logging, or receipts. Missing required settings or unsupported discovery must keep the purchase identity feature unavailable.

Public discovery was retrieved from the official [production](https://auth.world.org/.well-known/openid-configuration) and [sandbox](https://sandbox.auth.world.org/.well-known/openid-configuration) issuers. Both advertised code/query, S256, RS256 and the supported confidential-client methods. Production advertised Orb; sandbox also advertised Orb with Face Auth. These were metadata reads, without authentication.

Live acceptance still needs a privately configured registration, its exact public callback, and an eligible identity in the chosen environment. Sandbox identities must be visibly described as test identities. Sandbox authentication does not establish production personhood. See the official [testing guide](https://auth.world.org/docs/guides/testing).

## Request, completion and protected action

1. Central integration authenticates the account and freezes the order. It supplies `Binding{AccountID, OrderID, IntentHash, ExpectedSubject, AllowEnrollment}` from server state. The intent hash is lowercase 32-byte hexadecimal, optionally prefixed with `0x`, and is compared exactly. Existing links require their expected subject. An unlinked account requires explicit enrollment permission; the module never silently enables enrollment.
2. `Client.Begin` persists the attempt before returning `Authorization{attempt_id, authorization_url, expires_at, environment, demo}`. The redirect uses discovered endpoints, the exact callback, `scope=openid`, random state and nonce, S256 PKCE, `max_age=0`, and an essential ACR request. Only a SHA-256 digest of state is stored. The verifier stays private. The authenticated start handler sets the state in a short-lived host-only HttpOnly cookie, scoped to `/` with SameSite=Lax and Secure under HTTPS startup configuration. Expected subject is checked in the backend and is absent from the browser URL.
3. The callback requires a valid application session and exactly one matching browser state cookie before resolving its saved binding through `HashState`. The saved account must equal the authenticated account before `Client.Callback` can consume the attempt or exchange a code. Rejected foreign-browser or wrong-account requests leave the saved attempt usable by its initiating browser. A matched callback clears the state cookie; the durable attempt still prevents replay if that cookie is restored. It passes only the server-owned binding to `Client.Callback`. It must not accept an account, order, intent or return URL from callback query parameters. The attempt is consumed atomically before denial handling or code exchange. Both success and error callbacks require the exact issuer. Malformed, denied, expired, cancelled or replayed attempts return no evidence.
4. The backend exchanges the code with the registered authentication method and exact redirect/verifier. It checks RS256 against discovery JWKS, issuer, audience and authorized party, strict expiration, nonce, subject, ACR/AMR, issuance timestamps, and `auth_time` relative to both attempt start and allowed age. Evidence is returned only after durable `Complete` succeeds. Tokens are discarded rather than exposed as application credentials.
5. Central integration displays the saved purchase under `/purchases`, obtains a separate authenticated approve/deny decision, and rechecks evidence expiry, ownership, immutable intent, wallet control, limits and order status before any payment authorization. Callback success alone cannot approve or execute the purchase. Account-subject linking and wallet linking are distinct checks.

The official [OIDC guide](https://auth.world.org/docs/guides/oidc) defines the exchange and callback issuer rules. A new `iat` does not prove recent authentication. The module checks `auth_time` with a bounded start-time tolerance, and caps evidence validity at the earliest of authentication-age expiry, token expiry and attempt expiry. Consumers must recheck `ValidUntil` at execution time. See [step-up authentication](https://auth.world.org/docs/guides/step-up).

Evidence preserves `AuthenticationKind`: `world_proof` for Orb with `amr=["pop","iris"]` or the retained `amr=["pop"]`, `passkey` for Orb with `amr=["pop","user","mfa"]`, or `face_proof` for the separately required Face Auth class with `amr=["pop","face"]`. A passkey result must not be described as a fresh credential proof. Unsupported or inconsistent combinations are rejected. Sandbox evidence always has `Environment=sandbox` and `Demo=true`.

The official [ID token guide](https://auth.world.org/docs/guides/id-token), retrieved October 31, 2026 (UTC), documents `amr=["pop","iris"]` for an Orb proof. The exact pair is accepted in either order only with the required Orb ACR; duplicates and additional methods are rejected. Both controlled issuers emit this documented pair. The guide's other Face Auth and Selfie Check method combinations are outside this compatibility change; the existing Face Auth policy remains unchanged.

The complete meaningful protected-action journey and its unsuccessful path belong to central application acceptance. This leaf's simulations establish protocol behavior and zero returned evidence on failure. They do not establish that the application's purchase handler, database adapter, signer, or engine is wired correctly. The [human-in-the-loop guide](https://auth.world.org/docs/guides/human-in-the-loop) keeps business-action approval with the application.

## Durable adapter invariants

`world.Store` has four methods. Session64 owns the database implementation, migrations and central linking policy.

| Method | Required atomic behavior |
| --- | --- |
| `Create(ctx, Attempt)` | Insert unique attempt ID and state digest. Freeze the full binding, issuer/environment/client/callback, policy, nonce/verifier, creation and expiry. Encrypt nonce/verifier at rest. |
| `Consume(ctx, stateHash, Binding, now)` | Compare the full binding and expiry, then transition `pending` to `consumed` once across all processes. Return a private snapshot. Missing, mismatched, expired or terminal rows return `ErrAttemptUnavailable`. |
| `Complete(ctx, attemptID, Evidence, now)` | Transition only `consumed` to `validated`, once, while uncancelled and before both attempt and evidence expiry. Compare the saved binding and issuer/environment. Serialize with central order invalidation. Enforce existing subject ownership or explicitly authorized first enrollment with durable uniqueness. No approval or signer permission is created. |
| `Cancel(ctx, attemptID, Binding, now)` | Invalidate matching pending or consumed state, serialized with completion. A cancellation that commits first prevents evidence. Central order policy must still reject a cancelled order if completion committed first. |

After consumption, exchange failure, bad claims, dependency failure, crashes and lost responses never restore `pending` or extend expiry. Start a new attempt if needed. This intentionally gives up automatic code-exchange retry to keep the single-use rule simple and durable. A completion whose database response is lost is not retried through the callback. Central authenticated order lookup reconciles stored state.

Purge private nonce/verifier material when it is no longer required, without making a consumed state reusable. Retain terminal state or an equivalent durable tombstone through the original lifetime. The module has no process-only production replay store. Its test store models the contract; it does not prove database durability or central subject-link uniqueness.

JWKS caches expire after five minutes. Unknown key IDs and provider failures can cause at most one refresh every 30 seconds per client instance. Key overlap is supported. A refresh failure does not authorize use of an expired cache. A legitimate new key during the refresh cooldown can fail closed and require a new attempt.

## Callback privacy and sanitized errors

The inspected base router installs Chi's default logger. Chi v5.3.2 `DefaultLogFormatter.NewLogEntry` formats `r.RequestURI` before the handler. Handler-only redaction is therefore too late. Session64 must provide sanitized logging for the new callback before that formatter runs, while preserving query parsing privately for the callback handler. Limit the change to the new purchase/identity routes.

Never log callback query strings, authorization URLs, request authorization headers, token request/response bodies, verifier, nonce, subject, or raw storage errors. Responses should use stable classifications: disabled configuration, unavailable attempt, denied authentication, invalid callback/evidence, or unavailable provider/storage. The module never returns provider `error_description` or raw network/storage errors. `Config`, `Binding`, `Attempt` and `Evidence` formatting hides private values; this does not authorize logging their fields individually.

Return the browser only to an application-controlled purchase view derived from the saved order. Use `Cache-Control: no-store` and `Referrer-Policy: no-referrer` on authorization/callback responses. The callback must not render tokens or sensitive query values into HTML, receipts, analytics or console output. Independent integration checks should inject synthetic sentinels and verify request logs and failure output remain sanitized.

## Reusable local application simulator

`world/simulation` is an explicit local-only factory for central application acceptance. `simulation.New(redirectURI)` accepts only loopback callbacks at `/purchases/world/callback` or `/api/purchases/world/callback`, generates its own synthetic client registration and RSA key, and starts an owned loopback issuer. Call `Close` during runtime cleanup. No external client credentials are accepted.

`Registration()` supplies the synthetic issuer/client/callback settings. Construct the real `world.Client` with `Environment=world.Sandbox` and `HTTPClient()` as its injected transport. `BrowserURL(authorizationURL)` maps only that fixture's sandbox authorization endpoint to the loopback issuer. The transport rejects production and arbitrary external hosts. There is no production configuration bypass or environment-variable switch in the module.

`SupportedOutcomes()` lists success, denial, stale authentication, wrong audience, nonce or issuer, expired token, missing or future authentication time, invalid signature and token endpoint error. The selected outcome and subject are captured when authorization starts. Success requires local code exchange, PKCE and RSA validation by the real client. `SetSubject` changes future grants only. The fixture supports synthetic Basic client authentication; independent package tests also exercise POST and private-key JWT methods.

`Observations().TokenRequests` counts actual local token POST receipts, including rejected exchanges. It contains no payloads or credentials and never resets when `SetOutcome` changes. The private verification wrapper must map this count to `counters["world.token"]` and preserve it across scenario changes.

Central startup must select this factory only for an explicit controlled simulation with scratch application data and synthetic wallets. The actual UI must display the simulation mode throughout the journey. Simulator evidence always uses sandbox identity labels and must never satisfy a production-only policy. This is a test service, with process-local issuer grants; the application's attempt store still needs the real durable central adapter.

## Local verification and observed feedback

The controlled issuer uses local TLS and a transport that rejects every other host. It independently handles authorization, code uniqueness, client authentication, PKCE, JWT signing and JWKS. Test keys are generated at runtime. The simulated production-environment cases test policy only; they are not calls to production World.

The focused suite covers all three client-auth methods, nonce/state/code/PKCE replay, wrong account/order/intent/subject, explicit enrollment, wrong issuer/audience/algorithm/key, expiry and future timestamps, stale `auth_time` with a fresh `iat`, assurance mismatch, denial, duplicate parameters, two-instance concurrency, restart, cancellation during exchange, storage failure, environment separation and key rotation. Error and JSON serialization checks use synthetic private markers.

Run from `backend`, with private caches or explicitly approved caches:

```fish
env GOMAXPROCS=2 go test -p 1 ./internal/world/...
env GOMAXPROCS=2 go test -race -p 1 ./internal/world/...
env GOMAXPROCS=2 go build -p 1 ./internal/world/...
env GOMAXPROCS=2 go vet -p 1 ./internal/world/...
```

Full backend tests, database integration and actual application browser acceptance remain central integration checks. No screenshots are applicable to this backend-only leaf. No actual protected-action success or real World verification duration is reported.

Observed integration feedback: discovery exposes a production/sandbox assurance difference that startup must validate. Credential encoding needs special-character coverage, especially Basic authentication. The existing request logger would disclose callback values without route-specific handling. Fresh passkey authentication and fresh credential proof need distinct labels. These findings came from public documentation, inspected code and local simulations, not real identity enrollment.
