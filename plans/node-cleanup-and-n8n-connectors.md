# Plan — Remove dead nodes, then build n8n-class connectors

Status: proposed, nothing implemented yet.
Branch base: `master` @ `0facc96`.

Two independent bodies of work:

- **Part A — Deletion.** Remove palette entries that render a node which cannot ever do
  anything. Small, safe, do it first and merge it on its own.
- **Part B — Real connectors.** Make Gmail/Telegram-bot/Sheets/Calendar-class nodes possible.
  This is *not* "add more entries to the templates array" — five architectural gaps block it,
  listed in B0. Read B0 before estimating anything.

---

## Part A — Remove the nodes that don't work

Every item below was verified against the backend dispatch, not assumed from naming.

### A1. All six x402 palette templates (`TOOL402_TEMPLATES`) — `frontend/src/lib/data.ts:131-180`

`tavily`, `firecrawl`, `alpaca`, `ocr`, `flux`, `weather`.

Two independent reasons these are non-functional:

1. The palette's `map` for the x402 tab (`PalettePanel.tsx:82-88`) sets only
   `type`/`template`/`name`/`icon`/`sub`. It **never sets `endpoint`**. A node dragged from
   this tab has an empty endpoint and can never resolve, price, or call anything.
2. The advertised providers — `tavily.x402`, `firecrawl.x402`, `alpaca.x402`, `ocr.x402`,
   `flux.x402`, `weatherkit.x402` — are invented hostnames. No such hosts exist. Even with the
   endpoint wired, there is nothing on the other end.

The working path for x402 already exists and stays: the **"New x402 Endpoint" custom creator**
(paste URL → Discover → probes the real 402 challenge for method and price). That is the
`custom: true` flow, untouched by this deletion.

Changes:

- Delete `TOOL402_TEMPLATES` from `frontend/src/lib/data.ts`.
- `PalettePanel.tsx`: keep the `x402` tab, drop its `items()`/`map` template list, leave the
  tab rendering only the "New x402 Endpoint" creator card from `CREATE_META`. Remove the now
  unused `TOOL402_TEMPLATES` import.
- Verify the x402 tab still renders with an empty template list (the tab body must not assume
  ≥1 item).

**Not affected:** `SAMPLE_WORKFLOW`'s `n4` "x402 Weather" node is `custom: true` with a real
`http://localhost:4402/weather` endpoint and hand-written `discoveredParams` — it does not
reference the `weather` template. The sample keeps working.

### A2. Dead tool templates — `data.ts:113-129`

`ExecuteTool` (`backend/internal/engine/nodes/tool.go:117-128`) handles exactly three
templates: `calc`, `datetime`, `http`. Everything else hits
`default: return rc.Message(), nil` — it **echoes its input back and reports success**, which
is worse than failing.

Remove from `TOOL_TEMPLATES`:

- `code` ("Run JS/Python inline") — no sandbox, no runtime, nothing. Note: real code execution
  is genuinely available today via the **Tendril** tab's "Run a Job" node (`/x402/run`,
  Python, metered). If a "Code" node is wanted back later, it should route there rather than
  re-introduce a stub.
- `vector` ("Pinecone / pgvector") — no vector store integration exists anywhere in the repo.
- `memory` ("Conversation Memory") — no conversation store. `RunContext` is per-run and
  in-process; there is no cross-run memory to read.

**Add** while here: `datetime` is fully implemented in the backend but has no palette entry —
add `{ id: "datetime", name: "Current Time", desc: "UTC timestamp", icon: "◔" }`.

### A3. Dead action template — `data.ts:185`

- `db` ("Database Insert — Postgres / Neon"). No `case "db"` in `ExecuteAction`
  (`action.go:16-67`); falls through to `default: return "logged", nil`. Renders a green
  successful node having written to nothing. Remove it.

All 23 other `ACTION_TEMPLATES` entries map to a real implemented `case` — verified one by one
against `action.go`. Keep them.

### A4. Dead trigger — `data.ts:28`

- `cron` ("Schedule — Cron / interval"). There is **no scheduler in the backend at all**
  (`grep -ri "cron\|schedul" backend/internal` → zero non-test hits). A workflow whose only
  trigger is Schedule will never fire on its own; it can only be run by hand. Remove it from
  the palette.

Keep `manual`, `chat`, `webhook`. All three are no-ops in the graph
(`runner.go:704 → return rc.input, nil`) but that is correct — the distinction between them is
*how the run gets started* through the API, not what the node does.

Re-adding Schedule is a real feature, specced as **B5** below.

### A5. Marketing / mock references to the removed providers

- `frontend/src/components/landing/LandingPage.tsx:224` — the logo strip lists "Tavily" and
  "Firecrawl" as if integrated. Replace with providers actually reachable today (Tendril, plus
  whatever real x402 endpoints are in use) or drop the two chips.
- `frontend/src/lib/data.ts:133-135, 480-482` — mock endpoint-usage and settlement rows keyed
  to `tavily.x402`. These feed placeholder dashboard states. Swap to a real provider or delete
  the rows.

### A7. Connectors-area browse/search UX (implemented ahead of the rest of Part A)

The Actions tab is the one palette tab that actually functions as a connector library today
(23 real, working integrations after A3's single `db` removal) — the request was specifically
to make *that* area browsable/searchable "kind of like n8n," scoped to it alone. Implemented
directly in `PalettePanel.tsx` / `data.ts`, additive and gated behind `tab === "actions"` so
every other tab (Triggers, Agents, Providers, Tools, x402, Tendril, End) is untouched:

- Each `ACTION_TEMPLATES` entry gets a `category`, matching the backend's own
  `connectors_{messaging,productivity,devtools,data,media}.go` split (plus `Email`, which lives
  directly in `action.go` rather than a connectors file, kept as its own bucket rather than
  folded into Messaging since it has a materially different config shape — provider dropdown,
  from/subject/body — not just a webhook URL).
- Browsing (empty search): items grouped under mono uppercase category headers, in a fixed
  category order, instead of one flat 23-item list.
- Searching: flat, ranked list (name-starts-with > name-contains > sub/category-contains) with
  the matched substring highlighted using the app's own `--accent`/`--accent-soft` tokens — no
  new color introduced. A result-count line ("6 of 23" / "23 connectors") sits under the search
  box, Actions tab only.
- Every other tab keeps the original unranked `.includes()` filter and flat list, byte-for-byte.

### A6. Verification for Part A

```
cd frontend && pnpm tsc --noEmit && pnpm lint && pnpm build
cd backend  && go build ./... && go test ./...
```

Backend should be untouched by Part A — if `go test` output changes, something was removed
that was load-bearing. Then manually: open the canvas, click through **every** palette tab,
confirm each renders and the x402 tab still offers the custom creator.

---

## Part B — n8n-class connectors

### B0. Why this is not just "add more templates"

Five gaps between what exists and what a Gmail/Sheets/Telegram-bot node needs. Each must be
closed before *any* fetch-capable connector can work; they are shared infrastructure, not
per-connector work.

**Gap 1 — every connector is write-only.**
`doAndCheck` (`connector_helpers.go:91-102`) does `io.Copy(io.Discard, resp.Body)` and returns
a **sentinel string** (`"slack_sent"`, `"telegram_sent"`). No connector can return data. There
is currently no way for any node to *fetch* anything. Reading Gmail is impossible without a
new response-decoding helper.

**Gap 2 — connectors have no parameter schema, so an agent can't drive them.**
`buildFuncDecls` (`provider.go:155-216`) builds the LLM tool schema **only** from
`DiscoveredParams` and `CustomParams` — both tool402-specific fields. Any `action` or `tool`
node attached to an agent's `tools` port is handed to the model as a **zero-argument
function**. "Agent, search my inbox for X" cannot work: there is no `query` argument to pass.

**Gap 3 — one hardcoded operation per connector.**
`sendTelegram` only sends a message. n8n models connectors as `resource` + `operation` (Gmail:
`message:get|list|send|reply`, `draft:create`, `label:add`). Supporting reads means dispatching
on an operation *inside* each connector, not one Go function per connector.

**Gap 4 — no OAuth2. This is the big one.**
Every credential today is a static secret pasted into a text field and stored in the node's
`secrets` map. Gmail, Google Sheets, Google Calendar, Google Drive, Slack (beyond incoming
webhooks), Notion, and HubSpot all require **3-legged OAuth2 with refresh tokens**. There is no
consent redirect, no callback handler, no token table, no refresh-on-expiry, and no
credential-reuse model (n8n stores a credential once and references it from many nodes;
AgentMesh stores secrets per-node, so the same token would be pasted N times and expire N
times). **Gmail is not reachable without building this.** It is the single largest item in
Part B and should be its own milestone.

**Gap 5 — the inter-node payload is one string, and picking it is non-deterministic.**
`rc.Message()` (`context.go:60-71`) returns "the most recent output" by iterating
`rc.outputs`, **a Go map** — whose iteration order is randomized. With today's mostly-linear
graphs and sentinel returns this rarely bites. A fetch node returning a 50-message array makes
it both load-bearing and wrong. Fix ordering as a prerequisite: track insertion order (slice of
node IDs, or store a sequence number) so "most recent" is real. Separately, structured results
need a way to reach the next node without being flattened through `anyToString`'s
`json.Marshal` fallback.

> This is a real pre-existing bug independent of this plan and worth fixing on its own.

### B1. Milestone 1 — connector framework (no new integrations yet)

Backend:

- `connector_helpers.go`: add `getJSON`/`doAndDecode` — same SSRF guard and bounded read as
  `doAndCheck`, but decode the body into `any` and return it instead of discarding. Keep
  `readBounded`'s explicit over-limit error; do not silently truncate.
- Introduce a connector descriptor registered per service:
  ```go
  type ConnectorOp struct {
      Key, Label, Description string
      Params []ConnectorParam   // name, type, required, description
      Run    func(ctx, node, args map[string]any, rc) (any, error)
  }
  ```
  with a `map[string]Connector` registry replacing the flat `switch` in `ExecuteAction`. Keep
  the existing 23 connectors working by registering each as a single `send` op — no behavior
  change, so `connectors_*_test.go` should pass untouched.
- Thread the descriptor into `buildFuncDecls` so an attached connector node exposes its op's
  `Params` as a real function schema (closes Gap 2).
- Standalone (non-attached) connector nodes read their args from `config`, as today.

Frontend:

- Derive `CONNECTOR_CONFIG_FIELDS` (`Inspector.tsx:1694+`) from a shared descriptor rather than
  a hand-maintained parallel literal — it already duplicates the backend's field keys by hand
  and will drift badly once each connector has several operations. Simplest version: a
  generated JSON emitted by the backend and imported by the frontend, or a single
  hand-maintained shared file with a Go test asserting parity.
- Add an **Operation** dropdown to the Inspector for connectors declaring >1 op, with the
  parameter fields below it switching on the selection.

Ship Milestone 1 with zero new integrations. It is verifiable on its own: all existing
connectors still work, and an attached Telegram node now shows the model a real `text`
argument.

### B2. Milestone 2 — OAuth2 subsystem

Blocks Gmail, Sheets, Calendar, Drive, and proper Slack/Notion/HubSpot.

- Migration: `oauth_credentials` table — `id, user_id, provider, access_token(enc),
  refresh_token(enc), expires_at, scopes, account_label, created_at`. Encrypt at rest with the
  existing `ENCRYPTION_KEY` mechanism used for wallets.
- Handlers: `GET /api/oauth/:provider/start` (PKCE + signed `state` bound to the user),
  `GET /api/oauth/:provider/callback`, `GET /api/oauth/credentials`, `DELETE /…/:id`.
- Refresh: a `TokenSource`-style wrapper refreshing on expiry and persisting the rotated
  refresh token, with a single-flight lock so concurrent nodes in one run don't race two
  refreshes.
- Node model: nodes reference `oauthCredentialID` instead of pasting a secret; the same
  credential is reusable across nodes and workflows.
- Inspector: a "Connect account" button + saved-credential picker, replacing the token field
  for OAuth connectors.
- Register the redirect URI on the Google Cloud project; verification is required for
  restricted Gmail scopes — **start this early, approval is not instant.** Prefer
  `gmail.readonly` + `gmail.send` over full `mail.google.com` to reduce the review burden.

### B3. Milestone 3 — the connectors themselves

Ordered by (value ÷ cost). Each is small *given* B1 and B2.

Tier 1 — no OAuth, high value, unblocked by B1 alone:

- **Telegram Bot (read + more ops).** Already has a working send path and a bot token. Add
  `getUpdates`, `sendPhoto`/`sendDocument`, `answerCallbackQuery`. Cheapest real win here.
- **HTTP Request (proper).** Today's `http` tool has no headers, no auth, no body template, no
  response parsing. Upgrade it to n8n's generic HTTP node — this alone covers a long tail of
  "can it talk to X?" without a bespoke connector.
- **Slack read ops** (`conversations.history`, `chat.postMessage` via bot token rather than
  incoming webhook), **GitHub read** (list issues/PRs/files), **Notion read** (query database,
  read page).
- **Airtable / Supabase read** — both already have write connectors and static API keys.

Tier 2 — needs B2 (OAuth):

- **Gmail** — `message:list|get|send|reply`, `draft:create`, `label:add|remove`. The
  explicitly requested one. Note Gmail returns bodies base64url-encoded inside a nested MIME
  part tree; extracting `text/plain` needs a real walker, not a field read.
- **Google Sheets** — `row:append|read|update`, `sheet:read`. Highest-value non-mail Google
  node in practice.
- **Google Calendar** — `event:list|create|delete`.
- **Google Drive** — `file:list|download|upload`.

Tier 3 — after the above land:

- Outlook / Microsoft Graph, Dropbox, Stripe, Shopify, Twilio, OpenAI-compatible generic nodes.

### B4. Milestone 4 — data shape between nodes

n8n passes an **array of items** and runs downstream nodes once per item. AgentMesh passes one
value. Fetch nodes make the difference concrete: "read 20 emails → send 20 Telegram messages"
is a fan-out the engine cannot currently express.

Decide explicitly (this is a design fork, not a detail):

- **(a) Keep single-value**, and let list-returning ops emit a JSON array that downstream nodes
  handle themselves (agent reads it as text, connectors index into it). Cheap; no engine
  change; fan-out is impossible.
- **(b) Add item-array semantics** with per-item downstream execution. Matches n8n, unlocks the
  real use cases, and touches the runner, the SSE log format, billing (N calls, not one), and
  the canvas. Substantial.

Recommend **(a) for the first release**, with `rc.Set` already storing structured values so (b)
stays open. Do not half-build (b).

Whichever way: fix the `Message()` map-iteration ordering bug (Gap 5) first — it is a
prerequisite, not optional.

### B5. Milestone 5 — triggers (re-adds what A4 removes)

- **Schedule/cron trigger.** Needs a scheduler process: a `scheduled_workflows` table, a leader
  or advisory-lock-guarded ticker, and a run-enqueue path. Must be safe under multiple backend
  replicas — a naive in-process `time.Ticker` double-fires on every deploy with >1 instance.
- **Polling triggers** ("on new email", "on new row"): the scheduler plus per-trigger cursor
  state (last seen message ID / row / timestamp) persisted per workflow, with dedup on restart.
- Only after the scheduler exists should `cron` return to the palette.

### B6. Testing and verification

- Every connector op gets an `httptest`-based test, following the existing
  `SetTelegramAPIBaseForTest` base-URL-override pattern (`connectors_messaging.go:71-83`) —
  extend it to each new service rather than inventing a second mechanism.
- OAuth: test refresh-on-expiry, refresh-token rotation, and concurrent-refresh single-flight.
- Any new outbound host must go through `urlValidator` — the SSRF guard is not optional, and
  user-supplied endpoints (HTTP node, self-hosted Supabase) make it load-bearing.
- Frontend: `pnpm tsc --noEmit && pnpm lint && pnpm build` plus a real click-through of the
  OAuth connect flow in a browser.

---

## Sequencing

1. **Part A** — self-contained, low risk, merge alone.
2. **Gap 5 fix** (`Message()` ordering) — small, independent, unblocks B.
3. **B1** connector framework — no new integrations, all existing tests must stay green.
4. **B3 Tier 1** — Telegram read + real HTTP node. First user-visible new capability.
5. **B2** OAuth2 — start the Google verification paperwork at the same time as step 3, since
   approval latency is external.
6. **B3 Tier 2** — Gmail, then Sheets.
7. **B4** decision, **B5** scheduler.

## Open questions

- **B4 (a) or (b)?** Single-value vs. item arrays. Affects everything downstream; decide before
  B3 Tier 2, since Gmail's `message:list` is exactly the case that hurts.
- Are per-run **billing** semantics settled for connector calls? Today an action node is free;
  a Gmail read that fans out to 50 API calls may not want to be.
- Should OAuth credentials be **per-user or per-workspace**? Affects the table's ownership
  column and is painful to change later.
