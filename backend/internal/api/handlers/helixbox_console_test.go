package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/helixbox"
	"github.com/agentmesh/backend/internal/models"
)

// TestBuildHelixboxNodeRejectsAnUnknownEndpoint is the guard that keeps
// /helixbox/run from being an open x402 proxy. The URL called always comes from
// the endpoint table; a caller who could name an arbitrary target could spend
// their own credit against any host on the internet through our platform
// wallet.
func TestBuildHelixboxNodeRejectsAnUnknownEndpoint(t *testing.T) {
	for _, id := range []string{
		"",
		"cli",
		"https://evil.example.com/free-money",
		"../cli-hour",
		"CLI-HOUR",
		"/v2/x402/cli/hour",
	} {
		req := helixboxRunRequest{Endpoint: id, Fields: map[string]string{"code": "AfXavNVgBJ"}}
		if _, err := buildHelixboxNode(req); err == nil {
			t.Errorf("endpoint %q was accepted; it would have become a paid request URL", id)
		}
	}
}

// Every plan must produce a node the engine can actually execute, with the URL
// and method coming from the table rather than from anything a caller sent.
func TestEveryHelixboxPlanBuildsAPayableNode(t *testing.T) {
	for _, e := range helixbox.Endpoints() {
		node, err := buildHelixboxNode(helixboxRunRequest{
			Endpoint: e.ID,
			Fields:   map[string]string{"code": "AfXavNVgBJ"},
		})
		if err != nil {
			t.Errorf("%s: %v", e.ID, err)
			continue
		}
		if node.Endpoint != e.URL() {
			t.Errorf("%s: node calls %q, table says %q", e.ID, node.Endpoint, e.URL())
		}
		if node.Method != e.Method {
			t.Errorf("%s: node method %q, table says %q", e.ID, node.Method, e.Method)
		}
		if node.Type != models.NodeTypeTool402 {
			t.Errorf("%s: node type %q", e.ID, node.Type)
		}
		if !strings.HasPrefix(node.Endpoint, "https://"+helixbox.Host+"/") {
			t.Errorf("%s: node targets %q, off HelixBox's host", e.ID, node.Endpoint)
		}
		if node.BodyMode != models.BodyModeJSON {
			t.Errorf("%s: BodyMode = %q, want %q", e.ID, node.BodyMode, models.BodyModeJSON)
		}
		// The pairing code has to reach the body. Without it HelixBox settles
		// the payment and THEN answers 400 — see the internal/helixbox package
		// comment for what that cost.
		if !strings.Contains(node.BodyTemplate, "{{param:code}}") {
			t.Errorf("%s: BodyTemplate = %q, which sends no pairing code", e.ID, node.BodyTemplate)
		}
		var code string
		for _, p := range node.CustomParams {
			if p.Name == "code" {
				code = p.Value
			}
		}
		if code != "AfXavNVgBJ" {
			t.Errorf("%s: the pairing code did not reach the node's params (got %q)", e.ID, code)
		}
	}
}

// The blank-code case is the one that actually happened, twice, at $1.75 a
// time. It must fail here — before ExecuteTool402V2 is ever called — rather
// than at HelixBox's handler, which charges first and validates second.
func TestBuildHelixboxNodeRefusesAMissingPairingCodeBeforePaying(t *testing.T) {
	for _, fields := range []map[string]string{
		nil,
		{},
		{"code": ""},
		{"code": "   "},
		{"code": "\t\n"},
		{"notcode": "AfXavNVgBJ"},
	} {
		_, err := buildHelixboxNode(helixboxRunRequest{Endpoint: "cli-hour", Fields: fields})
		if err == nil {
			t.Errorf("fields %v were accepted; HelixBox would charge $1.75 and answer 400", fields)
			continue
		}
		// The message is read by someone about to spend money, so it has to
		// say what to do, not just refuse.
		if !strings.Contains(strings.ToLower(err.Error()), "pairing code") {
			t.Errorf("fields %v: unhelpful error %q", fields, err)
		}
	}
}

// A mangled paste buys time on a session that does not exist: HelixBox's
// getOrCreateAssembleSession creates one for any string it has not seen, so
// there is no server-side check to fall back on and no refund.
func TestBuildHelixboxNodeRejectsAMangledPairingCode(t *testing.T) {
	for _, bad := range []string{
		"AfXav NVgBJ",
		"AfXavNVgBJ extra",
		"AfXav\tNVgBJ",
		"AfXav\nNVgBJ",
	} {
		if _, err := buildHelixboxNode(helixboxRunRequest{
			Endpoint: "cli-hour",
			Fields:   map[string]string{"code": bad},
		}); err == nil {
			t.Errorf("%q was accepted; it would buy paid time on a session nobody can attach to", bad)
		}
	}

	// Surrounding whitespace is just a sloppy copy and is trimmed, not
	// refused — refusing it would block a code the user really does have.
	node, err := buildHelixboxNode(helixboxRunRequest{
		Endpoint: "cli-hour",
		Fields:   map[string]string{"code": "  AfXavNVgBJ  "},
	})
	if err != nil {
		t.Fatalf("a padded paste was refused: %v", err)
	}
	for _, p := range node.CustomParams {
		if p.Name == "code" && p.Value != "AfXavNVgBJ" {
			t.Errorf("padding survived trimming: %q", p.Value)
		}
	}
}

// The caller picks a plan id and fills in the declared fields. This pins that
// nothing ELSE in the payload can influence what gets called or what it costs.
func TestHelixboxRunRequestCannotSmuggleATargetOrAPrice(t *testing.T) {
	// Anything beyond `endpoint` in the payload is ignored by the decoder, so
	// a client cannot smuggle a body, a URL or an amount into the paid call.
	var req helixboxRunRequest
	raw := `{"endpoint":"cli-hour","fields":{"code":"AfXavNVgBJ"},"body":{"x":1},"amountMicros":1,"url":"https://evil.example.com"}`
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	if req.Endpoint != "cli-hour" {
		t.Fatalf("endpoint = %q", req.Endpoint)
	}
	node, err := buildHelixboxNode(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(node.Endpoint, "evil.example.com") {
		t.Errorf("the request body reached the target URL: %q", node.Endpoint)
	}
	if strings.Contains(node.BodyTemplate, `"x"`) {
		t.Errorf("the request body reached the paid body: %q", node.BodyTemplate)
	}
}

// The plan list has to carry the field spec, or the console has no form to
// render and every purchase goes out with a blank code.
func TestHelixboxEndpointsResponseCarriesTheFieldSpec(t *testing.T) {
	blob, err := json.Marshal(helixbox.Endpoints())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"fields"`, `"code"`, `"required":true`} {
		if !strings.Contains(string(blob), want) {
			t.Errorf("the plan list is missing %s, so the console cannot ask for a pairing code", want)
		}
	}
}

// The console posts a plan id; it never needs the body that gets paid for, and
// leaking it would invite a client to think it could change it.
func TestHelixboxEndpointsResponseHidesTheBodyTemplate(t *testing.T) {
	blob, err := json.Marshal(helixbox.Endpoints())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "bodyTemplate") || strings.Contains(string(blob), "BodyTemplate") {
		t.Errorf("the serialized plan list leaks its body template: %s", blob)
	}
	// The plan list IS meant to carry price, duration and access level — the
	// console cannot label or cost a plan without them.
	for _, want := range []string{"amountMicros", "durationSeconds", "accessLevel"} {
		if !strings.Contains(string(blob), want) {
			t.Errorf("the plan list is missing %s, which the console needs before anything is bought", want)
		}
	}
}

// TestHelixboxFeeIsTheSharedPlatformConstant pins the pricing the console
// advertises to the one the relay actually charges. These must be the same
// number, read from the same place: quoting a fee the ledger does not match is
// a billing dispute waiting to happen.
func TestHelixboxFeeIsTheSharedPlatformConstant(t *testing.T) {
	if models.X402PlatformFeeUSDMicros != 1_500_000 {
		t.Fatalf("platform fee = %d micros; the HelixBox console's cost display assumes $1.50 and must be revisited",
			models.X402PlatformFeeUSDMicros)
	}
}

// The two hourly plans cost the same by HelixBox's own pricing, which reads
// like a copy-paste bug unless it is deliberate. It is — both probed at 250000
// on 2026-09-08 — and the console says so in the UI. If HelixBox ever
// differentiates them, that copy needs removing.
func TestTheTwoHourlyPlansStillSharePrice(t *testing.T) {
	cli, ok := helixbox.Lookup("cli-hour")
	if !ok {
		t.Fatal("cli-hour is gone")
	}
	agent, ok := helixbox.Lookup("agent-session-hour")
	if !ok {
		t.Fatal("agent-session-hour is gone")
	}
	if cli.AmountMicros != agent.AmountMicros {
		t.Errorf("the hourly plans now differ (%d vs %d) — the console's \"same price\" copy is stale",
			cli.AmountMicros, agent.AmountMicros)
	}
	if cli.AccessLevel == agent.AccessLevel {
		t.Errorf("both hourly plans claim access level %q; nothing would distinguish them to a buyer", cli.AccessLevel)
	}
}
