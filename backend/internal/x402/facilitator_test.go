package x402_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/x402"
)

func TestVerifySendsPayloadAndParsesResult(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["paymentPayload"] == nil || body["paymentRequirements"] == nil {
			t.Errorf("want both paymentPayload and paymentRequirements in body, got %v", body)
		}
		json.NewEncoder(w).Encode(map[string]any{"isValid": true})
	}))
	defer srv.Close()

	c := x402.NewFacilitatorClient(srv.URL)
	result, err := c.Verify(context.Background(),
		x402.PaymentPayload{X402Version: 2, Scheme: "exact", Network: "algorand:testnet"},
		x402.PaymentRequirements{Scheme: "exact", Network: "algorand:testnet", PayTo: "ADDR"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsValid {
		t.Fatal("want IsValid true")
	}
	if gotPath != "/verify" {
		t.Fatalf("want POST /verify, got %s", gotPath)
	}
}

func TestSettleReturnsTransactionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/settle" {
			t.Errorf("want POST /settle, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "transaction": "TXID123"})
	}))
	defer srv.Close()

	c := x402.NewFacilitatorClient(srv.URL)
	result, err := c.Settle(context.Background(),
		x402.PaymentPayload{X402Version: 2, Scheme: "exact", Network: "algorand:testnet"},
		x402.PaymentRequirements{Scheme: "exact", Network: "algorand:testnet", PayTo: "ADDR"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.TxID != "TXID123" {
		t.Fatalf("want success+TXID123, got %+v", result)
	}
}

func TestSettleSurfacesFacilitatorFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"success": false, "errorReason": "insufficient_funds"})
	}))
	defer srv.Close()

	c := x402.NewFacilitatorClient(srv.URL)
	result, err := c.Settle(context.Background(), x402.PaymentPayload{}, x402.PaymentRequirements{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatal("want Success false")
	}
	if result.Error != "insufficient_funds" {
		t.Fatalf("want errorReason insufficient_funds, got %q", result.Error)
	}
}
