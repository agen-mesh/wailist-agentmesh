package purchase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	Store    Store
	Catalog  Catalog
	Merchant Merchant
	Identity Identity
	Rail     Rail
	Domain   string
	Enabled  bool
	Seal     func(string) (string, error)
	Open     func(string) (string, error)
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) ready() error {
	if s == nil || !s.Enabled || s.Store == nil || s.Catalog == nil || s.Merchant == nil || s.Identity == nil || s.Rail == nil || s.Seal == nil || s.Open == nil || s.Domain == "" {
		return ErrDisabled
	}
	return nil
}

type CreateRequest struct {
	OfferKey        string          `json:"offerKey"`
	Request         json.RawMessage `json:"request"`
	MaxMicros       string          `json:"maxMicros"`
	ClientRequestID string          `json:"clientRequestId"`
}

type Invocation struct{ WorkflowID, RunID, NodeID, ID string }

func (s *Service) Create(ctx context.Context, account string, req CreateRequest, invocation Invocation) (Order, error) {
	if err := s.ready(); err != nil {
		return Order{}, err
	}
	if account == "" || req.ClientRequestID == "" || len(req.ClientRequestID) > 128 || len(req.OfferKey) > 128 {
		return Order{}, ErrDenied
	}
	body, err := CanonicalRequest(req.Request)
	if err != nil {
		return Order{}, err
	}
	old, err := s.Store.ByRequest(ctx, account, req.ClientRequestID)
	if err == nil {
		if old.Offer.Key != req.OfferKey || old.Intent.MaxMicros != req.MaxMicros || old.Intent.RequestDigest != Digest(body) || old.Intent.RunID != invocation.RunID || old.Intent.NodeID != invocation.NodeID {
			return Order{}, ErrConflict
		}
		return s.accept(ctx, old)
	}
	if err != ErrNotFound {
		return Order{}, err
	}
	w, err := s.Store.Wallet(ctx, account)
	if err != nil {
		return Order{}, err
	}
	if w == nil {
		return Order{}, fmt.Errorf("link a wallet before creating an order")
	}
	offer, err := s.Catalog.Get(ctx, req.OfferKey)
	if err != nil {
		return Order{}, fmt.Errorf("offer: %w", err)
	}
	quote, err := s.Merchant.Quote(ctx, offer, body)
	if err != nil {
		return Order{}, fmt.Errorf("merchant challenge: %w", err)
	}
	o := Order{ID: uuid.NewString(), AccountID: account, ClientRequestID: req.ClientRequestID, Offer: offer, Quote: quote, Request: body, State: AwaitingIdentity, CreatedAt: s.now(), Intent: Intent{WorkflowID: invocation.WorkflowID, RunID: invocation.RunID, NodeID: invocation.NodeID, Invocation: invocation.ID}}
	o.Intent, o.IntentHash, err = NewIntent(o, w.Address, req.MaxMicros, s.now())
	if err != nil {
		return Order{}, err
	}
	stored, _, err := s.Store.Create(ctx, o)
	if err != nil {
		return Order{}, err
	}
	if stored.Intent.RequestDigest != o.Intent.RequestDigest || stored.Offer.Key != o.Offer.Key || stored.Intent.MaxMicros != o.Intent.MaxMicros {
		return Order{}, ErrConflict
	}
	return s.accept(ctx, stored)
}

func (s *Service) accept(ctx context.Context, o Order) (Order, error) {
	if o.MerchantAccepted {
		return o, nil
	}
	if err := s.Merchant.Accept(ctx, o); err != nil {
		return o, fmt.Errorf("merchant snapshot acceptance: %w", err)
	}
	return s.Store.Update(ctx, o.AccountID, o.ID, func(current *Order, _ Ledger) error {
		if current.IntentHash != o.IntentHash {
			return ErrConflict
		}
		current.MerchantAccepted = true
		return nil
	})
}

func (s *Service) ChallengeWallet(ctx context.Context, account, address string) (WalletChallenge, error) {
	if err := s.ready(); err != nil {
		return WalletChallenge{}, err
	}
	if account == "" {
		return WalletChallenge{}, ErrDenied
	}
	a, err := Address(address)
	if err != nil {
		return WalletChallenge{}, err
	}
	if err = s.Rail.IsEOA(ctx, a); err != nil {
		return WalletChallenge{}, err
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return WalletChallenge{}, err
	}
	c := WalletChallenge{ID: uuid.NewString(), AccountID: account, Address: a, ExpiresAt: s.now().Add(5 * time.Minute)}
	c.Message = fmt.Sprintf("AgentMesh wallet link\nDomain: %s\nAccount: %s\nAddress: %s\nChain: %d\nNonce: %s\nExpires: %s\nThis links a wallet and does not authorize a payment.", s.Domain, account, a, ChainID, hex.EncodeToString(nonce), c.ExpiresAt.Format(time.RFC3339))
	if err = s.Store.SaveChallenge(ctx, c); err != nil {
		return WalletChallenge{}, err
	}
	return c, nil
}

func (s *Service) LinkWallet(ctx context.Context, account, id, signature string) (Wallet, error) {
	if err := s.ready(); err != nil {
		return Wallet{}, err
	}
	return s.Store.LinkWallet(ctx, account, id, func(c WalletChallenge) error { return VerifyWallet(c, signature, s.now()) })
}

func (s *Service) BeginIdentity(ctx context.Context, account, id string) (IdentityStart, error) {
	if err := s.ready(); err != nil {
		return IdentityStart{}, err
	}
	o, err := s.Store.Get(ctx, account, id)
	if err != nil {
		return IdentityStart{}, err
	}
	if !o.MerchantAccepted || (o.State != AwaitingIdentity && o.State != AwaitingApproval) || !s.now().Before(Deadline(o)) {
		return IdentityStart{}, ErrConflict
	}
	return s.Identity.Begin(ctx, o)
}

func (s *Service) Decide(ctx context.Context, account, id, intent, decision string) (Order, error) {
	if err := s.ready(); err != nil {
		return Order{}, err
	}
	if decision == "approve" {
		o, err := s.Store.Get(ctx, account, id)
		if err != nil {
			return Order{}, err
		}
		if err = s.Catalog.Revalidate(ctx, o.Offer); err != nil {
			return Order{}, fmt.Errorf("%w: %w", ErrConflict, err)
		}
	}
	return s.Store.Update(ctx, account, id, func(o *Order, _ Ledger) error {
		if o.IntentHash != intent || o.SignatureReleasedAt != nil {
			return ErrConflict
		}
		if decision == "deny" && (o.State == AwaitingIdentity || o.State == AwaitingApproval || o.State == Approved) {
			o.State = Denied
			return nil
		}
		if decision != "approve" || o.State != AwaitingApproval || o.IdentityVerifiedAt == nil || o.IdentityValidUntil == nil || !s.now().Before(*o.IdentityValidUntil) || s.now().Sub(*o.IdentityVerifiedAt) > 5*time.Minute || !s.now().Before(Deadline(*o)) {
			return ErrDenied
		}
		now := s.now()
		o.State = Approved
		o.ApprovedAt = &now
		return nil
	})
}

func (s *Service) ReleaseAuthorization(ctx context.Context, account, id string) (Order, error) {
	if err := s.ready(); err != nil {
		return Order{}, err
	}
	o, err := s.Store.Get(ctx, account, id)
	if err != nil {
		return Order{}, err
	}
	if o.State == AuthorizationReleased {
		return o, nil
	}
	if o.State != Approved {
		return Order{}, ErrConflict
	}
	if err = s.Catalog.Revalidate(ctx, o.Offer); err != nil {
		return Order{}, fmt.Errorf("offer changed: %w", err)
	}
	if err = s.Rail.Check(ctx); err != nil {
		return Order{}, fmt.Errorf("payment domain: %w", err)
	}
	anchor, err := s.Rail.Anchor(ctx)
	if err != nil {
		return Order{}, err
	}
	if err = s.Rail.IsEOA(ctx, o.Intent.Payer); err != nil {
		return Order{}, err
	}
	return s.Store.Update(ctx, account, id, func(current *Order, l Ledger) error {
		if current.State == AuthorizationReleased {
			return nil
		}
		if current.State != Approved || current.IntentHash != o.IntentHash || !current.MerchantAccepted || current.IdentityVerifiedAt == nil || current.IdentityValidUntil == nil || !s.now().Before(*current.IdentityValidUntil) || s.now().Sub(*current.IdentityVerifiedAt) > 5*time.Minute || !s.now().Before(Deadline(*current).Add(-10*time.Second)) {
			return ErrConflict
		}
		if err := l.Reserve(current.Intent.AmountMicros); err != nil {
			return err
		}
		now := s.now()
		current.SignatureReleasedAt = &now
		current.AuthorizationBlock = anchor
		current.State = AuthorizationReleased
		return nil
	})
}

func (s *Service) Submit(ctx context.Context, account, id, signature string) (Order, error) {
	if err := s.ready(); err != nil {
		return Order{}, err
	}
	o, err := s.Store.Get(ctx, account, id)
	if err != nil {
		return Order{}, err
	}
	if o.State != AuthorizationReleased || o.SignatureReleasedAt == nil || o.SignedEnvelope != "" {
		return o, ErrConflict
	}
	if !s.now().Before(Deadline(o)) || o.IdentityValidUntil == nil || !s.now().Before(*o.IdentityValidUntil) {
		return o, ErrUncertain
	}
	if _, err = Authorization(o, signature); err != nil {
		return o, err
	}
	p, err := s.Rail.Prepare(ctx, o, signature)
	if err != nil {
		return o, fmt.Errorf("prepare relay: %w", err)
	}
	var raw string
	o, err = s.Store.Update(ctx, account, id, func(current *Order, l Ledger) error {
		if current.State != AuthorizationReleased || current.SignedEnvelope != "" || current.IntentHash != o.IntentHash || !s.now().Before(Deadline(*current)) || current.IdentityValidUntil == nil || !s.now().Before(*current.IdentityValidUntil) {
			return ErrConflict
		}
		nonce, err := l.RelayNonce(strings.ToLower(p.Relayer), p.ObservedNonce)
		if err != nil {
			return err
		}
		raw, current.TxHash, err = p.Sign(nonce)
		if err != nil {
			return err
		}
		envelope, err := json.Marshal(map[string]string{"raw": raw, "signature": signature})
		if err != nil {
			return err
		}
		current.SignedEnvelope, err = s.Seal(string(envelope))
		if err != nil {
			return err
		}
		current.State = Submitting
		return nil
	})
	if err != nil {
		return o, err
	}
	// Recovery can dispatch only this committed envelope, never a replacement.
	broadcastErr := s.Rail.Broadcast(ctx, raw, o.TxHash)
	updated, err := s.Store.Update(ctx, account, id, func(current *Order, _ Ledger) error {
		if current.State == Submitting {
			current.State = PaymentUnknown
			current.Reason = "Payment submitted; confirmed receipt reconciliation is required"
		}
		return nil
	})
	if err != nil {
		return o, fmt.Errorf("record broadcast uncertainty: %w", err)
	}
	if broadcastErr != nil {
		return updated, ErrUncertain
	}
	return s.Reconcile(ctx, account, id)
}

func (s *Service) Reconcile(ctx context.Context, account, id string) (Order, error) {
	if err := s.ready(); err != nil {
		return Order{}, err
	}
	o, err := s.Store.Get(ctx, account, id)
	if err != nil {
		return Order{}, err
	}
	o, err = s.recoverRelay(ctx, o)
	if err != nil && !errors.Is(err, ErrUncertain) {
		return o, err
	}
	if o.State == Completed || o.State == Expired || o.State == PaidToolFailed {
		return o, nil
	}
	if o.SignatureReleasedAt == nil {
		return o, ErrConflict
	}
	if o.PaymentEvent == nil {
		settlement, err := s.Rail.Reconcile(ctx, o)
		if err != nil {
			return o, fmt.Errorf("reconcile payment: %w", err)
		}
		if settlement.Event == nil && !settlement.ExpiredUnused {
			return o, ErrUncertain
		}
		o, err = s.Store.Update(ctx, account, id, func(current *Order, l Ledger) error {
			if current.PaymentEvent != nil {
				return nil
			}
			if current.State == Expired {
				return nil
			}
			if settlement.Event != nil {
				if err := l.BindEvent(*settlement.Event); err != nil {
					return err
				}
				if err := l.Consume(current.Intent.AmountMicros); err != nil {
					return err
				}
				current.PaymentEvent = settlement.Event
				current.State = Paid
				current.Reason = ""
				return nil
			}
			if err := l.Release(current.Intent.AmountMicros); err != nil {
				return err
			}
			current.State = Expired
			current.Reason = "Authorization expired unused at a confirmed chain block"
			return nil
		})
		if err != nil {
			return o, err
		}
		if o.State == Expired {
			return o, nil
		}
	}
	return s.fulfill(ctx, o)
}

func (s *Service) recoverRelay(ctx context.Context, o Order) (Order, error) {
	if o.SignedEnvelope == "" || o.RelayConfirmed {
		return o, nil
	}
	if o.SignatureReleasedAt == nil || o.TxHash == "" {
		return o, ErrDenied
	}
	plain, err := s.Open(o.SignedEnvelope)
	if err != nil {
		return o, ErrUncertain
	}
	var envelope struct {
		Raw       string `json:"raw"`
		Signature string `json:"signature"`
	}
	if json.Unmarshal([]byte(plain), &envelope) != nil {
		return o, ErrDenied
	}
	confirmed, err := s.Rail.Recover(ctx, o, envelope.Raw, envelope.Signature)
	if err != nil {
		return o, fmt.Errorf("recover committed relay: %w", err)
	}
	if !confirmed {
		return o, nil
	}
	return s.Store.Update(ctx, o.AccountID, o.ID, func(current *Order, _ Ledger) error {
		if current.TxHash != o.TxHash || current.SignedEnvelope != o.SignedEnvelope {
			return ErrConflict
		}
		current.RelayConfirmed = true
		return nil
	})
}

func (s *Service) fulfill(ctx context.Context, o Order) (Order, error) {
	result, status, err := s.Merchant.Lookup(ctx, o)
	if err != nil {
		return o, fmt.Errorf("merchant result lookup: %w", err)
	}
	if status == "accepted" {
		o, err = s.Store.Update(ctx, o.AccountID, o.ID, func(current *Order, _ Ledger) error {
			if current.PaymentEvent == nil {
				return ErrConflict
			}
			if current.State == Paid {
				current.State = Fulfilling
			}
			return nil
		})
		if err != nil {
			return o, err
		}
		result, status, err = s.Merchant.Fulfill(ctx, o)
	}
	if err != nil {
		status = "unknown"
	}
	return s.Store.Update(ctx, o.AccountID, o.ID, func(current *Order, _ Ledger) error {
		if current.State == Completed {
			return nil
		}
		switch status {
		case "completed":
			if !json.Valid(result) {
				return ErrUncertain
			}
			current.Result = result
			current.State = Completed
			current.Reason = ""
		case "failed":
			current.State = PaidToolFailed
			current.Reason = "Payment confirmed; merchant execution failed without a refund"
		default:
			current.State = FulfillmentUnknown
			current.Reason = "Payment confirmed; merchant result requires reconciliation"
		}
		return nil
	})
}

func (s *Service) Cancel(ctx context.Context, account, id string) (Order, error) {
	if err := s.ready(); err != nil {
		return Order{}, err
	}
	return s.Store.Update(ctx, account, id, func(o *Order, _ Ledger) error {
		if o.SignatureReleasedAt != nil {
			return ErrUncertain
		}
		if o.State != AwaitingIdentity && o.State != AwaitingApproval && o.State != Approved {
			return ErrConflict
		}
		o.State = Cancelled
		return nil
	})
}
