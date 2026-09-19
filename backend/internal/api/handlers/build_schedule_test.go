package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/engine/nodes"
)

// scheduleBuildServer is a model that sets a daily 09:00 schedule, then replies.
func scheduleBuildServer(t *testing.T) {
	t.Helper()
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn++
		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"set_schedule","args":{"cadence":"daily","time":"09:00"}}}]}}]}`)
			return
		}
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"Scheduled for 9 am every day. Deploy it to start."}]}}]}`)
	}))
	t.Cleanup(srv.Close)
	nodes.SetGeminiBaseURL(srv.URL)
	t.Cleanup(func() { nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com") })
}

// "Every morning at 9" from someone in India must be saved as 03:30 UTC, on
// the draft the builder is working on, without a trip to the Workflows page.
func TestBuildWorkflowSavesTheScheduleInTheUsersTimezone(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()
	scheduleBuildServer(t)

	user, err := d.Store.CreateUser(ctx, "wf-sched-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Morning brief", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	body, _ := json.Marshal(map[string]string{"message": "every morning at 9", "timeZone": "Asia/Kolkata"})
	req := withURLParam(httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/build", bytes.NewReader(body)), "id", wf.ID)
	rec := httptest.NewRecorder()
	d.BuildWorkflow(rec, withUser(req, user.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("build got %d: %s", rec.Code, rec.Body.String())
	}

	saved, err := d.Store.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ScheduleCron == nil || *saved.ScheduleCron != "30 3 * * *" {
		t.Fatalf("saved schedule = %v, want 30 3 * * *", saved.ScheduleCron)
	}
	// The response carries it too, so the canvas does not need a reload.
	var resp struct {
		Workflow struct {
			ScheduleCron string `json:"scheduleCron"`
		} `json:"workflow"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Workflow.ScheduleCron != "30 3 * * *" {
		t.Errorf("response scheduleCron = %q", resp.Workflow.ScheduleCron)
	}
}

// A schedule saved on a draft has a next run computed back then. Deploying
// later must count from the deploy, not fire a stale run the instant the
// workflow goes live.
func TestDeployRecomputesAStaleNextRun(t *testing.T) {
	d := testDeps(t)
	d.BaseURL = "http://localhost:8080"
	ctx := context.Background()

	wf, err := d.Store.CreateWorkflow(ctx, "Deploy later", "dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })
	stale := time.Now().UTC().Add(-72 * time.Hour)
	if err := d.Store.SetWorkflowSchedule(ctx, wf.ID, "30 3 * * *", stale); err != nil {
		t.Fatal(err)
	}

	req := withURLParam(httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/deploy", nil), "id", wf.ID)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "dev"))
	rec := httptest.NewRecorder()
	d.Deploy(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("deploy got %d: %s", rec.Code, rec.Body.String())
	}

	saved, err := d.Store.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ScheduleNextRunAt == nil || !saved.ScheduleNextRunAt.After(time.Now()) {
		t.Fatalf("next run = %v, want a time after the deploy", saved.ScheduleNextRunAt)
	}
	if got := saved.ScheduleNextRunAt.UTC(); got.Hour() != 3 || got.Minute() != 30 {
		t.Errorf("next run = %v, want 03:30 UTC", got)
	}
	if saved.ScheduleCron == nil || *saved.ScheduleCron != "30 3 * * *" {
		t.Errorf("deploy changed the schedule itself: %v", saved.ScheduleCron)
	}
}

func TestBuildWorkflowReplyMentionsNothingExtraWhenTheScheduleSaves(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()
	scheduleBuildServer(t)

	user, err := d.Store.CreateUser(ctx, "wf-sched-ok-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Morning brief", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	rec := buildWorkflowReq(d, wf.ID, user.ID, "every morning at 9")
	if rec.Code != http.StatusOK {
		t.Fatalf("build got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "could not be saved") {
		t.Errorf("a saved schedule was reported as failed: %s", rec.Body.String())
	}
}

// Deploy's recompute is conditional: a schedule removed after Deploy read
// the workflow is not written back.
func TestRescheduleLeavesAChangedScheduleAlone(t *testing.T) {
	d := testDeps(t)
	ctx := context.Background()
	wf, err := d.Store.CreateWorkflow(ctx, "Changed", "dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })
	if err := d.Store.SetWorkflowSchedule(ctx, wf.ID, "0 9 * * *", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := d.Store.ClearWorkflowSchedule(ctx, wf.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := d.Store.RescheduleWorkflowNextRun(ctx, wf.ID, "0 9 * * *", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Error("a removed schedule was updated")
	}
	saved, _ := d.Store.GetWorkflow(ctx, wf.ID)
	if saved.ScheduleCron != nil || saved.ScheduleNextRunAt != nil {
		t.Errorf("the removed schedule came back: %v %v", saved.ScheduleCron, saved.ScheduleNextRunAt)
	}
}
