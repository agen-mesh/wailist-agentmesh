package handlers

import (
	"context"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
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

// Signer signs EIP-3009 TransferWithAuthorization payments for the x402
// "exact" scheme on Whitechain. Satisfied by *wallet.Service; identical to
// nodes.PaymentSigner so d.Signer can be handed straight to the engine.
type Signer = nodes.PaymentSigner

var _ Signer = (*wallet.Service)(nil)

type Deps struct {
	Store *db.Store
	// WorkflowStatsLoader overrides the detail endpoint's stats read when set.
	// Production uses Store; the seam lets handler tests exercise its failure
	// response without damaging shared database tables.
	WorkflowStatsLoader func(context.Context, string, *models.Workflow) error
	Broker              *sse.Broker
	Wallet              *wallet.Service
	Engine              *engine.Runner
	BaseURL             string
	// RelayBaseURL is where THIS instance's own /x402/relay is reached from
	// (see main.go's relayBaseURL — distinct from BaseURL, which also signs
	// auth cookies). Needed by the Tendril console's direct-action endpoints
	// to build their own X402RelayConfig.
	RelayBaseURL  string
	JWTSecret     string
	EncryptionKey string

	FrontendURL        string
	GithubClientID     string
	GithubClientSecret string
	GoogleClientID     string
	GoogleClientSecret string

	// BazaarBaseURL is GoPlausible's facilitator, whose public
	// /discovery/resources catalog backs the Bazaar page. Overridable so tests
	// can point at a fake upstream.
	BazaarBaseURL string
	bazaarCache   bazaarCache

	Cashfree      CashfreeClient
	CashfreeAppID string

	NOWPayments NOWPaymentsClient

	PlatformWalletAddress string
	PlatformWalletEncKey  string
	// PlatformSpendWalletEncKey is Wallet 1 — the wallet that actually
	// signs and sends every relayed outbound x402 payment. Needed here (not
	// just inside engine.Runner) so the Tendril console's direct-action
	// endpoints can build their own X402RelayConfig without going through a
	// workflow run.
	PlatformSpendWalletEncKey string
	// PlatformGeminiAPIKey powers the chat-driven workflow builder's
	// meta-agent (nodes.BuildGraph) -- independent of engine.Runner's own
	// platform-key map (main.go's runner.SetPlatformKeys call) since Deps
	// has no access to Runner's private fields.
	PlatformGeminiAPIKey string
	FacilitatorClient    *x402.FacilitatorClient
	USDCAsset            string
	RelayNetwork         string
	Signer               Signer
	// MaxRelayOutboundUSDMicros caps a single outbound relay payment
	// (Wallet 2 -> target). The relay fetches a target's price quote twice
	// per cycle (once for the public challenge preview, once again at
	// settle-time to enforce/pay) — a target that answers cheap on the
	// first fetch and expensive on the second could otherwise cause the
	// platform to pay out more than the caller's inbound payment ever
	// covered, bounded only by whatever the GoPlausible facilitator itself
	// enforces. This is an independent, local backstop bounding worst-case
	// loss per call to a fixed ceiling regardless of facilitator behavior.
	// Zero means no cap (not recommended for a production deployment).
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
	TodoistClientID             string
	TodoistClientSecret         string
}

func (d *Deps) loadWorkflowStats(ctx context.Context, userID string, wf *models.Workflow) error {
	if d.WorkflowStatsLoader != nil {
		return d.WorkflowStatsLoader(ctx, userID, wf)
	}
	return d.Store.AttachWorkflowStats(ctx, userID, wf)
}
