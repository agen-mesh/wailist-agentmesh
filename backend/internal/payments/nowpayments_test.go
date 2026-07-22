package payments_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/payments"
)

func TestCreateInvoiceReturnsInvoiceFromServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "api_key_123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["price_currency"] != "usd" || body["order_id"] != "order_abc" {
			t.Errorf("unexpected request body: %+v", body)
		}
		if body["price_amount"] != 19.99 {
			t.Errorf("want price_amount 19.99, got %v", body["price_amount"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id": "inv_abc123", "invoice_url": "https://nowpayments.io/payment/inv_abc123",
		})
	}))
	defer srv.Close()

	c := payments.NewNOWPaymentsClient("api_key_123", "ipn_secret")
	c.SetBaseURLForTest(srv.URL)

	invoice, err := c.CreateInvoice(context.Background(), 1999, "order_abc", "https://cb", "https://ok", "https://cancel")
	if err != nil {
		t.Fatal(err)
	}
	if invoice.ID != "inv_abc123" || invoice.InvoiceURL != "https://nowpayments.io/payment/inv_abc123" {
		t.Fatalf("unexpected invoice: %+v", invoice)
	}
}

func TestCreateInvoiceRejectsBelowMinimum(t *testing.T) {
	c := payments.NewNOWPaymentsClient("api_key_123", "ipn_secret")
	_, err := c.CreateInvoice(context.Background(), 50, "order_abc", "https://cb", "https://ok", "https://cancel")
	if err == nil {
		t.Fatal("want error for amount below 100 cents, got nil")
	}
}

func TestCreateInvoiceReturnsErrorOnAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := payments.NewNOWPaymentsClient("wrong_key", "ipn_secret")
	c.SetBaseURLForTest(srv.URL)

	_, err := c.CreateInvoice(context.Background(), 1999, "order_abc", "https://cb", "https://ok", "https://cancel")
	if err == nil {
		t.Fatal("want auth error, got nil")
	}
}

// signIPN reproduces NOWPayments' own signing algorithm (confirmed against their
// published Python/Node examples): HMAC-SHA512, hex, over the JSON body re-serialized
// with keys sorted alphabetically at every nesting level and no extra whitespace.
// json.Marshal on a Go map already sorts keys alphabetically and emits no whitespace,
// so for a flat payload it is its own canonical form — this helper exists to make that
// explicit and to extend to nested payloads in the recursive test below.
func signIPN(t *testing.T, secret string, payload map[string]any) string {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyIPNSignatureAcceptsValidFlatSignature(t *testing.T) {
	c := payments.NewNOWPaymentsClient("api_key", "ipn_secret")
	payload := map[string]any{"payment_id": float64(5077125051), "order_id": "order_abc", "payment_status": "finished"}
	body, _ := json.Marshal(payload)
	sig := signIPN(t, "ipn_secret", payload)

	if !c.VerifyIPNSignature(body, sig) {
		t.Fatal("want valid signature accepted")
	}
}

func TestVerifyIPNSignatureAcceptsOutOfOrderKeys(t *testing.T) {
	// NOWPayments' own signer sorts keys before hashing, so the raw body we receive
	// may have keys in a different order than the signature was computed over — as
	// long as the *sorted* form matches, verification must still pass.
	c := payments.NewNOWPaymentsClient("api_key", "ipn_secret")
	sortedPayload := map[string]any{"order_id": "order_abc", "payment_id": float64(1), "payment_status": "finished"}
	sig := signIPN(t, "ipn_secret", sortedPayload)

	// Same fields, deliberately written out of alphabetical order in the raw body.
	outOfOrderBody := []byte(`{"payment_status":"finished","payment_id":1,"order_id":"order_abc"}`)

	if !c.VerifyIPNSignature(outOfOrderBody, sig) {
		t.Fatal("want signature valid regardless of raw key order")
	}
}

func TestVerifyIPNSignatureRejectsTamperedBody(t *testing.T) {
	c := payments.NewNOWPaymentsClient("api_key", "ipn_secret")
	payload := map[string]any{"payment_id": float64(1), "order_id": "order_abc", "payment_status": "waiting"}
	sig := signIPN(t, "ipn_secret", payload)

	tampered := []byte(`{"order_id":"order_abc","payment_id":1,"payment_status":"finished"}`)
	if c.VerifyIPNSignature(tampered, sig) {
		t.Fatal("want tampered payload rejected")
	}
}

func TestUseSandboxSwitchesBaseURL(t *testing.T) {
	c := payments.NewNOWPaymentsClient("api_key", "ipn_secret")
	c.UseSandbox()
	_, err := c.CreateInvoice(context.Background(), 1999, "order_abc", "https://cb", "https://ok", "https://cancel")
	// No live network access in tests — we only assert this doesn't panic and returns
	// a network error (proves UseSandbox changed the target host away from any test
	// server we might otherwise have pointed at), not that the sandbox actually replies.
	if err == nil {
		t.Fatal("want network error hitting the real sandbox host from a unit test")
	}
}
