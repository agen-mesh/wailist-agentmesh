package nodes

import (
	"strings"
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

// Review finding: only a single-line "LLM API <code>: " prefix was stripped,
// so a pretty-printed body survived past its first newline, and callHTTP's
// "http: GET 401: <body>" and every connector's "<Service> API <code>: <body>"
// were not stripped at all. All of them reach the model and the user's chat.
func TestSanitizeRunErrorStripsEveryUpstreamBody(t *testing.T) {
	cases := []struct{ in, want string }{
		{"agent: LLM API 429: {\"error\":{\n\"message\":\"quota\"}}", "agent: LLM API 429"},
		{"http: GET 401: {\"message\":\"bad key\"}", "http: GET 401"},
		{"Slack API 403: {\"error\":\"not_allowed\"}", "Slack API 403"},
		{"http: invalid headers JSON: unexpected end", "http: invalid headers JSON: unexpected end"},
	}
	for _, c := range cases {
		if got := SanitizeRunError(c.in); got != c.want {
			t.Errorf("SanitizeRunError(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := SanitizeRunError("weird executor: " + strings.Repeat("secret-looking-text ", 50))
	if len(long) > sanitizedErrorMax+len("…") {
		t.Errorf("an unknown error shape must still be bounded, got %d bytes", len(long))
	}
}

// A step that fails only because the user has not pasted their key yet is not
// a workflow to fix: the builder is forbidden from setting credentials, so
// telling it to "fix" this sends it round the repair loop for nothing.
func TestMissingCredentialErrorsAreNotFailures(t *testing.T) {
	for _, msg := range []string{"http: GET 401", "http: POST 403", "Airtable API 401"} {
		if !IsMissingCredentialError(msg) {
			t.Errorf("%q should read as a missing credential", msg)
		}
	}
	for _, msg := range []string{"http: GET 404", "http: GET 500", "Slack API 400", "json path not found"} {
		if IsMissingCredentialError(msg) {
			t.Errorf("%q is a real failure, not a missing credential", msg)
		}
	}
}
