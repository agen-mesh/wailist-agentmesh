package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/agentmesh/backend/internal/alert"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/respond"
)

const (
	minPayPalAmountUSDCents = 100     // $1
	maxPayPalAmountUSDCents = 600_000 // $6,000, matching the other USD gateways
)

// CreatePayPalOrder opens a PayPal-hosted approval flow for a USD top-up and
// returns the URL to send the payer to.
func (d *Deps) CreatePayPalOrder(w http.ResponseWriter, r *http.Request) {
	if d.PayPal == nil {
		respond.Error(w, http.StatusServiceUnavailable, "paypal payments are not configured")
		return
	}
	userID, _ := r.Context().Value(CtxUserID).(string)

	var body struct {
		AmountUSDCents int64 `json:"amount_usd_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.AmountUSDCents < minPayPalAmountUSDCents {
		respond.Error(w, http.StatusBadRequest, "amount must be at least 100 cents")
		return
	}
	if body.AmountUSDCents > maxPayPalAmountUSDCents {
		respond.Error(w, http.StatusBadRequest, "amount exceeds maximum allowed")
		return
	}

	orderID := uuid.New().String()

	// Ledger row first, same ordering as every other provider here.
	if _, err := d.Store.CreateUSDCreditTransaction(r.Context(), userID, "paypal", orderID, body.AmountUSDCents); err != nil {
		log.Printf("paypal order: create ledger row: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	order, err := d.PayPal.CreateOrder(
		r.Context(),
		body.AmountUSDCents,
		orderID,
		d.FrontendURL+"/billing?paypal=success&order_id="+orderID,
		d.FrontendURL+"/billing?paypal=cancelled",
	)
	if err != nil {
		log.Printf("paypal order: create order: %v", err)
		respond.Error(w, http.StatusBadGateway, "paypal order creation failed")
		return
	}

	respond.JSON(w, http.StatusCreated, map[string]any{
		"order_id":        orderID,
		"paypal_order_id": order.ID,
		"approve_url":     order.ApproveURL,
	})
}

// CapturePayPalOrder is the client-side return path: the payer approved on
// PayPal and came back, and this actually takes the money. PayPal orders are
// authorize-then-capture, so unlike Cashfree's verify this is not merely a
// read -- until it runs, nothing has been charged.
//
// The webhook is the backstop for a payer who approves and then closes the tab
// before the return lands. CompleteCreditTransaction is idempotent, and
// CaptureOrder treats an already-captured order as success, so the two paths
// racing is safe.
func (d *Deps) CapturePayPalOrder(w http.ResponseWriter, r *http.Request) {
	if d.PayPal == nil {
		respond.Error(w, http.StatusServiceUnavailable, "paypal payments are not configured")
		return
	}

	var body struct {
		OrderID       string `json:"order_id"`
		PayPalOrderID string `json:"paypal_order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.OrderID == "" || body.PayPalOrderID == "" {
		respond.Error(w, http.StatusBadRequest, "missing order_id or paypal_order_id")
		return
	}

	status, captureID, customID, err := d.PayPal.CaptureOrder(r.Context(), body.PayPalOrderID)
	if err != nil {
		log.Printf("paypal capture: %v", err)
		respond.Error(w, http.StatusBadGateway, "could not capture payment with paypal")
		return
	}
	// Same binding check as the Stripe path: order_id and paypal_order_id are
	// two independent client-supplied values, and capturing one order must
	// never be able to credit a different one. custom_id is the credit_ledger
	// order id we set at CreateOrder time, so PayPal is the authority on the
	// pairing rather than the caller.
	if customID == "" || customID != body.OrderID {
		log.Printf("paypal capture: paypal order %s belongs to order %q, not %q", body.PayPalOrderID, customID, body.OrderID)
		respond.Error(w, http.StatusBadRequest, "payment does not belong to this order")
		return
	}
	if status != "COMPLETED" {
		respond.Error(w, http.StatusPaymentRequired, "payment not completed")
		return
	}

	creditedMicros, applied, err := d.Store.CompleteCreditTransaction(r.Context(), "paypal", body.OrderID, captureID)
	if errors.Is(err, db.ErrCreditTransactionNotFound) {
		respond.Error(w, http.StatusBadRequest, "unknown order")
		return
	}
	if err != nil {
		log.Printf("paypal capture: complete transaction: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if applied {
		go alert.Notify(context.Background(), alert.ChannelCredits, fmt.Sprintf("credited $%.2f (order %s, capture %s, via paypal)", float64(creditedMicros)/1e6, body.OrderID, captureID))
	}

	respond.JSON(w, http.StatusOK, map[string]any{
		"status":              "credited",
		"credited_usd_micros": creditedMicros,
	})
}

// PayPalWebhook is the server-side backstop for the capture path above.
//
// Public, unauthenticated route. PayPal signs with a rotating certificate
// rather than a shared secret, so verification is a call back to PayPal --
// see PayPalClient.VerifyWebhookSignature.
func (d *Deps) PayPalWebhook(w http.ResponseWriter, r *http.Request) {
	if d.PayPal == nil {
		respond.Error(w, http.StatusServiceUnavailable, "paypal payments are not configured")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "could not read body")
		return
	}

	if !d.PayPal.VerifyWebhookSignature(r.Context(), body, r.Header) {
		log.Printf("paypal webhook: rejected signature from %s", r.RemoteAddr)
		go alert.Notify(context.Background(), alert.ChannelPayments, fmt.Sprintf("rejected paypal webhook signature from %s", r.RemoteAddr))
		respond.Error(w, http.StatusBadRequest, "signature verification failed")
		return
	}

	// custom_id sits at two different depths depending on the event. For
	// PAYMENT.CAPTURE.* the resource IS a capture and carries it directly;
	// for CHECKOUT.ORDER.* the resource is the order, and it lives on the
	// purchase unit. Reading only the top level made every VOIDED event
	// resolve to no order and get silently ignored.
	var event struct {
		EventType string `json:"event_type"`
		Resource  struct {
			ID            string `json:"id"`
			CustomID      string `json:"custom_id"`
			Status        string `json:"status"`
			PurchaseUnits []struct {
				CustomID string `json:"custom_id"`
			} `json:"purchase_units"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid payload")
		return
	}

	// custom_id is the credit_ledger order id we set at CreateOrder time and
	// is what survives onto the capture resource. An event without one is for
	// something we did not originate.
	orderID := event.Resource.CustomID
	if orderID == "" && len(event.Resource.PurchaseUnits) > 0 {
		orderID = event.Resource.PurchaseUnits[0].CustomID
	}
	if orderID == "" {
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	switch event.EventType {
	case "PAYMENT.CAPTURE.COMPLETED":
		creditedMicros, applied, err := d.Store.CompleteCreditTransaction(r.Context(), "paypal", orderID, event.Resource.ID)
		if err != nil {
			if errors.Is(err, db.ErrCreditTransactionNotFound) {
				// Permanently unresolvable (a stale test-mode delivery, a
				// restored database, a shared endpoint). 4xx would have PayPal
				// redeliver on a backoff for days; acknowledge instead.
				log.Printf("paypal webhook: unknown order_id %s (capture %s)", orderID, event.Resource.ID)
				respond.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
				return
			}
			log.Printf("paypal webhook: complete transaction: %v", err)
			go alert.Notify(context.Background(), alert.ChannelPayments, fmt.Sprintf("failed to complete paypal order %s: %v", orderID, err))
			respond.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		if applied {
			go alert.Notify(context.Background(), alert.ChannelCredits, fmt.Sprintf("credited $%.2f (order %s, capture %s, via paypal webhook)", float64(creditedMicros)/1e6, orderID, event.Resource.ID))
		}
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})

	case "PAYMENT.CAPTURE.DENIED", "CHECKOUT.ORDER.VOIDED":
		// Both mean the money never arrived, so the row is still pending and
		// MarkCreditTransactionStatus (which only touches pending rows) is the
		// right tool.
		if err := d.Store.MarkCreditTransactionStatus(r.Context(), "paypal", orderID, "failed"); err != nil {
			log.Printf("paypal webhook: mark failed: %v", err)
			respond.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})

	case "PAYMENT.CAPTURE.REVERSED", "PAYMENT.CAPTURE.REFUNDED":
		// A reversal arrives AFTER a successful capture, so the ledger row is
		// already 'completed' and the credit is already spendable.
		// MarkCreditTransactionStatus would match zero rows here, which is why
		// this is deliberately NOT routed through it -- doing so looked like
		// handling and was a silent no-op.
		//
		// Clawing the balance back automatically is not safe to do blind: the
		// user may already have spent the credits, and
		// users.credit_balance_usd_micros carries a CHECK (>= 0) that would
		// reject the debit outright. What happens then (negative balance,
		// partial claw, account hold) is a policy decision, not something to
		// improvise inside a webhook. So this alerts for manual handling and
		// says plainly that nothing was reversed.
		log.Printf("paypal webhook: %s on order %s (capture %s) — credit NOT reversed, needs manual reconciliation", event.EventType, orderID, event.Resource.ID)
		go alert.Notify(context.Background(), alert.ChannelPayments, fmt.Sprintf("paypal %s on order %s — credits were already granted and have NOT been clawed back; reconcile manually", event.EventType, orderID))
		respond.JSON(w, http.StatusOK, map[string]string{"status": "manual_reconciliation_required"})

	default:
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
}
