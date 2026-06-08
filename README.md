# AgentMesh

No-code platform for building autonomous agent workflows with Algorand wallets and x402 micropayments.

## Structure

```
frontend/   — Next.js 16 app (React 19, Tailwind 4, TypeScript)
backend/    — Go HTTP server (chi, pgx/v5, Algorand SDK v2)
docs/       — specs, plans, whitepaper
```

## Live deployment

| Service | URL | Host |
|---------|-----|------|
| Frontend | https://wailist-agentmesh-zeta.vercel.app | Vercel |
| Backend | https://wailist-agentmesh-production.up.railway.app | Railway |
| Database | Supabase Postgres (transaction pooler) | Supabase |

## Prerequisites

- Node.js 20+
- Go 1.25+
- PostgreSQL (local) or a Supabase/Railway database

---

## Authentication

Auth is real. The backend issues HS256 JWTs (7-day TTL) for three flows:

- **Email + password** — `bcrypt` hashing on signup, constant-time verification on signin.
- **GitHub OAuth** — authorization-code flow, requires a primary verified email.
- **Google OAuth** — authorization-code flow, requires `verified_email == true`.

All three converge on the **same JWT**, so every protected route works identically regardless of how the user signed in. The token is stored client-side under `localStorage["agentmesh_token"]` and sent as `Authorization: Bearer <token>`.

### How OAuth works

1. User clicks "Continue with GitHub/Google" → browser navigates to `GET /auth/oauth/{provider}`.
2. Backend sets a per-provider, single-use CSRF `state` cookie and redirects to the provider.
3. Provider redirects back to `GET /auth/oauth/{provider}/callback`.
4. Backend verifies state, exchanges the code, fetches a **verified** email, upserts the user, issues a JWT.
5. Backend redirects to `{FRONTEND_URL}/auth/callback#token=<jwt>` — the token rides in the **URL fragment** (never sent to a server, kept out of logs and `Referer`).
6. The callback page stores the token, scrubs it from history, and routes to `/workflows`.

### Security model

- OAuth never silently links to an existing **password** account (our signup does not verify email ownership, so auto-linking would enable account pre-hijacking). OAuth→OAuth linking by verified email is allowed; OAuth→password collision returns `?error=account_exists`.
- JWTs are refused if `JWT_SECRET` is shorter than 32 bytes.
- DB errors are logged server-side and masked to the client.
- `state` cookies are `HttpOnly`, `Secure`, `SameSite=Lax`, provider-scoped, and deleted after one use.

---

## Local development

### 1. Backend

```bash
cp backend/.env.example backend/.env
```

Minimum local config in `backend/.env`:

```bash
DATABASE_URL=postgres://postgres:password@localhost:5432/agentmesh?sslmode=disable
JWT_SECRET=$(openssl rand -hex 32)
ENCRYPTION_KEY=abcdefghijklmnopqrstuvwxyz123456   # 32 chars
PORT=8080
BASE_URL=http://localhost:8080
FRONTEND_URL=http://localhost:3000
CORS_ORIGIN=http://localhost:3000

# optional — needed for AI nodes
GEMINI_API_KEY=
OPENAI_API_KEY=
ANTHROPIC_API_KEY=

# optional — needed for social login locally
GITHUB_CLIENT_ID=
GITHUB_CLIENT_SECRET=
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
```

Create the database, then start the server:

```bash
createdb agentmesh
cd backend && go run ./cmd/server
# → AgentMesh backend listening on :8080
```

Migrations run automatically on startup.

Verify:
```bash
curl http://localhost:8080/health   # → ok
```

### 2. Frontend

```bash
echo "NEXT_PUBLIC_API_URL=http://localhost:8080" > frontend/.env.local
cd frontend && npm install && npm run dev
# → localhost:3000
```

`NEXT_PUBLIC_API_URL` is the single switch between mock data and the real backend. Unset = mock mode. It is inlined at **build time**, so a rebuild is required after changing it.

---

## Production deployment

### Backend (Railway)

Railway builds `backend/Dockerfile` (Go 1.25 Alpine) and deploys on push to `agentmesh-goa`.

Required environment variables:

| Var | Notes |
|-----|-------|
| `DATABASE_URL` | Supabase **transaction pooler** URL (IPv4, port 6543). The direct `db.<ref>.supabase.co` host is IPv6-only and unreachable from Railway. URL-encode special chars in the password (`@` → `%40`). |
| `JWT_SECRET` | `openssl rand -hex 32` |
| `ENCRYPTION_KEY` | 32 hex bytes (agent wallet mnemonic encryption) |
| `BASE_URL` | Public URL of this backend (used to build OAuth callback URLs) |
| `FRONTEND_URL` | Public URL of the frontend (OAuth redirects land here) |
| `CORS_ORIGIN` | Allowed browser origin (the frontend) |
| `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` | GitHub OAuth App |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | Google OAuth client |
| `GEMINI_API_KEY` | optional, AI nodes |

> Supabase note: the pooler runs PgBouncer in transaction mode, which forbids prepared statements. The backend forces pgx's simple query protocol (`db.go`) for both the pool and migrations.

### Frontend (Vercel)

Builds `agentmesh-goa`. Set one variable:

```
NEXT_PUBLIC_API_URL = https://wailist-agentmesh-production.up.railway.app
```

Then redeploy — the value inlines at build time, so a push or manual redeploy is required for it to take effect.

### OAuth app setup

**GitHub** (https://github.com/settings/developers → New OAuth App):
- Homepage URL: `https://wailist-agentmesh-zeta.vercel.app`
- Authorization callback URL: `https://wailist-agentmesh-production.up.railway.app/auth/oauth/github/callback`

**Google** (https://console.cloud.google.com/apis/credentials → OAuth client ID → Web application):
- Authorized JavaScript origin: `https://wailist-agentmesh-zeta.vercel.app`
- Authorized redirect URI: `https://wailist-agentmesh-production.up.railway.app/auth/oauth/google/callback`
- Consent screen scopes are `openid email profile` only, so the app stays usable in "Testing" mode without Google verification.

Callback URLs must match **exactly** — a trailing slash or http/https mismatch is the most common OAuth failure.

---

## Backend API

Routes are split into public (no token) and protected (JWT required).

### Public

| Method | Route | Description |
|--------|-------|-------------|
| GET | `/health` | Health check |
| POST | `/auth/signup` | Create account (email, password, org) → `{ token }` |
| POST | `/auth/signin` | Sign in → `{ token }` |
| POST | `/auth/signout` | No-op (client discards token) |
| GET | `/auth/oauth/{provider}` | Start OAuth (`github` \| `google`) |
| GET | `/auth/oauth/{provider}/callback` | OAuth callback → redirects to frontend with JWT |
| POST | `/waitlist` | Join the landing-page waitlist (`{ email }`) |
| POST | `/run/{workflowId}` | Public webhook trigger |

### Protected (require `Authorization: Bearer <jwt>`)

| Method | Route | Description |
|--------|-------|-------------|
| GET | `/auth/me` | Current user id from the token |
| GET | `/workflows` | List the user's workflows |
| POST | `/workflows` | Create workflow |
| GET | `/workflows/{id}` | Get workflow |
| PUT | `/workflows/{id}` | Update workflow |
| DELETE | `/workflows/{id}` | Delete workflow |
| POST | `/workflows/{id}/deploy` | Provision Algorand wallets per agent node |
| GET | `/workflows/{id}/agents/{agentId}/balance` | Agent wallet balance |
| POST | `/workflows/{id}/agents/{agentId}/fund` | Fund agent from testnet dispenser |
| POST | `/workflows/{id}/run` | Trigger a run |
| POST | `/workflows/{id}/stop` | Stop workflow |
| GET | `/runs/{runId}` | Get run + logs |
| GET | `/runs/{runId}/stream` | SSE log stream |
| POST | `/tools/x402/quote` | Quote an x402 endpoint |

---

## Database schema

Tables (created by `backend/internal/db/migrations/`):

- `users` — id, email (unique), password_hash (empty for OAuth-only accounts), created_at
- `workflows` — graph JSONB, status, deploy metadata, owned by `user_id`
- `agent_wallets` — per-agent Algorand wallet, encrypted mnemonic
- `tool_credentials` — encrypted per-provider API keys
- `runs` / `run_logs` — execution history and per-node logs
- `waitlist` — email (unique), created_at

Query the waitlist:
```sql
SELECT email, created_at FROM waitlist ORDER BY created_at DESC;
```

---

## Commands

```bash
# frontend
cd frontend
npm run dev      # dev server (localhost:3000)
npm run build    # production build
npm run lint     # eslint

# backend
cd backend
go run ./cmd/server   # run server
go build ./...        # build binary
go test ./...         # run tests
```

---

## Notes

- Algorand wallets use testnet by default. Set `ALGORAND_NETWORK=mainnet` with a real algod endpoint for production.
- Both Railway and Vercel deploy from the `agentmesh-goa` branch. Auto-deploy on push must be enabled per project, or each push needs a manual redeploy.
- See `docs/whats-left.md` for remaining gaps and roadmap.
- See `docs/backend-plan.md` for the full backend architecture reference.
