package nodes_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/engine/nodes"
)

// fakeWallet2Signer is a minimal USDCGroupSigner double for
// PayTargetFromWallet2 tests -- records every call it received (which
// method, and the feePayer arg for group calls) and returns a canned
// group/error.
type fakeWallet2Signer struct {
	group []string
	idx   int
	err   error
	calls int

	groupCalls   int
	singleCalls  int
	lastFeePayer string
}

func (f *fakeWallet2Signer) SignUSDCPaymentGroup(_ context.Context, _, _ string, _, _ uint64, feePayerAddr string) ([]string, int, error) {
	f.calls++
	f.groupCalls++
	f.lastFeePayer = feePayerAddr
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.group, f.idx, nil
}

func (f *fakeWallet2Signer) SignUSDCPaymentSingle(_ context.Context, _, _ string, _, _ uint64) ([]string, int, error) {
	f.calls++
	f.singleCalls++
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.group, f.idx, nil
}

func baseWallet2Cfg(signer nodes.USDCGroupSigner) nodes.Wallet2PayConfig {
	return nodes.Wallet2PayConfig{
		USDCSigner:                signer,
		PlatformWalletEncMnemonic: "enc-mnemonic",
		USDCAssetID:               10458941,
		RelayNetwork:              "algorand:testnet",
	}
}

func TestPayTargetFromWallet2RejectsUnexpectedAsset(t *testing.T) {
	signer := &fakeWallet2Signer{group: []string{"g0", "g1"}, idx: 0}
	cfg := baseWallet2Cfg(signer)

	quote := nodes.TargetQuote{PayTo: "TARGETADDR", Asset: "99999999", MaxAmountRequired: "50000"}
	result, err := nodes.PayTargetFromWallet2(context.Background(), cfg, "http://unused.example", "", nil, quote)

	if err == nil {
		t.Fatal("want error for mismatched asset id")
	}
	var payErr *nodes.Wallet2PayError
	if !errors.As(err, &payErr) {
		t.Fatalf("want *nodes.Wallet2PayError, got %T", err)
	}
	if payErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", payErr.StatusCode)
	}
	if result.Signed {
		t.Fatal("want Signed=false when asset id is rejected before signing")
	}
	if signer.calls != 0 {
		t.Fatalf("want signer never called, got %d calls", signer.calls)
	}
}

func TestPayTargetFromWallet2RejectsOverCap(t *testing.T) {
	signer := &fakeWallet2Signer{group: []string{"g0", "g1"}, idx: 0}
	cfg := baseWallet2Cfg(signer)
	cfg.MaxRelayOutboundUSDMicros = 10000

	quote := nodes.TargetQuote{PayTo: "TARGETADDR", Asset: "10458941", MaxAmountRequired: "50000"}
	result, err := nodes.PayTargetFromWallet2(context.Background(), cfg, "http://unused.example", "", nil, quote)

	if err == nil {
		t.Fatal("want error for amount exceeding cap")
	}
	var payErr *nodes.Wallet2PayError
	if !errors.As(err, &payErr) {
		t.Fatalf("want *nodes.Wallet2PayError, got %T", err)
	}
	if payErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", payErr.StatusCode)
	}
	if result.Signed {
		t.Fatal("want Signed=false when amount exceeds cap, checked before signing")
	}
	if signer.calls != 0 {
		t.Fatalf("want signer never called, got %d calls", signer.calls)
	}
}

func TestPayTargetFromWallet2SignFailureReturns500(t *testing.T) {
	signer := &fakeWallet2Signer{err: errors.New("invalid receiver address")}
	cfg := baseWallet2Cfg(signer)

	quote := nodes.TargetQuote{PayTo: "TARGETADDR", Asset: "10458941", MaxAmountRequired: "50000"}
	result, err := nodes.PayTargetFromWallet2(context.Background(), cfg, "http://unused.example", "", nil, quote)

	if err == nil {
		t.Fatal("want error when signing fails")
	}
	var payErr *nodes.Wallet2PayError
	if !errors.As(err, &payErr) {
		t.Fatalf("want *nodes.Wallet2PayError, got %T", err)
	}
	if payErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", payErr.StatusCode)
	}
	if result.Signed {
		t.Fatal("want Signed=false when signing itself fails")
	}
}

// TestPayTargetFromWallet2TargetNetworkFailureStillSigned is a
// reproduce-then-fix regression test: a network failure reaching the target
// AFTER a real signed payment group already exists must still report
// Signed=true, matching the pre-existing billing philosophy in
// x402relay.go -- money already committed isn't unwound by a downstream
// network hiccup. (Verified by temporarily changing the target-request-error
// branch in walletpay.go to return Wallet2PayResult{Signed: false} --
// confirmed this test fails that way, then restored Signed: true and
// confirmed it passes.)
func TestPayTargetFromWallet2TargetNetworkFailureStillSigned(t *testing.T) {
	signer := &fakeWallet2Signer{group: []string{"g0", "g1"}, idx: 0}
	cfg := baseWallet2Cfg(signer)

	// A target server that immediately closes so the outbound request fails
	// at the transport level rather than getting a normal HTTP response.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	target.Close() // closed before use -- guarantees a connection-refused error

	quote := nodes.TargetQuote{PayTo: "TARGETADDR", Asset: "10458941", MaxAmountRequired: "50000"}
	result, err := nodes.PayTargetFromWallet2(context.Background(), cfg, target.URL, "", nil, quote)

	if err == nil {
		t.Fatal("want error when the paid request to target fails")
	}
	var payErr *nodes.Wallet2PayError
	if !errors.As(err, &payErr) {
		t.Fatalf("want *nodes.Wallet2PayError, got %T", err)
	}
	if payErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", payErr.StatusCode)
	}
	if !result.Signed {
		t.Fatal("want Signed=true even though the target request failed -- a real signed group already exists by this point")
	}
	if signer.calls != 1 {
		t.Fatalf("want exactly one sign call, got %d", signer.calls)
	}
}

func TestPayTargetFromWallet2SuccessReturnsTargetResponse(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Payment") == "" {
			t.Fatal("want request to target to carry the signed X-Payment header")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":"paid response from target"}`))
	}))
	defer target.Close()

	signer := &fakeWallet2Signer{group: []string{"g0", "g1"}, idx: 0}
	cfg := baseWallet2Cfg(signer)

	quote := nodes.TargetQuote{PayTo: "TARGETADDR", Asset: "10458941", MaxAmountRequired: "50000"}
	result, err := nodes.PayTargetFromWallet2(context.Background(), cfg, target.URL, "", nil, quote)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Signed {
		t.Fatal("want Signed=true on success")
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", result.StatusCode)
	}
	if !result.Settled {
		t.Fatal("want Settled=true for a 2xx target response")
	}
	if string(result.ResponseBody) != `{"data":"paid response from target"}` {
		t.Fatalf("want target's response body relayed through, got %q", result.ResponseBody)
	}
	if signer.calls != 1 {
		t.Fatalf("want exactly one sign call, got %d", signer.calls)
	}
}

// TestPayTargetFromWallet2ExtractsOutboundTxIDFromPaymentResponseHeader
// confirms a real target's own settlement id (Wallet 2 -> target) is
// surfaced back -- confirmed live 2026-08-01: canix402-api.compx.io
// returns {"success":true,...,"transaction":"<real txid>"} base64-encoded
// under a Payment-Response header on a successful paid retry. This is what
// lets a workflow's console log show both real payment legs, not just the
// inbound one (caller -> Wallet 2).
func TestPayTargetFromWallet2ExtractsOutboundTxIDFromPaymentResponseHeader(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := `{"success":true,"payer":"WALLET2ADDR","transaction":"REALOUTBOUNDTXID123","network":"algorand:mainnet"}`
		w.Header().Set("Payment-Response", base64.StdEncoding.EncodeToString([]byte(payload)))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":"paid response from target"}`))
	}))
	defer target.Close()

	signer := &fakeWallet2Signer{group: []string{"g0", "g1"}, idx: 0}
	cfg := baseWallet2Cfg(signer)

	quote := nodes.TargetQuote{PayTo: "TARGETADDR", Asset: "10458941", MaxAmountRequired: "50000"}
	result, err := nodes.PayTargetFromWallet2(context.Background(), cfg, target.URL, "", nil, quote)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OutboundTxID != "REALOUTBOUNDTXID123" {
		t.Fatalf("want OutboundTxID extracted from Payment-Response header, got %q", result.OutboundTxID)
	}
}

// TestPayTargetFromWallet2OutboundTxIDEmptyWhenTargetReturnsNone confirms
// a target that doesn't return a Payment-Response header (most don't) just
// leaves OutboundTxID empty -- not an error, Settled/StatusCode already
// say whether the payment worked.
func TestPayTargetFromWallet2OutboundTxIDEmptyWhenTargetReturnsNone(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":"paid response from target"}`))
	}))
	defer target.Close()

	signer := &fakeWallet2Signer{group: []string{"g0", "g1"}, idx: 0}
	cfg := baseWallet2Cfg(signer)

	quote := nodes.TargetQuote{PayTo: "TARGETADDR", Asset: "10458941", MaxAmountRequired: "50000"}
	result, err := nodes.PayTargetFromWallet2(context.Background(), cfg, target.URL, "", nil, quote)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OutboundTxID != "" {
		t.Fatalf("want empty OutboundTxID when target returns no Payment-Response header, got %q", result.OutboundTxID)
	}
}

// TestPayTargetFromWallet2UsesGroupSignerWhenTargetNamesFeePayer
// reproduces a real bug found via live mainnet testing 2026-08-01: a real
// target (arbsignal-production.up.railway.app) named a feePayer in its own
// challenge's accepts[0].extra, but PayTargetFromWallet2 always signed a
// plain single transaction regardless -- the target's middleware expected
// the fee-pooled group convention (verified via a facilitator), didn't
// recognize what it got, and kept re-402'ing a payment that had, in fact,
// already left the wallet. quote.FeePayer must route to
// SignUSDCPaymentGroup, with that exact address passed through.
func TestPayTargetFromWallet2UsesGroupSignerWhenTargetNamesFeePayer(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"pair":"goBTC/USDC"}`))
	}))
	defer target.Close()

	signer := &fakeWallet2Signer{group: []string{"g0", "g1"}, idx: 1}
	cfg := baseWallet2Cfg(signer)

	quote := nodes.TargetQuote{
		PayTo: "TARGETADDR", Asset: "10458941", MaxAmountRequired: "1000",
		FeePayer: "ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA",
	}
	result, err := nodes.PayTargetFromWallet2(context.Background(), cfg, target.URL, "", nil, quote)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Settled {
		t.Fatal("want Settled=true")
	}
	if signer.groupCalls != 1 {
		t.Fatalf("want SignUSDCPaymentGroup called once, got %d", signer.groupCalls)
	}
	if signer.singleCalls != 0 {
		t.Fatalf("want SignUSDCPaymentSingle never called, got %d", signer.singleCalls)
	}
	if signer.lastFeePayer != "ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA" {
		t.Fatalf("want the target's own feePayer passed through, got %q", signer.lastFeePayer)
	}
}

// TestPayTargetFromWallet2UsesSingleSignerWhenTargetNamesNoFeePayer keeps
// the pre-existing behavior for a target whose challenge omits
// extra.feePayer entirely -- the plain, self-fee-paying "exact" scheme.
func TestPayTargetFromWallet2UsesSingleSignerWhenTargetNamesNoFeePayer(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	signer := &fakeWallet2Signer{group: []string{"g0"}, idx: 0}
	cfg := baseWallet2Cfg(signer)

	quote := nodes.TargetQuote{PayTo: "TARGETADDR", Asset: "10458941", MaxAmountRequired: "1000"}
	_, err := nodes.PayTargetFromWallet2(context.Background(), cfg, target.URL, "", nil, quote)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if signer.singleCalls != 1 {
		t.Fatalf("want SignUSDCPaymentSingle called once, got %d", signer.singleCalls)
	}
	if signer.groupCalls != 0 {
		t.Fatalf("want SignUSDCPaymentGroup never called, got %d", signer.groupCalls)
	}
}

func TestPayTargetFromWallet2NonSuccessStatusNotSettled(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"error":"still not enough"}`))
	}))
	defer target.Close()

	signer := &fakeWallet2Signer{group: []string{"g0", "g1"}, idx: 0}
	cfg := baseWallet2Cfg(signer)

	quote := nodes.TargetQuote{PayTo: "TARGETADDR", Asset: "10458941", MaxAmountRequired: "50000"}
	result, err := nodes.PayTargetFromWallet2(context.Background(), cfg, target.URL, "", nil, quote)

	// A non-2xx from the target is not itself a Wallet2PayError -- the paid
	// HTTP request to target succeeded at the transport level; it's the
	// target's own status code that says the payment wasn't accepted. The
	// caller (payTargetAndRespond) still relays this status/body through.
	if err != nil {
		t.Fatalf("want no error for a non-2xx target response (transport succeeded), got %v", err)
	}
	if !result.Signed {
		t.Fatal("want Signed=true -- a real signed group exists regardless of target's response status")
	}
	if result.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("want target's real status code (402) surfaced, got %d", result.StatusCode)
	}
	if result.Settled {
		t.Fatal("want Settled=false for a non-2xx target response")
	}
}

// TestPayTargetFromWallet2SendsConfiguredMethodAndBody is a
// reproduce-then-fix regression test for the real bug found live against
// Prism (https://prism-99h2.onrender.com/resume-screen-accurate) on
// 2026-07-31: a POST-only x402 target 404s a bare GET before it ever
// considers payment state at all, so the pre-existing hardcoded-GET/nil-body
// call could never even reach a POST-only real endpoint's paid leg. This
// target mimics that shape (404 on GET, real handling on POST) and asserts
// the paid request actually arrives as POST with the configured body and a
// JSON content type.
func TestPayTargetFromWallet2SendsConfiguredMethodAndBody(t *testing.T) {
	const wantBody = `{"task_description":"test","files":[]}`
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound) // mirrors Prism's real behavior for GET
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("want Content-Type application/json on the paid POST, got %q", ct)
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != wantBody {
			t.Fatalf("want body %q forwarded to target, got %q", wantBody, b)
		}
		if r.Header.Get("X-Payment") == "" {
			t.Fatal("want X-Payment header on the paid POST too")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"candidates":[]}`))
	}))
	defer target.Close()

	signer := &fakeWallet2Signer{group: []string{"g0", "g1"}, idx: 0}
	cfg := baseWallet2Cfg(signer)

	quote := nodes.TargetQuote{PayTo: "TARGETADDR", Asset: "10458941", MaxAmountRequired: "50000"}
	result, err := nodes.PayTargetFromWallet2(context.Background(), cfg, target.URL, http.MethodPost, []byte(wantBody), quote)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Settled || result.StatusCode != http.StatusOK {
		t.Fatalf("want a settled 200 once the target actually saw POST+body, got status=%d settled=%v", result.StatusCode, result.Settled)
	}
}
