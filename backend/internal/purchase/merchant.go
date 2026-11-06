package purchase

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/merchant"
)

type MerchantClient struct {
	Client      *http.Client
	Policy      func(context.Context, string) error
	BaseURL     string
	Resource    string
	Bearer      string
	PublicKey   ed25519.PublicKey
	Payee       string
	PriceMicros string
}

func merchantSnapshot(o Order) (merchant.Snapshot, error) {
	canonical, err := json.Marshal(o.Intent)
	if err != nil {
		return merchant.Snapshot{}, err
	}
	return merchant.Snapshot{OrderID: o.ID, IntentHash: o.IntentHash, CanonicalIntent: canonical, Payer: o.Intent.Payer, Payee: o.Intent.Payee, ChainID: o.Intent.ChainID, Asset: o.Intent.Asset, AmountMicros: o.Intent.AmountMicros, Method: o.Intent.Method, Resource: o.Intent.Resource, RequestDigest: o.Intent.RequestDigest, QuoteDigest: o.Intent.QuoteDigest, Deadline: Deadline(o), Request: o.Request}, nil
}
func (c *MerchantClient) request(ctx context.Context, path string, body any, out any) error {
	if c.Client == nil || c.Policy == nil || c.Bearer == "" {
		return ErrDisabled
	}
	if err := c.Policy(ctx, c.Resource); err != nil {
		return err
	}
	var reader io.Reader
	method := http.MethodGet
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Bearer)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("merchant transport unavailable")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("merchant response unavailable")
	}
	var envelope struct {
		Error string          `json:"error"`
		Order json.RawMessage `json:"order"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return fmt.Errorf("invalid merchant response")
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted && !(res.StatusCode == http.StatusBadGateway && envelope.Error == "paid_service_failed") {
		return fmt.Errorf("merchant request rejected (%d)", res.StatusCode)
	}
	if envelope.Error != "" {
		if len(envelope.Order) == 0 {
			return ErrUncertain
		}
		raw = envelope.Order
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("invalid merchant response")
	}
	return nil
}
func (c *MerchantClient) Quote(ctx context.Context, o Offer, request json.RawMessage) (Quote, error) {
	if o.Endpoint != c.Resource || o.Capability != merchant.Capability || !strings.EqualFold(o.Payee, c.Payee) || o.PriceMicros != c.PriceMicros {
		return Quote{}, fmt.Errorf("offer is not the configured controlled merchant")
	}
	var q Quote
	err := c.request(ctx, "/quote", merchant.QuoteRequest{Method: "POST", Resource: o.Endpoint, Request: request, Deadline: o.ValidUntil.UTC().Truncate(time.Second)}, &q)
	return q, err
}
func (c *MerchantClient) Accept(ctx context.Context, o Order) error {
	s, err := merchantSnapshot(o)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(o.Quote)
	if err != nil {
		return err
	}
	var q merchant.Quote
	if err = json.Unmarshal(raw, &q); err != nil {
		return err
	}
	if err = merchant.ValidateSnapshot(s, q, c.PublicKey); err != nil {
		return fmt.Errorf("merchant challenge signature rejected")
	}
	var record merchant.ResultView
	if err = c.request(ctx, "/accept", map[string]any{"snapshot": s, "quote": q}, &record); err != nil {
		return err
	}
	if record.IntentHash != o.IntentHash || record.OrderID != o.ID {
		return ErrConflict
	}
	return nil
}
func (c *MerchantClient) Lookup(ctx context.Context, o Order) (json.RawMessage, string, error) {
	var r merchant.ResultView
	err := c.request(ctx, "/results/"+url.PathEscape(o.ID)+"?intentHash="+url.QueryEscape(o.IntentHash), nil, &r)
	if err != nil {
		return nil, "unknown", err
	}
	if r.IntentHash != o.IntentHash || r.OrderID != o.ID {
		return nil, "unknown", ErrConflict
	}
	return r.Result, string(r.State), nil
}
func (c *MerchantClient) Fulfill(ctx context.Context, o Order) (json.RawMessage, string, error) {
	if o.PaymentEvent == nil {
		return nil, "unknown", ErrDenied
	}
	var r merchant.ResultView
	err := c.request(ctx, "/fulfill", map[string]any{"orderId": o.ID, "intentHash": o.IntentHash, "payment": o.PaymentEvent}, &r)
	if err != nil {
		return nil, "unknown", err
	}
	if r.IntentHash != o.IntentHash || r.OrderID != o.ID {
		return nil, "unknown", ErrConflict
	}
	return r.Result, string(r.State), nil
}

func (r *ArcRail) MerchantVerifier(ctx context.Context, s merchant.Snapshot, e merchant.PaymentEvent) (merchant.PaymentProof, error) {
	var intent Intent
	if err := json.Unmarshal(s.CanonicalIntent, &intent); err != nil {
		return merchant.PaymentProof{}, ErrDenied
	}
	o := Order{Intent: intent, IntentHash: s.IntentHash}
	event := PaymentEvent{ChainID: e.ChainID, TxHash: e.TxHash, LogIndex: e.LogIndex, BlockHash: e.BlockHash, BlockNumber: e.BlockNumber}
	if err := r.VerifyEvent(ctx, o, event); err != nil {
		return merchant.PaymentProof{}, merchant.ErrPaymentPending
	}
	var block *rpcBlock
	number, err := strconvBlock(e.BlockNumber)
	if err != nil {
		return merchant.PaymentProof{}, err
	}
	if err = r.call(ctx, "eth_getBlockByNumber", []any{number, false}, &block); err != nil || block == nil {
		return merchant.PaymentProof{}, merchant.ErrPaymentPending
	}
	if !strings.EqualFold(block.Hash, e.BlockHash) {
		return merchant.PaymentProof{}, merchant.ErrPaymentPending
	}
	timestamp, err := hexUint(block.Timestamp)
	if err != nil {
		return merchant.PaymentProof{}, err
	}
	return merchant.PaymentProof{Event: e, Payer: intent.Payer, Payee: intent.Payee, Asset: intent.Asset, AmountMicros: intent.AmountMicros, AuthorizationNonce: s.IntentHash, PaidAt: time.Unix(int64(timestamp), 0).UTC(), AuthorizationConfirmed: true, TransferConfirmed: true}, nil
}
func strconvBlock(decimal string) (string, error) {
	n, err := Amount(decimal)
	if err != nil {
		return "", err
	}
	return "0x" + n.Text(16), nil
}
