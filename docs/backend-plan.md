# AgentMesh — Backend Architecture

The backend is a Go HTTP server. This document describes what is built and how it works.

---

## Stack

| Layer | Technology |
|-------|-----------|
| HTTP server | `go-chi/chi` v5 |
| Database | PostgreSQL via `pgx/v5` |
| Migrations | `golang-migrate` |
| Algorand | `go-algorand-sdk/v2` |
| Auth | bcrypt + HS256 JWT |

---

## Module Overview

### Workflow CRUD
`GET/POST/PUT/DELETE /workflows` — store and retrieve workflow graphs as JSONB. Each workflow includes `nodes` and `edges`. The frontend sends the full graph on every save.

### Deploy Engine
`POST /workflows/:id/deploy`

For each `agent` node in the graph:
1. Generate a new Algorand Ed25519 keypair via the SDK
2. Encrypt the mnemonic with AES-GCM using `ENCRYPTION_KEY`
3. Store `(workflowId, agentNodeId, address, encryptedMnemonic)` in `agent_wallets`
4. Write the wallet address back into the node's `wallet` field in the graph

Wallets survive re-deploys — if a wallet already exists for an agent node ID it is reused, not regenerated.

### Run Engine
`POST /workflows/:id/run`

1. Topological sort of the workflow graph (`engine/graph.go`)
2. Build `attachMap` — maps each agent node to its attached provider and tools
3. Build `agentToolIDs` — set of tool/tool402 node IDs connected only via `attach → tools` edges. These are **skipped** in the main execution loop.
4. Execute levels in order (nodes in the same level run in parallel goroutines)
5. Each node result is stored in `RunContext` and streamed via SSE

### Node Executors (`engine/nodes/`)

| Node type | Executor | Status |
|-----------|----------|--------|
| trigger | Returns raw input from request body | ✓ |
| agent | `ExecuteAgent` — LLM function calling loop | ✓ |
| provider | Resolved via agent's `attachMap` | ✓ |
| tool | `ExecuteTool` — HTTP request | partial |
| tool402 | `ExecuteTool402` — x402 payment flow | ✓ |
| action | `ExecuteAction` — email (Resend), webhook | ✓ |
| end | Returns last message from RunContext | ✓ |

### LLM Function Calling (the core agentic loop)

`engine/nodes/provider.go` — `callGemini` and `callOpenAICompat`

```
loop (max 15 iterations):
  1. Call LLM with functionDeclarations built from attach.Tools[].DiscoveredParams
  2. If response has functionCall parts:
       - Execute each called tool (may be multiple in one Gemini turn)
       - Append results to conversation
       - Continue loop
  3. If response has text → return it
```

Function declarations are built from `WorkflowNode.DiscoveredParams` (populated when the user clicks "Discover" in the x402 inspector). Parameter names, types, required flags, and descriptions become the function schema the LLM uses to decide how to call the tool.

When a function is called, the LLM's chosen argument values are appended as query params to the tool's endpoint URL before the request is made.

### x402 Payment Flow

`engine/nodes/tool402.go`

```
1. GET endpoint
2. Response is 402 → parse X-Payment-Required header
   { price, unit, network, recipient, params }
3. Build Algorand payment transaction:
   - From: agent wallet address
   - To: recipient address
   - Amount: price in microALGO
4. Sign with agent's decrypted mnemonic
5. Submit to algod (testnet)
6. Retry original GET with X-Payment: <base64-signed-txn> header
7. Return response body
```

### SSE Streaming

`GET /runs/:runId/stream` — server-sent events, one `event: log` per node step, `event: done` when the run finishes.

EventSource cannot set custom headers, so the JWT is accepted as `?token=` query param (middleware falls back to this if `Authorization` header is absent).

### Auth

- `POST /auth/signup` / `/auth/signin` — bcrypt hashing, HS256 JWT (7-day TTL)
- `GET /auth/oauth/{provider}` + `GET /auth/oauth/{provider}/callback` — GitHub and Google, authorization-code flow, verified email required, single-use CSRF state cookie, token delivered via URL fragment
- All protected routes require `Authorization: Bearer <token>`

---

## Data Models

```
users           — id, email, password_hash, created_at
workflows       — id, user_id, name, status, graph(JSONB), deployed_at, run_endpoint
agent_wallets   — id, workflow_id, agent_node_id, address, encrypted_mnemonic, network
runs            — id, workflow_id, triggered_by, status, started_at, finished_at, input_context
run_logs        — id, run_id, step_index, node_id, node_type, status, input, output, duration_ms, ts
waitlist        — id, email, created_at
```

Key model: `WorkflowNode` includes `DiscoveredParams []ParamDef` — these are saved by the frontend after the user clicks "Discover" on an x402 node and are used at runtime to build LLM function declarations.

---

## API Routes

### Public

| Method | Route | Description |
|--------|-------|-------------|
| GET | `/health` | Health check |
| POST | `/auth/signup` | Create account → `{ token }` |
| POST | `/auth/signin` | Sign in → `{ token }` |
| GET | `/auth/oauth/{provider}` | Start OAuth |
| GET | `/auth/oauth/{provider}/callback` | OAuth callback |
| POST | `/waitlist` | Join waitlist |
| POST | `/run/{workflowId}` | Public webhook trigger (curl-able) |

### Protected (Bearer JWT)

| Method | Route | Description |
|--------|-------|-------------|
| GET | `/workflows` | List workflows |
| POST | `/workflows` | Create workflow |
| GET | `/workflows/{id}` | Get workflow |
| PUT | `/workflows/{id}` | Update workflow |
| DELETE | `/workflows/{id}` | Delete workflow |
| POST | `/workflows/{id}/deploy` | Provision agent wallets |
| GET | `/workflows/{id}/agents/{agentId}/balance` | Agent wallet balance |
| POST | `/workflows/{id}/run` | Trigger a run |
| POST | `/workflows/{id}/stop` | Cancel active run |
| GET | `/runs/{runId}` | Get run + logs |
| GET | `/runs/{runId}/stream` | SSE log stream (`?token=JWT`) |
| POST | `/tools/x402/quote` | Quote an x402 endpoint without paying |
