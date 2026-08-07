package nodes_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/x402"
)

// fakeRunFundUSDCSigner is a minimal USDCGroupSigner double for
// FundRunReserve tests — it doesn't need to produce a real signed group,
// just something FundRunReserve's payload can carry through to the fake
// facilitator below.
type fakeRunFundUSDCSigner struct {
	group  []string
	idx    int
	err    error
	called bool
}

func (f *fakeRunFundUSDCSigner) SignUSDCPaymentGroup(_ context.Context, _, _ string, _, _ uint64, _ string) ([]string, int, error) {
	f.called = true
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.group, f.idx, nil
}

// SignUSDCPaymentSingle is never exercised by FundRunReserve (it always uses
// the fee-pooled SignUSDCPaymentGroup) — this stub only satisfies the
// interface.
func (f *fakeRunFundUSDCSigner) SignUSDCPaymentSingle(_ context.Context, _, _ string, _, _ uint64) ([]string, int, error) {
	return f.group, f.idx, f.err
}

func TestFundRunReserveNoopOnNonPositiveAmount(t *testing.T) {
	facilitatorHit := false
	facilitator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		facilitatorHit = true
	}))
	defer facilitator.Close()

	signer := &fakeRunFundUSDCSigner{group: []string{"g0", "g1"}, idx: 0}
	cfg := nodes.RunPreFundConfig{
		USDCSigner:               signer,
		PlatformSpendEncMnemonic: "platform-enc-mnemonic",
		Facilitator:              x402.NewFacilitatorClient(facilitator.URL),
		PlatformWalletAddress:    "PLATFORMADDR",
		RelayNetwork:             "algorand:testnet",
		RelayFeePayer:            "FEEPAYERADDR",
		ExpectedAssetID:          10458941,
		FrontendURL:              "https://example.test",
	}

	for _, amount := range []int64{0, -1, -100} {
		txID, err := nodes.FundRunReserve(context.Background(), cfg, "run-1", amount)
		if err != nil {
			t.Fatalf("amount %d: unexpected error: %v", amount, err)
		}
		if txID != "" {
			t.Fatalf("amount %d: want empty txID, got %q", amount, txID)
		}
	}

	if signer.called {
		t.Fatal("want signer never called for a non-positive amount")
	}
	if facilitatorHit {
		t.Fatal("want facilitator never called for a non-positive amount")
	}
}

func TestFundRunReserveSuccess(t *testing.T) {
	const wantTxID = "RUNFUND-TX-123"

	var verifyReqs, settleReqs struct {
		PaymentRequirements x402.PaymentRequirements `json:"paymentRequirements"`
	}

	facilitator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/verify":
			json.Unmarshal(body, &verifyReqs)
			json.NewEncoder(w).Encode(map[string]any{"isValid": true})
		case "/settle":
			json.Unmarshal(body, &settleReqs)
			json.NewEncoder(w).Encode(map[string]any{"success": true, "transaction": wantTxID})
		default:
			t.Fatalf("unexpected facilitator path: %s", r.URL.Path)
		}
	}))
	defer facilitator.Close()

	signer := &fakeRunFundUSDCSigner{group: []string{"g0", "g1"}, idx: 0}
	cfg := nodes.RunPreFundConfig{
		USDCSigner:               signer,
		PlatformSpendEncMnemonic: "platform-enc-mnemonic",
		Facilitator:              x402.NewFacilitatorClient(facilitator.URL),
		PlatformWalletAddress:    "PLATFORMADDR",
		RelayNetwork:             "algorand:testnet",
		RelayFeePayer:            "FEEPAYERADDR",
		ExpectedAssetID:          10458941,
		FrontendURL:              "https://example.test",
	}

	txID, err := nodes.FundRunReserve(context.Background(), cfg, "run-1", 500000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if txID != wantTxID {
		t.Fatalf("want txID %q, got %q", wantTxID, txID)
	}
	if !signer.called {
		t.Fatal("want signer to have been called")
	}

	if verifyReqs.PaymentRequirements.PayTo != "PLATFORMADDR" {
		t.Fatalf("want verify PayTo=PLATFORMADDR (same payTo as every per-call relay settlement), got %q", verifyReqs.PaymentRequirements.PayTo)
	}
	if verifyReqs.PaymentRequirements.Extra["tag"] != "x402-global-challenge" {
		t.Fatalf("want verify Extra.tag=x402-global-challenge, got %v", verifyReqs.PaymentRequirements.Extra["tag"])
	}
	if settleReqs.PaymentRequirements.PayTo != "PLATFORMADDR" {
		t.Fatalf("want settle PayTo=PLATFORMADDR, got %q", settleReqs.PaymentRequirements.PayTo)
	}
	if settleReqs.PaymentRequirements.Extra["tag"] != "x402-global-challenge" {
		t.Fatalf("want settle Extra.tag=x402-global-challenge, got %v", settleReqs.PaymentRequirements.Extra["tag"])
	}
	// Resource is cfg.BaseURL + the real /x402/relay/run-funding path, not a
	// per-run runId-specific URL -- matches every actually-cataloged real
	// resource's convention of a real API endpoint on the resource server's
	// own domain, not an opaque identifier or a separate marketing domain.
	if want := "https://example.test/api/x402/relay/run-funding"; settleReqs.PaymentRequirements.Resource != want {
		t.Fatalf("want Resource=%q, got %q", want, settleReqs.PaymentRequirements.Resource)
	}
}

func TestFundRunReserveVerifyInvalidSurfacesError(t *testing.T) {
	facilitator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/verify":
			json.NewEncoder(w).Encode(map[string]any{"isValid": false, "invalidReason": "insufficient funds"})
		case "/settle":
			t.Fatal("want settle never called when verify says invalid")
		}
	}))
	defer facilitator.Close()

	signer := &fakeRunFundUSDCSigner{group: []string{"g0", "g1"}, idx: 0}
	cfg := nodes.RunPreFundConfig{
		USDCSigner:               signer,
		PlatformSpendEncMnemonic: "platform-enc-mnemonic",
		Facilitator:              x402.NewFacilitatorClient(facilitator.URL),
		PlatformWalletAddress:    "PLATFORMADDR",
		RelayNetwork:             "algorand:testnet",
		RelayFeePayer:            "FEEPAYERADDR",
		ExpectedAssetID:          10458941,
		FrontendURL:              "https://example.test",
	}

	txID, err := nodes.FundRunReserve(context.Background(), cfg, "run-1", 500000)
	if err == nil {
		t.Fatal("want error when verify reports invalid")
	}
	if txID != "" {
		t.Fatalf("want empty txID on error, got %q", txID)
	}
	if !strings.Contains(err.Error(), "insufficient funds") {
		t.Fatalf("want error to surface facilitator's invalid reason, got %v", err)
	}
}

func TestFundRunReserveSettleFailureSurfacesError(t *testing.T) {
	facilitator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/verify":
			json.NewEncoder(w).Encode(map[string]any{"isValid": true})
		case "/settle":
			json.NewEncoder(w).Encode(map[string]any{"success": false, "errorReason": "settlement rejected"})
		}
	}))
	defer facilitator.Close()

	signer := &fakeRunFundUSDCSigner{group: []string{"g0", "g1"}, idx: 0}
	cfg := nodes.RunPreFundConfig{
		USDCSigner:               signer,
		PlatformSpendEncMnemonic: "platform-enc-mnemonic",
		Facilitator:              x402.NewFacilitatorClient(facilitator.URL),
		PlatformWalletAddress:    "PLATFORMADDR",
		RelayNetwork:             "algorand:testnet",
		RelayFeePayer:            "FEEPAYERADDR",
		ExpectedAssetID:          10458941,
		FrontendURL:              "https://example.test",
	}

	txID, err := nodes.FundRunReserve(context.Background(), cfg, "run-1", 500000)
	if err == nil {
		t.Fatal("want error when settle reports failure")
	}
	if txID != "" {
		t.Fatalf("want empty txID on error, got %q", txID)
	}
	if !strings.Contains(err.Error(), "settlement rejected") {
		t.Fatalf("want error to surface facilitator's error reason, got %v", err)
	}
}

func TestFundRunReserveSettleTransportErrorIsIndeterminate(t *testing.T) {
	facilitator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/verify":
			json.NewEncoder(w).Encode(map[string]any{"isValid": true})
		case "/settle":
			// No decodable response body -- simulates the response being
			// lost after the facilitator accepted the request (timeout,
			// connection reset), not a definitive rejection.
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer facilitator.Close()

	signer := &fakeRunFundUSDCSigner{group: []string{"g0", "g1"}, idx: 0}
	cfg := nodes.RunPreFundConfig{
		USDCSigner:               signer,
		PlatformSpendEncMnemonic: "platform-enc-mnemonic",
		Facilitator:              x402.NewFacilitatorClient(facilitator.URL),
		PlatformWalletAddress:    "PLATFORMADDR",
		RelayNetwork:             "algorand:testnet",
		RelayFeePayer:            "FEEPAYERADDR",
		ExpectedAssetID:          10458941,
		FrontendURL:              "https://example.test",
	}

	txID, err := nodes.FundRunReserve(context.Background(), cfg, "run-1", 500000)
	if err == nil {
		t.Fatal("want error when the settle response is lost")
	}
	if txID != "" {
		t.Fatalf("want empty txID on error, got %q", txID)
	}
	if !errors.Is(err, nodes.ErrSettlementIndeterminate) {
		t.Fatalf("want error to wrap ErrSettlementIndeterminate (fate of the payment unknown), got %v", err)
	}
}
