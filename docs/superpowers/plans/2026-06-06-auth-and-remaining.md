# Auth Flow & Remaining Stubs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace every "dev" stub with real JWT auth, add the waitlist endpoint, and wire CORS/secret env vars so the deployed stack is fully functional.

**Architecture:** Backend issues HS256 JWTs signed with `JWT_SECRET`. The middleware validates every non-auth route. bcrypt hashes passwords — `golang.org/x/crypto/bcrypt` is already an indirect dep so no new library needed for that. `golang-jwt/jwt/v5` is added for token signing/parsing.

**Tech Stack:** Go 1.25, chi v5, pgx v5, bcrypt (x/crypto), golang-jwt/jwt v5, Next.js 16 (frontend unchanged — API layer already conditionally calls real backend when `NEXT_PUBLIC_API_URL` is set)

---

## File Map

| File | Action | What changes |
|------|--------|--------------|
| `backend/go.mod` + `go.sum` | Modify | add `github.com/golang-jwt/jwt/v5` |
| `backend/internal/db/store.go` | Modify | add `CreateUser`, `GetUserByEmail` |
| `backend/internal/api/handlers/deps.go` | Modify | add `JWTSecret string` field |
| `backend/internal/api/handlers/auth.go` | Modify | real signup/signin/me with bcrypt + JWT |
| `backend/internal/api/middleware.go` | Modify | real JWT validation via `newAuthMiddleware(secret)` |
| `backend/internal/api/router.go` | Modify | pass secret to middleware, skip auth on public routes |
| `backend/cmd/server/main.go` | Modify | pass `JWT_SECRET` from env to deps |
| `backend/internal/db/migrations/000002_waitlist.up.sql` | Create | `waitlist_signups` table |
| `backend/internal/db/migrations/000002_waitlist.down.sql` | Create | drop table |
| `backend/internal/db/store.go` | Modify | add `InsertWaitlistEmail` |
| `backend/internal/api/handlers/waitlist.go` | Create | `POST /waitlist` handler |
| `backend/internal/api/router.go` | Modify | register `/waitlist` route (public) |

---

## Task 1: Add golang-jwt dependency

**Files:**
- Modify: `backend/go.mod`

- [ ] **Step 1: Add the dependency**

```bash
cd backend && go get github.com/golang-jwt/jwt/v5
```

Expected output ends with: `go: added github.com/golang-jwt/jwt/v5 v5.x.x`

- [ ] **Step 2: Verify it builds**

```bash
go build ./...
```

Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add backend/go.mod backend/go.sum
git commit -m "build(deps): add golang-jwt/jwt/v5 for JWT auth"
```

---

## Task 2: Add user DB methods to store

**Files:**
- Modify: `backend/internal/db/store.go`

The `users` table already exists in the migration (`000001_init.up.sql`). Add two methods at the bottom of `store.go`:

- [ ] **Step 1: Write failing test**

Add to `backend/internal/db/db_test.go` (find the test helper that sets up a real DB connection — look for `TestMain` or a `newTestStore` function in `db_test.go`):

```go
func TestCreateAndGetUser(t *testing.T) {
    s := newTestStore(t)
    user, err := s.CreateUser(context.Background(), "test@example.com", "hashed-pw")
    if err != nil {
        t.Fatalf("CreateUser: %v", err)
    }
    if user.ID == "" {
        t.Fatal("expected non-empty ID")
    }
    got, err := s.GetUserByEmail(context.Background(), "test@example.com")
    if err != nil {
        t.Fatalf("GetUserByEmail: %v", err)
    }
    if got.ID != user.ID {
        t.Fatalf("want %s got %s", user.ID, got.ID)
    }
    if got.PasswordHash != "hashed-pw" {
        t.Fatal("password hash mismatch")
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd backend && go test ./internal/db/ -run TestCreateAndGetUser -v
```

Expected: compile error `s.CreateUser undefined`

- [ ] **Step 3: Add a `User` model to `backend/internal/models/types.go`**

Append after the existing type definitions:

```go
type User struct {
    ID           string    `json:"id"`
    Email        string    `json:"email"`
    PasswordHash string    `json:"-"`
    CreatedAt    time.Time `json:"createdAt"`
}
```

- [ ] **Step 4: Add `CreateUser` and `GetUserByEmail` to `backend/internal/db/store.go`**

Append to the end of the file:

```go
// --- User methods ---

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (models.User, error) {
    var u models.User
    err := s.pool.QueryRow(ctx, `
        INSERT INTO users (id, email, password_hash)
        VALUES (gen_random_uuid()::text, $1, $2)
        RETURNING id, email, password_hash, created_at
    `, email, passwordHash).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
    return u, err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
    var u models.User
    err := s.pool.QueryRow(ctx, `
        SELECT id, email, password_hash, created_at
        FROM users WHERE email = $1
    `, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
    return u, err
}
```

- [ ] **Step 5: Run test (requires live DB — skip if no test DB, mark as integration)**

```bash
cd backend && go test ./internal/db/ -run TestCreateAndGetUser -v
```

Expected: PASS (or skip if no `TEST_DATABASE_URL` set — see existing test setup)

- [ ] **Step 6: Verify build**

```bash
cd backend && go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add backend/internal/db/store.go backend/internal/models/types.go backend/internal/db/db_test.go
git commit -m "feat(db): add CreateUser and GetUserByEmail store methods"
```

---

## Task 3: Real auth handlers

**Files:**
- Modify: `backend/internal/api/handlers/deps.go`
- Modify: `backend/internal/api/handlers/auth.go`

- [ ] **Step 1: Add `JWTSecret` to `Deps`**

In `backend/internal/api/handlers/deps.go`, change the struct to:

```go
type Deps struct {
    Store     *db.Store
    Broker    *sse.Broker
    Wallet    *wallet.Service
    Engine    *engine.Runner
    BaseURL   string
    JWTSecret string
}
```

- [ ] **Step 2: Write failing tests for auth handlers**

Replace `backend/internal/api/handlers/auth_test.go` entirely:

```go
package handlers_test

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/agentmesh/backend/internal/api/handlers"
)

func TestSignUpReturnsBadRequestOnEmptyEmail(t *testing.T) {
    d := &handlers.Deps{JWTSecret: "testsecret"}
    body, _ := json.Marshal(map[string]string{"email": "", "password": "validpassword"})
    req := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(body))
    w := httptest.NewRecorder()
    d.SignUp(w, req)
    if w.Code != http.StatusBadRequest {
        t.Fatalf("want 400 got %d", w.Code)
    }
}

func TestSignUpReturnsBadRequestOnShortPassword(t *testing.T) {
    d := &handlers.Deps{JWTSecret: "testsecret"}
    body, _ := json.Marshal(map[string]string{"email": "a@b.com", "password": "short"})
    req := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(body))
    w := httptest.NewRecorder()
    d.SignUp(w, req)
    if w.Code != http.StatusBadRequest {
        t.Fatalf("want 400 got %d", w.Code)
    }
}
```

- [ ] **Step 3: Run tests to confirm they fail**

```bash
cd backend && go test ./internal/api/handlers/ -run "TestSignUp" -v
```

Expected: FAIL — `d.SignUp` returns 200 (old stub), not 400

- [ ] **Step 4: Rewrite `backend/internal/api/handlers/auth.go`**

```go
package handlers

import (
    "encoding/json"
    "net/http"
    "strings"
    "time"

    "github.com/golang-jwt/jwt/v5"
    "golang.org/x/crypto/bcrypt"

    "github.com/agentmesh/backend/internal/models"
    "github.com/agentmesh/backend/internal/respond"
)

const tokenTTL = 7 * 24 * time.Hour

type authClaims struct {
    UserID string `json:"sub"`
    Email  string `json:"email"`
    jwt.RegisteredClaims
}

func (d *Deps) SignUp(w http.ResponseWriter, r *http.Request) {
    var body struct {
        Email    string `json:"email"`
        Password string `json:"password"`
        Org      string `json:"org"`
    }
    json.NewDecoder(r.Body).Decode(&body)

    body.Email = strings.TrimSpace(strings.ToLower(body.Email))
    if body.Email == "" || !strings.Contains(body.Email, "@") {
        respond.Error(w, http.StatusBadRequest, "valid email required")
        return
    }
    if len(body.Password) < 8 {
        respond.Error(w, http.StatusBadRequest, "password must be at least 8 characters")
        return
    }

    hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
    if err != nil {
        respond.Error(w, http.StatusInternalServerError, "internal error")
        return
    }

    user, err := d.Store.CreateUser(r.Context(), body.Email, string(hash))
    if err != nil {
        // unique violation → 409
        if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
            respond.Error(w, http.StatusConflict, "email already registered")
            return
        }
        respond.Error(w, http.StatusInternalServerError, err.Error())
        return
    }

    token, err := d.issueToken(user)
    if err != nil {
        respond.Error(w, http.StatusInternalServerError, "could not issue token")
        return
    }
    respond.JSON(w, http.StatusCreated, map[string]string{"token": token})
}

func (d *Deps) SignIn(w http.ResponseWriter, r *http.Request) {
    var body struct {
        Email    string `json:"email"`
        Password string `json:"password"`
    }
    json.NewDecoder(r.Body).Decode(&body)

    body.Email = strings.TrimSpace(strings.ToLower(body.Email))
    if body.Email == "" || body.Password == "" {
        respond.Error(w, http.StatusBadRequest, "email and password required")
        return
    }

    user, err := d.Store.GetUserByEmail(r.Context(), body.Email)
    if err != nil {
        respond.Error(w, http.StatusUnauthorized, "invalid credentials")
        return
    }

    if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)); err != nil {
        respond.Error(w, http.StatusUnauthorized, "invalid credentials")
        return
    }

    token, err := d.issueToken(user)
    if err != nil {
        respond.Error(w, http.StatusInternalServerError, "could not issue token")
        return
    }
    respond.JSON(w, http.StatusOK, map[string]string{"token": token})
}

func (d *Deps) SignOut(w http.ResponseWriter, r *http.Request) {
    // JWTs are stateless — client drops the token. No server action needed.
    w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) Me(w http.ResponseWriter, r *http.Request) {
    userID, _ := r.Context().Value(CtxUserID).(string)
    respond.JSON(w, http.StatusOK, map[string]string{"id": userID})
}

func (d *Deps) issueToken(user models.User) (string, error) {
    claims := authClaims{
        UserID: user.ID,
        Email:  user.Email,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
        },
    }
    return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(d.JWTSecret))
}
```

- [ ] **Step 5: Run tests**

```bash
cd backend && go test ./internal/api/handlers/ -run "TestSignUp" -v
```

Expected: PASS

- [ ] **Step 6: Build**

```bash
cd backend && go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/handlers/auth.go backend/internal/api/handlers/auth_test.go backend/internal/api/handlers/deps.go
git commit -m "feat(auth): real signup/signin with bcrypt and JWT"
```

---

## Task 4: Real JWT middleware

**Files:**
- Modify: `backend/internal/api/middleware.go`
- Modify: `backend/internal/api/router.go`

Public routes that must NOT require a token: `POST /auth/signup`, `POST /auth/signin`, `POST /run/{workflowId}` (public webhook), `POST /waitlist`, `GET /health`.

- [ ] **Step 1: Write failing test for middleware**

Create `backend/internal/api/middleware_test.go`:

```go
package api_test

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/agentmesh/backend/internal/api"
)

func TestNewAuthMiddlewareRejectsNoToken(t *testing.T) {
    handler := api.NewAuthMiddleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    req := httptest.NewRequest(http.MethodGet, "/workflows", nil)
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)
    if w.Code != http.StatusUnauthorized {
        t.Fatalf("want 401 got %d", w.Code)
    }
}

func TestNewAuthMiddlewareAcceptsValidToken(t *testing.T) {
    // Build a valid token manually for the test
    // Use the exported helper so tests don't duplicate JWT logic
    token := api.TestMakeToken("secret", "user-123")
    handler := api.NewAuthMiddleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    req := httptest.NewRequest(http.MethodGet, "/workflows", nil)
    req.Header.Set("Authorization", "Bearer "+token)
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
        t.Fatalf("want 200 got %d", w.Code)
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd backend && go test ./internal/api/ -run "TestNewAuthMiddleware" -v
```

Expected: compile error — `api.NewAuthMiddleware` and `api.TestMakeToken` undefined

- [ ] **Step 3: Rewrite `backend/internal/api/middleware.go`**

```go
package api

import (
    "context"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/golang-jwt/jwt/v5"

    "github.com/agentmesh/backend/internal/api/handlers"
    "github.com/agentmesh/backend/internal/respond"
)

func corsMiddleware(next http.Handler) http.Handler {
    origin := os.Getenv("CORS_ORIGIN")
    if origin == "" {
        origin = "*"
    }
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", origin)
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
        if r.Method == http.MethodOptions {
            w.WriteHeader(http.StatusNoContent)
            return
        }
        next.ServeHTTP(w, r)
    })
}

// NewAuthMiddleware returns a middleware that validates HS256 JWTs.
// Routes that should be public must be registered BEFORE this middleware
// or use the router's public subrouter (see router.go).
func NewAuthMiddleware(secret string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
            if raw == "" {
                respond.Error(w, http.StatusUnauthorized, "missing token")
                return
            }
            claims := &jwtClaims{}
            _, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
                if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
                    return nil, jwt.ErrSignatureInvalid
                }
                return []byte(secret), nil
            })
            if err != nil {
                respond.Error(w, http.StatusUnauthorized, "invalid token")
                return
            }
            ctx := context.WithValue(r.Context(), handlers.CtxUserID, claims.UserID)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

type jwtClaims struct {
    UserID string `json:"sub"`
    Email  string `json:"email"`
    jwt.RegisteredClaims
}

// TestMakeToken creates a valid signed token for use in tests only.
func TestMakeToken(secret, userID string) string {
    claims := jwtClaims{
        UserID: userID,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
        },
    }
    t, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
    return t
}
```

- [ ] **Step 4: Update `backend/internal/api/router.go`**

The router needs public routes (no auth) and protected routes (auth required). Replace the router with:

```go
package api

import (
    "net/http"

    "github.com/go-chi/chi/v5"
    chimw "github.com/go-chi/chi/v5/middleware"

    "github.com/agentmesh/backend/internal/api/handlers"
)

func NewRouter(d *handlers.Deps) http.Handler {
    r := chi.NewRouter()
    r.Use(chimw.Logger)
    r.Use(chimw.Recoverer)
    r.Use(corsMiddleware)

    // Public routes — no JWT required
    r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
        w.Write([]byte("ok"))
    })
    r.Post("/auth/signup", d.SignUp)
    r.Post("/auth/signin", d.SignIn)
    r.Post("/auth/signout", d.SignOut)
    r.Post("/waitlist", d.JoinWaitlist)
    r.Post("/run/{workflowId}", d.PublicTrigger)

    // Protected routes — JWT required
    r.Group(func(r chi.Router) {
        r.Use(NewAuthMiddleware(d.JWTSecret))

        r.Get("/auth/me", d.Me)

        r.Get("/workflows", d.ListWorkflows)
        r.Post("/workflows", d.CreateWorkflow)
        r.Get("/workflows/{id}", d.GetWorkflow)
        r.Put("/workflows/{id}", d.UpdateWorkflow)
        r.Delete("/workflows/{id}", d.DeleteWorkflow)

        r.Post("/workflows/{id}/deploy", d.Deploy)
        r.Get("/workflows/{id}/agents/{agentId}/balance", d.AgentBalance)
        r.Post("/workflows/{id}/agents/{agentId}/fund", d.FundAgent)

        r.Post("/workflows/{id}/run", d.TriggerRun)
        r.Post("/workflows/{id}/stop", d.StopWorkflow)
        r.Get("/runs/{runId}", d.GetRun)
        r.Get("/runs/{runId}/stream", d.StreamRun)

        r.Post("/tools/x402/quote", d.X402Quote)
    })

    return r
}
```

- [ ] **Step 5: Run middleware tests**

```bash
cd backend && go test ./internal/api/ -run "TestNewAuthMiddleware" -v
```

Expected: PASS

- [ ] **Step 6: Build**

```bash
cd backend && go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/middleware.go backend/internal/api/middleware_test.go backend/internal/api/router.go
git commit -m "feat(auth): real JWT middleware, split public/protected routes"
```

---

## Task 5: Wire JWT_SECRET in main.go

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Update deps construction in `main.go`**

Find the `deps` struct literal and add `JWTSecret`:

```go
deps := &handlers.Deps{
    Store:     store,
    Broker:    broker,
    Wallet:    walletSvc,
    Engine:    runner,
    BaseURL:   envOr("BASE_URL", "http://localhost:8080"),
    JWTSecret: mustEnv("JWT_SECRET"),
}
```

- [ ] **Step 2: Build**

```bash
cd backend && go build ./...
```

Expected: success

- [ ] **Step 3: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(auth): wire JWT_SECRET env var into server deps"
```

---

## Task 6: Set Railway env vars

- [ ] **Step 1: Generate a strong JWT secret and set it**

```bash
openssl rand -hex 32
```

Copy the output, then:

```bash
railway variables set JWT_SECRET="<output-from-above>"
```

- [ ] **Step 2: Set CORS_ORIGIN to the Vercel frontend URL**

```bash
railway variables set CORS_ORIGIN="https://<your-vercel-app>.vercel.app"
```

If Vercel URL isn't known yet, set to `*` temporarily:

```bash
railway variables set CORS_ORIGIN="*"
```

- [ ] **Step 3: Verify Railway has all required vars**

```bash
railway variables 2>&1 | grep -E "DATABASE_URL|ENCRYPTION_KEY|JWT_SECRET|BASE_URL|CORS_ORIGIN"
```

Expected: all 5 lines present with non-empty values.

- [ ] **Step 4: Push to trigger Railway redeploy**

```bash
git push
```

Watch Railway dashboard — server should log `AgentMesh backend listening on :8080` within ~60 seconds.

---

## Task 7: Run DB migration on Supabase

- [ ] **Step 1: Open Supabase SQL Editor**

Go to [supabase.com](https://supabase.com) → your project → **SQL Editor** → **New query**

- [ ] **Step 2: Run migration 000001**

Paste the entire contents of `backend/internal/db/migrations/000001_init.up.sql` and click **Run**.

Expected: `Success. No rows returned`

- [ ] **Step 3: Verify tables exist**

Run:
```sql
SELECT table_name FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY table_name;
```

Expected: `agent_wallets`, `run_logs`, `runs`, `tool_credentials`, `users`, `workflows`

---

## Task 8: Waitlist endpoint

**Files:**
- Create: `backend/internal/db/migrations/000002_waitlist.up.sql`
- Create: `backend/internal/db/migrations/000002_waitlist.down.sql`
- Modify: `backend/internal/db/store.go`
- Create: `backend/internal/api/handlers/waitlist.go`

- [ ] **Step 1: Create migration files**

`backend/internal/db/migrations/000002_waitlist.up.sql`:
```sql
CREATE TABLE IF NOT EXISTS waitlist_signups (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    email      TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

`backend/internal/db/migrations/000002_waitlist.down.sql`:
```sql
DROP TABLE IF EXISTS waitlist_signups;
```

- [ ] **Step 2: Add `InsertWaitlistEmail` to store**

Append to `backend/internal/db/store.go`:

```go
// --- Waitlist methods ---

func (s *Store) InsertWaitlistEmail(ctx context.Context, email string) error {
    _, err := s.pool.Exec(ctx, `
        INSERT INTO waitlist_signups (email) VALUES ($1)
        ON CONFLICT (email) DO NOTHING
    `, email)
    return err
}
```

- [ ] **Step 3: Create `backend/internal/api/handlers/waitlist.go`**

```go
package handlers

import (
    "encoding/json"
    "net/http"
    "strings"

    "github.com/agentmesh/backend/internal/respond"
)

func (d *Deps) JoinWaitlist(w http.ResponseWriter, r *http.Request) {
    var body struct {
        Email string `json:"email"`
    }
    json.NewDecoder(r.Body).Decode(&body)
    body.Email = strings.TrimSpace(strings.ToLower(body.Email))
    if body.Email == "" || !strings.Contains(body.Email, "@") {
        respond.Error(w, http.StatusBadRequest, "valid email required")
        return
    }
    if err := d.Store.InsertWaitlistEmail(r.Context(), body.Email); err != nil {
        respond.Error(w, http.StatusInternalServerError, err.Error())
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Build**

```bash
cd backend && go build ./...
```

- [ ] **Step 5: Run migration 000002 in Supabase SQL Editor**

Paste contents of `000002_waitlist.up.sql` and run.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/db/migrations/ backend/internal/db/store.go backend/internal/api/handlers/waitlist.go
git commit -m "feat(waitlist): add waitlist table, store method, and POST /waitlist endpoint"
git push
```

---

## Task 9: Smoke test the live API

Once Railway redeploys (watch for `listening on :8080` in logs):

- [ ] **Step 1: Health check**

```bash
curl https://wailist-agentmesh-production.up.railway.app/health
```
Expected: `ok`

- [ ] **Step 2: Signup**

```bash
curl -s -X POST https://wailist-agentmesh-production.up.railway.app/auth/signup \
  -H "Content-Type: application/json" \
  -d '{"email":"you@test.com","password":"testpass123"}' | jq .
```
Expected: `{"token":"eyJ..."}`

- [ ] **Step 3: Use token to list workflows**

```bash
TOKEN="<token-from-step-2>"
curl -s https://wailist-agentmesh-production.up.railway.app/workflows \
  -H "Authorization: Bearer $TOKEN" | jq .
```
Expected: `[]`

- [ ] **Step 4: Confirm unauthenticated request is rejected**

```bash
curl -s https://wailist-agentmesh-production.up.railway.app/workflows | jq .
```
Expected: `{"error":"missing token"}` with HTTP 401

- [ ] **Step 5: Waitlist**

```bash
curl -s -X POST https://wailist-agentmesh-production.up.railway.app/waitlist \
  -H "Content-Type: application/json" \
  -d '{"email":"waitlist@test.com"}'
```
Expected: HTTP 204 (no body)

---

## Self-Review Checklist

- [x] Auth signup/signin: Task 3 ✓
- [x] JWT middleware replacing "dev" stub: Task 4 ✓
- [x] JWT_SECRET env var: Tasks 5 + 6 ✓
- [x] CORS_ORIGIN env var: Task 6 ✓
- [x] DB migration: Task 7 ✓
- [x] Waitlist endpoint: Task 8 ✓
- [x] Public routes (auth, run, waitlist, health) exempt from JWT: Task 4 router ✓
- [x] `Me` endpoint returns real userID from token: Task 3 ✓
- [x] Frontend already calls real endpoints when `NEXT_PUBLIC_API_URL` is set: no frontend changes needed ✓
- [x] `bcrypt` — already indirect dep, no new import needed ✓
- [x] `StopWorkflow` — still a stub (Phase 2: needs in-memory run tracking map; out of scope here)
