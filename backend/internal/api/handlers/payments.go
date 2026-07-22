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
	"github.com/agentmesh/backend/internal/payments"
	"github.com/agentmesh/backend/internal/respond"
)

const (
	minRazorpayAmountPaise = 100
	// 5,00,000 INR — well above any real top-up preset, guards against fat-fingered or
	// abusive amounts and keeps values comfortably inside float64 precision for the credit
	// math in CreateCreditTransaction.
	maxRazorpayAmountPaise = 5_00_000_00
)

func (d *Deps) CreateRazorpayOrder(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)

	var body struct {
		AmountINRPaise int64 `json:"amount_inr_paise"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.AmountINRPaise < minRazorpayAmountPaise {
		respond.Error(w, http.StatusBadRequest, "amount must be at least 100 paise")
		return
	}
	if body.AmountINRPaise > maxRazorpayAmountPaise {
		respond.Error(w, http.StatusBadRequest, "amount exceeds maximum allowed")
		return
	}

	rate, err := payments.FetchINRToUSDRate(r.Context())
	if err != nil {
		log.Printf("razorpay order: fx rate: %v", err)
		go alert.Notify(context.Background(), alert.ChannelPayments, fmt.Sprintf("FX rate fetch failing, top-ups are down: %v", err))
		respond.Error(w, http.StatusBadGateway, "could not fetch exchange rate")
		return
	}

	receipt := uuid.New().String()
	order, err := d.Razorpay.CreateOrder(r.Context(), body.AmountINRPaise, receipt)
	if err != nil {
		log.Printf("razorpay order: create order: %v", err)
		respond.Error(w, http.StatusBadGateway, "razorpay order creation failed")
		return
	}

	if _, err := d.Store.CreateCreditTransaction(r.Context(), userID, order.ID, body.AmountINRPaise, rate); err != nil {
		log.Printf("razorpay order: create ledger row: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	respond.JSON(w, http.StatusCreated, map[string]any{
		"order_id": order.ID,
		"amount":   body.AmountINRPaise,
		"currency": "INR",
		"key_id":   d.RazorpayKeyID,
	})
}

func (d *Deps) GetCreditBalance(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)

	balance, err := d.Store.GetCreditBalance(r.Context(), userID)
	if err != nil {
		log.Printf("credit balance: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	respond.JSON(w, http.StatusOK, map[string]any{"credit_usd_micros": balance})
}

func (d *Deps) VerifyRazorpayPayment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrderID   string `json:"razorpay_order_id"`
		PaymentID string `json:"razorpay_payment_id"`
		Signature string `json:"razorpay_signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		body.OrderID == "" || body.PaymentID == "" || body.Signature == "" {
		respond.Error(w, http.StatusBadRequest, "missing required fields")
		return
	}

	if !d.Razorpay.VerifySignature(body.OrderID, body.PaymentID, body.Signature) {
		respond.Error(w, http.StatusBadRequest, "signature verification failed")
		return
	}

	creditedMicros, applied, err := d.Store.CompleteCreditTransaction(r.Context(), body.OrderID, body.PaymentID)
	if errors.Is(err, db.ErrCreditTransactionNotFound) {
		respond.Error(w, http.StatusBadRequest, "unknown order")
		return
	}
	if err != nil {
		log.Printf("razorpay verify: complete transaction: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if applied {
		go alert.Notify(context.Background(), alert.ChannelCredits, fmt.Sprintf("credited $%.2f (order %s, payment %s)", float64(creditedMicros)/1e6, body.OrderID, body.PaymentID))
	}

	respond.JSON(w, http.StatusOK, map[string]any{
		"status":              "credited",
		"credited_usd_micros": creditedMicros,
	})
}

// RazorpayWebhook is the server-side backstop for CreateRazorpayOrder/VerifyRazorpayPayment:
// if a client-side verify call never lands (dropped connection, closed tab) after Razorpay
// actually captures a payment, this webhook independently completes the same ledger row.
// CompleteCreditTransaction is idempotent, so it's safe to call from both this webhook and
// the client verify path for the same order without double-crediting.
//
// This is a public, unauthenticated route (registered outside the JWT auth group) because
// Razorpay's servers call it directly, with no user session — the request is instead
// authenticated by the HMAC signature in the X-Razorpay-Signature header, verified against
// the webhook secret configured in the Razorpay dashboard.
func (d *Deps) RazorpayWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "could not read body")
		return
	}

	signature := r.Header.Get("X-Razorpay-Signature")
	if signature == "" || !d.Razorpay.VerifyWebhookSignature(body, signature) {
		log.Printf("razorpay webhook: rejected signature from %s", r.RemoteAddr)
		go alert.Notify(context.Background(), alert.ChannelPayments, fmt.Sprintf("rejected webhook signature from %s", r.RemoteAddr))
		respond.Error(w, http.StatusBadRequest, "signature verification failed")
		return
	}

	var event struct {
		Event   string `json:"event"`
		Payload struct {
			Payment struct {
				Entity struct {
					ID             string `json:"id"`
					OrderID        string `json:"order_id"`
					AmountRefunded int64  `json:"amount_refunded"`
				} `json:"entity"`
			} `json:"payment"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid payload")
		return
	}

	orderID := event.Payload.Payment.Entity.OrderID

	switch event.Event {
	case "payment.captured":
		paymentID := event.Payload.Payment.Entity.ID
		if orderID == "" || paymentID == "" {
			respond.Error(w, http.StatusBadRequest, "missing order or payment id")
			return
		}

		creditedMicros, applied, err := d.Store.CompleteCreditTransaction(r.Context(), orderID, paymentID)
		if err != nil {
			if errors.Is(err, db.ErrCreditTransactionNotFound) {
				// A 4xx here tells Razorpay to stop retrying — this order will never exist,
				// so retrying is pure noise, not a path to eventual success.
				log.Printf("razorpay webhook: unknown order_id %s (payment %s)", orderID, paymentID)
				respond.Error(w, http.StatusBadRequest, "unknown order")
				return
			}
			log.Printf("razorpay webhook: complete transaction: %v", err)
			go alert.Notify(context.Background(), alert.ChannelPayments, fmt.Sprintf("failed to complete order %s: %v", orderID, err))
			respond.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		if applied {
			go alert.Notify(context.Background(), alert.ChannelCredits, fmt.Sprintf("credited $%.2f (order %s, payment %s, via webhook)", float64(creditedMicros)/1e6, orderID, paymentID))
		}

		respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})

	case "refund.processed", "payment.refunded":
		if orderID == "" {
			respond.Error(w, http.StatusBadRequest, "missing order id")
			return
		}

		reversed, applied, err := d.Store.RefundCreditTransaction(r.Context(), orderID, event.Payload.Payment.Entity.AmountRefunded)
		if err != nil {
			if errors.Is(err, db.ErrCreditTransactionNotFound) {
				log.Printf("razorpay webhook: refund for unknown order_id %s", orderID)
				respond.Error(w, http.StatusBadRequest, "unknown order")
				return
			}
			log.Printf("razorpay webhook: refund order %s: %v", orderID, err)
			go alert.Notify(context.Background(), alert.ChannelPayments, fmt.Sprintf("failed to process refund for order %s: %v", orderID, err))
			respond.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		if applied {
			go alert.Notify(context.Background(), alert.ChannelCredits, fmt.Sprintf("refunded $%.2f reversed (order %s)", float64(reversed)/1e6, orderID))
		}

		respond.JSON(w, http.StatusOK, map[string]any{"status": "refunded", "reversed_usd_micros": reversed})

	default:
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
}
