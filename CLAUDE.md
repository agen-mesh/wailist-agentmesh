# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

@AGENTS.md

## Commands

```bash
# Frontend
cd frontend && npm run dev      # dev server (localhost:3000)
cd frontend && npm run build    # production build
cd frontend && npm run lint     # eslint

# Backend
cd backend && go run ./cmd/server   # Go server (localhost:8080)
cd backend && go build ./...        # build
cd backend && go test ./...         # tests
```

## Architecture

**AgentMesh** — no-code platform for building autonomous agent workflows with Algorand wallets and x402 micropayments.

```
frontend/   — Next.js 16 app (React 19, Tailwind 4, TypeScript)
backend/    — Go HTTP server (chi, pgx/v5, Algorand SDK)
docs/       — specs, plans, whitepaper
```

### Frontend (`frontend/`)

App Router. Routes:

| Route | Auth |
|-------|------|
| `/` | public — landing page |
| `/signin`, `/signup` | public |
| `/workflows` | protected |
| `/workflows/[id]` | protected — canvas editor |

**Auth** is mocked: `useAuth` hook (in `frontend/src/hooks/useAuth.ts`) stores a flag in `localStorage` under `agentmesh_signed_in`. Token stored under `agentmesh_token`. Swap for real JWT when backend is ready.

**API layer** (`frontend/src/lib/api.ts`): all backend calls are stubbed here. When `NEXT_PUBLIC_API_URL` is set, calls go to the Go server; otherwise returns mock data. Every stub has a `// TODO: API` comment.

**Canvas** (`frontend/src/components/canvas/`): custom SVG pan/zoom — no ReactFlow. `CanvasPage.tsx` owns all state. `CanvasGraph.tsx` handles the SVG surface, drag/drop (HTML5 native), and port-to-port wiring. Seven node types live in `canvas/nodes/`.

**Port logic** (`frontend/src/lib/portUtils.ts`): `portWorld()` computes canvas-space coordinates for a port, `isValidConnection()` enforces wiring rules.

**Types** (`frontend/src/lib/types.ts`): `WorkflowNode`, `WorkflowEdge`, `Workflow`, `NodeType`, `PortName`.

**Mock data** (`frontend/src/lib/data.ts`): node templates, sample workflow, mock workflow list.

### Design system

Dark violet theme. CSS vars defined in `frontend/src/app/globals.css`:

- `--bg: #08070C` → `--bg-elev-3: #1F1D34`
- `--accent: #A78BFA`, `--accent-strong: #8B5CF6`
- Magenta `#E879F9` for x402 nodes, warm `#FFB547` for wallet/balance

Fonts: Geist Sans + Geist Mono via `next/font/google`, injected as CSS vars.

### Backend (`backend/`)

Go HTTP server (chi/v5, pgx/v5, golang-migrate, go-algorand-sdk/v2). See `docs/backend-plan.md` for full spec.
- Workflow CRUD
- Deploy engine (provisions Algorand wallets per agent node)
- Run engine (topological graph walk, goroutine-per-node dispatch)
- Node executors: AI provider (OpenAI/Anthropic/Gemini), built-in tools, x402 tool, action nodes
- SSE streaming for live run logs

Run endpoint after deploy: `POST /run/:workflowId` — curl-able from outside.

## Key decisions

- Canvas is custom SVG — no third-party canvas lib. Matches design exactly and avoids ReactFlow overhead.
- `NEXT_PUBLIC_API_URL` env var switches frontend between mock and real backend.
- Auth stub (`localStorage`) is intentionally thin — `useAuth.ts` is the only place to update when real auth ships.
