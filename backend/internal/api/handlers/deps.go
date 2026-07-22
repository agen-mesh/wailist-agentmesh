package handlers

import (
	"context"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/payments"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/wallet"
)

type contextKey string

const CtxUserID contextKey = "userID"

// RazorpayClient is the subset of *payments.RazorpayClient the handlers need.
// Defined here so tests can inject a fake without hitting the real API.
type RazorpayClient interface {
	CreateOrder(ctx context.Context, amountPaise int64, receipt string) (payments.RazorpayOrder, error)
	VerifySignature(orderID, paymentID, signature string) bool
	VerifyWebhookSignature(body []byte, signature string) bool
}

type Deps struct {
	Store         *db.Store
	Broker        *sse.Broker
	Wallet        *wallet.Service
	Engine        *engine.Runner
	BaseURL       string
	JWTSecret     string
	EncryptionKey string

	FrontendURL        string
	GithubClientID     string
	GithubClientSecret string
	GoogleClientID     string
	GoogleClientSecret string

	Razorpay      RazorpayClient
	RazorpayKeyID string
}
