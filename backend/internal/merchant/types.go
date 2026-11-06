package merchant

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"time"
)

const (
	ChainID         int64 = 5042002
	Asset                 = "0x3600000000000000000000000000000000000000"
	Protocol              = "agentmesh-purchase-v1"
	Capability            = "text.digest.v1"
	MaxRequestBytes       = 65536
	MaxTextBytes          = 16384
)

var (
	ErrInvalid        = errors.New("invalid merchant input")
	ErrConflict       = errors.New("merchant intent conflict")
	ErrNotFound       = errors.New("merchant order not accepted")
	ErrExpired        = errors.New("merchant quote expired")
	ErrPayment        = errors.New("payment evidence rejected")
	ErrPaymentPending = errors.New("payment confirmation pending")
	ErrUnknown        = errors.New("fulfillment requires reconciliation")
	ErrUnavailable    = errors.New("merchant unavailable")
	ErrService        = errors.New("paid service failed")
)

type Snapshot struct {
	OrderID         string          `json:"orderId"`
	IntentHash      string          `json:"intentHash"`
	CanonicalIntent json.RawMessage `json:"canonicalIntent"`
	Payer           string          `json:"payer"`
	Payee           string          `json:"payee"`
	ChainID         int64           `json:"chainId"`
	Asset           string          `json:"asset"`
	AmountMicros    string          `json:"amountMicros"`
	Method          string          `json:"method"`
	Resource        string          `json:"resource"`
	RequestDigest   string          `json:"requestDigest"`
	QuoteDigest     string          `json:"quoteDigest"`
	Deadline        time.Time       `json:"deadline"`
	Request         json.RawMessage `json:"request"`
}

// Field order, string encoding and tags are the version 1 wire commitment.
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

type QuoteRequest struct {
	Method   string          `json:"method"`
	Resource string          `json:"resource"`
	Request  json.RawMessage `json:"request"`
	Deadline time.Time       `json:"deadline"`
}

type PaymentEvent struct {
	ChainID     int64  `json:"chainId"`
	TxHash      string `json:"txHash"`
	LogIndex    string `json:"logIndex"`
	BlockHash   string `json:"blockHash"`
	BlockNumber string `json:"blockNumber"`
}

// Only the injected chain verifier returns these facts, never the HTTP caller.
type PaymentProof struct {
	Event                  PaymentEvent
	Payer                  string
	Payee                  string
	Asset                  string
	AmountMicros           string
	AuthorizationNonce     string
	PaidAt                 time.Time
	AuthorizationConfirmed bool
	TransferConfirmed      bool
}

type VerifyPayment func(context.Context, Snapshot, PaymentEvent) (PaymentProof, error)

type State string

const (
	Accepted  State = "accepted"
	Pending   State = "pending"
	Started   State = "started"
	Completed State = "completed"
	Failed    State = "failed"
	Unknown   State = "unknown"
)

type Record struct {
	Snapshot    Snapshot        `json:"snapshot"`
	State       State           `json:"state"`
	Payment     *PaymentEvent   `json:"payment,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	FailureCode string          `json:"failureCode,omitempty"`
	AcceptedAt  time.Time       `json:"acceptedAt"`
}

type Outcome struct {
	State       State           `json:"state"`
	Result      json.RawMessage `json:"result,omitempty"`
	FailureCode string          `json:"failureCode,omitempty"`
}

// Implementations must persist atomically and retain immutable snapshots/results.
// Claim reserves chain/transaction/log globally before transitioning to Started.
// Only its true result grants execution; Started and Unknown never grant it again.
type Store interface {
	Accept(context.Context, Snapshot) (Record, error)
	Get(context.Context, string) (Record, error)
	Claim(context.Context, string, string, PaymentEvent) (Record, bool, error)
	Finish(context.Context, string, string, Outcome) (Record, error)
}

type Execute func(context.Context, json.RawMessage) (json.RawMessage, error)

type Config struct {
	Resource     string
	Payee        string
	AmountMicros string
	Capability   string
	PrivateKey   ed25519.PrivateKey
	QuoteTTL     time.Duration
	Now          func() time.Time
	Execute      Execute
}
