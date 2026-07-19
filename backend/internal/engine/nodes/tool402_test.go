package nodes_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

// TestMain sets the permissive URL validator once for the whole nodes_test
// binary, so every test that dials an httptest.NewServer target works
// regardless of file/test execution order. No test in this package exercises
// the real SSRF-blocking validator, so there's nothing to preserve by
// toggling it per-test.
func TestMain(m *testing.M) {
	nodes.SetURLValidatorForTest(func(string) error { return nil })
	os.Exit(m.Run())
}

type mockSigner struct {
	txID string
	err  error
}

func (m *mockSigner) SignAndSendPayment(_ context.Context, _, _ string, _ uint64) (string, error) {
	return m.txID, m.err
}

func TestX402FreeEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":"free response"}`))
	}))
	defer srv.Close()
	node := models.WorkflowNode{ID: "x1", Type: models.NodeTypeTool402, Endpoint: srv.URL}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteTool402(context.Background(), node, rc, models.AgentWallet{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]any)
	if !ok || m["data"] != "free response" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestX402ParseQuote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Payment-Required", `{"price":"0.001","unit":"call","network":"algorand-testnet","recipient":"ALGO123"}`)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()
	price, err := nodes.QuoteX402(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if price["price"] != "0.001" {
		t.Fatalf("want price 0.001 got %v", price["price"])
	}
}

// TestX402PaymentSigned verifies the full sign-and-retry flow: the runner
// receives a 402, calls the signer, and retries with X-Payment-Txid.
func TestX402PaymentSigned(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("X-Payment-Txid"); h != "" {
			gotHeader = h
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Header().Set("X-Payment-Required", `{"price":"0.001","unit":"call","network":"algorand-testnet","recipient":"ALGO123"}`)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	node := models.WorkflowNode{ID: "x1", Type: models.NodeTypeTool402, Endpoint: srv.URL}
	rc := engine.NewRunContext("r1", nil)
	signer := &mockSigner{txID: "TX-SIGNED-123"}
	aw := models.AgentWallet{AgentNodeID: "a1", EncryptedMnemonic: "enc-mnemonic"}

	result, err := nodes.ExecuteTool402(context.Background(), node, rc, aw, signer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T: %v", result, result)
	}
	if m["txId"] != "TX-SIGNED-123" {
		t.Fatalf("want txId TX-SIGNED-123, got %v", m["txId"])
	}
	if gotHeader != "TX-SIGNED-123" {
		t.Fatalf("retry request missing X-Payment-Txid header, got %q", gotHeader)
	}
	resp, _ := m["response"].(map[string]any)
	if resp == nil || resp["ok"] != true {
		t.Fatalf("want response.ok=true, got %v", m["response"])
	}
}

// TestX402NoWallet verifies that a 402 response with no wallet configured
// returns a graceful error map (not a Go error that would fail the run).
func TestX402NoWallet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Payment-Required", `{"price":"0.001","unit":"call","network":"algorand-testnet","recipient":"ALGO123"}`)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	node := models.WorkflowNode{ID: "x1", Type: models.NodeTypeTool402, Endpoint: srv.URL}
	rc := engine.NewRunContext("r1", nil)

	result, err := nodes.ExecuteTool402(context.Background(), node, rc, models.AgentWallet{}, nil)
	if err != nil {
		t.Fatalf("want nil Go error (graceful degradation), got %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok || m["error"] == nil {
		t.Fatalf("want error key in result map, got %v", result)
	}
	if !strings.Contains(m["error"].(string), "no agent wallet") {
		t.Fatalf("want 'no agent wallet' in error message, got %v", m["error"])
	}
}

// TestX402SignerError verifies that a signer failure (e.g. insufficient funds)
// propagates as a Go error so the run log marks the node as failed.
func TestX402SignerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Payment-Required", `{"price":"0.001","unit":"call","network":"algorand-testnet","recipient":"ALGO123"}`)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	node := models.WorkflowNode{ID: "x1", Type: models.NodeTypeTool402, Endpoint: srv.URL}
	rc := engine.NewRunContext("r1", nil)
	signer := &mockSigner{err: errors.New("insufficient balance")}
	aw := models.AgentWallet{AgentNodeID: "a1", EncryptedMnemonic: "enc-mnemonic"}

	_, err := nodes.ExecuteTool402(context.Background(), node, rc, aw, signer)
	if err == nil {
		t.Fatal("want error from signer failure, got nil")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Fatalf("want 'insufficient balance' in error, got %v", err)
	}
}
