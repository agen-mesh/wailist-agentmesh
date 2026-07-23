package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/x402"
)

// TestMain relaxes the SSRF guard for this package's tests, mirroring the
// identical override in internal/engine/nodes and internal/engine's own
// TestMain — without it, the relay's SSRF check (added specifically because
// this route is public and unauthenticated) blocks every httptest.NewServer
// target (127.0.0.1), which is exactly what these tests use as fake
// downstream targets and a fake facilitator. No test in this package
// exercises the real SSRF-blocking validator.
func TestMain(m *testing.M) {
	nodes.SetURLValidatorForTest(func(string) error { return nil })
	os.Exit(m.Run())
}

func TestX402RelayNoPaymentMirrorsTargetPriceAsChallengeTag(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Payment") != "" {
			w.Write([]byte(`{"data":"paid response"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]any{
			"x402Version": 2,
			"accepts": []map[string]any{{
				"scheme":            "exact",
				"network":           "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=",
				"maxAmountRequired": "100000",
				"payTo":             "TARGETADDR",
				"asset":             "10458941",
			}},
		})
	}))
	defer target.Close()

	d := &handlers.Deps{
		PlatformWalletAddress: "PLATFORMADDR",
		USDCAssetID:           10458941,
		RelayNetwork:          "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=",
		RelayFeePayer:         "FEEPAYERADDR",
	}
	req := httptest.NewRequest(http.MethodGet, "/x402/relay?target="+target.URL, nil)
	w := httptest.NewRecorder()

	d.X402Relay(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("want 402, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Accepts []map[string]any `json:"accepts"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Accepts) != 1 {
		t.Fatalf("want 1 accepts entry, got %d", len(body.Accepts))
	}
	extra, _ := body.Accepts[0]["extra"].(map[string]any)
	if extra["tag"] != "x402-global-challenge" {
		t.Fatalf("want tag x402-global-challenge in extra, got %v", extra)
	}
	if body.Accepts[0]["payTo"] != "PLATFORMADDR" {
		t.Fatalf("want payTo=PLATFORMADDR (our own wallet, not the target's), got %v", body.Accepts[0]["payTo"])
	}
	if body.Accepts[0]["maxAmountRequired"] != "100000" {
		t.Fatalf("want price mirrored from target (100000), got %v", body.Accepts[0]["maxAmountRequired"])
	}
	_ = x402.PaymentPayload{} // referenced so import stays used once payment-path test is added below
}

func newTestStoreForHandlers(t *testing.T) *db.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

// signCall captures one invocation of fakeUSDCSigner.SignUSDCPaymentGroup, so
// tests can assert on exactly what the relay actually signed and broadcast
// for the outbound platform-wallet payment.
type signCall struct {
	payTo        string
	assetID      uint64
	amountMicros uint64
}

type fakeUSDCSigner struct {
	group []string
	idx   int
	err   error

	calls []signCall
}

func (f *fakeUSDCSigner) SignUSDCPaymentGroup(_ context.Context, _, payTo string, assetID, amountMicros uint64, _ string) ([]string, int, error) {
	f.calls = append(f.calls, signCall{payTo: payTo, assetID: assetID, amountMicros: amountMicros})
	return f.group, f.idx, f.err
}

func TestX402RelayPaysTargetFromPlatformWalletAfterInboundSettles(t *testing.T) {
	var targetGotXPayment string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("X-Payment"); h != "" {
			targetGotXPayment = h
			w.Write([]byte(`{"data":"paid response from target"}`))
			return
		}
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]any{
			"accepts": []map[string]any{{"payTo": "TARGETADDR", "asset": "10458941", "maxAmountRequired": "50000"}},
		})
	}))
	defer target.Close()

	store := newTestStoreForHandlers(t) // TEST_DATABASE_URL-gated, see helper below

	// Captures the paymentRequirements the relay sent on /verify and /settle
	// so the test can assert the real target-quoted amount (50000, from the
	// fake target above) was actually threaded through and enforced, rather
	// than the previous hardcoded-0/no-enforcement behavior.
	var verifyReqs, settleReqs struct {
		PaymentRequirements struct {
			MaxAmountRequired string `json:"maxAmountRequired"`
		} `json:"paymentRequirements"`
	}
	facilitator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/verify" {
			json.Unmarshal(body, &verifyReqs)
			json.NewEncoder(w).Encode(map[string]any{"isValid": true})
			return
		}
		json.Unmarshal(body, &settleReqs)
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
		USDCSigner:                &fakeUSDCSigner{group: []string{"g0", "g1"}, idx: 0},
	}

	payload, _ := json.Marshal(map[string]any{"x402Version": 2, "scheme": "exact", "network": "algorand:testnet"})
	req := httptest.NewRequest(http.MethodGet, "/x402/relay?target="+target.URL, nil)
	req.Header.Set("X-Payment", string(payload))
	w := httptest.NewRecorder()

	d.X402Relay(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if targetGotXPayment == "" {
		t.Fatal("want relay to have paid the target with its own X-Payment header")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("paid response from target")) {
		t.Fatalf("want target's response relayed back, got %s", w.Body.String())
	}

	if verifyReqs.PaymentRequirements.MaxAmountRequired != "50000" {
		t.Fatalf("want facilitator Verify called with MaxAmountRequired=50000 (the target's real quote, for price enforcement), got %q", verifyReqs.PaymentRequirements.MaxAmountRequired)
	}
	if settleReqs.PaymentRequirements.MaxAmountRequired != "50000" {
		t.Fatalf("want facilitator Settle called with MaxAmountRequired=50000, got %q", settleReqs.PaymentRequirements.MaxAmountRequired)
	}

	row, err := store.GetX402RelaySettlementByInboundTx(context.Background(), "INBOUND-TX-"+target.URL)
	if err != nil {
		t.Fatalf("want to find the recorded ledger row: %v", err)
	}
	if row.AmountAssetMicros != 50000 {
		t.Fatalf("want ledger row to record the real settled amount (50000), got %d", row.AmountAssetMicros)
	}
}

// TestX402RelayUsesSingleQuoteForBothSettlementAndOutboundPayment is a
// regression test for a fund-drain vector: relaySettleAndForward used to
// fetch the target's price quote once (to enforce/record it), and
// payTargetAndRespond then independently re-fetched the SAME caller-supplied
// target URL a second time to learn what to actually sign and pay. Since
// `target` is arbitrary and attacker-controlled on this public,
// unauthenticated route, a malicious target could answer the first
// (price-enforcement) fetch with a cheap price and the second (pay-time)
// fetch with an inflated amount and/or a different payTo — causing the
// platform wallet to sign and broadcast a payment for more than was ever
// collected from the caller, to an address the caller chose.
//
// The fake target below tracks how many unauthenticated (no X-Payment)
// requests it has received and answers the first one cheaply
// (TARGETADDR-CHEAP / 50000) and every subsequent one expensively
// (ATTACKERADDR / 999000000). Under the old buggy code this is exactly two
// independent fetches — one in relaySettleAndForward, one in
// payTargetAndRespond — so the outbound payment would be signed for
// 999000000 to ATTACKERADDR while the ledger recorded only 50000. Under the
// fixed code there is only one fetch, reused for both purposes, so the
// signed outbound payment must match the cheap, recorded amount and address.
func TestX402RelayUsesSingleQuoteForBothSettlementAndOutboundPayment(t *testing.T) {
	var quoteFetches int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Payment") != "" {
			w.Write([]byte(`{"data":"paid response from target"}`))
			return
		}
		quoteFetches++
		w.WriteHeader(http.StatusPaymentRequired)
		if quoteFetches == 1 {
			json.NewEncoder(w).Encode(map[string]any{
				"accepts": []map[string]any{{"payTo": "TARGETADDR-CHEAP", "asset": "10458941", "maxAmountRequired": "50000"}},
			})
			return
		}
		// A malicious target would only do this on a second, pay-time-only
		// fetch that should never happen under the fix.
		json.NewEncoder(w).Encode(map[string]any{
			"accepts": []map[string]any{{"payTo": "ATTACKERADDR", "asset": "10458941", "maxAmountRequired": "999000000"}},
		})
	}))
	defer target.Close()

	store := newTestStoreForHandlers(t)

	facilitator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/verify" {
			_ = body
			json.NewEncoder(w).Encode(map[string]any{"isValid": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "transaction": "INBOUND-TX-SINGLEQUOTE-" + target.URL})
	}))
	defer facilitator.Close()

	signer := &fakeUSDCSigner{group: []string{"g0", "g1"}, idx: 0}
	d := &handlers.Deps{
		Store:                     store,
		PlatformWalletAddress:     "PLATFORMADDR",
		PlatformWalletEncMnemonic: "enc-mnemonic",
		FacilitatorClient:         x402.NewFacilitatorClient(facilitator.URL),
		USDCAssetID:               10458941,
		RelayNetwork:              "algorand:testnet",
		RelayFeePayer:             "FEEPAYERADDR",
		USDCSigner:                signer,
	}

	payload, _ := json.Marshal(map[string]any{"x402Version": 2, "scheme": "exact", "network": "algorand:testnet"})
	req := httptest.NewRequest(http.MethodGet, "/x402/relay?target="+target.URL, nil)
	req.Header.Set("X-Payment", string(payload))
	w := httptest.NewRecorder()

	d.X402Relay(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	row, err := store.GetX402RelaySettlementByInboundTx(context.Background(), "INBOUND-TX-SINGLEQUOTE-"+target.URL)
	if err != nil {
		t.Fatalf("want to find the recorded ledger row: %v", err)
	}
	if row.AmountAssetMicros != 50000 {
		t.Fatalf("want ledger row to record the cheap quoted amount (50000), got %d", row.AmountAssetMicros)
	}

	if quoteFetches != 1 {
		t.Fatalf("want exactly one price-quote fetch of the caller-supplied target per relay cycle, got %d — a second independent fetch is exactly the fund-drain gap this test guards against", quoteFetches)
	}

	if len(signer.calls) != 1 {
		t.Fatalf("want exactly one outbound sign call, got %d", len(signer.calls))
	}
	got := signer.calls[0]
	if uint64(row.AmountAssetMicros) != got.amountMicros {
		t.Fatalf("want the amount actually signed for the outbound payment (%d) to match the amount recorded in the ledger (%d) — a mismatch means the relay paid a different amount than it collected/recorded", got.amountMicros, row.AmountAssetMicros)
	}
	if got.payTo != "TARGETADDR-CHEAP" {
		t.Fatalf("want outbound payment signed to the same payTo used for the recorded settlement (TARGETADDR-CHEAP), got %q — this is the attacker-address-substitution half of the fund-drain vector", got.payTo)
	}
}
