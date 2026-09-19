package db

import (
	"context"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// The workflows list carries each workflow's 30-day run count, spend and
// newest run. Nothing tested those figures before the phone list began
// sorting on them.
func TestListWorkflowsAttachesRunsSpendAndLastRun(t *testing.T) {
	s := runsListStore(t)
	ctx := context.Background()
	user := runsListUser(t, s)
	busy := runsListWorkflow(t, s, user, "Busy")
	idle := runsListWorkflow(t, s, user, "Idle")

	now := time.Now().UTC().Truncate(time.Microsecond)
	newest := now.Add(-1 * time.Hour)
	runAt(t, s, busy, now.Add(-2*time.Hour))
	runID := runAt(t, s, busy, newest)
	// Outside the window: neither counted nor taken as the newest run.
	runAt(t, s, busy, now.Add(-40*24*time.Hour))

	if _, err := s.pool.Exec(ctx, `UPDATE users SET credit_balance_usd_micros = 10000000 WHERE id = $1`, user); err != nil {
		t.Fatal(err)
	}
	if err := s.DebitCredits(ctx, user, 1_500_000, models.DebitKindByokFlatFee, busy, runID, "n1"); err != nil {
		t.Fatal(err)
	}

	wfs, err := s.ListWorkflows(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]int{}
	for i, w := range wfs {
		byID[w.ID] = i
	}

	b := wfs[byID[busy]]
	if b.Runs != 2 {
		t.Errorf("busy runs = %d, want 2", b.Runs)
	}
	if b.Spend != "1.50" {
		t.Errorf("busy spend = %q, want 1.50", b.Spend)
	}
	if b.LastRunAt == nil || !b.LastRunAt.Equal(newest) {
		t.Errorf("busy lastRunAt = %v, want %v", b.LastRunAt, newest)
	}

	i := wfs[byID[idle]]
	if i.Runs != 0 || i.Spend != "" || i.LastRunAt != nil {
		t.Errorf("idle = runs %d spend %q lastRunAt %v, want 0, empty, nil", i.Runs, i.Spend, i.LastRunAt)
	}
}

func TestSetWorkflowDescriptionAndCountRuns(t *testing.T) {
	s := runsListStore(t)
	ctx := context.Background()
	user := runsListUser(t, s)
	wf := runsListWorkflow(t, s, user, "Described")

	description := func() string {
		t.Helper()
		got, err := s.GetWorkflow(ctx, wf)
		if err != nil {
			t.Fatal(err)
		}
		return got.Description
	}
	if got := description(); got != "" {
		t.Fatalf("new workflow description = %q, want empty", got)
	}
	if err := s.SetWorkflowDescription(ctx, wf, "Sorts support email."); err != nil {
		t.Fatal(err)
	}
	if got := description(); got != "Sorts support email." {
		t.Fatalf("description = %q after set", got)
	}
	// A graph save through UpdateWorkflow must not touch it.
	if _, err := s.UpdateWorkflow(ctx, wf, "Described", models.WorkflowGraph{}); err != nil {
		t.Fatal(err)
	}
	if got := description(); got != "Sorts support email." {
		t.Fatalf("description = %q after a graph save", got)
	}
	if err := s.SetWorkflowDescription(ctx, wf, ""); err != nil {
		t.Fatal(err)
	}
	if got := description(); got != "" {
		t.Fatalf("description = %q after clearing", got)
	}

	now := time.Now().UTC()
	runAt(t, s, wf, now.Add(-time.Hour))
	// Older than the list's 30-day window, but still a run.
	runAt(t, s, wf, now.Add(-90*24*time.Hour))
	if n, err := s.CountRuns(ctx, wf); err != nil || n != 2 {
		t.Fatalf("CountRuns = %d, %v; want 2", n, err)
	}
}
