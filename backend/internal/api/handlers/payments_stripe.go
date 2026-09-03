package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/agentmesh/backend/internal/alert"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/respond"
)

const (
	// Stripe's own floor for a USD charge is $0.50; anything lower is
	// rejected by their API, so reject it here with a message that says why.
	minStripeAmountUSDCents = 50
	maxStripeAmountUSDCents = 600_000 // $6,000, matching the crypto ceiling
)

// CreateStripeCheckout opens a hosted Stripe Checkout session for a
// USD-denominated top-up and returns the URL to send the browser to.
func (d *Deps) CreateStripeCheckout(w http.ResponseWriter, r *http.Request) {
	if d.Stripe == nil {
		respond.Error(w, http.StatusServiceUnavailable, "stripe payments are not configured")
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
	if body.AmountUSDCents < minStripeAmountUSDCents {
		respond.Error(w, http.StatusBadRequest, "amount must be at least 50 cents")
		return
	}
	if body.AmountUSDCents > maxStripeAmountUSDCents {
		respond.Error(w, http.StatusBadRequest, "amount exceeds maximum allowed")
		return
	}

	user, err := d.Store.GetUserByID(r.Context(), userID)
	if err != nil {
		log.Printf("stripe checkout: get user: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	orderID := uuid.New().String()

	// Ledger row first, same ordering as the Cashfree path: a Stripe failure
	// then leaves a harmless dead pending row rather than a real payable
	// session with no row to complete.
	if _, err := d.Store.CreateUSDCreditTransaction(r.Context(), userID, "stripe", orderID, body.AmountUSDCents); err != nil {
		log.Printf("stripe checkout: create ledger row: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	session, err := d.Stripe.CreateCheckoutSession(
		r.Context(),
		body.AmountUSDCents,
		orderID,
		user.Email,
		d.FrontendURL+"/billing?stripe=success&order_id="+orderID,
		d.FrontendURL+"/billing?stripe=cancelled",
	)
	if err != nil {
		log.Printf("stripe checkout: create session: %v", err)
		respond.Error(w, http.StatusBadGateway, "stripe checkout creation failed")
		return
	}

	respond.JSON(w, http.StatusCreated, map[string]any{
		"order_id":     orderID,
		"session_id":   session.ID,
		"checkout_url": session.URL,
	})
}

// VerifyStripePayment is the client-side return path: the browser comes back
// from Stripe and asks us to confirm. It re-reads the session server-to-server
// rather than trusting the redirect, exactly as VerifyCashfreePayment does.
// The webhook remains the authoritative backstop; both are idempotent.
func (d *Deps) VerifyStripePayment(w http.ResponseWriter, r *http.Request) {
	if d.Stripe == nil {
		respond.Error(w, http.StatusServiceUnavailable, "stripe payments are not configured")
		return
	}

	var body struct {
		OrderID   string `json:"order_id"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.OrderID == "" || body.SessionID == "" {
		respond.Error(w, http.StatusBadRequest, "missing order_id or session_id")
		return
	}

	status, paymentIntentID, sessionOrderID, err := d.Stripe.GetCheckoutSessionStatus(r.Context(), body.SessionID)
	if err != nil {
		log.Printf("stripe verify: get session: %v", err)
		respond.Error(w, http.StatusBadGateway, "could not verify payment with stripe")
		return
	}
	// order_id and session_id arrive as two independent client-supplied
	// values, so proving the session is paid says nothing about WHICH order it
	// paid for. Without this check a caller could pair any paid session
	// (their own $1 top-up, replayed) with any pending order of any size and
	// have it credited. Stripe's own client_reference_id is the authority on
	// which ledger row this session belongs to.
	if sessionOrderID == "" || sessionOrderID != body.OrderID {
		log.Printf("stripe verify: session %s belongs to order %q, not %q", body.SessionID, sessionOrderID, body.OrderID)
		respond.Error(w, http.StatusBadRequest, "session does not belong to this order")
		return
	}
	if status != "paid" {
		respond.Error(w, http.StatusPaymentRequired, "payment not completed")
		return
	}

	creditedMicros, applied, err := d.Store.CompleteCreditTransaction(r.Context(), "stripe", body.OrderID, paymentIntentID)
	if errors.Is(err, db.ErrCreditTransactionNotFound) {
		respond.Error(w, http.StatusBadRequest, "unknown order")
		return
	}
	if err != nil {
		log.Printf("stripe verify: complete transaction: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if applied {
		go alert.Notify(context.Background(), alert.ChannelCredits, fmt.Sprintf("credited $%.2f (order %s, via stripe)", float64(creditedMicros)/1e6, body.OrderID))
	}

	respond.JSON(w, http.StatusOK, map[string]any{
		"status":              "credited",
		"credited_usd_micros": creditedMicros,
	})
}

// StripeWebhook is the server-side backstop for the verify path above.
//
// Public, unauthenticated route -- Stripe's servers call it directly,
// authenticated by the HMAC-SHA256 signature in the Stripe-Signature header.
func (d *Deps) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	if d.Stripe == nil {
		respond.Error(w, http.StatusServiceUnavailable, "stripe payments are not configured")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "could not read body")
		return
	}

	if !d.Stripe.VerifyWebhookSignature(body, r.Header.Get("Stripe-Signature"), time.Now()) {
		log.Printf("stripe webhook: rejected signature from %s", r.RemoteAddr)
		go alert.Notify(context.Background(), alert.ChannelPayments, fmt.Sprintf("rejected stripe webhook signature from %s", r.RemoteAddr))
		respond.Error(w, http.StatusBadRequest, "signature verification failed")
		return
	}

	var event struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID                string `json:"id"`
				ClientReferenceID string `json:"client_reference_id"`
				PaymentStatus     string `json:"payment_status"`
				PaymentIntent     string `json:"payment_intent"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid payload")
		return
	}

	orderID := event.Data.Object.ClientReferenceID
	if orderID == "" {
		// Every event this endpoint acts on carries our own reference. One
		// that doesn't is for something we didn't originate -- acknowledge it
		// so Stripe stops retrying, but do nothing.
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	switch event.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		// A completed session is NOT necessarily a paid one: with delayed
		// payment methods the session completes first and the money arrives
		// later, as async_payment_succeeded. Crediting on completion alone
		// would hand out credits for a payment that can still fail.
		if event.Data.Object.PaymentStatus != "paid" {
			respond.JSON(w, http.StatusOK, map[string]string{"status": "pending"})
			return
		}
		creditedMicros, applied, err := d.Store.CompleteCreditTransaction(r.Context(), "stripe", orderID, event.Data.Object.PaymentIntent)
		if err != nil {
			if errors.Is(err, db.ErrCreditTransactionNotFound) {
				// Permanently unresolvable, so a 4xx would only make Stripe
				// redeliver on a backoff for up to three days. Acknowledge it.
				log.Printf("stripe webhook: unknown order_id %s", orderID)
				respond.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
				return
			}
			log.Printf("stripe webhook: complete transaction: %v", err)
			go alert.Notify(context.Background(), alert.ChannelPayments, fmt.Sprintf("failed to complete stripe order %s: %v", orderID, err))
			respond.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		if applied {
			go alert.Notify(context.Background(), alert.ChannelCredits, fmt.Sprintf("credited $%.2f (order %s, via stripe webhook)", float64(creditedMicros)/1e6, orderID))
		}
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})

	case "checkout.session.expired", "checkout.session.async_payment_failed":
		if err := d.Store.MarkCreditTransactionStatus(r.Context(), "stripe", orderID, "failed"); err != nil {
			log.Printf("stripe webhook: mark failed: %v", err)
			respond.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})

	default:
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
}
