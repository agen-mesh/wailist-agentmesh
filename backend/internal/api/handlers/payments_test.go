package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/payments"
)

type fakeRazorpay struct {
	order               payments.RazorpayOrder
	createErr           error
	verifyResult        bool
	verifyWebhookResult bool
}

func (f *fakeRazorpay) CreateOrder(ctx context.Context, amountPaise int64, receipt string) (payments.RazorpayOrder, error) {
	return f.order, f.createErr
}

func (f *fakeRazorpay) VerifySignature(orderID, paymentID, signature string) bool {
	return f.verifyResult
}

func (f *fakeRazorpay) VerifyWebhookSignature(body []byte, signature string) bool {
	return f.verifyWebhookResult
}

func TestCreateRazorpayOrderRejectsBelowMinimum(t *testing.T) {
	d := &handlers.Deps{Razorpay: &fakeRazorpay{}}
	body, _ := json.Marshal(map[string]int64{"amount_inr_paise": 50})
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/order", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "user1"))
	w := httptest.NewRecorder()

	d.CreateRazorpayOrder(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestVerifyRazorpayPaymentRejectsMissingFields(t *testing.T) {
	d := &handlers.Deps{Razorpay: &fakeRazorpay{verifyResult: true}}
	body, _ := json.Marshal(map[string]string{"razorpay_order_id": "order_1"})
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/verify", bytes.NewReader(body))
	w := httptest.NewRecorder()

	d.VerifyRazorpayPayment(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestRazorpayWebhookRejectsMissingSignatureHeader(t *testing.T) {
	d := &handlers.Deps{Razorpay: &fakeRazorpay{verifyWebhookResult: true}}
	body := []byte(`{"event":"payment.captured"}`)
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/webhook", bytes.NewReader(body))
	w := httptest.NewRecorder()

	d.RazorpayWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestRazorpayWebhookRejectsBadSignature(t *testing.T) {
	d := &handlers.Deps{Razorpay: &fakeRazorpay{verifyWebhookResult: false}}
	body := []byte(`{"event":"payment.captured"}`)
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/webhook", bytes.NewReader(body))
	req.Header.Set("X-Razorpay-Signature", "bad")
	w := httptest.NewRecorder()

	d.RazorpayWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestRazorpayWebhookIgnoresNonCapturedEvent(t *testing.T) {
	d := &handlers.Deps{Razorpay: &fakeRazorpay{verifyWebhookResult: true}}
	body := []byte(`{"event":"payment.failed","payload":{"payment":{"entity":{"id":"pay_1","order_id":"order_1"}}}}`)
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/webhook", bytes.NewReader(body))
	req.Header.Set("X-Razorpay-Signature", "valid")
	w := httptest.NewRecorder()

	d.RazorpayWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
}

func TestRazorpayWebhookRejectsMissingOrderOrPaymentID(t *testing.T) {
	d := &handlers.Deps{Razorpay: &fakeRazorpay{verifyWebhookResult: true}}
	body := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"","order_id":""}}}}`)
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/webhook", bytes.NewReader(body))
	req.Header.Set("X-Razorpay-Signature", "valid")
	w := httptest.NewRecorder()

	d.RazorpayWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestVerifyRazorpayPaymentRejectsBadSignature(t *testing.T) {
	d := &handlers.Deps{Razorpay: &fakeRazorpay{verifyResult: false}}
	body, _ := json.Marshal(map[string]string{
		"razorpay_order_id": "order_1", "razorpay_payment_id": "pay_1", "razorpay_signature": "bad",
	})
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/verify", bytes.NewReader(body))
	w := httptest.NewRecorder()

	d.VerifyRazorpayPayment(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}
