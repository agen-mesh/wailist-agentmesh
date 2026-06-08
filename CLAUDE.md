# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

@AGENTS.md

## Commands

```bash
# Frontend
cd frontend && npm run dev      # dev server (localhost:3000)
cd frontend && npm run build    # production build
cd frontend && npm run lint     # eslint

# Backend (Docker — preferred for local dev)
docker-compose up -d            # starts postgres + backend together
docker-compose up -d --build backend   # rebuild after Go changes

# Backend (direct)
cd backend && go run ./cmd/server   # Go server (localhost:8080)
cd backend && go build ./...        # build
cd backend && go test ./...         # tests
```

## Architecture

**AgentMesh** — no-code platform for building autonomous agent workflows with Algorand wallets and x402 micropayments.

```
frontend/   — Next.js 16 app (React 19, Tailwind 4, TypeScript)
backend/    — Go HTTP server (chi, pgx/v5, Algorand SDK v2)
docs/       — specs and whitepaper
test-x402/  — local x402 test server (weather endpoint, algosdk payment verification)
```

### Frontend (`frontend/`)

App Router. Routes:

| Route | Auth |
|-------|------|
| `/` | public — landing page |
| `/signin`, `/signup` | public |
| `/auth/callback` | public — OAuth token landing |
| `/workflows` | protected |
| `/workflows/[id]` | protected — canvas editor |

**Auth** is real. `useAuth` hook (`frontend/src/hooks/useAuth.ts`) reads `agentmesh_token` from `localStorage`. Token is a real HS256 JWT issued by the backend. OAuth delivers the token via URL fragment to `/auth/callback`.

**API layer** (`frontend/src/lib/api.ts`): all calls go to `NEXT_PUBLIC_API_URL`. No mock fallbacks remain — set that env var or calls will 404.

**Canvas** (`frontend/src/components/canvas/`): custom SVG pan/zoom — no ReactFlow. `CanvasPage.tsx` owns all state. `CanvasGraph.tsx` handles the SVG surface, drag/drop, and port-to-port wiring. All node types in `canvas/nodes/index.tsx`.

**Port logic** (`frontend/src/lib/portUtils.ts`): `portWorld()` computes canvas-space coordinates for a port, `isValidConnection()` enforces wiring rules.

**Types** (`frontend/src/lib/types.ts`): `WorkflowNode`, `WorkflowEdge`, `Workflow`, `NodeType`, `PortName`.

**Node templates + sample data** (`frontend/src/lib/data.ts`).

### Design system

Dark violet theme. CSS vars defined in `frontend/src/app/globals.css`:

- `--bg: #08070C` → `--bg-elev-3: #1F1D34`
- `--accent: #A78BFA`, `--accent-strong: #8B5CF6`
- Magenta `#E879F9` for x402 nodes, warm `#FFB547` for wallet/balance

Fonts: Geist Sans + Geist Mono via `next/font/google`, injected as CSS vars.

### Backend (`backend/`)

Go HTTP server (chi/v5, pgx/v5, golang-migrate, go-algorand-sdk/v2).

**What is implemented and working:**
- Workflow CRUD (`GET/POST/PUT/DELETE /workflows`)
- Deploy engine — provisions real Algorand Ed25519 keypairs per agent node, stores encrypted mnemonic
- Run engine — topological graph walk, parallel level execution, goroutine-per-node
- **LLM function calling agentic loop** — Gemini and OpenAI run a full tool-call loop (up to 15 iterations); the agent LLM decides when and with what params to call tools. Anthropic is basic chat only.
- x402 tool executor — full 402 → parse → sign → pay → retry flow using the agent's Algorand wallet
- Tool402 nodes attached to agents are skipped from standalone execution; the agent drives them via function calling
- Email action node (Resend API, configurable fields per node)
- SSE streaming — `GET /runs/:runId/stream` streams `event: log` and `event: done`
- JWT middleware — `?token=` query param fallback for EventSource (which can't set headers)
- Real HS256 JWT auth (bcrypt signup/signin)
- GitHub + Google OAuth (authorization-code flow, verified email required)

**Key files:**
- `backend/internal/engine/runner.go` — topological executor, builds `agentToolIDs` to skip attached tools
- `backend/internal/engine/nodes/provider.go` — LLM callers + agentic function calling loop
- `backend/internal/engine/nodes/tool402.go` — x402 payment flow
- `backend/internal/engine/nodes/action.go` — email + webhook actions
- `backend/internal/models/types.go` — `WorkflowNode`, `ParamDef`, all data models
- `backend/internal/api/middleware.go` — JWT auth + `?token=` SSE fallback

**x402 flow in detail:**
1. Runner skips tool402 nodes that are only connected via `attach → tools` edges
2. Agent's LLM call includes `functionDeclarations` built from `node.DiscoveredParams`
3. When Gemini returns a `functionCall` part → `executeFunctionCall` appends args as query params to the endpoint URL → calls `ExecuteTool402`
4. `ExecuteTool402`: GET endpoint → 402 → parse `X-Payment-Required` header → sign Algorand payment from agent wallet → retry with payment header → return data
5. Tool result fed back into conversation → LLM synthesises final answer

**What is NOT yet implemented:**
- Standard HTTP tool node (`ExecuteTool` is a stub)
- Anthropic function calling (tool loop)
- Webhook / schedule triggers
- Memory and vector store nodes
- Stop run (backend `Stop()` exists but frontend doesn't call it)
- Run history browsing UI

## Key decisions

- Canvas is custom SVG — no third-party canvas lib. Matches design exactly, avoids ReactFlow overhead.
- `NEXT_PUBLIC_API_URL` env var points frontend at backend. Must be set — no mock fallback.
- Tool402 nodes are NOT executed by the runner standalone. The agent LLM decides when to call them via function calling. This is the correct agentic architecture.
- `DiscoveredParams` on a tool402 node (populated by the frontend "Discover" button) become Gemini/OpenAI `functionDeclarations` parameters at runtime.
