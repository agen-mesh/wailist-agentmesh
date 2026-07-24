package engine_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/x402"
)

// TestOrchestratorRoundTripBothLegsSettle exercises the full orchestrator
// flow end-to-end over real HTTP round trips (no internal function calls):
// a fake "agent" HTTP client drives the relay through both its phases —
// first with no payment (gets the mirrored 402), then with an X-Payment
// header (settles the inbound leg through a fake facilitator, pays a real
// downstream target, and relays the target's real response back). This is
// the shape the x402 Global Challenge scores: "client pays your endpoint,
// your backend pays the downstream endpoint, and the synthesized paid
// response is returned."
//
// It uses the same package-wide TestMain (in debit_test.go) that relaxes the
// SSRF URL validator for httptest.NewServer targets — no new TestMain is
// declared here.
func TestOrchestratorRoundTripBothLegsSettle(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Fake downstream target: unauthenticated GET gets a 402 quoting the
	// platform's real USDC asset id (10458941) so the relay's asset-id safety
	// check in payTargetAndRespond passes; an authenticated GET (X-Payment
	// set) returns the real paid payload the whole test is proving comes back
	// to the caller. relaySettleAndForward re-fetches this 402 quote (it isn't
	// just fetched once at the top anymore), so this handler must answer the
	// unauthenticated branch consistently across repeated calls — it does,
	// since it's a fixed response with no counter/state.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Payment") != "" {
			w.Write([]byte(`{"data":"real downstream result"}`))
			return
		}
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"accepts":[{"scheme":"exact","payTo":"TARGETADDR","asset":"10458941","maxAmountRequired":"75000"}]}`))
	}))
	defer target.Close()

	// Fake facilitator: records which paths get hit (only the inbound leg
	// goes through this client — the relay pays the target directly via the
	// USDC signer, not through FacilitatorClient) and settles with a txid
	// keyed off target.URL, which gets a fresh random port every run, so
	// rerunning this test against a real, persistent Postgres never collides
	// with a previous run's inbound_tx_id (which has a uniqueness
	// constraint).
	var facilitatorCalls []string
	facilitator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		facilitatorCalls = append(facilitatorCalls, r.URL.Path)
		if r.URL.Path == "/verify" {
			json.NewEncoder(w).Encode(map[string]any{"isValid": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "transaction": "INBOUND-TX-" + target.URL})
	}))
	defer facilitator.Close()

	d := &handlers.Deps{
		Store:                     store,
		PlatformWalletAddress:     "PLATFORMADDR",
		PlatformWalletEncMnemonic: "enc-mnemonic",
		FacilitatorClient:         x402.NewFacilitatorClient(facilitator.URL),
		USDCAssetID:               10458941,
		RelayNetwork:              "algorand:testnet",
		RelayFeePayer:             "FEEPAYERADDR",
		USDCSigner:                &fakeUSDCSignerForIntegration{},
	}

	// The relay handler itself, served over real HTTP — the "agent" below
	// only ever talks to it via httptest.NewServer + http.Get/http.Do, never
	// by calling d.X402Relay or any unexported function directly.
	relay := httptest.NewServer(http.HandlerFunc(d.X402Relay))
	defer relay.Close()

	// Agent's first call: no payment, gets the mirrored 402 tagged for the
	// challenge (payTo = platform wallet, price mirrored from the target).
	quoteResp, err := http.Get(relay.URL + "?target=" + target.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer quoteResp.Body.Close()
	if quoteResp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("want 402 on first call, got %d", quoteResp.StatusCode)
	}
	var quoteBody struct {
		Accepts []map[string]any `json:"accepts"`
	}
	if err := json.NewDecoder(quoteResp.Body).Decode(&quoteBody); err != nil {
		t.Fatal(err)
	}
	if len(quoteBody.Accepts) != 1 || quoteBody.Accepts[0]["payTo"] != "PLATFORMADDR" {
		t.Fatalf("want challenge mirrored with payTo=PLATFORMADDR, got %v", quoteBody.Accepts)
	}
	if len(facilitatorCalls) != 0 {
		t.Fatalf("want zero facilitator calls before any payment is presented, got %v", facilitatorCalls)
	}

	// Agent's second call: with payment, should verify+settle the inbound
	// leg via the fake facilitator, pay the real target from the platform
	// wallet, and relay the target's real paid response back.
	payload, _ := json.Marshal(map[string]any{"x402Version": 2, "scheme": "exact", "network": "algorand:testnet"})
	payReq, err := http.NewRequest(http.MethodGet, relay.URL+"?target="+target.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	payReq.Header.Set("X-Payment", string(payload))
	payResp, err := http.DefaultClient.Do(payReq)
	if err != nil {
		t.Fatal(err)
	}
	defer payResp.Body.Close()
	if payResp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 on paid call, got %d", payResp.StatusCode)
	}
	var final map[string]any
	if err := json.NewDecoder(payResp.Body).Decode(&final); err != nil {
		t.Fatal(err)
	}
	if final["data"] != "real downstream result" {
		t.Fatalf("want the target's real response relayed back to the caller, got %v", final)
	}

	// Exactly 2 facilitator calls for the inbound leg: /verify then /settle.
	// The outbound leg to the target never touches FacilitatorClient (the
	// relay signs and sends that payment directly via USDCSigner), so this
	// also proves no extra/duplicate facilitator round trips happened.
	want := []string{"/verify", "/settle"}
	if !reflect.DeepEqual(facilitatorCalls, want) {
		t.Fatalf("want exactly %v facilitator calls (verify+settle for the inbound leg only), got %v", want, facilitatorCalls)
	}

	// The inbound settlement should be recorded and, after the outbound leg
	// to the target also succeeded, marked settled with the real quoted
	// amount — confirming the ledger row this whole flow is supposed to
	// produce actually exists and is correct, not just that an HTTP 200 came
	// back.
	row, err := store.GetX402RelaySettlementByInboundTx(context.Background(), "INBOUND-TX-"+target.URL)
	if err != nil {
		t.Fatalf("want to find the recorded ledger row: %v", err)
	}
	if row.AmountAssetMicros != 75000 {
		t.Fatalf("want ledger row to record the target's real quoted amount (75000), got %d", row.AmountAssetMicros)
	}
	if row.Status != "settled" {
		t.Fatalf("want ledger row status settled after the outbound leg succeeded, got %q", row.Status)
	}
}

// fakeUSDCSignerForIntegration is a minimal stand-in for the real
// wallet.Service-backed signer, matching the established fake pattern in
// backend/internal/api/handlers/x402relay_test.go's fakeUSDCSigner: this
// test proves the relay's HTTP orchestration end-to-end, not real Algorand
// transaction signing, so it returns a fixed fake payment group.
type fakeUSDCSignerForIntegration struct{}

func (f *fakeUSDCSignerForIntegration) SignUSDCPaymentGroup(_ context.Context, _, _ string, _, _ uint64, _ string) ([]string, int, error) {
	return []string{"g0", "g1"}, 0, nil
}
