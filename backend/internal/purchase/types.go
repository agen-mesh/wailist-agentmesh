package purchase

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const ChainID int64 = 5042002
const Asset = "0x3600000000000000000000000000000000000000"
const Protocol = "agentmesh-purchase-v1"

var (
	ErrDisabled  = errors.New("purchases are not configured")
	ErrConflict  = errors.New("purchase state conflicts with this action")
	ErrNotFound  = errors.New("purchase not found")
	ErrDenied    = errors.New("purchase authorization denied")
	ErrUncertain = errors.New("purchase requires reconciliation")
)

type State string

const (
	AwaitingIdentity      State = "awaiting_identity"
	AwaitingApproval      State = "awaiting_approval"
	Approved              State = "approved"
	AuthorizationReleased State = "authorization_released"
	Submitting            State = "submitting"
	PaymentUnknown        State = "payment_unknown"
	Paid                  State = "paid"
	Fulfilling            State = "fulfilling"
	Completed             State = "completed"
	PaidToolFailed        State = "paid_tool_failed"
	FulfillmentUnknown    State = "fulfillment_unknown"
	Denied                State = "denied"
	Expired               State = "expired"
	Cancelled             State = "cancelled"
)

type Offer struct {
	Key            string          `json:"key"`
	Owner          string          `json:"owner"`
	Version        string          `json:"version"`
	ContentHash    string          `json:"contentHash"`
	SnapshotHash   string          `json:"snapshotHash"`
	Endpoint       string          `json:"endpoint"`
	Method         string          `json:"method"`
	Capability     string          `json:"capability"`
	InputSchema    json.RawMessage `json:"inputSchema"`
	ChainID        int64           `json:"chainId"`
	Asset          string          `json:"asset"`
	Payee          string          `json:"payee"`
	PriceMicros    string          `json:"priceMicros"`
	ValidUntil     time.Time       `json:"validUntil"`
	ExpiresAtBlock string          `json:"expiresAtBlock"`
	ObservedBlock  string          `json:"observedBlock"`
	Source         json.RawMessage `json:"source,omitempty"`
}

type Quote struct {
	ID            string    `json:"id"`
	Method        string    `json:"method"`
	Resource      string    `json:"resource"`
	RequestDigest string    `json:"requestDigest"`
	ChainID       int64     `json:"chainId"`
	Asset         string    `json:"asset"`
	Payee         string    `json:"payee"`
	AmountMicros  string    `json:"amountMicros"`
	Deadline      time.Time `json:"deadline"`
	Proof         string    `json:"proof"`
}

// Field order and normalized strings are part of version 1's commitment.
type Intent struct {
	Version           string `json:"version"`
	OrderID           string `json:"orderId"`
	AccountID         string `json:"accountId"`
	WorkflowID        string `json:"workflowId"`
	RunID             string `json:"runId"`
	NodeID            string `json:"nodeId"`
	Invocation        string `json:"invocation"`
	OfferKey          string `json:"offerKey"`
	OfferOwner        string `json:"offerOwner"`
	OfferVersion      string `json:"offerVersion"`
	OfferContentHash  string `json:"offerContentHash"`
	OfferSnapshotHash string `json:"offerSnapshotHash"`
	Payer             string `json:"payer"`
	Payee             string `json:"payee"`
	ChainID           int64  `json:"chainId"`
	Asset             string `json:"asset"`
	AmountMicros      string `json:"amountMicros"`
	MaxMicros         string `json:"maxMicros"`
	Method            string `json:"method"`
	Resource          string `json:"resource"`
	RequestDigest     string `json:"requestDigest"`
	QuoteDigest       string `json:"quoteDigest"`
	OrderSalt         string `json:"orderSalt"`
	ValidAfter        string `json:"validAfter"`
	ValidBefore       string `json:"validBefore"`
}

type PaymentEvent struct {
	ChainID     int64  `json:"chainId"`
	TxHash      string `json:"txHash"`
	LogIndex    string `json:"logIndex"`
	BlockHash   string `json:"blockHash"`
	BlockNumber string `json:"blockNumber"`
}

type Order struct {
	ID                  string          `json:"id"`
	AccountID           string          `json:"accountId"`
	ClientRequestID     string          `json:"clientRequestId"`
	Intent              Intent          `json:"intent"`
	IntentHash          string          `json:"intentHash"`
	Offer               Offer           `json:"offer"`
	Quote               Quote           `json:"quote"`
	Request             json.RawMessage `json:"request"`
	State               State           `json:"state"`
	MerchantAccepted    bool            `json:"merchantAccepted"`
	CreatedAt           time.Time       `json:"createdAt"`
	IdentityVerifiedAt  *time.Time      `json:"identityVerifiedAt,omitempty"`
	ApprovedAt          *time.Time      `json:"approvedAt,omitempty"`
	SignatureReleasedAt *time.Time      `json:"signatureReleasedAt,omitempty"`
	AuthorizationBlock  string          `json:"authorizationBlock,omitempty"`
	TxHash              string          `json:"txHash,omitempty"`
	SignedEnvelope      string          `json:"signedEnvelope,omitempty"`
	PaymentEvent        *PaymentEvent   `json:"paymentEvent,omitempty"`
	Result              json.RawMessage `json:"result,omitempty"`
	Reason              string          `json:"reason,omitempty"`
}

type Wallet struct {
	Address    string    `json:"address"`
	VerifiedAt time.Time `json:"verifiedAt"`
}

type WalletChallenge struct {
	ID        string    `json:"id"`
	AccountID string    `json:"accountId"`
	Address   string    `json:"address"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Allowance struct {
	Wallet          string `json:"wallet"`
	ChainID         int64  `json:"chainId"`
	Asset           string `json:"asset"`
	LimitMicros     string `json:"limitMicros"`
	ReservedMicros  string `json:"reservedMicros"`
	ConsumedMicros  string `json:"consumedMicros"`
	AvailableMicros string `json:"availableMicros"`
}

type Ledger interface {
	Reserve(amount string) error
	Consume(amount string) error
	Release(amount string) error
	BindEvent(PaymentEvent) error
	RelayNonce(relayer string, observed uint64) (uint64, error)
}

type Store interface {
	Create(context.Context, Order) (Order, bool, error)
	Get(context.Context, string, string) (Order, error)
	ByRequest(context.Context, string, string) (Order, error)
	Update(context.Context, string, string, func(*Order, Ledger) error) (Order, error)
	Wallet(context.Context, string) (*Wallet, error)
	SaveChallenge(context.Context, WalletChallenge) error
	LinkWallet(context.Context, string, string, func(WalletChallenge) error) (Wallet, error)
	Allowance(context.Context, string, string) (Allowance, error)
	SetAllowance(context.Context, string, string, string) (Allowance, error)
}

type Catalog interface {
	List(context.Context, string, string) ([]Offer, error)
	Get(context.Context, string) (Offer, error)
	Revalidate(context.Context, Offer) error
}

type Merchant interface {
	Quote(context.Context, Offer, json.RawMessage) (Quote, error)
	Accept(context.Context, Order) error
	Fulfill(context.Context, Order) (json.RawMessage, string, error)
	Lookup(context.Context, Order) (json.RawMessage, string, error)
}

type IdentityStart struct {
	AuthorizationURL string    `json:"authorizationUrl"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

type Identity interface {
	Begin(context.Context, Order) (IdentityStart, error)
}

type Prepared struct {
	Relayer       string
	ObservedNonce uint64
	Sign          func(uint64) (raw string, hash string, err error)
}

type Settlement struct {
	Event         *PaymentEvent
	ExpiredUnused bool
}

type Rail interface {
	Check(context.Context) error
	Anchor(context.Context) (string, error)
	IsEOA(context.Context, string) error
	Prepare(context.Context, Order, string) (Prepared, error)
	Broadcast(context.Context, string, string) error
	Reconcile(context.Context, Order) (Settlement, error)
}
