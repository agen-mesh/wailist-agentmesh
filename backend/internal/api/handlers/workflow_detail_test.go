package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/models"
)

func newTestUser(t *testing.T, d *handlers.Deps) string {
	t.Helper()
	u, err := d.Store.CreateUser(t.Context(), fmt.Sprintf("upcoming-%d@example.com", time.Now().UnixNano()), "hash")
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// scheduledWorkflow makes a workflow for userID, deployed unless draft is
// set, and gives it cron (when not empty) firing next at next.
func scheduledWorkflow(t *testing.T, d *handlers.Deps, userID, name, cron string, next time.Time, draft bool) string {
	t.Helper()
	wf, err := d.Store.CreateWorkflow(t.Context(), name, userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })
	if !draft {
		if err := d.Store.SetWorkflowDeployed(t.Context(), wf.ID, "https://example.com/run", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if cron != "" {
		if err := d.Store.SetWorkflowSchedule(t.Context(), wf.ID, cron, next); err != nil {
			t.Fatal(err)
		}
	}
	return wf.ID
}

func TestGetWorkflowCarriesItsRunFigures(t *testing.T) {
	d := testDeps(t)
	user := newTestUser(t, d)
	id := scheduledWorkflow(t, d, user, "Counted", "", time.Time{}, false)
	for i := 0; i < 3; i++ {
		if _, err := d.Store.CreateRun(t.Context(), id, "manual", []byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/workflows/"+id, nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, user))
	req = withURLParam(req, "id", id)
	w := httptest.NewRecorder()
	d.GetWorkflow(w, req)
	var body struct {
		Runs      int        `json:"runs"`
		TotalRuns int        `json:"totalRuns"`
		LastRunAt *time.Time `json:"lastRunAt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TotalRuns != 3 || body.Runs != 3 || body.LastRunAt == nil {
		t.Fatalf("got totalRuns=%d runs=%d lastRunAt=%v, want 3, 3, set", body.TotalRuns, body.Runs, body.LastRunAt)
	}
}

// A workflow that has never run says so with a zero; leaving the field out
// made the app show a dash, as if the count had failed.
func TestGetWorkflowSendsAZeroRunCount(t *testing.T) {
	d := testDeps(t)
	user := newTestUser(t, d)
	id := scheduledWorkflow(t, d, user, "Never run", "", time.Time{}, false)
	req := httptest.NewRequest(http.MethodGet, "/workflows/"+id, nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, user))
	req = withURLParam(req, "id", id)
	w := httptest.NewRecorder()
	d.GetWorkflow(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got, ok := body["totalRuns"]; !ok || got != float64(0) {
		t.Fatalf("totalRuns = %v (present %v), want 0", got, ok)
	}
}

// The detail endpoint marks its 30-day figures unavailable when the
// aggregation behind them fails, because the zero values it falls back to
// are indistinguishable from a workflow that had no runs and no spend.
//
// This covers the success half of that contract: a healthy read must NOT set
// the flag, or every workflow's figures would show as dashes. The failure
// half is covered separately through the handler's stats-loader seam.
func TestGetWorkflowLeavesStatsAvailableWhenTheyAggregate(t *testing.T) {
	d := testDeps(t)
	user := newTestUser(t, d)
	id := scheduledWorkflow(t, d, user, "Aggregated", "", time.Time{}, false)
	if _, err := d.Store.CreateRun(t.Context(), id, "manual", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/workflows/"+id, nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, user))
	req = withURLParam(req, "id", id)
	w := httptest.NewRecorder()
	d.GetWorkflow(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got, ok := body["statsUnavailable"]; ok {
		t.Fatalf("statsUnavailable = %v, want it omitted on a successful read", got)
	}
	if body["runs"] != float64(1) {
		t.Fatalf("runs = %v, want 1", body["runs"])
	}
}

func TestGetWorkflowMarksStatsUnavailableWhenAggregationFails(t *testing.T) {
	d := testDeps(t)
	user := newTestUser(t, d)
	id := scheduledWorkflow(t, d, user, "Unavailable stats", "", time.Time{}, false)
	d.WorkflowStatsLoader = func(context.Context, string, *models.Workflow) error {
		return errors.New("stats database unavailable")
	}
	req := httptest.NewRequest(http.MethodGet, "/workflows/"+id, nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, user))
	req = withURLParam(req, "id", id)
	w := httptest.NewRecorder()
	d.GetWorkflow(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		StatsUnavailable bool `json:"statsUnavailable"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.StatsUnavailable {
		t.Fatal("statsUnavailable = false, want true after aggregation failure")
	}
}

func TestUpdateWorkflowDescription(t *testing.T) {
	d := testDeps(t)
	user := newTestUser(t, d)
	id := scheduledWorkflow(t, d, user, "Described", "", time.Time{}, true)
	put := func(body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/workflows/"+id, bytes.NewReader(raw))
		req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, user))
		req = withURLParam(req, "id", id)
		w := httptest.NewRecorder()
		d.UpdateWorkflow(w, req)
		return w
	}
	description := func() string {
		wf, err := d.Store.GetWorkflow(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		return wf.Description
	}

	if w := put(map[string]any{"name": "Described", "description": "  Sorts support email.  "}); w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body.String())
	}
	if got := description(); got != "Sorts support email." {
		t.Fatalf("description = %q, want it trimmed", got)
	}
	// The editor's save sends no description and must leave it alone.
	if w := put(map[string]any{"name": "Described"}); w.Code != http.StatusOK {
		t.Fatalf("graph save: %d", w.Code)
	}
	if got := description(); got != "Sorts support email." {
		t.Fatalf("description = %q after a save without one", got)
	}
	if w := put(map[string]any{"name": "Described", "description": strings.Repeat("a", 2001)}); w.Code != http.StatusBadRequest {
		t.Fatalf("too long: status %d, want 400", w.Code)
	}
	if got := description(); got != "Sorts support email." {
		t.Fatalf("a rejected description changed it to %q", got)
	}
}
