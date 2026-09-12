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
// telling it to "fix" this sends it round the repair loop for nothing. Only a
// node whose template takes a credential qualifies, and only on the sanitized
// message.
func TestCredentialProblem(t *testing.T) {
	httpNode := models.WorkflowNode{Type: models.NodeTypeTool, Template: "http"}
	withKey := httpNode
	withKey.Secrets = map[string]string{"httpHeadersJSON": "{}"}
	agent := models.WorkflowNode{Type: models.NodeTypeAgent, Template: "agent"}
	coingecko := models.WorkflowNode{Type: models.NodeTypeAction, Template: "coingecko"}
	cases := []struct {
		name   string
		node   models.WorkflowNode
		msg    string
		reason string
	}{
		{"http 401, no key yet", httpNode, "http: GET 401: denied", MissingCredentialReason},
		{"http 403, no key yet", httpNode, "http: POST 403", MissingCredentialReason},
		{"http 401 with a stored key", withKey, "http: GET 401", RejectedCredentialReason},
		{"http 404", httpNode, "http: GET 404: not found", ""},
		{"http 500 whose body mentions API 403", httpNode, "http: GET 500: upstream API 403 forbidden", ""},
		{"proxy 407 is not the node's credential", httpNode, "http: GET 407", ""},
		{"an agent has no credential to add", agent, "agent: LLM API 401: bad key", ""},
		{"a keyless connector's 403 is real", coingecko, "CoinGecko API 403: blocked", ""},
	}
	for _, c := range cases {
		reason, ok := CredentialProblem(c.node, c.msg)
		if reason != c.reason || ok != (c.reason != "") {
			t.Errorf("%s: got (%q, %v), want %q", c.name, reason, ok, c.reason)
		}
	}
}

// Only a missing credential skip is the user's to fix; any other skip is a
// setting the builder can fill in.
func TestIsCredentialSkip(t *testing.T) {
	for _, code := range []string{"weather_skipped_no_api_key", "telegram_skipped_no_bot_token", "twilio_skipped_no_auth_token", "slack_skipped_no_webhook_url"} {
		if !IsCredentialSkip(code) {
			t.Errorf("%q is a missing credential", code)
		}
	}
	for _, code := range []any{"coingecko_skipped_no_ids", "rss_skipped_no_url", "weather_skipped_no_city", "notion_skipped_missing_config", nil, 42} {
		if IsCredentialSkip(code) {
			t.Errorf("%v is a missing setting, not a credential", code)
		}
	}
}
