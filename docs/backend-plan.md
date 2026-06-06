# AgentMesh — Backend Plan

## What We're Building

A backend server for AgentMesh that:
- Stores and executes agent workflows
- Provisions each deployed agent with its own Algorand wallet
- Spins up a dedicated API endpoint per deployed agent so it can receive calls
- Executes workflow runs triggered by HTTP, chat, schedule, or webhook
- Supports AI provider nodes (OpenAI, Anthropic, Gemini) as callable steps
- Supports tool nodes (weather, search, etc.) with real external API calls
- Supports x402 tool nodes that can pay paywalled APIs and return their response
- Returns a workflow run ID and a stable curl-able endpoint after deploy

---

## High-Level Architecture

```
frontend (Next.js)
        │
        ▼
  REST API server          ← single entry point, handles all frontend requests
        │
        ├── Workflow store  ← persists workflow graphs (nodes + edges + config)
        │
        ├── Deploy engine   ← on deploy: provisions wallets, registers agents,
        │                      creates run endpoints
        │
        ├── Run engine      ← executes a workflow graph step by step
        │   ├── Trigger resolver     ← decides what starts the run
        │   ├── Node executor        ← dispatches each node type
        │   │   ├── AI provider      ← calls LLM APIs
        │   │   ├── Tool executor    ← calls external tool APIs
        │   │   ├── x402 executor    ← pays 402-gated APIs, returns data
        │   │   └── Action executor  ← side effects (send email, post, etc.)
        │   └── Run log store        ← streams step logs back to frontend
        │
        └── Agent wallet service     ← AlgoKit/KMD: create, fund, sign per agent
```

---

## Modules / What Each Part Does

### 1. Workflow CRUD

Store, retrieve, update, and delete workflow definitions. A workflow is a graph of nodes and edges with configuration per node. The frontend sends the full graph on save; the backend persists it.

**Endpoints:**
```
GET    /workflows              → list all workflows for the user
POST   /workflows              → create a new workflow
GET    /workflows/:id          → get one workflow (graph + status)
PUT    /workflows/:id          → save/overwrite the graph
DELETE /workflows/:id          → delete workflow
```

---

### 2. Deploy Engine

When the user clicks Deploy, the backend:

1. Validates the graph (all required fields filled, no dangling edges)
2. For each Agent node in the graph — creates a new Algorand account via AlgoKit's account creation (generates a keypair, stores the mnemonic encrypted). This account is the agent's wallet.
3. Stores the wallet address alongside the agent node ID so the frontend can show the agent's address and balance.
4. Creates a stable run endpoint for the workflow:
   ```
   POST /run/:workflowId
   ```
   This endpoint is the "API" users can curl to trigger a run from outside the platform. It also becomes the webhook URL they share.
5. Returns a `deployedAt` timestamp, the per-agent wallet addresses, and the run endpoint URL.

**Endpoints:**
```
POST   /workflows/:id/deploy   → deploy; returns { runEndpoint, agents: [{id, address}] }
GET    /workflows/:id/agents/:agentId/balance   → fetch on-chain ALGO balance for agent wallet
POST   /workflows/:id/agents/:agentId/fund      → send ALGO from platform dispenser to agent wallet (testnet only)
```

> Each deployed workflow does NOT need a separate long-running server process per agent. The agents share the run engine — the wallet is just a keypair stored in the DB that gets loaded when a node executes. If a future requirement needs isolated processes, that can be added later.

---

### 3. Triggers

A workflow must have exactly one Trigger node. The trigger determines what causes a run to start.

**Supported trigger types:**

| Type | How it fires |
|------|-------------|
| Chat | `POST /run/:workflowId` with `{ message: "..." }` in the body |
| Webhook | `POST /run/:workflowId` with any JSON body |
| Schedule | Cron job stored in DB; backend fires the run on schedule |
| HTTP | `POST /run/:workflowId` — same as webhook, different label |

The run endpoint `POST /run/:workflowId` handles all trigger types — the trigger node's config tells the engine which fields to extract from the request body as the starting context.

**Endpoints:**
```
POST   /run/:workflowId        → start a run (this is the public curl-able endpoint)
GET    /runs/:runId            → get status + logs for a run
GET    /workflows/:id/runs     → list past runs for a workflow
```

---

### 4. Node Executors

The run engine walks the graph in topological order. For each node it calls the right executor.

#### AI Provider Node
- Config: provider (OpenAI / Anthropic / Gemini), model, system prompt, temperature
- Execution: calls the provider's API with the accumulated context, appends the response to the run context
- The API key for each provider is stored per-user in the DB (set in workflow settings / Inspector)

#### Tool Node — built-in tools
Each tool is a registered handler. The node config contains the tool name + parameter bindings. Execution calls the handler, returns structured data.

**Example tools to implement (with real APIs):**
- `weather` — fetch current conditions for a city (OpenWeatherMap free tier)
- `web_search` — search the web (SerpAPI or Brave Search API)
- `http_request` — generic HTTP GET/POST with configurable URL and headers
- `calculator` — evaluate a math expression (no external API)
- `datetime` — return current UTC time and date

#### x402 Tool Node
- Config: target URL (the paywalled API endpoint), expected response field to surface
- Execution flow:
  1. GET the target URL
  2. Receive 402 + `Payment-Required` header
  3. Parse the payment instructions (amount, network, recipient address)
  4. Sign and submit the payment from the agent's Algorand wallet
  5. Retry the original request with `Payment-Signature` header
  6. Return the response body (and the price paid) to the run context
- The "Get Price" button in the Inspector calls a helper endpoint that fires step 1–3 without paying, just returning the parsed price.

**Endpoint for Get Price:**
```
POST   /tools/x402/quote       → body: { url }, returns { price, network, recipient }
```

#### Action Node
Side effects at the end of a workflow:
- `send_email` — SMTP or a transactional email API
- `post_webhook` — POST to an external URL with run context as body
- `log` — write to run log only (no-op externally)

---

### 5. Run Log / Streaming

Each run produces step-by-step log entries. The frontend polls or subscribes to see live progress.

- Each step appends a log entry: `{ stepId, nodeId, nodeType, status, input, output, durationMs, ts }`
- Logs are stored per run in the DB
- Frontend polls `GET /runs/:runId` or uses SSE (server-sent events) at `GET /runs/:runId/stream`

---

### 6. Auth (mock for now)

Auth is mocked on the frontend. The backend should:
- Accept any request (no token validation yet)
- Reserve the endpoints below for when real auth is wired in
- Tag all DB records with a placeholder `userId = "dev"`

**Future endpoints (stubbed):**
```
POST   /auth/signup
POST   /auth/signin
POST   /auth/signout
GET    /auth/me
```

---

### 7. Agent Wallet Service

Wraps AlgoKit account management. Responsibilities:
- Create a new Algorand keypair on deploy, store address + encrypted mnemonic in DB
- Load the keypair on demand (for x402 payments or any on-chain action)
- Check balance via Algod REST API
- Fund from testnet dispenser (AlgoKit localnet or public testnet faucet)

No separate process per agent — the wallet is a credential stored in the DB, loaded by the run engine when that agent's node executes.

---

## Data Models (conceptual)

```
User
  id, email, passwordHash, createdAt

Workflow
  id, userId, name, status(draft|deployed|error), graph(JSON), deployedAt, runEndpoint, createdAt, updatedAt

AgentWallet
  id, workflowId, agentNodeId, address, encryptedMnemonic, network(testnet|mainnet)

ToolCredential
  id, userId, provider(openai|anthropic|gemini|openweather|...), encryptedApiKey

Run
  id, workflowId, triggeredBy(chat|webhook|schedule|manual), status(running|success|failed), startedAt, finishedAt, inputContext(JSON)

RunLog
  id, runId, stepIndex, nodeId, nodeType, status, input(JSON), output(JSON), durationMs, ts

WebhookDelivery
  id, runId, workflowId, targetUrl, payload(JSON), status(pending|success|failed), responseCode, attemptCount, lastAttemptAt

SpendCap
  id, workflowId, agentNodeId, maxAlgoPerRun, maxTokensPerRun, maxRunsPerDay
```

---

## What the Frontend Gets After Deploy

```json
{
  "workflowId": "wf_abc123",
  "status": "deployed",
  "runEndpoint": "https://api.agentmesh.io/run/wf_abc123",
  "agents": [
    { "nodeId": "agent-1", "address": "ALGO...", "network": "testnet" }
  ],
  "deployedAt": "2026-06-03T10:00:00Z"
}
```

The user can then run the workflow from the terminal:
```bash
curl -X POST https://api.agentmesh.io/run/wf_abc123 \
  -H "Content-Type: application/json" \
  -d '{ "message": "What is the weather in NYC?" }'
```

---

## Module 8 — Webhook Delivery (Outbound Events)

After a run completes (success or failure), the backend POSTs the run result to a user-configured URL. This lets users integrate AgentMesh into external systems without polling.

**Config:** Set a `notifyUrl` on the workflow. After every run finishes, the server delivers:
```json
{
  "workflowId": "wf_abc123",
  "runId": "run_xyz",
  "status": "success",
  "triggeredBy": "chat",
  "startedAt": "...",
  "finishedAt": "...",
  "output": { ... }
}
```

**Delivery behavior:**
- Fires asynchronously after run completion (does not block the run)
- Retries up to 3 times with exponential backoff if the target returns a non-2xx
- Stores delivery attempt history in `WebhookDelivery` table
- If all retries fail, marks delivery as `failed` and surfaces it in the run log

**Endpoints:**
```
PUT    /workflows/:id/notify    → set or update the notifyUrl
DELETE /workflows/:id/notify    → remove the notifyUrl
GET    /runs/:runId/delivery    → get delivery status + attempt history for a run
```

---

## Module 9 — Run Replay

Store the full input context of every run so it can be re-executed exactly as it was — same trigger payload, same starting context. Useful for debugging broken workflows without having to re-trigger from outside.

**How it works:**
- `Run.inputContext` (already in the data model) stores the full incoming payload at trigger time
- Replay creates a new Run record, copies `inputContext` from the source run, and executes it fresh
- The new run's logs are independent; the original run is untouched

**Endpoints:**
```
POST   /runs/:runId/replay      → create a new run using the same input as runId; returns new runId
```

---

## Module 10 — Rate Limiting / Spend Cap

Let users set guardrails per workflow to prevent runaway costs from LLM calls or on-chain payments.

**Two types of limits:**

| Limit | What it guards |
|-------|---------------|
| `maxAlgoPerRun` | Max ALGO the agent wallet can spend in a single run (x402 payments) |
| `maxTokensPerRun` | Max LLM tokens (across all AI provider nodes) in a single run |
| `maxRunsPerDay` | Max number of runs that can be triggered per calendar day |

**How it works:**
- Before each billable node step, the run engine checks accumulated spend against the caps
- If a cap is exceeded, the step is skipped with status `cap_exceeded`, the run is marked `aborted`, and a reason is written to the run log
- `maxRunsPerDay` is checked at trigger time before the run starts — returns HTTP 429 with a `retryAfter` field

**Endpoints:**
```
PUT    /workflows/:id/caps      → set spend caps ({ maxAlgoPerRun, maxTokensPerRun, maxRunsPerDay })
GET    /workflows/:id/caps      → get current caps
```

**Future plans:**
- Multi-agent messaging (agents in the same workflow posting tasks to each other and awaiting responses)

---

## Implementation Order

1. **Core server + DB schema** — set up the HTTP server, database, and migrations
2. **Workflow CRUD** — store and retrieve workflow graphs
3. **Deploy endpoint** — wallet creation via AlgoKit, run endpoint registration
4. **Trigger handler** — `POST /run/:workflowId` parses trigger type, starts run
5. **Run engine** — topological graph walk, node dispatch
6. **AI provider nodes** — OpenAI first, then Anthropic, Gemini
7. **Built-in tool nodes** — weather, search, http_request, calculator, datetime
8. **x402 tool node** — 402 flow + quote endpoint
9. **Action nodes** — webhook post, log
10. **Run logs + SSE streaming** — live progress feed
11. **Balance + fund endpoints** — for the agent wallet UI in Inspector
12. **Webhook delivery** — async outbound POST on run completion with retries
13. **Run replay** — re-execute any past run from its stored input context
14. **Rate limiting / spend caps** — per-run ALGO + token caps, per-day run cap
15. **Auth stubs** — placeholder endpoints, `userId = "dev"` on all records

---

## External APIs Needed

| Purpose | Service | Notes |
|---------|---------|-------|
| Algorand accounts | AlgoKit + Algod | testnet endpoint: https://testnet-api.algonode.cloud |
| Testnet faucet | AlgoKit dispenser or testnet faucet | fund agent wallets for testing |
| x402 facilitator | x402.org or Coinbase facilitator | verify + settle payments |
| Weather tool | OpenWeatherMap | free tier, 1M calls/month |
| Web search tool | Brave Search API or SerpAPI | Brave has a free tier |
| LLM: OpenAI | OpenAI API | user supplies key |
| LLM: Anthropic | Anthropic API | user supplies key |
| LLM: Gemini | Google AI API | user supplies key |
