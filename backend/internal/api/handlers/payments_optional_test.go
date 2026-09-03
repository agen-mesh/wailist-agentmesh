package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
)

// Stripe and PayPal are optional: a deployment that never sets their
// credentials leaves d.Stripe / d.PayPal nil. Every one of their handlers must
// answer 503 rather than panic on a nil interface -- a nil-pointer panic in an
// HTTP handler takes the whole server down, so this is the difference between
// "that gateway is off" and an outage.
func TestStripeHandlersFailClosedWhenUnconfigured(t *testing.T) {
	d := &handlers.Deps{} // Stripe is nil

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"checkout", d.CreateStripeCheckout, "/payments/stripe/checkout"},
		{"verify", d.VerifyStripePayment, "/payments/stripe/verify"},
		{"webhook", d.StripeWebhook, "/payments/stripe/webhook"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{}`))
			rec := httptest.NewRecorder()
			tc.handler(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "not configured") {
				t.Errorf("body = %q, want it to say stripe is not configured", rec.Body.String())
			}
		})
	}
}

func TestPayPalHandlersFailClosedWhenUnconfigured(t *testing.T) {
	d := &handlers.Deps{} // PayPal is nil

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"order", d.CreatePayPalOrder, "/payments/paypal/order"},
		{"capture", d.CapturePayPalOrder, "/payments/paypal/capture"},
		{"webhook", d.PayPalWebhook, "/payments/paypal/webhook"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{}`))
			rec := httptest.NewRecorder()
			tc.handler(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "not configured") {
				t.Errorf("body = %q, want it to say paypal is not configured", rec.Body.String())
			}
		})
	}
}
