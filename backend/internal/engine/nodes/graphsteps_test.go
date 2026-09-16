package nodes

import (
	"context"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// A live build tested a web search with an input, passed, and told the user
// it worked -- while a real manual run reaches it with nothing to search.
func TestTestRunIgnoresInputForAManualTrigger(t *testing.T) {
	var got string
	tester := &testTracker{run: func(ctx context.Context, g models.WorkflowGraph, input string) DryRunResult {
		got = input
		return DryRunResult{Steps: []DryRunStep{}}
	}}
	graph := &models.WorkflowGraph{Nodes: []models.WorkflowNode{
		{ID: "t", Type: models.NodeTypeTrigger, Template: "manual"},
	}}
	res := dispatchBuildCall(context.Background(), graph, geminiFuncCall{
		name: "test_run", args: map[string]any{"input": "latest news about Algorand"},
	}, "k", nil, map[string]string{}, nil, tester)
	if got != "" {
		t.Errorf("a manual trigger carries no message, but the test ran with %q", got)
	}
	if text, _ := res["result"].(string); !strings.Contains(text, "manual trigger") {
		t.Errorf("the model must be told why its input was dropped, got %q", text)
	}

	// A chat trigger does take one.
	graph.Nodes[0].Template = "chat"
	dispatchBuildCall(context.Background(), graph, geminiFuncCall{
		name: "test_run", args: map[string]any{"input": "hello"},
	}, "k", nil, map[string]string{}, nil, tester)
	if got != "hello" {
		t.Errorf("a chat trigger's test input must be kept, got %q", got)
	}
}

// DryRunResult.Degraded was written by the dry run and read by nothing: it
// rode along in every replayed test response costing tokens, while the model
// was told only "the run FAILED" about a run that had in fact answered.
func TestTestRunTellsTheModelWhenAReadDegradedRatherThanFailed(t *testing.T) {
	degraded := DryRunResult{
		Failed:      true,
		Degraded:    true,
		FinalOutput: "I could not reach the price source.",
		Steps:       []DryRunStep{{Name: "CoinGecko Price", Status: "failed", Error: "http: GET 503"}},
	}
	hardFail := DryRunResult{
		Failed: true,
		Steps:  []DryRunStep{{Name: "Send", Status: "failed", Error: "slack API 500"}},
	}

	run := func(res DryRunResult) string {
		tester := &testTracker{run: func(ctx context.Context, g models.WorkflowGraph, input string) DryRunResult {
			return res
		}}
		out := dispatchBuildCall(context.Background(), &models.WorkflowGraph{}, geminiFuncCall{name: "test_run"}, "k", nil, nil, nil, tester)
		text, _ := out["result"].(string)
		return text
	}

	got := run(degraded)
	if !strings.Contains(got, "DOES answer") {
		t.Errorf("a degraded test run must tell the model the workflow still answered, got:\n%s", got)
	}
	if strings.Contains(got, "The run FAILED") {
		t.Errorf("a degraded run was reported as a plain hard failure, got:\n%s", got)
	}

	got = run(hardFail)
	if !strings.Contains(got, "The run FAILED") {
		t.Errorf("a hard failure must still be reported as one, got:\n%s", got)
	}
	if strings.Contains(got, "DOES answer") {
		t.Errorf("a hard failure was described as having answered, got:\n%s", got)
	}
}
