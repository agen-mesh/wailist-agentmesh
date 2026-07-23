package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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
	if _, _, err := d.Store.CompleteCreditTransaction(context.Background(), "razorpay", orderID, "pay_webhook_refund_test"); err != nil {
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
	wantMicros, _, err := d.Store.CompleteCreditTransaction(context.Background(), "razorpay", orderID, "pay_balance_test")
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

// --- fakeNOWPayments: same role as this file's existing fakeRazorpay ---

type fakeNOWPayments struct {
	invoice   payments.Invoice
	createErr error
	sigValid  bool
}

func (f *fakeNOWPayments) CreateInvoice(ctx context.Context, amountUSDCents int64, orderID, ipnCallbackURL, successURL, cancelURL string) (payments.Invoice, error) {
	if f.createErr != nil {
		return payments.Invoice{}, f.createErr
	}
	return f.invoice, nil
}

func (f *fakeNOWPayments) VerifyIPNSignature(body []byte, signature string) bool {
	return f.sigValid
}

func TestCreateCryptoInvoiceHappyPath(t *testing.T) {
	base := testDeps(t)
	d := &handlers.Deps{
		Store:       base.Store,
		NOWPayments: &fakeNOWPayments{invoice: payments.Invoice{ID: "inv_1", InvoiceURL: "https://nowpayments.io/payment/inv_1"}},
	}

	email := fmt.Sprintf("crypto-invoice-test-%d@example.com", time.Now().UnixNano())
	user, err := d.Store.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]int64{"amount_usd_cents": 1999})
	req := httptest.NewRequest(http.MethodPost, "/payments/nowpayments/invoice", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, user.ID))
	w := httptest.NewRecorder()

	d.CreateCryptoInvoice(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		OrderID    string `json:"order_id"`
		InvoiceURL string `json:"invoice_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.InvoiceURL != "https://nowpayments.io/payment/inv_1" || resp.OrderID == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCreateCryptoInvoiceRejectsBelowMinimum(t *testing.T) {
	d := &handlers.Deps{NOWPayments: &fakeNOWPayments{}}
	body, _ := json.Marshal(map[string]int64{"amount_usd_cents": 50})
	req := httptest.NewRequest(http.MethodPost, "/payments/nowpayments/invoice", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "user1"))
	w := httptest.NewRecorder()

	d.CreateCryptoInvoice(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestCreateCryptoInvoiceLeavesOrphanedPendingRowOnInvoiceFailure(t *testing.T) {
	// The ledger row is created BEFORE the NOWPayments invoice request precisely so that a
	// failure calling NOWPayments leaves only a harmless dead 'pending' row — never a real
	// invoice with no matching ledger row (see CreateCryptoInvoice's doc comment). This
	// test proves the orphan actually lands as 'pending' (not lost, not left in some other
	// state) when invoice creation fails.
	base := testDeps(t)
	d := &handlers.Deps{
		Store:       base.Store,
		NOWPayments: &fakeNOWPayments{createErr: fmt.Errorf("nowpayments: simulated outage")},
	}

	email := fmt.Sprintf("crypto-invoice-orphan-test-%d@example.com", time.Now().UnixNano())
	user, err := d.Store.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]int64{"amount_usd_cents": 1999})
	req := httptest.NewRequest(http.MethodPost, "/payments/nowpayments/invoice", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, user.ID))
	w := httptest.NewRecorder()

	d.CreateCryptoInvoice(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d: %s", w.Code, w.Body.String())
	}

	// Verify by user_id (fresh per test run) rather than order_id, since the handler
	// doesn't — and shouldn't — leak the internal order_id in an error response.
	url := os.Getenv("TEST_DATABASE_URL")
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM credit_ledger WHERE user_id = $1 AND provider = 'nowpayments' AND status = 'pending'`,
		user.ID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("want exactly 1 orphaned pending ledger row for this user, got %d", count)
	}
}

func TestNOWPaymentsWebhookRejectsBadSignature(t *testing.T) {
	d := &handlers.Deps{NOWPayments: &fakeNOWPayments{sigValid: false}}
	req := httptest.NewRequest(http.MethodPost, "/payments/nowpayments/webhook", strings.NewReader(`{"order_id":"x","payment_status":"finished","payment_id":1}`))
	req.Header.Set("x-nowpayments-sig", "bad-sig")
	w := httptest.NewRecorder()

	d.NOWPaymentsWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestNOWPaymentsWebhookCreditsOnFinished(t *testing.T) {
	base := testDeps(t)
	d := &handlers.Deps{Store: base.Store, NOWPayments: &fakeNOWPayments{sigValid: true}}

	email := fmt.Sprintf("crypto-webhook-test-%d@example.com", time.Now().UnixNano())
	user, err := d.Store.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	orderID := fmt.Sprintf("order_wh_%d", time.Now().UnixNano())
	if _, err := d.Store.CreateCryptoCreditTransaction(context.Background(), user.ID, "nowpayments", orderID, 1999); err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`{"order_id":"%s","payment_status":"finished","payment_id":6084744717}`, orderID)
	req := httptest.NewRequest(http.MethodPost, "/payments/nowpayments/webhook", strings.NewReader(body))
	req.Header.Set("x-nowpayments-sig", "sig-does-not-matter-fake-always-valid")
	w := httptest.NewRecorder()

	d.NOWPaymentsWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	balance, err := d.Store.GetCreditBalance(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 1999*10_000 {
		t.Fatalf("want balance %d got %d", 1999*10_000, balance)
	}
}

func TestNOWPaymentsWebhookDoesNotCreditOnConfirmed(t *testing.T) {
	// "confirmed" means on-chain-confirmed but not yet settled to us — only "finished"
	// is safe to credit (verified against NOWPayments' documented status sequence:
	// waiting -> confirming -> confirmed -> sending -> finished).
	base := testDeps(t)
	d := &handlers.Deps{Store: base.Store, NOWPayments: &fakeNOWPayments{sigValid: true}}

	email := fmt.Sprintf("crypto-webhook-confirmed-test-%d@example.com", time.Now().UnixNano())
	user, err := d.Store.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	orderID := fmt.Sprintf("order_wh_confirmed_%d", time.Now().UnixNano())
	if _, err := d.Store.CreateCryptoCreditTransaction(context.Background(), user.ID, "nowpayments", orderID, 1999); err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`{"order_id":"%s","payment_status":"confirmed","payment_id":1}`, orderID)
	req := httptest.NewRequest(http.MethodPost, "/payments/nowpayments/webhook", strings.NewReader(body))
	req.Header.Set("x-nowpayments-sig", "sig-does-not-matter-fake-always-valid")
	w := httptest.NewRecorder()

	d.NOWPaymentsWebhook(w, req)

	balance, err := d.Store.GetCreditBalance(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatalf("want balance untouched at 0 on mere 'confirmed', got %d", balance)
	}
}

func TestNOWPaymentsWebhookMarksPartialWithoutCrediting(t *testing.T) {
	base := testDeps(t)
	d := &handlers.Deps{Store: base.Store, NOWPayments: &fakeNOWPayments{sigValid: true}}

	email := fmt.Sprintf("crypto-webhook-partial-test-%d@example.com", time.Now().UnixNano())
	user, err := d.Store.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	orderID := fmt.Sprintf("order_wh_partial_%d", time.Now().UnixNano())
	if _, err := d.Store.CreateCryptoCreditTransaction(context.Background(), user.ID, "nowpayments", orderID, 1999); err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`{"order_id":"%s","payment_status":"partially_paid","payment_id":1}`, orderID)
	req := httptest.NewRequest(http.MethodPost, "/payments/nowpayments/webhook", strings.NewReader(body))
	req.Header.Set("x-nowpayments-sig", "sig-does-not-matter-fake-always-valid")
	w := httptest.NewRecorder()

	d.NOWPaymentsWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	balance, err := d.Store.GetCreditBalance(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatalf("want balance untouched at 0 pending manual reconciliation, got %d", balance)
	}
}
