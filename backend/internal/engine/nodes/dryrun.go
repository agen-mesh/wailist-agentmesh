package nodes

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/agentmesh/backend/internal/models"
)

// A dry run is how the chat builder checks its own work: it runs the workflow
// it built and reads the result before telling the user it is done. The
// executors are the real ones (engine.DryRun calls ExecuteTool, ExecuteAction
// and ExecuteAgent exactly as a run does); what differs is which steps are
// allowed to execute. Anything that would act on the world -- send a message,
// pay, rent compute, write state, change a remote system -- is simulated.
//
// It exists because a workflow can "succeed" and still be useless: a
// CoinGecko lookup on a wrong coin id returned {}, and the agent after it
// answered with a made-up price. Only reading the actual output catches that.

// DryRunStep is one executed or simulated step.
type DryRunStep struct {
	NodeID   string `json:"nodeId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Template string `json:"template,omitempty"`
	// Status is "ran", "simulated", "empty" (ran but produced nothing),
	// "failed", or "unverified" (could not be checked: its input came from a
	// simulated step, or it needs credits the user does not have).
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// DryRunResult is what the builder reads back.
type DryRunResult struct {
	Steps []DryRunStep `json:"steps"`
	// Answer is the last agent's reply -- the thing the user will read.
	Answer string `json:"answer,omitempty"`
	// FinalOutput is what the run ends with.
	FinalOutput string `json:"finalOutput,omitempty"`
	Failed      bool   `json:"failed"`
	// Empty is set when any executed step, or the run's end, produced nothing.
	Empty bool `json:"empty"`
	// Unverified is set when part of the workflow could not be checked. It
	// is not a failure: the workflow may well be right, a test run just
	// cannot show it, so nothing should be "fixed" because of it.
	Unverified bool   `json:"unverified,omitempty"`
	Error      string `json:"error,omitempty"`
}

// readOnlyActions are connectors that only fetch public data, so a dry run
// may call them for real.
var readOnlyActions = map[string]bool{
	"coingecko": true, "openweathermap": true, "hackernews": true, "rss": true,
}

// DryRunExecutes says whether a dry run executes this node for real, and if
// not, why -- the reason is shown to the builder and the user.
func DryRunExecutes(n models.WorkflowNode) (bool, string) {
	switch n.Type {
	case models.NodeTypeTrigger, models.NodeTypeEnd, models.NodeTypeAgent, models.NodeTypeProvider:
		return true, ""
	case models.NodeTypeTool:
		if n.Template == "http" {
			if m := strings.ToUpper(strings.TrimSpace(n.Method)); m != "" && m != "GET" {
				return false, "an HTTP " + m + " could change something on the other side"
			}
		}
		return true, ""
	case models.NodeTypeAction:
		if readOnlyActions[n.Template] {
			return true, ""
		}
		return false, "it would send or change something (" + n.Template + ")"
	case models.NodeTypeTool402:
		return false, "it is a paid x402 call"
	case models.NodeTypeTendril:
		return false, "it would rent or use Tendril compute"
	case models.NodeTypeState:
		return false, "it reads or writes the workflow's saved state"
	case models.NodeTypeGoogle:
		return false, "it needs the user's Google account"
	}
	return false, "this step type is not run in a test"
}

// IsEmptyOutput reports whether a step's output carries nothing: nil, an empty
// string/object/array, or an agent reply whose message is one of those.
func IsEmptyOutput(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		switch strings.TrimSpace(x) {
		case "", "{}", "[]", "null", `""`:
			return true
		}
		return false
	case map[string]any:
		if len(x) == 0 {
			return true
		}
		if msg, ok := x["message"]; ok {
			if s, isString := msg.(string); isString {
				return IsEmptyOutput(s)
			}
		}
		return false
	case []any:
		return len(x) == 0
	}
	return false
}

// upstreamBody matches an error that carries a third party's raw response
// body after a status code -- an LLM API error, or callHTTP's
// "http: GET 401: <body>". (?s) matters: a pretty-printed JSON body runs
// over many lines, and without it only the first line was ever removed.
var upstreamBody = regexp.MustCompile(`(?s)([A-Za-z][\w .-]* API \d{3}|http: [A-Z]+ \d{3}): .*`)

// sanitizedErrorMax bounds whatever is left after the known shapes are
// stripped. An executor this code does not know about can wrap anything --
// a URL with a key in its query, a provider's error payload -- and this
// text goes both to the model and into the user's chat.
const sanitizedErrorMax = 200

// SanitizeRunError strips a raw upstream response body from an error before
// it reaches the model or the chat, keeping the status code, and bounds
// what is left.
func SanitizeRunError(msg string) string {
	msg = upstreamBody.ReplaceAllString(msg, "$1")
	if i := strings.IndexAny(msg, "\n\r"); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > sanitizedErrorMax {
		cut := sanitizedErrorMax
		for cut > 0 && !utf8.RuneStart(msg[cut]) {
			cut--
		}
		msg = msg[:cut] + "…"
	}
	return msg
}

// authStatus matches an auth rejection in the error shapes SanitizeRunError
// keeps ("Slack API 401", "http: GET 403"). 407 is deliberately absent: it is
// a proxy between AgentMesh and the service asking for its own login, not a
// credential that belongs on the node.
var authStatus = regexp.MustCompile(`(?:API|http: [A-Z]+) (401|403)$`)

// MissingCredentialReason is what the builder and the user are told when a
// step could not be tested because its credential has not been added.
const MissingCredentialReason = "it needs a credential that has not been added yet, so a test run cannot check it -- the user adds it on the node in the Inspector"

// RejectedCredentialReason is the same situation when the node already has a
// credential and the service turned it down.
const RejectedCredentialReason = "the service rejected the credential on this node, so a test run cannot check it -- the user should check that credential in the Inspector"

// CredentialProblem reports whether a step failed for want of a credential
// the user supplies, rather than because the workflow is wrong, and the
// reason to give. The builder is forbidden from setting credentials
// (nodeRules.parse), and judgeProbe deliberately lets an http node that
// answers 401/403 onto the canvas for the user to add headers to -- so that
// case must read as something a test run could not check, not a workflow to
// "fix".
//
// Only for a node whose template takes a credential from the user. An agent
// has none of its own: a 401 from its model call is a revoked or misconfigured
// key the user cannot add anywhere, and a keyless connector's 403 is a real
// failure. The status is read from the sanitized message, so an upstream body
// that merely mentions "API 403" is never mistaken for one.
func CredentialProblem(n models.WorkflowNode, msg string) (string, bool) {
	if !authStatus.MatchString(SanitizeRunError(msg)) {
		return "", false
	}
	tpl, ok := catalogTemplate(string(n.Type), n.Template)
	if !ok || len(userSuppliedKeys(string(n.Type), tpl)) == 0 {
		return "", false
	}
	if hasStoredCredential(n) {
		return RejectedCredentialReason, true
	}
	return MissingCredentialReason, true
}

// credentialSkips are the skip codes a connector returns when a credential is
// missing ("weather_skipped_no_api_key"). Every other skip -- no ids, no
// query, no city, missing config -- is a setting the builder can fill in.
var credentialSkips = []string{
	"_skipped_no_api_key", "_skipped_no_api_token", "_skipped_no_access_token",
	"_skipped_no_token", "_skipped_no_bot_token", "_skipped_no_auth_token",
	"_skipped_no_credentials", "_skipped_no_webhook_url",
}

// IsCredentialSkip reports whether a connector's skip code means its
// credential is missing.
func IsCredentialSkip(out any) bool {
	code, ok := out.(string)
	if !ok {
		return false
	}
	for _, suffix := range credentialSkips {
		if strings.HasSuffix(code, suffix) {
			return true
		}
	}
	return false
}
