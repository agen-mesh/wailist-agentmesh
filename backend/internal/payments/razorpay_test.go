package payments_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/payments"
)

func TestCreateOrderReturnsOrderFromServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "key_id" || pass != "key_secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id": "order_abc123", "amount": 50000, "currency": "INR",
		})
	}))
	defer srv.Close()

	c := payments.NewRazorpayClient("key_id", "key_secret", "webhook_secret")
	c.SetBaseURLForTest(srv.URL)

	order, err := c.CreateOrder(context.Background(), 50000, "receipt_1")
	if err != nil {
		t.Fatal(err)
	}
	if order.ID != "order_abc123" || order.Amount != 50000 || order.Currency != "INR" {
		t.Fatalf("unexpected order: %+v", order)
	}
}

func TestCreateOrderRejectsBelowMinimum(t *testing.T) {
	c := payments.NewRazorpayClient("key_id", "key_secret", "webhook_secret")
	_, err := c.CreateOrder(context.Background(), 50, "receipt_1")
	if err == nil {
		t.Fatal("want error for amount below 100 paise, got nil")
	}
}

func TestCreateOrderReturnsErrorOnAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := payments.NewRazorpayClient("wrong", "creds", "webhook_secret")
	c.SetBaseURLForTest(srv.URL)

	_, err := c.CreateOrder(context.Background(), 50000, "receipt_1")
	if err == nil {
		t.Fatal("want auth error, got nil")
	}
}

func TestVerifySignatureAcceptsValidSignature(t *testing.T) {
	c := payments.NewRazorpayClient("key_id", "key_secret", "webhook_secret")
	mac := hmac.New(sha256.New, []byte("key_secret"))
	mac.Write([]byte("order_abc123|pay_xyz789"))
	sig := hex.EncodeToString(mac.Sum(nil))

	if !c.VerifySignature("order_abc123", "pay_xyz789", sig) {
		t.Fatal("want valid signature accepted")
	}
}

func TestVerifySignatureRejectsTamperedSignature(t *testing.T) {
	c := payments.NewRazorpayClient("key_id", "key_secret", "webhook_secret")
	if c.VerifySignature("order_abc123", "pay_xyz789", "not-a-real-signature") {
		t.Fatal("want tampered signature rejected")
	}
}

func TestVerifyWebhookSignatureAcceptsValidSignature(t *testing.T) {
	c := payments.NewRazorpayClient("key_id", "key_secret", "webhook_secret")
	body := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_1","order_id":"order_1"}}}}`)
	mac := hmac.New(sha256.New, []byte("webhook_secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if !c.VerifyWebhookSignature(body, sig) {
		t.Fatal("want valid webhook signature accepted")
	}
}

func TestVerifyWebhookSignatureRejectsTamperedBody(t *testing.T) {
	c := payments.NewRazorpayClient("key_id", "key_secret", "webhook_secret")
	body := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_1","order_id":"order_1"}}}}`)
	mac := hmac.New(sha256.New, []byte("webhook_secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	tampered := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_1","order_id":"order_EVIL"}}}}`)
	if c.VerifyWebhookSignature(tampered, sig) {
		t.Fatal("want tampered body rejected")
	}
}

func TestVerifyWebhookSignatureRejectsWrongSecret(t *testing.T) {
	c := payments.NewRazorpayClient("key_id", "key_secret", "webhook_secret")
	body := []byte(`{"event":"payment.captured"}`)
	mac := hmac.New(sha256.New, []byte("a_different_secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if c.VerifyWebhookSignature(body, sig) {
		t.Fatal("want signature computed with wrong secret rejected")
	}
}
