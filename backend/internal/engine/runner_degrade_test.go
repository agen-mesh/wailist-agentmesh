package engine_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sse"
)

// TestReadNodeDegradesAndRunContinues: a GET that answers 500 on every
// attempt used to stop the run. It now leaves an error payload behind and the
// run reaches its end node.
func TestReadNodeDegradesAndRunContinues(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	wf, err := store.CreateWorkflow(ctx, "Degrade Test", fundedTestUser(t, store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "n2", Type: models.NodeTypeTool, Template: "http", Name: "Fetch", URL: srv.URL, Method: "GET"},
			{ID: "n3", Type: models.NodeTypeEnd, Template: "done"},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "n2", To: "n3", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Run(ctx, wf, run, 0)

	finalRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalRun.Status != models.RunStatusSuccess {
		t.Fatalf("run status = %q, want %q", finalRun.Status, models.RunStatusSuccess)
	}

	logs, err := store.GetRunLogs(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawDegraded, sawEnd bool
	for _, l := range logs {
		if l.NodeID == "n2" && l.Status == models.LogStatusDegraded {
			sawDegraded = true
			out, ok := l.Output.(map[string]any)
			if !ok {
				t.Fatalf("degraded output is %T, want an object carrying the error", l.Output)
			}
			if out["degraded"] != true {
				t.Errorf("degraded output does not mark itself degraded: %+v", out)
			}
			if s, _ := out["error"].(string); s == "" {
				t.Errorf("degraded output carries no error text: %+v", out)
			}
		}
		if l.NodeID == "n3" && l.Status == models.LogStatusSuccess {
			sawEnd = true
		}
	}
	if !sawDegraded {
		t.Error("no degraded log row for the failed read node")
	}
	if !sawEnd {
		t.Error("the run did not reach its end node")
	}
}

// TestActionNodeStillFailsTheRun is the other half of the contract: a send
// that fails must still stop the run.
func TestActionNodeStillFailsTheRun(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	wf, err := store.CreateWorkflow(ctx, "Action Fail Test", fundedTestUser(t, store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			// A POST is an action even on the http template.
			{ID: "n2", Type: models.NodeTypeTool, Template: "http", Name: "Send", URL: srv.URL, Method: "POST"},
			{ID: "n3", Type: models.NodeTypeEnd, Template: "done"},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "n2", To: "n3", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Run(ctx, wf, run, 0)

	finalRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalRun.Status != models.RunStatusFailed {
		t.Fatalf("run status = %q, want %q", finalRun.Status, models.RunStatusFailed)
	}
}

// TestDegradedNodeIsNotDeadLettered: a dead-letter row means "this run failed
// here and can be resumed from here". A degraded run finishes successfully and
// MarkRunRunning only claims a failed or stopped run, so such a row would
// promise a resume that is refused. The degraded run_logs row is the audit
// record instead.
func TestDegradedNodeIsNotDeadLettered(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	wf, err := store.CreateWorkflow(ctx, "Degrade Dead Letter Test", fundedTestUser(t, store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "n2", Type: models.NodeTypeTool, Template: "http", Name: "Fetch", URL: srv.URL, Method: "GET"},
			{ID: "n3", Type: models.NodeTypeEnd, Template: "done"},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "n2", To: "n3", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Run(ctx, wf, run, 0)

	dls, err := store.GetDeadLetterRuns(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dls) != 0 {
		t.Fatalf("dead-letter rows = %d, want 0: a degraded run cannot be resumed, so the row would offer a resume that MarkRunRunning refuses", len(dls))
	}

	// The failure must still be recorded somewhere an operator can find it.
	logs, err := store.GetRunLogs(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range logs {
		if l.NodeID == "n2" && l.Status == models.LogStatusDegraded {
			found = true
			out, _ := l.Output.(map[string]any)
			if s, _ := out["error"].(string); !strings.Contains(s, "500") {
				t.Errorf("degraded row's error = %q, want it to mention the 500 status", s)
			}
		}
	}
	if !found {
		t.Error("the failure is recorded nowhere: no degraded run_logs row")
	}
}
