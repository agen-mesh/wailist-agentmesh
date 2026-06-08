# What's Left

Current state of the platform and remaining gaps.

---

## What works end-to-end ✓

| Feature | Notes |
|---------|-------|
| Algorand wallet per agent | Real Ed25519 keypair, encrypted mnemonic in DB, provisioned on deploy |
| x402 micropayments | Full flow: 402 → parse → sign → pay → retry. Agent wallet pays automatically. |
| LLM function calling loop | Gemini + OpenAI. Agent decides when/how to call tools. Up to 15 tool calls per run. Parallel tool calls supported (Gemini). |
| Multi-tool + repeat-tool | Agent can call the same tool multiple times or chain different tools in one run. |
| x402 tool discovery | Frontend "Discover" button fetches real price + params from endpoint. Params become function declarations at runtime. |
| SSE live log streaming | `EventSource` → `/runs/:runId/stream?token=JWT`. Live per-node status, duration, output. |
| Chat trigger run modal | Detects chat-trigger workflow, shows input modal before run, sends `{ message }` to backend. |
| Workflow CRUD + auto-save | Canvas changes debounced + auto-saved. Deployed wallet survives re-saves. |
| Real JWT auth | bcrypt signup/signin, HS256 JWTs, 7-day TTL. |
| GitHub + Google OAuth | Authorization-code flow, verified email required, token via URL fragment. |
| Email action node | Resend API, all fields configurable in inspector (from/to/subject/body/apiKey/provider). |
| Provider model dropdowns | All 5 providers: Gemini (5 models), OpenAI (5), Anthropic (4), Groq (4), Mistral (4). |
| Agent wallet inspector | Address with copy button, live balance, refresh, fund hint. |
| API key masking on canvas | Provider card shows first 4 chars + bullets, never the full key. |

---

## High priority (blocks common use cases)

### 1. Standard HTTP tool node
`ExecuteTool` in `backend/internal/engine/nodes/tool.go` is mostly a stub. A generic HTTP GET/POST tool that the agent can call with arbitrary URLs and headers would unlock a huge amount of use cases without needing x402.

### 2. Anthropic function calling
`callAnthropic` in `provider.go` does basic chat only. Needs the same tool-loop treatment as Gemini/OpenAI using Anthropic's `tools` API format.

### 3. Stop run wired to frontend
`Runner.Stop()` exists and cancels the context, but the canvas Stop button doesn't call `POST /workflows/:id/stop`. Wire it up.

### 4. Webhook + Schedule triggers
Trigger node types exist in the UI but the backend only handles manual/chat triggers. Webhook trigger = receive `POST /run/:workflowId` and extract payload. Schedule trigger = cron job using `robfig/cron/v3`.

---

## Medium priority

### 5. Run history UI
`GET /runs/:runId` returns logs but there's no UI to browse past runs per workflow. A simple list on the canvas page (run ID, status, timestamp, duration) with log replay would complete the observability story.

### 6. Memory nodes
Conversation memory (last N turns) and vector store (pgvector RAG) nodes exist in the palette but have no backend executor. Without memory, every run is stateless.

### 7. Spend caps
No guardrails on ALGO spend per run. Add `maxAlgoPerRun` cap on workflows that the runner checks before each x402 payment.

---

## Lower priority / future

| Item | Notes |
|------|-------|
| Agent-to-Agent (A2A) | A deployed workflow callable as an x402 tool by another agent. Real agent economy. |
| x402 marketplace | Built-in directory of x402 APIs — browse, one-click add to canvas, live price. |
| Spend dashboard | ALGO spent per workflow / per run / per tool call. Chart view. |
| On-chain run receipts | Anchor run hash on Algorand for immutable audit trail. |
| Public workflow templates | Share workflows as one-click templates (like GitHub Gist for agents). |
| Code node (JS/Python) | Execute arbitrary code inline as a tool. Needs sandboxing. |
| Slack / Discord / DB actions | Action nodes that exist in UI but have no backend executor. |
| Rate limiting | Per-IP and per-user limits on auth, run trigger, and fund endpoints before public launch. |
| Webhook delivery confirmation | Synchronous run mode — wait for completion and return final output from `/run/:workflowId`. |

---

## Known technical debt

| Location | Issue |
|----------|-------|
| `backend/internal/api/handlers/auth.go` | Email signup doesn't verify email ownership. OAuth deliberately refuses to link to a password account (`account_exists`) until a proper `identities` table is added. |
| `backend/internal/engine/nodes/tool402.go` | URL validator blocks private IPs — local x402 endpoints require a Cloudflare tunnel or similar. |
| `frontend/src/components/canvas/CanvasGraph.tsx` | No undo/redo history on the canvas. |
