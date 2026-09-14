package nodes

import (
	"context"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// A manual trigger carries no message, so a test run started with one proves
// nothing about a real run. A live build tested "news about Algorand" with an
// input, passed, and told the user it worked -- while a real run of that
// workflow reached the web search with nothing to search for.
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
	}, "k", nil, map[string]string{}, tester)
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
	}, "k", nil, map[string]string{}, tester)
	if got != "hello" {
		t.Errorf("a chat trigger's test input must be kept, got %q", got)
	}
}
