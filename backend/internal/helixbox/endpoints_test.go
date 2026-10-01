package helixbox

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestEndpointsAreWellFormed guards the facts that cost money when wrong. Every
// assertion here maps to a real failure mode: a stray host pays a stranger, a
// zero amount underquotes the Bazaar card, a duplicate id makes Lookup return
// the wrong plan.
func TestEndpointsAreWellFormed(t *testing.T) {
	eps := Endpoints()
	if len(eps) != 3 {
		t.Fatalf("want 3 HelixBox plans, got %d", len(eps))
	}

	seen := map[string]bool{}
	paths := map[string]bool{}
	for _, e := range eps {
		if seen[e.ID] {
			t.Errorf("duplicate endpoint id %q — Lookup would return whichever came first", e.ID)
		}
		seen[e.ID] = true

		if paths[e.Path] {
			t.Errorf("duplicate path %q", e.Path)
		}
		paths[e.Path] = true

		if !strings.HasPrefix(e.Path, "/") {
			t.Errorf("%s: path %q must start with /", e.ID, e.Path)
		}
		if got := e.URL(); !strings.HasPrefix(got, "https://"+Host+"/") {
			// A spec pointing off-host would be paid for against whatever it
			// named, with HelixBox's PayTo attached.
			t.Errorf("%s: URL %q is not on %s", e.ID, got, Host)
		}
		if e.Method != http.MethodPost {
			t.Errorf("%s: method %q — all three HelixBox endpoints are POST", e.ID, e.Method)
		}
		if e.AmountMicros <= 0 {
			t.Errorf("%s: amount %d — a zero quote would price the Bazaar card at the platform fee alone", e.ID, e.AmountMicros)
		}
		if e.DurationSeconds <= 0 {
			t.Errorf("%s: duration %d", e.ID, e.DurationSeconds)
		}
		if e.AccessLevel == "" {
			t.Errorf("%s: no access level", e.ID)
		}
		if e.Title == "" || e.Description == "" || e.Blurb == "" {
			t.Errorf("%s: title, description and blurb are all shown to a buyer before they pay", e.ID)
		}
		if e.Verified != VerifiedLive && e.Verified != VerifiedSource {
			t.Errorf("%s: unknown verification state %q", e.ID, e.Verified)
		}
	}
}

// TestBodyTemplateCarriesThePairingCode is the regression test for the most
// expensive mistake in this package's history.
//
// The body used to be a literal `{}`, because the 402 challenge declared an
// empty input schema. HelixBox's real handler reads body.code and answers 400
// "CLI pairing code is required" when it is missing — AFTER taking payment.
// Two live purchases were lost to that before the vendor's source settled it.
//
// If this test ever fails because someone "simplified" the template back to an
// empty object, they have re-broken it and every purchase will be charged and
// refused.
func TestBodyTemplateCarriesThePairingCode(t *testing.T) {
	for _, e := range Endpoints() {
		if strings.TrimSpace(e.BodyTemplate) == "" {
			t.Errorf("%s: empty BodyTemplate — buildTargetRequest would send no body and no Content-Type", e.ID)
			continue
		}
		if !strings.Contains(e.BodyTemplate, "{{param:code}}") {
			t.Errorf("%s: BodyTemplate %q does not send a pairing code; HelixBox would charge for it and answer 400",
				e.ID, e.BodyTemplate)
		}
		// Valid JSON once the token is filled in. json.Valid on the raw
		// template would fail on the braces, so substitute a realistic value
		// first — the same thing expandBodyTemplate does at request time.
		filled := strings.ReplaceAll(e.BodyTemplate, "{{param:code}}", "AfXavNVgBJ")
		if !json.Valid([]byte(filled)) {
			t.Errorf("%s: BodyTemplate is not valid JSON once filled in: %s", e.ID, filled)
			continue
		}
		var into map[string]any
		if err := json.Unmarshal([]byte(filled), &into); err != nil {
			t.Errorf("%s: filled body is not a JSON object: %v", e.ID, err)
			continue
		}
		if into["code"] != "AfXavNVgBJ" {
			t.Errorf("%s: filled body has no `code` field: %v", e.ID, into)
		}
		// The handler reads body.code and nothing else. Extra fields are not
		// rejected, but anything we invent here is untested weight on a paid
		// request.
		if len(into) != 1 {
			t.Errorf("%s: body carries %d fields; HelixBox's handler reads only `code`", e.ID, len(into))
		}
	}
}

// Every plan must declare the pairing code as a REQUIRED field. A field the
// console renders as optional is a field a user can leave blank, and a blank
// code is a guaranteed paid-then-rejected call.
func TestEveryPlanRequiresThePairingCode(t *testing.T) {
	for _, e := range Endpoints() {
		var found bool
		for _, f := range e.Fields {
			if f.Name != "code" {
				continue
			}
			found = true
			if !f.Required {
				t.Errorf("%s: the pairing code is optional; HelixBox charges and then rejects a blank one", e.ID)
			}
			if f.Kind != FieldText {
				t.Errorf("%s: pairing code kind %q, want %q", e.ID, f.Kind, FieldText)
			}
			if f.Label == "" || f.Description == "" {
				// Nobody can guess where the code comes from. The description
				// is the only thing that says "run npx helixbox-cli".
				t.Errorf("%s: the pairing code field needs a label and an explanation", e.ID)
			}
		}
		if !found {
			t.Errorf("%s: no `code` field, so the console cannot collect one", e.ID)
		}
	}
}

// The three plans share one settlement address, verified identical across all
// three live probes. A per-endpoint override would be a transcription slip.
func TestEveryEndpointSettlesToTheProbedAddress(t *testing.T) {
	// Algorand addresses are 58 base32 characters. A truncated or mangled one
	// — exactly what the spec document's own base64 blob decodes to — would
	// fail here rather than at settlement time.
	if len(PayTo) != 58 {
		t.Fatalf("PayTo is %d characters, want 58: %q", len(PayTo), PayTo)
	}
	if AssetID != "31566704" {
		t.Errorf("AssetID = %q, want mainnet USDC 31566704", AssetID)
	}
	if !strings.HasPrefix(Network, "algorand:") {
		t.Errorf("Network = %q, want a CAIP-2 algorand id", Network)
	}
}

// The prices are the reason to re-probe rather than trust a doc, so they are
// pinned individually against what the live challenges declared on 2026-09-08.
func TestPricesMatchTheProbedChallenges(t *testing.T) {
	want := map[string]int64{
		"cli-hour":           250_000,
		"agent-session-hour": 250_000,
		"premium-week":       2_000_000,
	}
	for id, amount := range want {
		e, ok := Lookup(id)
		if !ok {
			t.Errorf("%s is gone", id)
			continue
		}
		if e.AmountMicros != amount {
			t.Errorf("%s: %d micros, probed challenge said %d", id, e.AmountMicros, amount)
		}
	}
}

// Durations have to agree with the expiresIn the endpoint returns, because the
// console labels the plan from THIS value before anything is bought.
func TestDurationsMatchTheDocumentedExpiry(t *testing.T) {
	want := map[string]int64{
		"cli-hour":           3600,
		"agent-session-hour": 3600,
		"premium-week":       604800,
	}
	for id, secs := range want {
		e, _ := Lookup(id)
		if e.DurationSeconds != secs {
			t.Errorf("%s: duration %d, endpoint returns expiresIn %d", id, e.DurationSeconds, secs)
		}
	}
}

// Lookup is the gate that keeps /helixbox/run from becoming an open x402
// proxy: the URL comes from this table or the request is refused.
func TestLookupRejectsAnythingNotInTheTable(t *testing.T) {
	for _, bad := range []string{
		"",
		"unknown",
		"https://evil.example.com/drain",
		"/v2/x402/cli/hour",
		"CLI-HOUR",
	} {
		if _, ok := Lookup(bad); ok {
			t.Errorf("Lookup(%q) succeeded — that id would become a paid request URL", bad)
		}
	}
	if _, ok := Lookup("cli-hour"); !ok {
		t.Error("Lookup(\"cli-hour\") failed on a real id")
	}
}

// Endpoints() must hand out a fresh slice: it is returned to a JSON encoder on
// a request path, and a caller that mutates a shared table would change what
// every later buyer is quoted.
func TestEndpointsIsNotSharedState(t *testing.T) {
	a := Endpoints()
	a[0].AmountMicros = 1
	a[0].Path = "/drained"
	if b := Endpoints(); b[0].AmountMicros == 1 || b[0].Path == "/drained" {
		t.Error("Endpoints() hands out shared state; a mutation leaked into the next call")
	}
}
