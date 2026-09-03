package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// signStripe builds a Stripe-Signature header the way Stripe's servers do.
func signStripe(t *testing.T, secret string, ts time.Time, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts.Unix())
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", ts.Unix(), hex.EncodeToString(mac.Sum(nil)))
}

func TestVerifyWebhookSignatureAcceptsAGenuineHeader(t *testing.T) {
	c := NewStripeClient("sk_test_x", "whsec_test")
	body := []byte(`{"type":"checkout.session.completed"}`)
	now := time.Now()

	if !c.VerifyWebhookSignature(body, signStripe(t, "whsec_test", now, body), now) {
		t.Error("a correctly signed webhook was rejected")
	}
}

func TestVerifyWebhookSignatureRejectsATamperedBody(t *testing.T) {
	c := NewStripeClient("sk_test_x", "whsec_test")
	now := time.Now()
	header := signStripe(t, "whsec_test", now, []byte(`{"amount":100}`))

	if c.VerifyWebhookSignature([]byte(`{"amount":999999}`), header, now) {
		t.Error("a body edited after signing was accepted")
	}
}

func TestVerifyWebhookSignatureRejectsTheWrongSecret(t *testing.T) {
	c := NewStripeClient("sk_test_x", "whsec_real")
	body := []byte(`{"type":"checkout.session.completed"}`)
	now := time.Now()

	if c.VerifyWebhookSignature(body, signStripe(t, "whsec_attacker", now, body), now) {
		t.Error("a signature made with the wrong secret was accepted")
	}
}

// A captured delivery must stop being replayable once it ages out, which is
// the only thing that expires a Stripe signature.
func TestVerifyWebhookSignatureRejectsAStaleTimestamp(t *testing.T) {
	c := NewStripeClient("sk_test_x", "whsec_test")
	body := []byte(`{"type":"checkout.session.completed"}`)
	signedAt := time.Now().Add(-30 * time.Minute)

	if c.VerifyWebhookSignature(body, signStripe(t, "whsec_test", signedAt, body), time.Now()) {
		t.Error("a signature from 30 minutes ago was accepted")
	}
}

// A far-future timestamp is as much a forgery signal as a stale one, and
// would otherwise buy an attacker an arbitrarily long replay window.
func TestVerifyWebhookSignatureRejectsAFutureTimestamp(t *testing.T) {
	c := NewStripeClient("sk_test_x", "whsec_test")
	body := []byte(`{"type":"checkout.session.completed"}`)
	signedAt := time.Now().Add(30 * time.Minute)

	if c.VerifyWebhookSignature(body, signStripe(t, "whsec_test", signedAt, body), time.Now()) {
		t.Error("a signature timestamped 30 minutes in the future was accepted")
	}
}

// Stripe sends several v1 values while a signing secret is being rotated;
// a match on any one of them is a valid delivery.
func TestVerifyWebhookSignatureAcceptsOneOfSeveralCandidates(t *testing.T) {
	c := NewStripeClient("sk_test_x", "whsec_test")
	body := []byte(`{"type":"checkout.session.completed"}`)
	now := time.Now()

	real := signStripe(t, "whsec_test", now, body)
	_, v1, _ := strings.Cut(real, "v1=")
	header := fmt.Sprintf("t=%d,v1=%s,v1=%s", now.Unix(), strings.Repeat("0", 64), v1)

	if !c.VerifyWebhookSignature(body, header, now) {
		t.Error("a header carrying an old and a current signature was rejected")
	}
}

func TestVerifyWebhookSignatureRejectsAMalformedHeader(t *testing.T) {
	c := NewStripeClient("sk_test_x", "whsec_test")
	body := []byte(`{}`)
	for _, header := range []string{"", "garbage", "t=abc,v1=deadbeef", "v1=deadbeef"} {
		if c.VerifyWebhookSignature(body, header, time.Now()) {
			t.Errorf("malformed header %q was accepted", header)
		}
	}
}

// An unset webhook secret must fail closed rather than verifying against "".
func TestVerifyWebhookSignatureFailsClosedWithNoSecret(t *testing.T) {
	c := NewStripeClient("sk_test_x", "")
	body := []byte(`{}`)
	if c.VerifyWebhookSignature(body, signStripe(t, "", time.Now(), body), time.Now()) {
		t.Error("verification succeeded with no webhook secret configured")
	}
}

func TestCreateCheckoutSessionSendsTheOrderIDAndAmount(t *testing.T) {
	var gotForm string
	var gotAuth, gotIdempotency string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm = string(b)
		gotAuth = r.Header.Get("Authorization")
		gotIdempotency = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"cs_test_123","url":"https://checkout.stripe.com/c/pay/cs_test_123"}`)
	}))
	defer srv.Close()

	c := NewStripeClient("sk_test_x", "whsec_test")
	c.SetBaseURLForTest(srv.URL)

	session, err := c.CreateCheckoutSession(context.Background(), 2500, "order-abc", "a@b.test", "https://app.test/ok", "https://app.test/no")
	if err != nil {
		t.Fatalf("CreateCheckoutSession: %v", err)
	}
	if session.URL != "https://checkout.stripe.com/c/pay/cs_test_123" {
		t.Errorf("url = %q", session.URL)
	}
	// client_reference_id is how the webhook finds the ledger row, so its
	// presence is load-bearing, not cosmetic.
	if !strings.Contains(gotForm, "client_reference_id=order-abc") {
		t.Errorf("form did not carry client_reference_id: %s", gotForm)
	}
	if !strings.Contains(gotForm, "%5Bunit_amount%5D=2500") {
		t.Errorf("form did not carry the amount: %s", gotForm)
	}
	if gotAuth != "Bearer sk_test_x" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotIdempotency != "order-abc" {
		t.Errorf("idempotency key = %q, want the order id", gotIdempotency)
	}
}

// Stripe rejects sub-50-cent USD charges itself; catching it locally saves a
// round trip and gives a clearer error.
func TestCreateCheckoutSessionRejectsATinyAmount(t *testing.T) {
	c := NewStripeClient("sk_test_x", "whsec_test")
	if _, err := c.CreateCheckoutSession(context.Background(), 10, "order-abc", "", "", ""); err == nil {
		t.Error("expected an error for a 10-cent charge")
	}
}

func TestCreateCheckoutSessionSurfacesAnAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"no such price"}}`)
	}))
	defer srv.Close()

	c := NewStripeClient("sk_test_x", "whsec_test")
	c.SetBaseURLForTest(srv.URL)

	if _, err := c.CreateCheckoutSession(context.Background(), 2500, "order-abc", "", "", ""); err == nil {
		t.Error("expected an error from a 400 response")
	}
}

// A 200 that carries no redirect URL is unusable, and returning it as success
// would strand the payer on a blank redirect.
func TestCreateCheckoutSessionRejectsAResponseWithNoURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"cs_test_123"}`)
	}))
	defer srv.Close()

	c := NewStripeClient("sk_test_x", "whsec_test")
	c.SetBaseURLForTest(srv.URL)

	if _, err := c.CreateCheckoutSession(context.Background(), 2500, "order-abc", "", "", ""); err == nil {
		t.Error("expected an error when the session carried no url")
	}
}

func TestGetCheckoutSessionStatusReadsAStringPaymentIntent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"payment_status":"paid","payment_intent":"pi_123"}`)
	}))
	defer srv.Close()

	c := NewStripeClient("sk_test_x", "whsec_test")
	c.SetBaseURLForTest(srv.URL)

	status, pi, _, err := c.GetCheckoutSessionStatus(context.Background(), "cs_test_123")
	if err != nil {
		t.Fatalf("GetCheckoutSessionStatus: %v", err)
	}
	if status != "paid" || pi != "pi_123" {
		t.Errorf("status=%q payment_intent=%q", status, pi)
	}
}

// An expanded payment_intent arrives as an object rather than an id string;
// both shapes have to yield the same id.
func TestGetCheckoutSessionStatusReadsAnExpandedPaymentIntent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"payment_status":"paid","payment_intent":{"id":"pi_456","status":"succeeded"}}`)
	}))
	defer srv.Close()

	c := NewStripeClient("sk_test_x", "whsec_test")
	c.SetBaseURLForTest(srv.URL)

	status, pi, _, err := c.GetCheckoutSessionStatus(context.Background(), "cs_test_123")
	if err != nil {
		t.Fatalf("GetCheckoutSessionStatus: %v", err)
	}
	if status != "paid" || pi != "pi_456" {
		t.Errorf("status=%q payment_intent=%q", status, pi)
	}
}

// The order id a session belongs to has to come back from Stripe, not from
// the caller -- VerifyStripePayment relies on it to refuse crediting order A
// on the strength of a session that paid for order B.
func TestGetCheckoutSessionStatusReturnsTheClientReferenceID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"payment_status":"paid","payment_intent":"pi_1","client_reference_id":"order-abc"}`)
	}))
	defer srv.Close()

	c := NewStripeClient("sk_test_x", "whsec_test")
	c.SetBaseURLForTest(srv.URL)

	_, _, ref, err := c.GetCheckoutSessionStatus(context.Background(), "cs_1")
	if err != nil {
		t.Fatalf("GetCheckoutSessionStatus: %v", err)
	}
	if ref != "order-abc" {
		t.Errorf("client_reference_id = %q, want order-abc", ref)
	}
}
