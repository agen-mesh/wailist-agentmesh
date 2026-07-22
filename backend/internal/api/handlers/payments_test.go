package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestCreateRazorpayOrderRejectsAboveMaximum(t *testing.T) {
	d := &handlers.Deps{Razorpay: &fakeRazorpay{}}
	body, _ := json.Marshal(map[string]int64{"amount_inr_paise": 5_00_000_01})
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

func TestRazorpayWebhookRejectsUnknownOrder(t *testing.T) {
	base := testDeps(t)
	d := &handlers.Deps{Store: base.Store, Razorpay: &fakeRazorpay{verifyWebhookResult: true}}

	orderID := fmt.Sprintf("order_unknown_%d", time.Now().UnixNano())
	body := []byte(fmt.Sprintf(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_1","order_id":"%s"}}}}`, orderID))
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/webhook", bytes.NewReader(body))
	req.Header.Set("X-Razorpay-Signature", "valid")
	w := httptest.NewRecorder()

	d.RazorpayWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestVerifyRazorpayPaymentRejectsUnknownOrder(t *testing.T) {
	base := testDeps(t)
	d := &handlers.Deps{Store: base.Store, Razorpay: &fakeRazorpay{verifyResult: true}}

	orderID := fmt.Sprintf("order_unknown_%d", time.Now().UnixNano())
	body, _ := json.Marshal(map[string]string{
		"razorpay_order_id": orderID, "razorpay_payment_id": "pay_1", "razorpay_signature": "sig",
	})
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/verify", bytes.NewReader(body))
	w := httptest.NewRecorder()

	d.VerifyRazorpayPayment(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestRazorpayWebhookProcessesRefund(t *testing.T) {
	base := testDeps(t)
	d := &handlers.Deps{Store: base.Store, Razorpay: &fakeRazorpay{verifyWebhookResult: true}}

	email := fmt.Sprintf("webhook-refund-test-%d@example.com", time.Now().UnixNano())
	user, err := d.Store.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	orderID := fmt.Sprintf("order_webhook_refund_%d", time.Now().UnixNano())
	if _, err := d.Store.CreateCreditTransaction(context.Background(), user.ID, orderID, 50000, 0.012); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Store.CompleteCreditTransaction(context.Background(), orderID, "pay_webhook_refund_test"); err != nil {
		t.Fatal(err)
	}

	body := []byte(fmt.Sprintf(`{"event":"refund.processed","payload":{"payment":{"entity":{"id":"pay_webhook_refund_test","order_id":"%s","amount_refunded":50000}}}}`, orderID))
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/webhook", bytes.NewReader(body))
	req.Header.Set("X-Razorpay-Signature", "valid")
	w := httptest.NewRecorder()

	d.RazorpayWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}

	balance, err := d.Store.GetCreditBalance(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatalf("want balance 0 after full refund, got %d", balance)
	}
}

func TestRazorpayWebhookRefundRejectsUnknownOrder(t *testing.T) {
	base := testDeps(t)
	d := &handlers.Deps{Store: base.Store, Razorpay: &fakeRazorpay{verifyWebhookResult: true}}

	orderID := fmt.Sprintf("order_unknown_refund_%d", time.Now().UnixNano())
	body := []byte(fmt.Sprintf(`{"event":"refund.processed","payload":{"payment":{"entity":{"id":"pay_1","order_id":"%s","amount_refunded":100}}}}`, orderID))
	req := httptest.NewRequest(http.MethodPost, "/payments/razorpay/webhook", bytes.NewReader(body))
	req.Header.Set("X-Razorpay-Signature", "valid")
	w := httptest.NewRecorder()

	d.RazorpayWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestGetCreditBalanceReturnsStoredBalance(t *testing.T) {
	d := testDeps(t)

	email := fmt.Sprintf("balance-test-%d@example.com", time.Now().UnixNano())
	user, err := d.Store.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	orderID := fmt.Sprintf("order_balance_%d", time.Now().UnixNano())
	if _, err := d.Store.CreateCreditTransaction(context.Background(), user.ID, orderID, 10000, 0.012); err != nil {
		t.Fatal(err)
	}
	wantMicros, _, err := d.Store.CompleteCreditTransaction(context.Background(), orderID, "pay_balance_test")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/credits/balance", nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, user.ID))
	w := httptest.NewRecorder()

	d.GetCreditBalance(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var body struct {
		CreditUSDMicros int64 `json:"credit_usd_micros"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.CreditUSDMicros != wantMicros {
		t.Fatalf("want %d got %d", wantMicros, body.CreditUSDMicros)
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
