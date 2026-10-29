package merchant

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"time"
)

type Service struct {
	store    Store
	verify   VerifyPayment
	execute  Execute
	now      func() time.Time
	key      ed25519.PrivateKey
	public   ed25519.PublicKey
	resource string
	payee    string
	amount   string
	ttl      time.Duration
}

func New(c Config, store Store, verify VerifyPayment) (*Service, error) {
	if store == nil || nilValue(store) || verify == nil || !validResource(c.Resource) ||
		!validAddress(c.Payee) || !validAmount(c.AmountMicros) || c.Capability != Capability ||
		len(c.PrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: configuration", ErrInvalid)
	}
	key := bytes.Clone(c.PrivateKey)
	if !bytes.Equal(ed25519.NewKeyFromSeed(key[:ed25519.SeedSize]), key) {
		return nil, fmt.Errorf("%w: signing key", ErrInvalid)
	}
	if c.QuoteTTL == 0 {
		c.QuoteTTL = 5 * time.Minute
	}
	if c.QuoteTTL < 31*time.Second || c.QuoteTTL > 15*time.Minute || c.QuoteTTL%time.Second != 0 {
		return nil, fmt.Errorf("%w: quote lifetime", ErrInvalid)
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Execute == nil {
		c.Execute = TextDigest
	}
	return &Service{
		store: store, verify: verify, execute: c.Execute, now: c.Now,
		key: key, public: bytes.Clone(key[ed25519.SeedSize:]),
		resource: c.Resource, payee: c.Payee, amount: c.AmountMicros, ttl: c.QuoteTTL,
	}, nil
}

func nilValue(v any) bool {
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

func (s *Service) Quote(ctx context.Context, r QuoteRequest) (Quote, error) {
	if err := ctx.Err(); err != nil {
		return Quote{}, err
	}
	if r.Method != http.MethodPost || r.Resource != s.resource {
		return Quote{}, fmt.Errorf("%w: requested service", ErrInvalid)
	}
	if _, err := ParseRequest(r.Request); err != nil {
		return Quote{}, err
	}
	now := s.now().UTC()
	deadline := now.Add(s.ttl).Truncate(time.Second)
	if !r.Deadline.IsZero() {
		if !wholeUTC(r.Deadline) {
			return Quote{}, fmt.Errorf("%w: deadline encoding", ErrInvalid)
		}
		if r.Deadline.Before(deadline) {
			deadline = r.Deadline.UTC()
		}
	}
	if !deadline.After(now.Add(30 * time.Second)) {
		return Quote{}, ErrExpired
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Quote{}, fmt.Errorf("merchant challenge: %w", err)
	}
	q := Quote{
		ID: "0x" + hex.EncodeToString(nonce[:]), Method: http.MethodPost, Resource: s.resource,
		RequestDigest: Digest(r.Request), ChainID: ChainID, Asset: Asset, Payee: s.payee,
		AmountMicros: s.amount, Deadline: deadline,
	}
	message, err := quoteMessage(q)
	if err != nil {
		return Quote{}, err
	}
	q.Proof = "0x" + hex.EncodeToString(ed25519.Sign(s.key, message))
	return q, nil
}

func (s *Service) Accept(ctx context.Context, snapshot Snapshot, q Quote) (Record, error) {
	snapshot = cloneSnapshot(snapshot)
	if err := ValidateSnapshot(snapshot, q, s.public); err != nil {
		return Record{}, err
	}
	old, err := s.store.Get(ctx, snapshot.OrderID)
	if err == nil {
		if !sameSnapshot(old.Snapshot, snapshot) {
			return Record{}, ErrConflict
		}
		return cloneRecord(old), nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Record{}, fmt.Errorf("%w: lookup before acceptance", ErrUnavailable)
	}
	if !s.now().Before(snapshot.Deadline) {
		return Record{}, ErrExpired
	}
	if snapshot.Resource != s.resource || snapshot.Payee != s.payee || snapshot.AmountMicros != s.amount {
		return Record{}, fmt.Errorf("%w: configured service terms", ErrInvalid)
	}
	r, err := s.store.Accept(ctx, snapshot)
	if err != nil {
		return Record{}, storeError(err)
	}
	if !sameSnapshot(r.Snapshot, snapshot) || !validState(r.State) {
		return Record{}, fmt.Errorf("%w: acceptance result", ErrUnknown)
	}
	return cloneRecord(r), nil
}

func (s *Service) Result(ctx context.Context, orderID, intentHash string) (Record, error) {
	if !validID(orderID) || !validHex(intentHash, 32) {
		return Record{}, ErrInvalid
	}
	r, err := s.store.Get(ctx, orderID)
	if err != nil {
		return Record{}, storeError(err)
	}
	if r.Snapshot.OrderID != orderID || r.Snapshot.IntentHash != intentHash {
		return Record{}, ErrConflict
	}
	if !validState(r.State) || (r.State == Completed && (len(r.Result) == 0 || !json.Valid(r.Result))) {
		return Record{}, fmt.Errorf("%w: stored result", ErrUnknown)
	}
	if r.State != Completed {
		r.Result = nil
	}
	return cloneRecord(r), nil
}

func (s *Service) Fulfill(ctx context.Context, orderID, intentHash string, event PaymentEvent) (Record, error) {
	r, err := s.Result(ctx, orderID, intentHash)
	if err != nil {
		return Record{}, err
	}
	if r.State != Accepted {
		return r, nil
	}
	if err := validateEvent(event); err != nil {
		return r, err
	}
	proof, err := s.verify(ctx, cloneSnapshot(r.Snapshot), event)
	if err != nil {
		if errors.Is(err, ErrPaymentPending) {
			r.State = Pending
			return r, ErrPaymentPending
		}
		if errors.Is(err, ErrPayment) {
			return r, ErrPayment
		}
		return r, fmt.Errorf("%w: payment verification", ErrUnavailable)
	}
	if err := verifyProof(r.Snapshot, event, proof, s.now()); err != nil {
		return r, err
	}
	claimed, owns, err := s.store.Claim(ctx, orderID, intentHash, event)
	if err != nil {
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
			return Record{}, err
		}
		r.State = Unknown
		r.Payment = &event
		return r, fmt.Errorf("%w: claim outcome", ErrUnknown)
	}
	if !sameSnapshot(claimed.Snapshot, r.Snapshot) || !validState(claimed.State) {
		return Record{}, fmt.Errorf("%w: claim record", ErrUnknown)
	}
	if !owns {
		if claimed.State == Accepted || claimed.State == Pending {
			return Record{}, fmt.Errorf("%w: unclaimed state", ErrUnknown)
		}
		return cloneRecord(claimed), nil
	}
	if claimed.State != Started || claimed.Payment == nil || *claimed.Payment != event {
		return Record{}, fmt.Errorf("%w: claim invariant", ErrUnknown)
	}
	result, execErr := s.executeSafely(ctx, bytes.Clone(r.Snapshot.Request))
	outcome := Outcome{State: Completed, Result: result}
	if execErr != nil {
		outcome = Outcome{State: Failed, FailureCode: "service_failed"}
		if errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) || errors.Is(execErr, ErrUnknown) {
			outcome = Outcome{State: Unknown, FailureCode: "execution_unknown"}
		}
	} else if len(result) == 0 || len(result) > MaxRequestBytes || !json.Valid(result) {
		outcome = Outcome{State: Unknown, FailureCode: "invalid_result"}
	}
	// A lost HTTP response must not cancel persistence of an already computed result.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finished, err := s.store.Finish(finishCtx, orderID, intentHash, outcome)
	if err != nil {
		claimed.State = Unknown
		claimed.Result = nil
		return cloneRecord(claimed), fmt.Errorf("%w: result persistence", ErrUnknown)
	}
	if !sameSnapshot(finished.Snapshot, r.Snapshot) || finished.State != outcome.State ||
		!bytes.Equal(finished.Result, outcome.Result) || finished.FailureCode != outcome.FailureCode ||
		finished.Payment == nil || *finished.Payment != event {
		return Record{}, fmt.Errorf("%w: persisted outcome", ErrUnknown)
	}
	if outcome.State == Failed {
		return cloneRecord(finished), ErrService
	}
	if outcome.State == Unknown {
		return cloneRecord(finished), ErrUnknown
	}
	return cloneRecord(finished), nil
}

func (s *Service) executeSafely(ctx context.Context, raw json.RawMessage) (result json.RawMessage, err error) {
	defer func() {
		if recover() != nil {
			result, err = nil, ErrUnknown
		}
	}()
	return s.execute(ctx, raw)
}

func validateEvent(e PaymentEvent) error {
	log, lerr := decimal(e.LogIndex)
	block, berr := decimal(e.BlockNumber)
	if e.ChainID != ChainID || !validHex(e.TxHash, 32) || !validHex(e.BlockHash, 32) || lerr != nil || berr != nil || log < 0 || block <= 0 {
		return ErrPayment
	}
	return nil
}

func verifyProof(s Snapshot, e PaymentEvent, p PaymentProof, now time.Time) error {
	i, err := DecodeIntent(s.CanonicalIntent)
	if err != nil || Digest(s.CanonicalIntent) != s.IntentHash || Digest(s.Request) != s.RequestDigest {
		return ErrPayment
	}
	after, _ := decimal(i.ValidAfter)
	if p.Event != e || !p.AuthorizationConfirmed || !p.TransferConfirmed ||
		p.Payer != s.Payer || p.Payee != s.Payee || p.Asset != s.Asset ||
		p.AmountMicros != s.AmountMicros || p.AuthorizationNonce != s.IntentHash ||
		!wholeUTC(p.PaidAt) || p.PaidAt.After(now) || !p.PaidAt.After(time.Unix(after, 0)) || !p.PaidAt.Before(s.Deadline) {
		return ErrPayment
	}
	return nil
}

func validState(state State) bool {
	switch state {
	case Accepted, Started, Completed, Failed, Unknown:
		return true
	}
	return false
}

func storeError(err error) error {
	for _, known := range []error{ErrNotFound, ErrConflict, ErrExpired} {
		if errors.Is(err, known) {
			return known
		}
	}
	return fmt.Errorf("%w: durable store", ErrUnavailable)
}

func cloneSnapshot(s Snapshot) Snapshot {
	s.CanonicalIntent = bytes.Clone(s.CanonicalIntent)
	s.Request = bytes.Clone(s.Request)
	return s
}

func cloneRecord(r Record) Record {
	r.Snapshot = cloneSnapshot(r.Snapshot)
	r.Result = bytes.Clone(r.Result)
	if r.Payment != nil {
		p := *r.Payment
		r.Payment = &p
	}
	return r
}

func sameSnapshot(a, b Snapshot) bool {
	return a.OrderID == b.OrderID && a.IntentHash == b.IntentHash && bytes.Equal(a.CanonicalIntent, b.CanonicalIntent) &&
		a.Payer == b.Payer && a.Payee == b.Payee && a.ChainID == b.ChainID && a.Asset == b.Asset &&
		a.AmountMicros == b.AmountMicros && a.Method == b.Method && a.Resource == b.Resource &&
		a.RequestDigest == b.RequestDigest && a.QuoteDigest == b.QuoteDigest && a.Deadline.Equal(b.Deadline) && bytes.Equal(a.Request, b.Request)
}
