package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/payments"
)

// fakeStripeBinding reports a session that is paid but belongs to whatever
// order the test names -- the point being that the caller does not get to
// decide which order a session paid for.
type fakeStripeBinding struct{ sessionOrderID string }

func (f *fakeStripeBinding) CreateCheckoutSession(ctx context.Context, amountUSDCents int64, orderID, customerEmail, successURL, cancelURL string) (payments.CheckoutSession, error) {
	return payments.CheckoutSession{}, nil
}
func (f *fakeStripeBinding) GetCheckoutSessionStatus(ctx context.Context, sessionID string) (string, string, string, error) {
	return "paid", "pi_1", f.sessionOrderID, nil
}
func (f *fakeStripeBinding) VerifyWebhookSignature(body []byte, header string, now time.Time) bool {
	return false
}

type fakePayPalBinding struct{ customID string }

func (f *fakePayPalBinding) CreateOrder(ctx context.Context, amountUSDCents int64, orderID, returnURL, cancelURL string) (payments.PayPalOrder, error) {
	return payments.PayPalOrder{}, nil
}
func (f *fakePayPalBinding) CaptureOrder(ctx context.Context, payPalOrderID string) (string, string, string, error) {
	return "COMPLETED", "3C6", f.customID, nil
}
func (f *fakePayPalBinding) VerifyWebhookSignature(ctx context.Context, body []byte, h http.Header) bool {
	return false
}

// Pairing a genuinely-paid session with someone else's pending order must be
// refused. Without this, a single $1 payment could be replayed to complete
// any number of unrelated, larger orders.
func TestVerifyStripeRejectsASessionForADifferentOrder(t *testing.T) {
	d := &handlers.Deps{Stripe: &fakeStripeBinding{sessionOrderID: "order-that-was-paid"}}

	req := httptest.NewRequest(http.MethodPost, "/payments/stripe/verify",
		strings.NewReader(`{"order_id":"some-other-pending-order","session_id":"cs_1"}`))
	rec := httptest.NewRecorder()
	d.VerifyStripePayment(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 — a paid session was applied to an unrelated order", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "does not belong") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// A session carrying no client_reference_id proves nothing about which order
// it paid for, so it must not be usable either.
func TestVerifyStripeRejectsASessionWithNoReference(t *testing.T) {
	d := &handlers.Deps{Stripe: &fakeStripeBinding{sessionOrderID: ""}}

	req := httptest.NewRequest(http.MethodPost, "/payments/stripe/verify",
		strings.NewReader(`{"order_id":"any-order","session_id":"cs_1"}`))
	rec := httptest.NewRecorder()
	d.VerifyStripePayment(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestCapturePayPalRejectsACaptureForADifferentOrder(t *testing.T) {
	d := &handlers.Deps{PayPal: &fakePayPalBinding{customID: "order-that-was-paid"}}

	req := httptest.NewRequest(http.MethodPost, "/payments/paypal/capture",
		strings.NewReader(`{"order_id":"some-other-pending-order","paypal_order_id":"5O1"}`))
	rec := httptest.NewRecorder()
	d.CapturePayPalOrder(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 — a capture was applied to an unrelated order", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "does not belong") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestCapturePayPalRejectsACaptureWithNoCustomID(t *testing.T) {
	d := &handlers.Deps{PayPal: &fakePayPalBinding{customID: ""}}

	req := httptest.NewRequest(http.MethodPost, "/payments/paypal/capture",
		strings.NewReader(`{"order_id":"any-order","paypal_order_id":"5O1"}`))
	rec := httptest.NewRecorder()
	d.CapturePayPalOrder(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// fakePayPalWebhook accepts any signature so the decode/dispatch path can be
// tested on its own.
type fakePayPalWebhook struct{ fakePayPalBinding }

func (f *fakePayPalWebhook) VerifyWebhookSignature(ctx context.Context, body []byte, h http.Header) bool {
	return true
}

// custom_id sits at two different depths depending on the event: on the
// resource for PAYMENT.CAPTURE.*, and under the purchase unit for
// CHECKOUT.ORDER.* -- which is the case that was broken, since reading only
// the top level made every voided order resolve to no order and get silently
// ignored.
//
// The decoder is shared across event types, so this exercises the nested
// lookup through PAYMENT.CAPTURE.REVERSED: it is the one branch that resolves
// an order without touching the store, which keeps this a real unit test
// rather than one that skips whenever TEST_DATABASE_URL is unset.
func TestPayPalWebhookFindsCustomIDNestedUnderPurchaseUnits(t *testing.T) {
	d := &handlers.Deps{PayPal: &fakePayPalWebhook{}}

	body := `{"event_type":"PAYMENT.CAPTURE.REVERSED","resource":{"id":"3C6",
		"purchase_units":[{"custom_id":"order-abc"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/payments/paypal/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()
	d.PayPalWebhook(rec, req)

	// Without the nested lookup the order id is empty and this returns
	// "ignored" instead of resolving to order-abc.
	if strings.Contains(rec.Body.String(), "ignored") {
		t.Errorf("nested custom_id was not read; body = %q", rec.Body.String())
	}
}

// A reversal lands after the credit is already granted and spendable. It must
// not pretend to have reversed anything.
func TestPayPalWebhookReportsReversalAsNeedingManualWork(t *testing.T) {
	d := &handlers.Deps{PayPal: &fakePayPalWebhook{}}

	body := `{"event_type":"PAYMENT.CAPTURE.REVERSED","resource":{"id":"3C6","custom_id":"order-abc"}}`
	req := httptest.NewRequest(http.MethodPost, "/payments/paypal/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()
	d.PayPalWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "manual_reconciliation_required") {
		t.Errorf("body = %q, want it to say the credit was not clawed back", rec.Body.String())
	}
}
