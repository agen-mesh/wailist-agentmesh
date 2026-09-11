package nodes

import (
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// A test run executes a workflow for real, except for the steps that would do
// something the user did not ask for yet: send a message, pay, rent compute,
// write state, or change a remote system.
func TestDryRunPolicy(t *testing.T) {
	cases := []struct {
		node    models.WorkflowNode
		execute bool
	}{
		{models.WorkflowNode{Type: models.NodeTypeTrigger, Template: "manual"}, true},
		{models.WorkflowNode{Type: models.NodeTypeTool, Template: "http", Method: "GET"}, true},
		{models.WorkflowNode{Type: models.NodeTypeTool, Template: "http"}, true}, // blank method is GET
		{models.WorkflowNode{Type: models.NodeTypeTool, Template: "http", Method: "POST"}, false},
		{models.WorkflowNode{Type: models.NodeTypeTool, Template: "json_extract"}, true},
		{models.WorkflowNode{Type: models.NodeTypeTool, Template: "websearch"}, true},
		{models.WorkflowNode{Type: models.NodeTypeAction, Template: "coingecko"}, true},
		{models.WorkflowNode{Type: models.NodeTypeAction, Template: "openweathermap"}, true},
		{models.WorkflowNode{Type: models.NodeTypeAction, Template: "slack"}, false},
		{models.WorkflowNode{Type: models.NodeTypeAction, Template: "email"}, false},
		{models.WorkflowNode{Type: models.NodeTypeTool402}, false},
		{models.WorkflowNode{Type: models.NodeTypeTendril, Template: "tendril_rent"}, false},
		{models.WorkflowNode{Type: models.NodeTypeState, Template: "set"}, false},
		{models.WorkflowNode{Type: models.NodeTypeGoogle, Template: "gmail_send"}, false},
		{models.WorkflowNode{Type: models.NodeTypeAgent, Template: "agent"}, true},
		{models.WorkflowNode{Type: models.NodeTypeEnd, Template: "done"}, true},
	}
	for _, c := range cases {
		execute, reason := DryRunExecutes(c.node)
		if execute != c.execute {
			t.Errorf("%s/%s method=%q: execute=%v, want %v (reason %q)", c.node.Type, c.node.Template, c.node.Method, execute, c.execute, reason)
		}
		if !execute && reason == "" {
			t.Errorf("%s/%s is simulated but gives no reason", c.node.Type, c.node.Template)
		}
	}
}

// "{}" is exactly what the user saw from a CoinGecko lookup on a wrong coin id
// -- a run that "succeeded" with nothing in it.
func TestIsEmptyOutput(t *testing.T) {
	empty := []any{nil, "", "  ", "{}", "[]", "null", map[string]any{}, []any{},
		map[string]any{"message": ""}, map[string]any{"message": "{}"}}
	for _, v := range empty {
		if !IsEmptyOutput(v) {
			t.Errorf("%#v should count as empty", v)
		}
	}
	full := []any{"0.00021", 0, false, map[string]any{"myrad": map[string]any{"usd": 0.0002}},
		map[string]any{"message": "The price is $0.0002"}, []any{1}}
	for _, v := range full {
		if IsEmptyOutput(v) {
			t.Errorf("%#v should not count as empty", v)
		}
	}
}

// An upstream error body must not reach the model or the chat through a
// test-run result.
func TestSanitizeRunError(t *testing.T) {
	got := SanitizeRunError(`agent: LLM API 429: {"error":{"message":"quota"}}`)
	if got != "agent: LLM API 429" {
		t.Fatalf("got %q", got)
	}
}
