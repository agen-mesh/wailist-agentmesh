package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/api/handlers"
)

type upcomingPage struct {
	Upcoming []struct {
		WorkflowID   string    `json:"workflowId"`
		WorkflowName string    `json:"workflowName"`
		At           time.Time `json:"at"`
		Cron         string    `json:"cron"`
	} `json:"upcoming"`
}

func getUpcoming(t *testing.T, d *handlers.Deps, userID, query string) (*httptest.ResponseRecorder, upcomingPage) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/schedules/upcoming"+query, nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, userID))
	w := httptest.NewRecorder()
	d.ListUpcomingRuns(w, req)
	var page upcomingPage
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
	}
	return w, page
}

func TestListUpcomingRunsInterleavesSchedulesSoonestFirst(t *testing.T) {
	d := testDeps(t)
	user := newTestUser(t, d)
	base := time.Now().UTC().Truncate(time.Hour).Add(3 * time.Hour)
	scheduledWorkflow(t, d, user, "On the hour", "0 * * * *", base, false)
	scheduledWorkflow(t, d, user, "On the half hour", "30 * * * *", base.Add(30*time.Minute), false)

	w, page := getUpcoming(t, d, user, "?per=2")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var got []string
	for _, u := range page.Upcoming {
		got = append(got, fmt.Sprintf("%s@%s", u.WorkflowName, u.At.Sub(base)))
	}
	want := []string{"On the hour@0s", "On the half hour@30m0s", "On the hour@1h0m0s", "On the half hour@1h30m0s"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("upcoming = %v, want %v", got, want)
	}

	_, page = getUpcoming(t, d, user, "?per=2&limit=3")
	if len(page.Upcoming) != 3 {
		t.Fatalf("limit=3 returned %d", len(page.Upcoming))
	}
}

func TestListUpcomingRunsLeavesOutWhatWillNotFire(t *testing.T) {
	d := testDeps(t)
	user := newTestUser(t, d)
	other := newTestUser(t, d)
	next := time.Now().UTC().Truncate(time.Hour).Add(2 * time.Hour)
	fires := scheduledWorkflow(t, d, user, "Fires", "0 * * * *", next, false)
	scheduledWorkflow(t, d, user, "Draft with a schedule", "0 * * * *", next, true)
	scheduledWorkflow(t, d, user, "Deployed, no schedule", "", next, false)
	scheduledWorkflow(t, d, user, "Unparseable", "not a cron", next, false)
	scheduledWorkflow(t, d, other, "Another user's", "0 * * * *", next, false)

	_, page := getUpcoming(t, d, user, "?per=1")
	if len(page.Upcoming) != 1 || page.Upcoming[0].WorkflowID != fires {
		t.Fatalf("upcoming = %+v, want only %q", page.Upcoming, fires)
	}
	if !page.Upcoming[0].At.Equal(next) {
		t.Errorf("first occurrence = %v, want the stored next run %v", page.Upcoming[0].At, next)
	}
}

func TestListUpcomingRunsRejectsABadLimit(t *testing.T) {
	d := testDeps(t)
	for _, q := range []string{"?limit=0", "?limit=x", "?per=-1"} {
		if w, _ := getUpcoming(t, d, "dev", q); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, w.Code)
		}
	}
}

// After an outage the stored next run is in the past. The scheduler fires it
// once and then jumps past every missed tick, so only that one overdue row may
// be listed before the list moves on to the future.
func TestListUpcomingRunsSkipsTicksMissedDuringAnOutage(t *testing.T) {
	d := testDeps(t)
	user := newTestUser(t, d)
	overdue := time.Now().UTC().Truncate(time.Hour).Add(-48 * time.Hour)
	scheduledWorkflow(t, d, user, "Hourly", "0 * * * *", overdue, false)

	_, page := getUpcoming(t, d, user, "?per=3")
	if len(page.Upcoming) != 3 {
		t.Fatalf("got %d upcoming, want 3", len(page.Upcoming))
	}
	if !page.Upcoming[0].At.Equal(overdue) {
		t.Errorf("first = %v, want the overdue stored run %v", page.Upcoming[0].At, overdue)
	}
	now := time.Now()
	for _, u := range page.Upcoming[1:] {
		if !u.At.After(now) {
			t.Errorf("listed %v, which is in the past and will never fire", u.At)
		}
	}
	if gap := page.Upcoming[2].At.Sub(page.Upcoming[1].At); gap != time.Hour {
		t.Errorf("later runs are %v apart, want 1h", gap)
	}
}

// A workflow's own screen asks for its runs alone. Filtering the global top
// 50 instead lost them once enough other schedules came sooner.
func TestListUpcomingRunsForOneWorkflow(t *testing.T) {
	d := testDeps(t)
	user := newTestUser(t, d)
	soon := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	for i := 0; i < 20; i++ {
		scheduledWorkflow(t, d, user, fmt.Sprintf("Busy %d", i), "0 * * * *", soon, false)
	}
	later := scheduledWorkflow(t, d, user, "Daily", "0 9 * * *", soon.Add(24*time.Hour), false)

	_, page := getUpcoming(t, d, user, "?limit=50&per=3&workflowId="+later)
	if len(page.Upcoming) != 3 {
		t.Fatalf("got %d upcoming for the workflow, want 3", len(page.Upcoming))
	}
	for _, u := range page.Upcoming {
		if u.WorkflowID != later {
			t.Fatalf("listed %q, want only %q", u.WorkflowID, later)
		}
	}
}
