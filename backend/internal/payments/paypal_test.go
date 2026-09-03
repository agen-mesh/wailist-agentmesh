package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// payPalTestServer stands in for PayPal's API: it issues a token and then
// dispatches to the handler under test.
func payPalTestServer(t *testing.T, handler http.HandlerFunc) (*PayPalClient, *httptest.Server, *int) {
	t.Helper()
	tokenCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"A21AA","expires_in":32400}`)
			return
		}
		handler(w, r)
	}))
	c := NewPayPalClient("client-id", "client-secret", "WH-123")
	c.SetBaseURLForTest(srv.URL)
	return c, srv, &tokenCalls
}

func TestCreateOrderSendsCustomIDAndReturnsTheApprovalLink(t *testing.T) {
	var gotBody map[string]any
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		fmt.Fprint(w, `{"id":"5O190127TN364715T","status":"PAYER_ACTION_REQUIRED","links":[
			{"href":"https://api.paypal.com/v2/checkout/orders/5O1","rel":"self"},
			{"href":"https://www.paypal.com/checkoutnow?token=5O1","rel":"payer-action"}]}`)
	})
	defer srv.Close()

	order, err := c.CreateOrder(context.Background(), 2500, "order-abc", "https://app.test/ok", "https://app.test/no")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if order.ID != "5O190127TN364715T" {
		t.Errorf("id = %q", order.ID)
	}
	if order.ApproveURL != "https://www.paypal.com/checkoutnow?token=5O1" {
		t.Errorf("approve url = %q", order.ApproveURL)
	}

	// custom_id is what survives onto the capture resource and is the only
	// way the webhook finds the ledger row.
	units := gotBody["purchase_units"].([]any)
	unit := units[0].(map[string]any)
	if unit["custom_id"] != "order-abc" {
		t.Errorf("custom_id = %v, want order-abc", unit["custom_id"])
	}
	amount := unit["amount"].(map[string]any)
	if amount["value"] != "25.00" {
		t.Errorf("amount value = %v, want the decimal string 25.00", amount["value"])
	}
	if amount["currency_code"] != "USD" {
		t.Errorf("currency = %v", amount["currency_code"])
	}
}

// PayPal wants a two-decimal string, and a naive cents/100 float would send
// "25" or "0.5" for these.
func TestCreateOrderFormatsAmountsWithTwoDecimals(t *testing.T) {
	for _, tc := range []struct {
		cents int64
		want  string
	}{
		{100, "1.00"},
		{2500, "25.00"},
		{2505, "25.05"},
		{99999, "999.99"},
	} {
		var gotBody map[string]any
		c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &gotBody)
			fmt.Fprint(w, `{"id":"5O1","links":[{"href":"https://p.test/a","rel":"payer-action"}]}`)
		})
		if _, err := c.CreateOrder(context.Background(), tc.cents, "o", "", ""); err != nil {
			t.Fatalf("CreateOrder(%d): %v", tc.cents, err)
		}
		unit := gotBody["purchase_units"].([]any)[0].(map[string]any)
		got := unit["amount"].(map[string]any)["value"]
		if got != tc.want {
			t.Errorf("%d cents formatted as %v, want %s", tc.cents, got, tc.want)
		}
		srv.Close()
	}
}

// "approve" is the older application_context spelling; a PayPal-side default
// change back to it must not strand the checkout.
func TestCreateOrderAcceptsTheOlderApproveRel(t *testing.T) {
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"5O1","links":[{"href":"https://www.paypal.com/x","rel":"approve"}]}`)
	})
	defer srv.Close()

	order, err := c.CreateOrder(context.Background(), 2500, "order-abc", "", "")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if order.ApproveURL != "https://www.paypal.com/x" {
		t.Errorf("approve url = %q", order.ApproveURL)
	}
}

// Without an approval link there is nowhere to send the payer, so a response
// missing one is an error rather than a half-usable order.
func TestCreateOrderRejectsAResponseWithNoApprovalLink(t *testing.T) {
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"5O1","links":[{"href":"https://api.paypal.com/v2/x","rel":"self"}]}`)
	})
	defer srv.Close()

	if _, err := c.CreateOrder(context.Background(), 2500, "order-abc", "", ""); err == nil {
		t.Error("expected an error when no approval link was returned")
	}
}

func TestCaptureOrderReturnsTheCaptureID(t *testing.T) {
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"COMPLETED","purchase_units":[{"payments":{"captures":[
			{"id":"3C679366HH908993F","status":"COMPLETED"}]}}]}`)
	})
	defer srv.Close()

	status, captureID, _, err := c.CaptureOrder(context.Background(), "5O1")
	if err != nil {
		t.Fatalf("CaptureOrder: %v", err)
	}
	if status != "COMPLETED" || captureID != "3C679366HH908993F" {
		t.Errorf("status=%q captureID=%q", status, captureID)
	}
}

// The payer double-clicking the return link, or the webhook racing the return
// path, must not read as a failure once the money has actually moved.
func TestCaptureOrderTreatsAlreadyCapturedAsSuccess(t *testing.T) {
	captureAttempts := 0
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/capture") {
			captureAttempts++
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"details":[{"issue":"ORDER_ALREADY_CAPTURED"}]}`)
			return
		}
		// The re-read that recovers the original capture.
		fmt.Fprint(w, `{"status":"COMPLETED","purchase_units":[{"payments":{"captures":[
			{"id":"3C6","status":"COMPLETED"}]}}]}`)
	})
	defer srv.Close()

	status, captureID, _, err := c.CaptureOrder(context.Background(), "5O1")
	if err != nil {
		t.Fatalf("CaptureOrder: %v", err)
	}
	if status != "COMPLETED" || captureID != "3C6" {
		t.Errorf("status=%q captureID=%q", status, captureID)
	}
	if captureAttempts != 1 {
		t.Errorf("capture attempted %d times, want 1", captureAttempts)
	}
}

func TestCaptureOrderSurfacesARealFailure(t *testing.T) {
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"details":[{"issue":"INSTRUMENT_DECLINED"}]}`)
	})
	defer srv.Close()

	if _, _, _, err := c.CaptureOrder(context.Background(), "5O1"); err == nil {
		t.Error("expected an error for a declined instrument")
	}
}

// A bearer token is good for ~9 hours; re-fetching one per call would triple
// the request count on every checkout.
func TestAccessTokenIsCachedAcrossCalls(t *testing.T) {
	c, srv, tokenCalls := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"5O1","links":[{"href":"https://p.test/a","rel":"payer-action"}]}`)
	})
	defer srv.Close()

	for i := 0; i < 3; i++ {
		if _, err := c.CreateOrder(context.Background(), 2500, "order-abc", "", ""); err != nil {
			t.Fatalf("CreateOrder: %v", err)
		}
	}
	if *tokenCalls != 1 {
		t.Errorf("fetched %d tokens for 3 calls, want 1", *tokenCalls)
	}
}

// A burst of concurrent checkouts must not stampede the token endpoint.
func TestAccessTokenIsFetchedOnceUnderConcurrency(t *testing.T) {
	c, srv, tokenCalls := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"5O1","links":[{"href":"https://p.test/a","rel":"payer-action"}]}`)
	})
	defer srv.Close()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.CreateOrder(context.Background(), 2500, "order-abc", "", "")
		}()
	}
	wg.Wait()

	if *tokenCalls != 1 {
		t.Errorf("fetched %d tokens across 8 concurrent calls, want 1", *tokenCalls)
	}
}

func TestVerifyWebhookSignatureAcceptsAVerifiedDelivery(t *testing.T) {
	var gotVerifyBody map[string]any
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotVerifyBody)
		fmt.Fprint(w, `{"verification_status":"SUCCESS"}`)
	})
	defer srv.Close()

	body := []byte(`{"event_type":"PAYMENT.CAPTURE.COMPLETED"}`)
	if !c.VerifyWebhookSignature(context.Background(), body, payPalHeaders("https://api.paypal.com/cert.pem")) {
		t.Error("a delivery PayPal verified was rejected")
	}
	// The webhook id is half the check -- a signature is only valid for the
	// subscription it was sent for.
	if gotVerifyBody["webhook_id"] != "WH-123" {
		t.Errorf("webhook_id = %v", gotVerifyBody["webhook_id"])
	}
}

func TestVerifyWebhookSignatureRejectsAFailedVerification(t *testing.T) {
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"verification_status":"FAILURE"}`)
	})
	defer srv.Close()

	if c.VerifyWebhookSignature(context.Background(), []byte(`{}`), payPalHeaders("https://api.paypal.com/cert.pem")) {
		t.Error("a delivery PayPal rejected was accepted")
	}
}

// cert_url comes from an attacker-controlled header and is echoed back to
// PayPal's verifier, so a non-PayPal host must never leave this process.
func TestVerifyWebhookSignatureRejectsAForeignCertURL(t *testing.T) {
	called := false
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		fmt.Fprint(w, `{"verification_status":"SUCCESS"}`)
	})
	defer srv.Close()

	for _, certURL := range []string{
		"https://evil.test/cert.pem",
		"https://paypal.com.evil.test/cert.pem",
		"http://api.paypal.com/cert.pem", // not https
		"https://notpaypal.com/cert.pem",
	} {
		if c.VerifyWebhookSignature(context.Background(), []byte(`{}`), payPalHeaders(certURL)) {
			t.Errorf("accepted a delivery naming cert_url %q", certURL)
		}
	}
	if called {
		t.Error("a foreign cert_url was forwarded to PayPal's verifier")
	}
}

// A delivery missing any signature header cannot be verified, so it must be
// rejected rather than passed to PayPal as a partial request.
func TestVerifyWebhookSignatureRejectsMissingHeaders(t *testing.T) {
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"verification_status":"SUCCESS"}`)
	})
	defer srv.Close()

	for _, drop := range []string{
		"paypal-transmission-id", "paypal-transmission-time",
		"paypal-transmission-sig", "paypal-cert-url", "paypal-auth-algo",
	} {
		h := payPalHeaders("https://api.paypal.com/cert.pem")
		h.Del(drop)
		if c.VerifyWebhookSignature(context.Background(), []byte(`{}`), h) {
			t.Errorf("accepted a delivery with %s missing", drop)
		}
	}
}

// Verification necessarily makes a network call, and an unreachable PayPal
// must read as "not verified" rather than "assume good".
func TestVerifyWebhookSignatureFailsClosedWhenPayPalIsUnreachable(t *testing.T) {
	c := NewPayPalClient("id", "secret", "WH-123")
	c.SetBaseURLForTest("http://127.0.0.1:1") // nothing listening

	if c.VerifyWebhookSignature(context.Background(), []byte(`{}`), payPalHeaders("https://api.paypal.com/cert.pem")) {
		t.Error("verification succeeded while PayPal was unreachable")
	}
}

// An unconfigured webhook id means we cannot verify anything at all.
func TestVerifyWebhookSignatureFailsClosedWithNoWebhookID(t *testing.T) {
	c := NewPayPalClient("id", "secret", "")
	if c.VerifyWebhookSignature(context.Background(), []byte(`{}`), payPalHeaders("https://api.paypal.com/cert.pem")) {
		t.Error("verification succeeded with no webhook id configured")
	}
}

func payPalHeaders(certURL string) http.Header {
	h := http.Header{}
	h.Set("paypal-transmission-id", "b8a1c0e0")
	h.Set("paypal-transmission-time", "2026-09-03T10:00:00Z")
	h.Set("paypal-transmission-sig", "sig==")
	h.Set("paypal-cert-url", certURL)
	h.Set("paypal-auth-algo", "SHA256withRSA")
	return h
}

// custom_id is the credit_ledger order id, and CapturePayPalOrder uses it to
// refuse crediting one order with another order's captured payment.
func TestCaptureOrderReturnsTheCustomID(t *testing.T) {
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"COMPLETED","purchase_units":[{"custom_id":"order-abc","payments":{"captures":[
			{"id":"3C6","status":"COMPLETED","custom_id":"order-abc"}]}}]}`)
	})
	defer srv.Close()

	_, _, customID, err := c.CaptureOrder(context.Background(), "5O1")
	if err != nil {
		t.Fatalf("CaptureOrder: %v", err)
	}
	if customID != "order-abc" {
		t.Errorf("custom_id = %q, want order-abc", customID)
	}
}

// The already-captured re-read path must recover custom_id too, or a retry
// racing the webhook would fail the binding check and reject a real payment.
func TestCaptureOrderRecoversCustomIDOnAlreadyCaptured(t *testing.T) {
	c, srv, _ := payPalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/capture") {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"details":[{"issue":"ORDER_ALREADY_CAPTURED"}]}`)
			return
		}
		fmt.Fprint(w, `{"status":"COMPLETED","purchase_units":[{"custom_id":"order-abc","payments":{"captures":[
			{"id":"3C6","status":"COMPLETED"}]}}]}`)
	})
	defer srv.Close()

	status, captureID, customID, err := c.CaptureOrder(context.Background(), "5O1")
	if err != nil {
		t.Fatalf("CaptureOrder: %v", err)
	}
	if status != "COMPLETED" || captureID != "3C6" || customID != "order-abc" {
		t.Errorf("status=%q captureID=%q customID=%q", status, captureID, customID)
	}
}
