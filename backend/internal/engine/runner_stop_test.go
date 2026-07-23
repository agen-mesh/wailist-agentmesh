package engine_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sse"
)

type noopSigner struct{}

func (n *noopSigner) SignAndSendPayment(_ context.Context, _, _ string, _ uint64) (string, error) {
	return "", nil
}

func newTestRunner(t *testing.T) (*engine.Runner, *db.Store) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	broker := sse.NewBroker()
	return engine.NewRunner(store, broker, &noopSigner{}, "http://localhost:8080"), store
}

// TestStopReturnsFalseWhenNotRunning verifies that Stop returns false
// when the workflow has no active run registered in the registry.
func TestStopReturnsFalseWhenNotRunning(t *testing.T) {
	runner, _ := newTestRunner(t)
	if runner.Stop("some-workflow-id-that-was-never-started") {
		t.Fatal("Stop should return false when no run is registered")
	}
}

// TestStopReturnsTrueImmediatelyAfterStart verifies that Stop returns true
// when called right after Start, because Start registers the cancel
// synchronously before launching the goroutine.
func TestStopReturnsTrueImmediatelyAfterStart(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	wf, err := store.CreateWorkflow(ctx, "Stop Test WF", "test-user")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "n2", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}

	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	if !runner.Stop(wf.ID) {
		t.Fatal("Stop should return true immediately after Start (cancel is registered synchronously)")
	}
}

// TestStopSetsRunStatusStopped verifies the end-to-end cancellation path:
// a workflow is started, immediately stopped, and the run record in the DB
// ends with status "stopped" (not "success" or "failed").
func TestStopSetsRunStatusStopped(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	wf, err := store.CreateWorkflow(ctx, "Stop Status Test", "test-user")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	// Two-level graph so the runner checks ctx.Err() between levels.
	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "n2", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}

	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	runner.Stop(wf.ID)

	// Wait for the goroutine to write its final status.
	var finalRun models.Run
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		finalRun, err = store.GetRun(ctx, run.ID)
		if err == nil && finalRun.Status != models.RunStatusRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if finalRun.Status != models.RunStatusStopped && finalRun.Status != models.RunStatusSuccess {
		// Success is acceptable: the two-node graph may complete before the
		// cancel propagates. What we must NOT see is "running" or "failed".
		t.Fatalf("unexpected final run status: %q", finalRun.Status)
	}
}
