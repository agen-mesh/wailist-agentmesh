# Workflow Marketplace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a workflow creator marketplace — users publish workflows to `/marketplace`, others discover and import them to their own canvas, and creators earn 1% of every run cost on imported workflows.

**Architecture:** Backend-persisted. A new `published_workflows` table holds published snapshots with upvote/run counts. `workflows` gains a nullable `source_published_id` FK. On run completion the runner credits 1% of cost to the creator. The existing `/marketplace` page gains a Workflows tab. The canvas topbar gains a Publish button. The node library panel (PalettePanel) gains a Saved tab showing the user's imported workflows.

**Tech Stack:** Go (chi/v5, pgx/v5, golang-migrate), Next.js App Router, React 19, TypeScript, inline CSS using existing CSS vars from `globals.css`.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `backend/internal/db/migrations/000004_marketplace.up.sql` | Create | New tables + FK column |
| `backend/internal/db/migrations/000004_marketplace.down.sql` | Create | Rollback |
| `backend/internal/models/types.go` | Modify | Add `PublishedWorkflow`, `SourcePublishedID` on `Workflow` |
| `backend/internal/db/store.go` | Modify | Store methods: publish, list, import, upvote, credit creator; update workflow scans |
| `backend/internal/db/marketplace_test.go` | Create | DB-level tests for all new store methods |
| `backend/internal/engine/runner.go` | Modify | Credit creator after successful run |
| `backend/internal/api/handlers/marketplace_workflows.go` | Create | HTTP handlers for publish/list/import/upvote |
| `backend/internal/api/router.go` | Modify | Register four new routes |
| `frontend/src/lib/types.ts` | Modify | Add `PublishedWorkflow`, `sourcePublishedId` on `Workflow` |
| `frontend/src/lib/api.ts` | Modify | Add `marketplace.listWorkflows`, `publishWorkflow`, `importWorkflow`, `upvoteWorkflow` |
| `frontend/src/components/marketplace/MarketplacePage.tsx` | Modify | Add "Workflows" tab with cards, search, upvote, import |
| `frontend/src/components/canvas/PublishModal.tsx` | Create | Publish-to-marketplace form modal |
| `frontend/src/components/canvas/CanvasPage.tsx` | Modify | Publish button in topbar; fetch saved workflows; pass to PalettePanel |
| `frontend/src/components/canvas/PalettePanel.tsx` | Modify | Add "Saved" tab from prop; click to open workflow |

---

## Task 1: DB Migration — marketplace tables

**Files:**
- Create: `backend/internal/db/migrations/000004_marketplace.up.sql`
- Create: `backend/internal/db/migrations/000004_marketplace.down.sql`

- [ ] **Step 1: Write the up migration**

Create `backend/internal/db/migrations/000004_marketplace.up.sql`:

```sql
CREATE TABLE IF NOT EXISTS published_workflows (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    creator_id    TEXT NOT NULL REFERENCES users(id),
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    tags          TEXT[] NOT NULL DEFAULT '{}',
    graph         JSONB NOT NULL DEFAULT '{"nodes":[],"edges":[]}',
    fee_per_run   DECIMAL(10,6) NOT NULL DEFAULT 0,
    run_count     INT NOT NULL DEFAULT 0,
    upvote_count  INT NOT NULL DEFAULT 0,
    published_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS published_workflow_votes (
    user_id     TEXT NOT NULL REFERENCES users(id),
    workflow_id TEXT NOT NULL REFERENCES published_workflows(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, workflow_id)
);

ALTER TABLE workflows ADD COLUMN IF NOT EXISTS source_published_id TEXT REFERENCES published_workflows(id);

CREATE INDEX IF NOT EXISTS idx_published_workflows_rank
    ON published_workflows (upvote_count DESC, run_count DESC);
CREATE INDEX IF NOT EXISTS idx_published_workflows_creator
    ON published_workflows (creator_id);
CREATE INDEX IF NOT EXISTS idx_workflows_source_published
    ON workflows (source_published_id) WHERE source_published_id IS NOT NULL;
```

- [ ] **Step 2: Write the down migration**

Create `backend/internal/db/migrations/000004_marketplace.down.sql`:

```sql
ALTER TABLE workflows DROP COLUMN IF EXISTS source_published_id;
DROP TABLE IF EXISTS published_workflow_votes;
DROP TABLE IF EXISTS published_workflows;
```

- [ ] **Step 3: Verify migration files exist**

```bash
ls backend/internal/db/migrations/000004_marketplace*
```

Expected: two files listed.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/db/migrations/000004_marketplace.up.sql \
        backend/internal/db/migrations/000004_marketplace.down.sql
git commit -m "feat(db): add published_workflows tables and source_published_id FK"
```

---

## Task 2: Backend models — PublishedWorkflow type

**Files:**
- Modify: `backend/internal/models/types.go`

- [ ] **Step 1: Add `SourcePublishedID` to `Workflow` and add `PublishedWorkflow` model**

Open `backend/internal/models/types.go`. After the closing brace of `Workflow` (after line 112), append:

1. Add `SourcePublishedID *string` field to the `Workflow` struct — insert it after `UpdatedAt`:

```go
	SourcePublishedID *string `json:"sourcePublishedId,omitempty"`
```

So the `Workflow` struct ends with:
```go
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	SourcePublishedID *string    `json:"sourcePublishedId,omitempty"`
}
```

2. Append `PublishedWorkflow` type at the end of the file (after `User`):

```go
type PublishedWorkflow struct {
	ID           string     `json:"id"`
	CreatorID    string     `json:"creatorId"`
	CreatorEmail string     `json:"creatorEmail"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Tags         []string   `json:"tags"`
	Nodes        []WorkflowNode `json:"nodes,omitempty"`
	Edges        []WorkflowEdge `json:"edges,omitempty"`
	FeePerRun    float64    `json:"feePerRun"`
	RunCount     int        `json:"runCount"`
	UpvoteCount  int        `json:"upvoteCount"`
	PublishedAt  time.Time  `json:"publishedAt"`
}
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd backend && go build ./...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/models/types.go
git commit -m "feat(models): add PublishedWorkflow type and SourcePublishedID on Workflow"
```

---

## Task 3: Backend store — marketplace CRUD methods

**Files:**
- Create: `backend/internal/db/marketplace_test.go`
- Modify: `backend/internal/db/store.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/db/marketplace_test.go`:

```go
package db_test

import (
	"context"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

func TestPublishedWorkflowCRUD(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Need a user to be the creator
	creator, err := store.CreateUser(ctx, "creator@test.com", "hash")
	if err != nil {
		t.Fatal(err)
	}

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{{ID: "n1", Type: models.NodeTypeTrigger}},
		Edges: []models.WorkflowEdge{},
	}

	pw, err := store.PublishWorkflow(ctx, creator.ID, "My Workflow", "Does stuff", []string{"ai", "search"}, graph, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if pw.Title != "My Workflow" {
		t.Fatalf("want title 'My Workflow' got %q", pw.Title)
	}
	if pw.CreatorID != creator.ID {
		t.Fatal("creator id mismatch")
	}

	// List returns it
	list, err := store.ListPublishedWorkflows(ctx, "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range list {
		if w.ID == pw.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("published workflow not in list")
	}

	// Search by title
	results, err := store.ListPublishedWorkflows(ctx, "My Workflow", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("search returned no results")
	}

	// Search no match
	empty, err := store.ListPublishedWorkflows(ctx, "zzznomatch", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatal("expected empty search result")
	}
}

func TestImportPublishedWorkflow(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	creator, _ := store.CreateUser(ctx, "creator2@test.com", "hash")
	importer, _ := store.CreateUser(ctx, "importer@test.com", "hash")

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{{ID: "n1", Type: models.NodeTypeAgent}},
		Edges: []models.WorkflowEdge{},
	}
	pw, _ := store.PublishWorkflow(ctx, creator.ID, "Importable WF", "desc", []string{}, graph, 0.005)

	imported, err := store.ImportPublishedWorkflow(ctx, importer.ID, pw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if imported.UserID != importer.ID {
		t.Fatal("importer id mismatch")
	}
	if imported.SourcePublishedID == nil || *imported.SourcePublishedID != pw.ID {
		t.Fatal("source_published_id not set on imported workflow")
	}
	if len(imported.Nodes) != 1 {
		t.Fatal("nodes not copied from published workflow")
	}
}

func TestToggleUpvote(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	creator, _ := store.CreateUser(ctx, "creator3@test.com", "hash")
	voter, _ := store.CreateUser(ctx, "voter@test.com", "hash")

	pw, _ := store.PublishWorkflow(ctx, creator.ID, "Votable WF", "desc", []string{}, models.WorkflowGraph{}, 0)

	// First vote: upvote
	count, upvoted, err := store.ToggleUpvote(ctx, voter.ID, pw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !upvoted {
		t.Fatal("expected upvoted=true on first vote")
	}
	if count != 1 {
		t.Fatalf("expected count=1 got %d", count)
	}

	// Second vote: unvote
	count2, upvoted2, err := store.ToggleUpvote(ctx, voter.ID, pw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if upvoted2 {
		t.Fatal("expected upvoted=false on second vote (unvote)")
	}
	if count2 != 0 {
		t.Fatalf("expected count=0 got %d", count2)
	}
}

func TestCreditMarketplaceCreator(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	creator, _ := store.CreateUser(ctx, "creator4@test.com", "hash")
	importer, _ := store.CreateUser(ctx, "importer2@test.com", "hash")

	pw, _ := store.PublishWorkflow(ctx, creator.ID, "Paid WF", "desc", []string{}, models.WorkflowGraph{}, 0.01)
	imported, _ := store.ImportPublishedWorkflow(ctx, importer.ID, pw.ID)

	initialCredits := creator.Credits

	err := store.CreditMarketplaceCreator(ctx, imported.ID, 1.0) // 1% of $1.00 = $0.01
	if err != nil {
		t.Fatal(err)
	}

	// Check creator balance increased
	updated, err := store.GetUserByID(ctx, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := initialCredits + 0.01
	if updated.Credits < want-0.0001 || updated.Credits > want+0.0001 {
		t.Fatalf("expected credits ~%.4f got %.4f", want, updated.Credits)
	}

	// Check run_count incremented on published workflow
	list, _ := store.ListPublishedWorkflows(ctx, "Paid WF", 10, 0)
	if len(list) == 0 {
		t.Fatal("workflow not found")
	}
	if list[0].RunCount != 1 {
		t.Fatalf("expected run_count=1 got %d", list[0].RunCount)
	}
}
```

- [ ] **Step 2: Run tests to confirm they all fail**

```bash
cd backend && go test ./internal/db/... -run "TestPublishedWorkflow|TestImportPublished|TestToggleUpvote|TestCreditMarketplace" -v 2>&1 | head -40
```

Expected: compile error — `PublishWorkflow`, `ListPublishedWorkflows`, `ImportPublishedWorkflow`, `ToggleUpvote`, `CreditMarketplaceCreator` not defined.

- [ ] **Step 3: Add `ListWorkflows` and `GetWorkflow` to return `source_published_id`**

In `backend/internal/db/store.go`, find `GetWorkflow` (around line 55). Update the SELECT and scan:

```go
func (s *Store) GetWorkflow(ctx context.Context, id string) (models.Workflow, error) {
	var w models.Workflow
	var graphJSON []byte
	var runEndpoint *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, status, graph, deployed_at, run_endpoint, source_published_id, created_at, updated_at
		FROM workflows WHERE id = $1
	`, id).Scan(
		&w.ID, &w.UserID, &w.Name, &w.Status, &graphJSON,
		&w.DeployedAt, &runEndpoint, &w.SourcePublishedID, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return w, err
	}
	if runEndpoint != nil {
		w.RunEndpoint = *runEndpoint
	}
	unmarshalGraph(graphJSON, &w)
	return w, nil
}
```

Find `ListWorkflows` (around line 76). Update its SELECT, scan, and inner scan:

```go
func (s *Store) ListWorkflows(ctx context.Context, userID string) ([]models.Workflow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, name, status, graph, deployed_at, run_endpoint, source_published_id, created_at, updated_at
		FROM workflows WHERE user_id = $1 ORDER BY updated_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var wfs []models.Workflow
	for rows.Next() {
		var w models.Workflow
		var graphJSON []byte
		var runEndpoint *string
		if err := rows.Scan(
			&w.ID, &w.UserID, &w.Name, &w.Status, &graphJSON,
			&w.DeployedAt, &runEndpoint, &w.SourcePublishedID, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if runEndpoint != nil {
			w.RunEndpoint = *runEndpoint
		}
		unmarshalGraph(graphJSON, &w)
		wfs = append(wfs, w)
	}
	return wfs, rows.Err()
}
```

- [ ] **Step 4: Add marketplace store methods to `store.go`**

Append all five methods to the end of `backend/internal/db/store.go`:

```go
// --- Published Workflow methods ---

func (s *Store) PublishWorkflow(ctx context.Context, creatorID, title, description string, tags []string, graph models.WorkflowGraph, feePerRun float64) (models.PublishedWorkflow, error) {
	graphJSON, _ := json.Marshal(graph)
	if tags == nil {
		tags = []string{}
	}
	var pw models.PublishedWorkflow
	var graphOut []byte
	err := s.pool.QueryRow(ctx, `
		INSERT INTO published_workflows (creator_id, title, description, tags, graph, fee_per_run)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		RETURNING id, creator_id, title, description, tags, graph, fee_per_run, run_count, upvote_count, published_at
	`, creatorID, title, description, tags, string(graphJSON), feePerRun).Scan(
		&pw.ID, &pw.CreatorID, &pw.Title, &pw.Description, &pw.Tags,
		&graphOut, &pw.FeePerRun, &pw.RunCount, &pw.UpvoteCount, &pw.PublishedAt,
	)
	if err != nil {
		return pw, err
	}
	var g models.WorkflowGraph
	if err := json.Unmarshal(graphOut, &g); err == nil {
		pw.Nodes = g.Nodes
		pw.Edges = g.Edges
	}
	return pw, nil
}

func (s *Store) ListPublishedWorkflows(ctx context.Context, query string, limit, offset int) ([]models.PublishedWorkflow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pw.id, pw.creator_id, u.email, pw.title, pw.description, pw.tags,
		       pw.fee_per_run, pw.run_count, pw.upvote_count, pw.published_at
		FROM published_workflows pw
		JOIN users u ON u.id = pw.creator_id
		WHERE $1 = ''
		   OR pw.title ILIKE '%' || $1 || '%'
		   OR pw.description ILIKE '%' || $1 || '%'
		   OR $1 = ANY(pw.tags)
		ORDER BY pw.upvote_count DESC, pw.run_count DESC
		LIMIT $2 OFFSET $3
	`, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PublishedWorkflow
	for rows.Next() {
		var pw models.PublishedWorkflow
		if err := rows.Scan(
			&pw.ID, &pw.CreatorID, &pw.CreatorEmail, &pw.Title, &pw.Description, &pw.Tags,
			&pw.FeePerRun, &pw.RunCount, &pw.UpvoteCount, &pw.PublishedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, pw)
	}
	return out, rows.Err()
}

// ImportPublishedWorkflow creates a copy of a published workflow for the given user,
// setting source_published_id so run-cost credit flows back to the creator.
func (s *Store) ImportPublishedWorkflow(ctx context.Context, userID, publishedID string) (models.Workflow, error) {
	// Fetch published graph + title
	var title string
	var graphJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT title, graph FROM published_workflows WHERE id = $1
	`, publishedID).Scan(&title, &graphJSON)
	if err != nil {
		return models.Workflow{}, err
	}

	id := uuid.New().String()
	var w models.Workflow
	var gJSON []byte
	var runEndpoint *string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO workflows (id, user_id, name, status, graph, source_published_id)
		VALUES ($1, $2, $3, 'draft', $4::jsonb, $5)
		RETURNING id, user_id, name, status, graph, deployed_at, run_endpoint, source_published_id, created_at, updated_at
	`, id, userID, title, string(graphJSON), publishedID).Scan(
		&w.ID, &w.UserID, &w.Name, &w.Status, &gJSON,
		&w.DeployedAt, &runEndpoint, &w.SourcePublishedID, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return w, err
	}
	if runEndpoint != nil {
		w.RunEndpoint = *runEndpoint
	}
	unmarshalGraph(gJSON, &w)
	return w, nil
}

// ToggleUpvote adds an upvote if not present, removes it if present.
// Returns (new_count, is_now_upvoted, error).
func (s *Store) ToggleUpvote(ctx context.Context, userID, publishedID string) (int, bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO published_workflow_votes (user_id, workflow_id) VALUES ($1, $2)
		ON CONFLICT (user_id, workflow_id) DO NOTHING
	`, userID, publishedID)
	if err != nil {
		return 0, false, err
	}
	added := tag.RowsAffected() == 1

	if !added {
		// Already voted — remove it
		if _, err := s.pool.Exec(ctx, `
			DELETE FROM published_workflow_votes WHERE user_id=$1 AND workflow_id=$2
		`, userID, publishedID); err != nil {
			return 0, false, err
		}
	}

	// Recalculate and persist upvote_count
	var count int
	err = s.pool.QueryRow(ctx, `
		UPDATE published_workflows
		SET upvote_count = (SELECT COUNT(*) FROM published_workflow_votes WHERE workflow_id = $1)
		WHERE id = $1
		RETURNING upvote_count
	`, publishedID).Scan(&count)
	return count, added, err
}

// CreditMarketplaceCreator credits the creator 1% of the run cost and increments
// run_count on the published workflow. No-op if the workflow has no source_published_id.
func (s *Store) CreditMarketplaceCreator(ctx context.Context, workflowID string, cost float64) error {
	if cost <= 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		WITH src AS (
			SELECT pw.creator_id, pw.id AS published_id
			FROM workflows w
			JOIN published_workflows pw ON pw.id = w.source_published_id
			WHERE w.id = $1
		),
		credit AS (
			UPDATE users
			SET credits = credits + ($2 * 0.01)
			WHERE id = (SELECT creator_id FROM src)
		)
		UPDATE published_workflows
		SET run_count = run_count + 1
		WHERE id = (SELECT published_id FROM src)
	`, workflowID, cost)
	return err
}
```

- [ ] **Step 5: Add `uuid` import to `store.go`**

`store.go` already imports `github.com/google/uuid` — verify it's present:

```bash
grep "google/uuid" backend/internal/db/store.go
```

Expected: one line matching. If missing, add `"github.com/google/uuid"` to the import block.

- [ ] **Step 6: Build to verify**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 7: Run the new tests (requires TEST_DATABASE_URL)**

```bash
cd backend && go test ./internal/db/... -run "TestPublishedWorkflow|TestImportPublished|TestToggleUpvote|TestCreditMarketplace" -v
```

Expected: all tests PASS (or SKIP if `TEST_DATABASE_URL` not set — that's fine).

Also run existing workflow tests to confirm no regression:

```bash
cd backend && go test ./internal/db/... -run TestWorkflowCRUD -v
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/db/store.go backend/internal/db/marketplace_test.go
git commit -m "feat(db): add store methods for published workflow marketplace"
```

---

## Task 4: Backend runner — creator cut on successful run

**Files:**
- Modify: `backend/internal/engine/runner.go`

- [ ] **Step 1: Identify the success `FinishRunWithCost` call**

Open `backend/internal/engine/runner.go`. The success path is the last call (around line 175):

```go
r.store.FinishRunWithCost(context.Background(), run.ID, models.RunStatusSuccess, cost)
```

- [ ] **Step 2: Add creator credit call after success**

Replace that single line with:

```go
r.store.FinishRunWithCost(context.Background(), run.ID, models.RunStatusSuccess, cost)
go r.store.CreditMarketplaceCreator(context.Background(), wf.ID, cost)
```

The `go` makes it non-blocking — the SSE stream has already closed at this point so the run is effectively done regardless.

- [ ] **Step 3: Build to verify**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 4: Run all backend tests**

```bash
cd backend && go test ./... 2>&1 | tail -20
```

Expected: `ok` lines or `SKIP` lines only — no `FAIL`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/engine/runner.go
git commit -m "feat(engine): credit marketplace creator 1% on successful run"
```

---

## Task 5: Backend handlers — publish/list/import/upvote

**Files:**
- Create: `backend/internal/api/handlers/marketplace_workflows.go`
- Modify: `backend/internal/api/router.go`

- [ ] **Step 1: Create the handler file**

Create `backend/internal/api/handlers/marketplace_workflows.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

// ListPublishedWorkflows — GET /marketplace/workflows?q=&limit=24&offset=0 (public)
func (d *Deps) ListPublishedWorkflows(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 || limit > 100 {
		limit = 24
	}
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}
	workflows, err := d.Store.ListPublishedWorkflows(r.Context(), q, limit, offset)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if workflows == nil {
		workflows = []models.PublishedWorkflow{}
	}
	respond.JSON(w, http.StatusOK, map[string]any{"workflows": workflows})
}

// PublishWorkflow — POST /marketplace/workflows (protected)
func (d *Deps) PublishWorkflow(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	var body struct {
		WorkflowID  string   `json:"workflowId"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
		FeePerRun   float64  `json:"feePerRun"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.WorkflowID == "" || body.Title == "" {
		respond.Error(w, http.StatusBadRequest, "workflowId and title required")
		return
	}

	// Verify the workflow belongs to this user
	wf, err := d.Store.GetWorkflow(r.Context(), body.WorkflowID)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}

	graph := models.WorkflowGraph{Nodes: wf.Nodes, Edges: wf.Edges}
	if body.Tags == nil {
		body.Tags = []string{}
	}
	pw, err := d.Store.PublishWorkflow(r.Context(), userID, body.Title, body.Description, body.Tags, graph, body.FeePerRun)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond.JSON(w, http.StatusCreated, pw)
}

// ImportWorkflow — POST /marketplace/workflows/:id/import (protected)
func (d *Deps) ImportWorkflow(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	publishedID := chi.URLParam(r, "id")
	wf, err := d.Store.ImportPublishedWorkflow(r.Context(), userID, publishedID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "published workflow not found")
		return
	}
	respond.JSON(w, http.StatusCreated, wf)
}

// UpvoteWorkflow — POST /marketplace/workflows/:id/upvote (protected)
func (d *Deps) UpvoteWorkflow(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	publishedID := chi.URLParam(r, "id")
	count, upvoted, err := d.Store.ToggleUpvote(r.Context(), userID, publishedID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"upvoted": upvoted, "count": count})
}
```

- [ ] **Step 2: Register routes in router.go**

Open `backend/internal/api/router.go`. 

Add one public route after the existing Bazaar routes:

```go
r.Get("/marketplace/workflows", d.ListPublishedWorkflows)
```

Add three protected routes inside the `r.Group` (after existing `/marketplace/bazaar` public routes, within the JWT-protected group):

```go
r.Post("/marketplace/workflows", d.PublishWorkflow)
r.Post("/marketplace/workflows/{id}/import", d.ImportWorkflow)
r.Post("/marketplace/workflows/{id}/upvote", d.UpvoteWorkflow)
```

- [ ] **Step 3: Build to verify**

```bash
cd backend && go build ./...
```

Expected: no output.

- [ ] **Step 4: Run all backend tests**

```bash
cd backend && go test ./... 2>&1 | tail -20
```

Expected: `ok` or `SKIP` only.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/handlers/marketplace_workflows.go \
        backend/internal/api/router.go
git commit -m "feat(api): add publish/list/import/upvote handlers for marketplace workflows"
```

---

## Task 6: Frontend — types and API layer

**Files:**
- Modify: `frontend/src/lib/types.ts`
- Modify: `frontend/src/lib/api.ts`

- [ ] **Step 1: Add `PublishedWorkflow` type and update `Workflow` in `types.ts`**

Open `frontend/src/lib/types.ts`.

1. Add `sourcePublishedId?: string` to the `Workflow` interface (after the `tags` field):

```typescript
export interface Workflow {
  id: string;
  name: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  status?: "active" | "paused" | "draft";
  updated?: string;
  updatedAt?: string;
  agents?: number;
  runs?: number;
  spend?: string;
  tags?: string[];
  sourcePublishedId?: string;
}
```

2. Append `PublishedWorkflow` at the end of the file:

```typescript
export interface PublishedWorkflow {
  id: string;
  creatorId: string;
  creatorEmail: string;
  title: string;
  description: string;
  tags: string[];
  feePerRun: number;
  runCount: number;
  upvoteCount: number;
  publishedAt: string;
  // client-only state (not from API)
  hasUpvoted?: boolean;
}
```

- [ ] **Step 2: Add marketplace workflow methods to `api.ts`**

Open `frontend/src/lib/api.ts`. 

1. Update the import line at the top to include `PublishedWorkflow`:

```typescript
import { Workflow, ParamDef, MarketplaceEndpoint, PublishedWorkflow } from "./types";
```

2. In the `marketplace` object (after `goplausibleList`), add four new methods:

```typescript
  listWorkflows: async (q = "", limit = 24, offset = 0): Promise<{ workflows: PublishedWorkflow[] }> => {
    if (BASE) {
      const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
      if (q) params.set("q", q);
      const res = await fetch(`${BASE}/marketplace/workflows?${params}`, { credentials: "include" });
      if (!res.ok) throw new Error(`marketplace ${res.status}`);
      return res.json();
    }
    return { workflows: [] };
  },

  publishWorkflow: async (
    workflowId: string,
    title: string,
    description: string,
    tags: string[],
    feePerRun: number,
  ): Promise<PublishedWorkflow> => {
    if (BASE) {
      const res = await fetch(`${BASE}/marketplace/workflows`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ workflowId, title, description, tags, feePerRun }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "publish failed");
      return data;
    }
    throw new Error("no backend configured");
  },

  importWorkflow: async (publishedId: string): Promise<Workflow> => {
    if (BASE) {
      const res = await fetch(`${BASE}/marketplace/workflows/${publishedId}/import`, {
        method: "POST",
        credentials: "include",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "import failed");
      return data;
    }
    throw new Error("no backend configured");
  },

  upvoteWorkflow: async (publishedId: string): Promise<{ upvoted: boolean; count: number }> => {
    if (BASE) {
      const res = await fetch(`${BASE}/marketplace/workflows/${publishedId}/upvote`, {
        method: "POST",
        credentials: "include",
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error ?? "upvote failed");
      return data;
    }
    return { upvoted: false, count: 0 };
  },
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd frontend && npm run build 2>&1 | tail -20
```

Expected: no TypeScript errors. (Build may fail on other unrelated things — look only at type errors.)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/types.ts frontend/src/lib/api.ts
git commit -m "feat(frontend): add PublishedWorkflow type and marketplace API methods"
```

---

## Task 7: Frontend — Workflows tab in MarketplacePage

**Files:**
- Modify: `frontend/src/components/marketplace/MarketplacePage.tsx`

- [ ] **Step 1: Add tab state, workflow data state, and search effect**

Open `frontend/src/components/marketplace/MarketplacePage.tsx`.

1. Add `PublishedWorkflow` to the import at the top:

```typescript
import type { MarketplaceEndpoint, PublishedWorkflow } from "@/lib/types";
```

2. Add a top-level tab state (after the existing `query` state):

```typescript
const [mainTab, setMainTab] = useState<"endpoints" | "workflows">("endpoints");
const [wfQuery, setWfQuery] = useState("");
const [publishedWorkflows, setPublishedWorkflows] = useState<PublishedWorkflow[]>([]);
const [wfLoading, setWfLoading] = useState(false);
```

3. Add a fetch effect for workflows tab (after the Bazaar `useEffect`):

```typescript
useEffect(() => {
  if (mainTab !== "workflows") return;
  setWfLoading(true);
  marketplaceApi
    .listWorkflows(wfQuery.trim(), 24, 0)
    .then(({ workflows }) => setPublishedWorkflows(workflows ?? []))
    .catch(() => {})
    .finally(() => setWfLoading(false));
}, [mainTab, wfQuery]);
```

4. Add a debounced search effect for wfQuery:

```typescript
const wfSearchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
useEffect(() => {
  if (mainTab !== "workflows") return;
  if (wfSearchTimer.current) clearTimeout(wfSearchTimer.current);
  wfSearchTimer.current = setTimeout(() => {
    setWfLoading(true);
    marketplaceApi
      .listWorkflows(wfQuery.trim(), 24, 0)
      .then(({ workflows }) => setPublishedWorkflows(workflows ?? []))
      .catch(() => {})
      .finally(() => setWfLoading(false));
  }, 350);
  return () => { if (wfSearchTimer.current) clearTimeout(wfSearchTimer.current); };
}, [wfQuery, mainTab]);
```

- [ ] **Step 2: Add `handleImport` and `handleUpvote` callbacks**

Add these functions inside `MarketplacePage` (after `handleAdd`):

```typescript
const handleImport = async (pw: PublishedWorkflow) => {
  try {
    const wf = await marketplaceApi.importWorkflow(pw.id);
    showToast(`"${pw.title}" imported — opening canvas…`);
    setTimeout(() => router.push(`/workflows/${wf.id}`), 800);
  } catch {
    showToast("Import failed — are you signed in?");
  }
};

const handleUpvote = async (pw: PublishedWorkflow) => {
  try {
    const { upvoted, count } = await marketplaceApi.upvoteWorkflow(pw.id);
    setPublishedWorkflows((prev) =>
      prev.map((w) => w.id === pw.id ? { ...w, upvoteCount: count, hasUpvoted: upvoted } : w)
    );
  } catch {
    showToast("Upvote failed — are you signed in?");
  }
};
```

- [ ] **Step 3: Add tab switcher to the hero section**

In the JSX, after the `</div>` closing the search bar div (before the `{/* ── Content ── */}` comment), add:

```tsx
{/* ── Tab switcher ── */}
<div style={{ display: "flex", gap: 0, borderTop: "1px solid var(--border)", marginTop: 24 }}>
  {(["endpoints", "workflows"] as const).map((t) => (
    <button
      key={t}
      onClick={() => setMainTab(t)}
      style={{
        flex: 1, height: 38, border: "none", cursor: "pointer",
        background: mainTab === t ? "var(--bg)" : "transparent",
        borderBottom: mainTab === t ? "2px solid var(--accent)" : "2px solid transparent",
        color: mainTab === t ? "var(--fg)" : "var(--fg-muted)",
        fontSize: 13, fontWeight: 500, fontFamily: "var(--font-sans)",
        textTransform: "capitalize", transition: "color 0.15s",
      }}
    >
      {t}
    </button>
  ))}
</div>
```

- [ ] **Step 4: Wrap existing content in endpoints tab guard and add workflows tab content**

In the content `<div>`, wrap the existing category chips and sections in a conditional:

```tsx
{mainTab === "endpoints" && (
  <>
    {/* existing category chips, static section, GoPlausible section, Bazaar section */}
  </>
)}

{mainTab === "workflows" && (
  <div style={{ paddingTop: 24 }}>
    {/* Search bar */}
    <div style={{ display: "flex", alignItems: "center", gap: 10, maxWidth: 480, marginBottom: 28, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)", padding: "0 14px", height: 40 }}>
      <IconSearch size={14} />
      <input
        value={wfQuery}
        onChange={(e) => setWfQuery(e.target.value)}
        placeholder="Search workflows…"
        style={{ flex: 1, background: "transparent", border: "none", outline: "none", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)" }}
      />
    </div>

    {wfLoading && (
      <div style={{ display: "grid", gridTemplateColumns: "repeat(2, 1fr)", gap: 16 }}>
        {Array.from({ length: 4 }).map((_, i) => <SkeletonCard key={i} />)}
      </div>
    )}

    {!wfLoading && publishedWorkflows.length === 0 && (
      <div style={{ textAlign: "center", padding: "48px 0", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 13 }}>
        {wfQuery ? `No workflows matching "${wfQuery}"` : "No workflows published yet — be the first!"}
      </div>
    )}

    {!wfLoading && publishedWorkflows.length > 0 && (
      <div style={{ display: "grid", gridTemplateColumns: "repeat(2, 1fr)", gap: 16 }}>
        {publishedWorkflows.map((pw) => (
          <WorkflowCard key={pw.id} pw={pw} onImport={() => handleImport(pw)} onUpvote={() => handleUpvote(pw)} />
        ))}
      </div>
    )}
  </div>
)}
```

- [ ] **Step 5: Add `WorkflowCard` component**

Add this component after `EndpointCard` in the file:

```tsx
function WorkflowCard({ pw, onImport, onUpvote }: { pw: PublishedWorkflow; onImport: () => void; onUpvote: () => void }) {
  const [hovered, setHovered] = useState(false);
  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        background: "var(--bg-elev-1)",
        border: `1px solid ${hovered ? "var(--accent-line)" : "var(--border)"}`,
        borderRadius: "var(--r-3)", padding: "18px 20px",
        display: "flex", flexDirection: "column", gap: 12, transition: "border-color 0.15s",
      }}
    >
      <div style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
        <div style={{ width: 40, height: 40, borderRadius: "var(--r-2)", background: "var(--accent-soft)", border: "1px solid var(--accent-line)", display: "flex", alignItems: "center", justifyContent: "center", fontSize: 18, flexShrink: 0 }}>◈</div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)", marginBottom: 3 }}>{pw.title}</div>
          <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>by {pw.creatorEmail}</div>
        </div>
        {pw.feePerRun > 0 && (
          <div style={{ textAlign: "right", flexShrink: 0 }}>
            <div style={{ fontSize: 14, fontWeight: 700, color: "var(--accent)" }}>${pw.feePerRun.toFixed(4)}</div>
            <div style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>per run</div>
          </div>
        )}
      </div>
      {pw.description && (
        <p style={{ margin: 0, fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.6 }}>{pw.description}</p>
      )}
      {pw.tags.length > 0 && (
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
          {pw.tags.map((t) => (
            <span key={t} style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", background: "var(--bg-elev-3)", borderRadius: "var(--r-1)", padding: "2px 7px", border: "1px solid var(--border)" }}>{t}</span>
          ))}
        </div>
      )}
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginTop: 4 }}>
        <div style={{ display: "flex", gap: 14 }}>
          <button onClick={onUpvote} style={{ background: "transparent", border: "none", cursor: "pointer", display: "flex", alignItems: "center", gap: 4, fontFamily: "var(--font-mono)", fontSize: 12, color: pw.hasUpvoted ? "var(--accent)" : "var(--fg-dim)", padding: 0 }}>
            ▲ {pw.upvoteCount}
          </button>
          <span style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>⟳ {pw.runCount}</span>
        </div>
        <button onClick={onImport} style={ghostBtnStyle}>Import to canvas</button>
      </div>
    </div>
  );
}
```

- [ ] **Step 6: Check for TypeScript errors**

```bash
cd frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/marketplace/MarketplacePage.tsx
git commit -m "feat(frontend): add Workflows tab to marketplace with search, upvote, import"
```

---

## Task 8: Frontend — Publish button and modal on canvas

**Files:**
- Create: `frontend/src/components/canvas/PublishModal.tsx`
- Modify: `frontend/src/components/canvas/CanvasPage.tsx`

- [ ] **Step 1: Create `PublishModal.tsx`**

Create `frontend/src/components/canvas/PublishModal.tsx`:

```tsx
"use client";
import { useState } from "react";
import { marketplace as marketplaceApi } from "@/lib/api";

interface PublishModalProps {
  workflowId: string;
  defaultTitle: string;
  onClose: () => void;
  onPublished: (msg: string) => void;
}

export function PublishModal({ workflowId, defaultTitle, onClose, onPublished }: PublishModalProps) {
  const [title, setTitle] = useState(defaultTitle);
  const [description, setDescription] = useState("");
  const [tagsRaw, setTagsRaw] = useState("");
  const [feePerRun, setFeePerRun] = useState("0");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const valid = title.trim().length > 1;

  const handleSubmit = async () => {
    if (!valid || loading) return;
    setLoading(true);
    setError("");
    try {
      const tags = tagsRaw.split(",").map((t) => t.trim()).filter(Boolean);
      await marketplaceApi.publishWorkflow(workflowId, title.trim(), description.trim(), tags, parseFloat(feePerRun) || 0);
      onPublished(`"${title}" published to marketplace`);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "publish failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div
      style={{ position: "fixed", inset: 0, background: "rgba(8,7,12,0.72)", backdropFilter: "blur(4px)", zIndex: 120, display: "flex", alignItems: "center", justifyContent: "center" }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div style={{ width: 480, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-3)", padding: 28, display: "flex", flexDirection: "column", gap: 18 }}>
        <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between" }}>
          <div>
            <div style={{ fontSize: 15, fontWeight: 600, color: "var(--fg)" }}>Publish to Marketplace</div>
            <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", marginTop: 3 }}>
              Earn 1% of run cost each time someone uses your workflow
            </div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--fg-muted)", cursor: "pointer", fontSize: 18, padding: 0, lineHeight: 1 }}>✕</button>
        </div>

        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <Field label="Title" value={title} onChange={setTitle} placeholder="e.g. News Summariser Agent" />
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            <label style={labelStyle}>Description</label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What does this workflow do? What inputs does it expect?"
              style={{ width: "100%", minHeight: 72, padding: "8px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", resize: "vertical", outline: "none", lineHeight: 1.6, boxSizing: "border-box" }}
            />
          </div>
          <Field label="Tags (comma-separated)" value={tagsRaw} onChange={setTagsRaw} placeholder="ai, search, finance" />
          <Field label="Fee per run (USD)" value={feePerRun} onChange={setFeePerRun} placeholder="0.01" type="number" />
        </div>

        {error && <div style={{ fontSize: 12, color: "#f87171", fontFamily: "var(--font-mono)" }}>{error}</div>}

        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <button onClick={onClose} style={ghostBtnStyle}>Cancel</button>
          <button
            onClick={handleSubmit}
            disabled={!valid || loading}
            style={{ ...primaryBtnStyle, opacity: valid && !loading ? 1 : 0.5, cursor: valid && !loading ? "pointer" : "default" }}
          >
            {loading ? "Publishing…" : "Publish"}
          </button>
        </div>
      </div>
    </div>
  );
}

function Field({ label, value, onChange, placeholder, type = "text" }: { label: string; value: string; onChange: (v: string) => void; placeholder: string; type?: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <label style={labelStyle}>{label}</label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        style={{ height: 36, padding: "0 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", outline: "none" }}
      />
    </div>
  );
}

const labelStyle: React.CSSProperties = { fontSize: 12, fontWeight: 500, color: "var(--fg-muted)", fontFamily: "var(--font-sans)" };
const ghostBtnStyle: React.CSSProperties = { height: 32, padding: "0 14px", fontSize: 12, fontWeight: 500, background: "transparent", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)", color: "var(--fg-muted)", cursor: "pointer", fontFamily: "var(--font-sans)" };
const primaryBtnStyle: React.CSSProperties = { height: 32, padding: "0 16px", fontSize: 12, fontWeight: 600, background: "var(--accent)", border: "1px solid var(--accent)", borderRadius: "var(--r-2)", color: "var(--accent-fg)", cursor: "pointer", fontFamily: "var(--font-sans)" };
```

- [ ] **Step 2: Wire PublishModal into CanvasPage**

Open `frontend/src/components/canvas/CanvasPage.tsx`.

1. Add import at top:

```typescript
import { PublishModal } from "./PublishModal";
```

2. Add state inside `CanvasPage` component (after `chatPrompt` state):

```typescript
const [publishOpen, setPublishOpen] = useState(false);
```

3. Pass `onPublish` to `CanvasTopbar`. Update the `<CanvasTopbar>` JSX call:

```tsx
<CanvasTopbar
  workflow={workflow} setWorkflow={setWorkflowNN}
  deployed={deployed} running={running}
  onDeploy={onDeploy} onRun={onRun}
  totalSpend={totalSpend} spend24h={spend24h} saveLabel={saveLabel}
  onBack={() => router.push("/workflows")}
  estimatedCost={estimatedCost}
  onPublish={() => setPublishOpen(true)}
/>
```

4. Add `PublishModal` render at the bottom of the JSX (after `{chatPrompt !== null && ...}`):

```tsx
{publishOpen && workflow && (
  <PublishModal
    workflowId={workflow.id}
    defaultTitle={workflow.name}
    onClose={() => setPublishOpen(false)}
    onPublished={(msg) => { setPublishOpen(false); showToast(msg); }}
  />
)}
```

- [ ] **Step 3: Add `onPublish` prop and button to `CanvasTopbar`**

In `CanvasPage.tsx`, update the `CanvasTopbar` function signature to accept `onPublish`:

```typescript
function CanvasTopbar({ workflow, setWorkflow, deployed, running, onDeploy, onRun, totalSpend, spend24h, saveLabel, onBack, estimatedCost, onPublish }: {
  workflow: Workflow;
  setWorkflow: React.Dispatch<React.SetStateAction<Workflow>>;
  deployed: boolean; running: boolean;
  onDeploy: () => void; onRun: () => void;
  totalSpend: string; spend24h: string; saveLabel: string;
  onBack: () => void;
  estimatedCost: { usd: number; algo: number };
  onPublish: () => void;
})
```

Add a "Publish" button in the topbar JSX — replace the existing `<button style={{ ...ghostBtnSm, flexShrink: 0 }}>Share</button>` line with:

```tsx
<button onClick={onPublish} style={{ ...ghostBtnSm, flexShrink: 0 }}>Publish</button>
```

- [ ] **Step 4: Check TypeScript**

```bash
cd frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/canvas/PublishModal.tsx \
        frontend/src/components/canvas/CanvasPage.tsx
git commit -m "feat(frontend): add Publish button and modal to canvas topbar"
```

---

## Task 9: Frontend — Saved workflows in PalettePanel

**Files:**
- Modify: `frontend/src/components/canvas/CanvasPage.tsx`
- Modify: `frontend/src/components/canvas/PalettePanel.tsx`

- [ ] **Step 1: Fetch saved workflows in CanvasPage**

Open `frontend/src/components/canvas/CanvasPage.tsx`.

1. Add import:

```typescript
import { Workflow } from "@/lib/types";
```

(Already imported — skip if present.)

2. Add state inside `CanvasPage` (after `publishOpen`):

```typescript
const [savedWorkflows, setSavedWorkflows] = useState<Workflow[]>([]);
```

3. Add a fetch after the existing `refreshSpend` effect:

```typescript
useEffect(() => {
  workflowsApi.list()
    .then((all) => setSavedWorkflows(all.filter((wf) => !!wf.sourcePublishedId)))
    .catch(() => {});
}, []);
```

4. Pass `savedWorkflows` and `onOpenSaved` to `PalettePanel`:

```tsx
<PalettePanel
  onDragNodeStart={onDragNodeStart}
  savedWorkflows={savedWorkflows}
  onOpenSaved={(id) => router.push(`/workflows/${id}`)}
/>
```

- [ ] **Step 2: Add "Saved" tab to PalettePanel**

Open `frontend/src/components/canvas/PalettePanel.tsx`.

1. Update `PalettePanelProps`:

```typescript
interface PalettePanelProps {
  onDragNodeStart: (e: React.DragEvent, meta: Partial<WorkflowNode>) => void;
  savedWorkflows?: Array<{ id: string; name: string; sourcePublishedId?: string }>;
  onOpenSaved?: (id: string) => void;
}
```

2. Update the `PalettePanel` function signature to destructure new props:

```typescript
export function PalettePanel({ onDragNodeStart, savedWorkflows = [], onOpenSaved }: PalettePanelProps) {
```

3. Update the `tab` state type. The existing tabs use `typeof PALETTE_TABS[number]["id"]`. Add `"saved"` as a separate option:

```typescript
type TabId = typeof PALETTE_TABS[number]["id"] | "saved";
```

4. In the tab row JSX, add a "Saved" button at the end of the tab grid:

```tsx
<button
  key="saved"
  onClick={() => setTab("saved" as TabId)}
  style={{
    flex: "1 0 calc(33% - 4px)", height: 26, border: "none", cursor: "pointer",
    background: tab === "saved" ? "var(--bg-elev-3)" : "transparent",
    color: tab === "saved" ? "var(--accent)" : "var(--fg-muted)",
    borderRadius: 5, fontSize: 11, fontWeight: 500, fontFamily: "var(--font-sans)",
  }}
>
  Saved
</button>
```

5. After the existing `{filtered.length === 0 && ...}` empty state block, add a conditional render for the Saved tab content. The current "overflow scrollable items" div should only render for non-saved tabs. Wrap it:

Replace the current items render area with:

```tsx
{tab === "saved" ? (
  <div style={{ padding: "4px 10px", display: "flex", flexDirection: "column", gap: 6, overflowY: "auto", flex: 1 }}>
    {savedWorkflows.length === 0 && (
      <div style={{ padding: "24px 8px", fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)", textAlign: "center", lineHeight: 1.5 }}>
        no saved workflows yet<br />
        import from marketplace →
      </div>
    )}
    {savedWorkflows.map((wf) => (
      <button
        key={wf.id}
        onClick={() => onOpenSaved?.(wf.id)}
        style={{ display: "flex", alignItems: "center", gap: 10, padding: "8px 10px", background: "var(--bg-elev-2)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", cursor: "pointer", width: "100%", textAlign: "left" }}
        onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.borderColor = "var(--accent-line)"; }}
        onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.borderColor = "var(--border)"; }}
      >
        <span style={{ width: 22, height: 22, borderRadius: 6, background: "var(--accent-soft)", color: "var(--accent)", display: "inline-flex", alignItems: "center", justifyContent: "center", fontSize: 12, flexShrink: 0, fontWeight: 600 }}>◈</span>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 12, fontWeight: 500, color: "var(--fg)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{wf.name}</div>
          <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-muted)" }}>from marketplace</div>
        </div>
      </button>
    ))}
  </div>
) : (
  <div style={{ padding: "4px 10px", display: "flex", flexDirection: "column", gap: 6, overflowY: "auto", flex: 1 }}>
    {/* Create row */}
    <CreateRow meta={CREATE_META[tab as keyof typeof CREATE_META]} onDragStart={(e) => onDragNodeStart(e, CREATE_META[tab as keyof typeof CREATE_META])} isX402={tab === "x402"} />

    {filtered.map((it, i) => (
      <DraggableRow key={i}
        icon={(it.icon ?? "") as string}
        title={(it.name ?? it.label ?? "") as string}
        sub={(it.sub ?? "") as string}
        dotColor={tabDef.dotColor}
        onDragStart={(e) => onDragNodeStart(e, it)}
      />
    ))}

    {filtered.length === 0 && (
      <div style={{ padding: "24px 8px", fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)", textAlign: "center" }}>
        no presets — drag the + above to build your own
      </div>
    )}
  </div>
)}
```

Note: the existing `tabDef` and `items`/`mapped`/`filtered` variables are only used in the non-saved branch, so guard them:

At the top of `PalettePanel`, after the `setTab` state:

```typescript
const tabDef = tab !== "saved" ? PALETTE_TABS.find((t) => t.id === tab)! : PALETTE_TABS[0];
const items = tab !== "saved" ? (tabDef.items() as unknown[]) : [];
const mapped = tab !== "saved" ? (items as Parameters<typeof tabDef.map>[0][]).map(tabDef.map as (it: Parameters<typeof tabDef.map>[0]) => Partial<WorkflowNode>) : [];
const filtered = mapped.filter((i) =>
  ((i.name ?? i.label ?? "") as string).toLowerCase().includes(q.toLowerCase()) ||
  (i.sub ?? "").toLowerCase().includes(q.toLowerCase())
);
```

Also hide the search input when on the Saved tab (replace the search input conditional to show only when `tab !== "saved"`):

```tsx
{tab !== "saved" && (
  <div style={{ padding: "6px 14px 10px" }}>
    <div style={{ position: "relative" }}>
      <span style={{ position: "absolute", left: 10, top: 10, color: "var(--fg-dim)" }}><IconSearch size={12} /></span>
      <input
        style={{ height: 32, paddingLeft: 30, paddingRight: 10, width: "100%", background: "var(--bg-elev-2)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontFamily: "var(--font-sans)", fontSize: 12, outline: "none" }}
        placeholder={`search ${tabDef.label.toLowerCase()}…`}
        value={q} onChange={(e) => setQ(e.target.value)}
      />
    </div>
  </div>
)}
```

- [ ] **Step 3: Check TypeScript**

```bash
cd frontend && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/canvas/CanvasPage.tsx \
        frontend/src/components/canvas/PalettePanel.tsx
git commit -m "feat(frontend): add Saved workflows tab to palette panel with marketplace imports"
```

---

## Final Verification

- [ ] **Step 1: Full backend build and test**

```bash
cd backend && go build ./... && go test ./... 2>&1 | tail -30
```

Expected: no `FAIL` lines.

- [ ] **Step 2: Frontend build**

```bash
cd frontend && npm run build 2>&1 | tail -20
```

Expected: no TypeScript errors. (Next.js may warn about image domains or similar — those are unrelated.)

- [ ] **Step 3: Smoke test the flow (if backend is running)**

```bash
# Start backend
docker-compose up -d

# Start frontend
cd frontend && npm run dev
```

Open `http://localhost:3000`:
1. Go to `/workflows`, open a workflow
2. Click "Publish" in the topbar — modal appears with title prefilled
3. Fill in description, click Publish — toast shows success
4. Go to `/marketplace` → Workflows tab — the published workflow appears
5. Click "Import to canvas" — new workflow opens in canvas
6. In the canvas library, click "Saved" tab — the imported workflow appears
7. Click the saved workflow entry — navigates to its canvas

- [ ] **Step 4: Final commit if everything passes**

```bash
git add -p  # stage any unstaged final tweaks
git commit -m "feat: workflow marketplace — publish, discover, import, saved library, creator cut"
```
