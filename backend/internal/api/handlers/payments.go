package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/agentmesh/backend/internal/payments"
	"github.com/agentmesh/backend/internal/respond"
)

const minRazorpayAmountPaise = 100

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

	rate, err := payments.FetchINRToUSDRate(r.Context())
	if err != nil {
		log.Printf("razorpay order: fx rate: %v", err)
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

	creditedMicros, err := d.Store.CompleteCreditTransaction(r.Context(), body.OrderID, body.PaymentID)
	if err != nil {
		log.Printf("razorpay verify: complete transaction: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	respond.JSON(w, http.StatusOK, map[string]any{
		"status":              "credited",
		"credited_usd_micros": creditedMicros,
	})
}
