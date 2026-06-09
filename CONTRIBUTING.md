# Contributing to AgentMesh

Thanks for your interest in contributing. There are a few different ways to get involved depending on what you want to work on.

---

## Ways to contribute

### 1. Build x402-compatible API endpoints

The most impactful contribution is publishing a paid API that AgentMesh agents can use autonomously. Any HTTP endpoint that implements the x402 payment protocol works as a tool node in AgentMesh.

See [`x402-examples/CONTRIBUTING.md`](./x402-examples/CONTRIBUTING.md) for the full protocol spec and a step-by-step guide to building a compatible endpoint.

### 2. Contribute to the core platform

AgentMesh is split into:

- **`frontend/`** — Next.js 16, React 19, Tailwind 4, TypeScript. Custom SVG canvas, no third-party canvas lib.
- **`backend/`** — Go HTTP server (chi, pgx/v5, Algorand SDK v2). Handles workflow execution, LLM calls, Algorand wallets, x402 payments.

**Before opening a PR:**
- Open an issue first for anything non-trivial so we can align on approach
- Match the existing code style — no linting rule changes, no reformatting unrelated files
- Keep PRs focused: one thing per PR

**Running locally:**

```bash
# Backend
cd backend && go run ./cmd/server   # starts on :8080

# Frontend
cd frontend && npm install && npm run dev   # starts on :3000
```

Set `NEXT_PUBLIC_API_URL=http://localhost:8080` in `frontend/.env.local` to connect the two.

Full env var reference is in the backend's `.env.example`.

### 3. Report bugs and suggest features

Open a [GitHub Issue](../../issues). Include:
- What you expected to happen
- What actually happened
- Steps to reproduce (for bugs)

---

## Code of conduct

Be direct, be kind, keep feedback about the code not the person.
