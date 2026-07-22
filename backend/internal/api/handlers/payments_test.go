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
	order        payments.RazorpayOrder
	createErr    error
	verifyResult bool
}

func (f *fakeRazorpay) CreateOrder(ctx context.Context, amountPaise int64, receipt string) (payments.RazorpayOrder, error) {
	return f.order, f.createErr
}

func (f *fakeRazorpay) VerifySignature(orderID, paymentID, signature string) bool {
	return f.verifyResult
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
