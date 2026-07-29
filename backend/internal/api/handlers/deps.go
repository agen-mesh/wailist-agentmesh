package handlers

import (
	"context"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/payments"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/wallet"
	"github.com/agentmesh/backend/internal/x402"
)

type contextKey string

const CtxUserID contextKey = "userID"

// CashfreeClient is the subset of *payments.CashfreeClient the handlers need.
// Defined here so tests can inject a fake without hitting the real API.
type CashfreeClient interface {
	CreateOrder(ctx context.Context, amountPaise int64, orderID, customerID, customerEmail, customerPhone string) (payments.CashfreeOrder, error)
	GetOrderStatus(ctx context.Context, orderID string) (string, error)
	VerifyWebhookSignature(body []byte, signature, timestamp string) bool
}

// NOWPaymentsClient is the subset of *payments.NOWPaymentsClient the handlers need.
// Defined here so tests can inject a fake without hitting the real API.
type NOWPaymentsClient interface {
	CreateInvoice(ctx context.Context, amountUSDCents int64, orderID, ipnCallbackURL, successURL, cancelURL string) (payments.Invoice, error)
	VerifyIPNSignature(body []byte, signature string) bool
}

// USDCSigner builds a gasless USDC atomic-payment group for the X-Payment
// header. Satisfied by *wallet.Service (SignUSDCPaymentGroup).
type USDCSigner interface {
	SignUSDCPaymentGroup(ctx context.Context, encMnemonic, payTo string, assetID, amountMicros uint64, feePayerAddr string) ([]string, int, error)
}

var _ USDCSigner = (*wallet.Service)(nil)

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

	Cashfree      CashfreeClient
	CashfreeAppID string

	NOWPayments NOWPaymentsClient

	PlatformWalletAddress     string
	PlatformWalletEncMnemonic string
	FacilitatorClient         *x402.FacilitatorClient
	USDCAssetID               uint64
	RelayNetwork              string
	RelayFeePayer             string
	USDCSigner                USDCSigner
	// MaxRelayOutboundUSDMicros caps a single outbound relay payment.
	// Zero means no cap (not recommended for production).
	MaxRelayOutboundUSDMicros int64

	SlackOAuthClientID          string
	SlackOAuthClientSecret      string
	GitHubConnectorClientID     string
	GitHubConnectorClientSecret string
	NotionClientID              string
	NotionClientSecret          string
	AirtableClientID            string
	AirtableClientSecret        string
	HubSpotClientID             string
	HubSpotClientSecret         string
	AsanaClientID               string
	AsanaClientSecret           string
	ClickUpClientID             string
	ClickUpClientSecret         string
	JiraClientID                string
	JiraClientSecret            string
	LinearClientID              string
	LinearClientSecret          string
	MailchimpClientID           string
	MailchimpClientSecret       string
	GitLabClientID              string
	GitLabClientSecret          string
	TrelloClientID              string
	TrelloClientSecret          string
	TodoistClientID             string
	TodoistClientSecret         string
}
