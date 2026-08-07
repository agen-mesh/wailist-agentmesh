package nodes

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func decodeSignatureHeader(t *testing.T, value string) map[string]any {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("want Payment-Signature to be valid base64, got error: %v", err)
	}
	var wrapper map[string]any
	if err := json.Unmarshal(decoded, &wrapper); err != nil {
		t.Fatalf("want the decoded header to be valid JSON, got error: %v", err)
	}
	return wrapper
}

// challengeFixture is a target's own 402 challenge, as the probe captures it.
func challengeFixture(network string) (accept, challenge map[string]any) {
	accept = map[string]any{
		"payTo": "TARGETADDR", "asset": "31566704", "amount": "100000",
		"scheme": "exact", "network": network,
	}
	challenge = map[string]any{
		"accepts":    []any{accept},
		"resource":   map[string]any{"url": "https://example.test/resource"},
		"extensions": map[string]any{"bazaar": map[string]any{}},
	}
	return accept, challenge
}

// TestBuildPaymentHeadersSendsSpecHeader pins the fix for payments a
// spec-compliant target could not see at all. @x402/core 2.20's server-side
// extractPayment reads "payment-signature"/"PAYMENT-SIGNATURE" and nothing
// else, base64-decoding it; we sent only raw JSON under X-Payment, so
// api.scrape402.site answered five real $0.10 settlements with its unchanged
// 402 challenge (2026-08-03). Both headers now go out: the spec one so an
// SDK-based target can read it, the legacy one so anything already working
// keeps working.
func TestBuildPaymentHeadersSendsSpecHeader(t *testing.T) {
	accept, challenge := challengeFixture("algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=")
	quote := TargetQuote{PayTo: "TARGETADDR", Asset: "31566704", MaxAmountRequired: "100000", RawAccept: accept, RawChallenge: challenge}

	headers := buildPaymentHeaders("https://api.scrape402.site/scrape", "algorand:mainnet", []string{"g0", "g1"}, 1, quote)

	sig, has := headers["Payment-Signature"]
	if !has {
		t.Fatal("want a Payment-Signature header -- the only one a target built on @x402/core ever reads")
	}
	wrapper := decodeSignatureHeader(t, sig)
	if wrapper["accepted"] == nil {
		t.Fatal("want \"accepted\" echoed: PaymentPayloadV2Schema requires it, so a validating target rejects the payload without it")
	}
	if wrapper["resource"] == nil || wrapper["extensions"] == nil {
		t.Fatalf("want resource/extensions copied from the challenge, got resource=%v extensions=%v", wrapper["resource"], wrapper["extensions"])
	}
	payload, ok := wrapper["payload"].(map[string]any)
	if !ok {
		t.Fatalf("want a payload object, got %T", wrapper["payload"])
	}
	if payload["paymentIndex"] != float64(1) {
		t.Fatalf("want paymentIndex=1 passed through, got %v", payload["paymentIndex"])
	}

	raw, has := headers["X-Payment"]
	if !has {
		t.Fatal("want the legacy raw-JSON X-Payment kept alongside it, so targets on that dialect keep working")
	}
	var legacy map[string]any
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		t.Fatalf("want X-Payment to stay raw (non-base64) JSON, got error: %v", err)
	}
	// Same bytes under both names: at most one can settle, since a duplicate
	// submission of the same signed group is rejected on-chain.
	if base64.StdEncoding.EncodeToString([]byte(raw)) != sig {
		t.Fatal("want both headers to carry the identical payload")
	}
}

// TestBuildPaymentHeadersAreHostAgnostic is the anti-regression guard for the
// hardcoding this replaced: canix402 used to be recognized by hostname and
// handed a hand-written dialect. Two targets whose challenges are identical
// must now get byte-identical headers no matter what they are called -- a
// user can paste any x402 URL into a node, and the payment path cannot have
// been taught that merchant's name in advance.
func TestBuildPaymentHeadersAreHostAgnostic(t *testing.T) {
	accept, challenge := challengeFixture("algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=")
	quote := TargetQuote{PayTo: "TARGETADDR", Asset: "31566704", MaxAmountRequired: "100000", RawAccept: accept, RawChallenge: challenge}

	known := buildPaymentHeaders("https://canix402-api.compx.io/opportunities", "algorand:mainnet", []string{"g0", "g1"}, 1, quote)
	stranger := buildPaymentHeaders("https://some-endpoint-nobody-has-seen.example/api", "algorand:mainnet", []string{"g0", "g1"}, 1, quote)

	if len(known) != len(stranger) {
		t.Fatalf("want the same header set for both, got %v vs %v", known, stranger)
	}
	for name, value := range known {
		if stranger[name] != value {
			t.Fatalf("header %q differs by hostname: a payment path that branches on who the merchant is cannot serve an arbitrary pasted URL", name)
		}
	}
}

// TestBuildPaymentHeadersEchoDeclaredNetwork covers what replaced the
// hardcoded host->network map: the id a target advertises is the id it is
// sent, so a merchant that changes its advertised network is followed
// automatically instead of paying against a stale constant.
func TestBuildPaymentHeadersEchoDeclaredNetwork(t *testing.T) {
	accept, challenge := challengeFixture("algorand-mainnet") // a target's own short form
	quote := TargetQuote{PayTo: "TARGETADDR", Asset: "31566704", MaxAmountRequired: "100000", RawAccept: accept, RawChallenge: challenge}

	wrapper := decodeSignatureHeader(t, buildPaymentHeaders("https://example.test/resource", "algorand:mainnet", []string{"g0"}, 0, quote)["Payment-Signature"])

	if wrapper["network"] != "algorand-mainnet" {
		t.Fatalf("want the target's own declared network echoed, got %v", wrapper["network"])
	}
	if wrapper["paymentRequired"] == nil {
		t.Fatal("want the full challenge echoed under \"paymentRequired\" -- required by at least one real target, ignored by non-strict SDK schemas")
	}
}

// TestBuildPaymentHeadersWithoutChallenge confirms a quote built by hand
// (not from a real probe) still produces a well-formed payload: the echoes
// are simply absent rather than null, and the configured network is used.
func TestBuildPaymentHeadersWithoutChallenge(t *testing.T) {
	quote := TargetQuote{PayTo: "TARGETADDR", Asset: "31566704", MaxAmountRequired: "10000"}

	headers := buildPaymentHeaders("https://example.test/resource", "algorand:mainnet", []string{"g0", "g1"}, 1, quote)

	wrapper := decodeSignatureHeader(t, headers["Payment-Signature"])
	if wrapper["network"] != "algorand:mainnet" {
		t.Fatalf("want the configured network when the quote declares none, got %v", wrapper["network"])
	}
	for _, field := range []string{"accepted", "paymentRequired", "resource", "extensions"} {
		if _, has := wrapper[field]; has {
			t.Fatalf("want %q omitted when there is no challenge to echo", field)
		}
	}
}
