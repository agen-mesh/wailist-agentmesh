package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

const (
	defaultRunHistoryLimit = 20
	maxRunHistoryLimit     = 50
)

// ListWorkflowRuns serves GET /workflows/{id}/runs: one workflow's runs,
// newest first, a page at a time.
func (d *Deps) ListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)
	workflowID := chi.URLParam(r, "id")

	limit, cursor, ok := parseRunHistoryQuery(w, r)
	if !ok {
		return
	}

	// The same answer for a workflow that does not exist and one that belongs
	// to someone else, as GetRun gives, so the response confirms nothing.
	wf, err := d.Store.GetWorkflow(ctx, workflowID)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}

	runs, err := d.Store.ListWorkflowRuns(ctx, userID, workflowID, cursor, limit+1)
	if err != nil {
		log.Printf("list workflow runs: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	respond.JSON(w, http.StatusOK, runPage(runs, limit))
}

// ListRecentRuns serves GET /runs: the user's newest runs across their own
// workflows, a page at a time.
func (d *Deps) ListRecentRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	limit, cursor, ok := parseRunHistoryQuery(w, r)
	if !ok {
		return
	}

	runs, err := d.Store.ListRecentRuns(ctx, userID, cursor, limit+1)
	if err != nil {
		log.Printf("list recent runs: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	respond.JSON(w, http.StatusOK, runPage(runs, limit))
}

// parseRunHistoryQuery reads ?limit and ?cursor, answering 400 itself when
// either is malformed. The limit defaults to 20 and is clamped to 50.
func parseRunHistoryQuery(w http.ResponseWriter, r *http.Request) (int, *db.RunCursor, bool) {
	q := r.URL.Query()

	limit := defaultRunHistoryLimit
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			respond.Error(w, http.StatusBadRequest, "limit must be a positive number")
			return 0, nil, false
		}
		limit = min(v, maxRunHistoryLimit)
	}

	var cursor *db.RunCursor
	if raw := q.Get("cursor"); raw != "" {
		c, err := db.ParseRunCursor(raw)
		if err != nil {
			respond.Error(w, http.StatusBadRequest, "invalid cursor")
			return 0, nil, false
		}
		cursor = &c
	}
	return limit, cursor, true
}

// runPage turns limit+1 fetched rows into a page: the extra row only says
// whether there is a next page, and the cursor points at the last row shown.
func runPage(runs []models.RunSummary, limit int) models.RunPage {
	page := models.RunPage{Runs: runs}
	if len(runs) > limit {
		page.Runs = runs[:limit]
		last := page.Runs[limit-1]
		next := db.RunCursor{StartedAt: last.StartedAt, ID: last.ID}.Encode()
		page.NextCursor = &next
	}
	return page
}
