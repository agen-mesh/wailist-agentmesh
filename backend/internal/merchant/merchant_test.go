package merchant

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This synchronized double models the required adapter contract, not a durable database.
type testStore struct {
	mu          sync.Mutex
	rows        map[string]Record
	events      map[string]string
	now         func() time.Time
	finishFault string
	claimFault  bool
}

func copyRow(r Record) Record {
	b, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	var out Record
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

func (m *testStore) Get(ctx context.Context, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return copyRow(r), nil
}

func (m *testStore) Accept(ctx context.Context, s Snapshot) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[s.OrderID]; ok {
		if r.Snapshot.IntentHash != s.IntentHash {
			return Record{}, ErrConflict
		}
		return copyRow(r), nil
	}
	if !m.now().Before(s.Deadline) {
		return Record{}, ErrExpired
	}
	r := copyRow(Record{Snapshot: s, State: Accepted, AcceptedAt: m.now()})
	m.rows[s.OrderID] = r
	return copyRow(r), nil
}

func (m *testStore) Claim(ctx context.Context, id, intent string, e PaymentEvent) (Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return Record{}, false, ErrNotFound
	}
	if r.Snapshot.IntentHash != intent {
		return Record{}, false, ErrConflict
	}
	if r.State != Accepted {
		return copyRow(r), false, nil
	}
	key := fmt.Sprintf("%d/%s/%s", e.ChainID, e.TxHash, e.LogIndex)
	if existing, ok := m.events[key]; ok && existing != id {
		return Record{}, false, ErrConflict
	}
	m.events[key] = id
	r.State, r.Payment = Started, &e
	m.rows[id] = copyRow(r)
	if m.claimFault {
		return Record{}, false, errors.New("simulated claim response loss")
	}
	return copyRow(r), true, nil
}

func (m *testStore) Finish(ctx context.Context, id, intent string, o Outcome) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	if r.Snapshot.IntentHash != intent || r.State != Started {
		return Record{}, ErrConflict
	}
	if m.finishFault == "before" {
		return Record{}, errors.New("simulated result write failure")
	}
	r.State, r.Result, r.FailureCode = o.State, bytes.Clone(o.Result), o.FailureCode
	m.rows[id] = copyRow(r)
	if m.finishFault == "after" {
		return Record{}, errors.New("simulated result response loss")
	}
	return copyRow(r), nil
}

type fixture struct {
	svc    *Service
	store  *testStore
	cfg    Config
	now    time.Time
	paidAt time.Time
	quote  Quote
	snap   Snapshot
	event  PaymentEvent
	calls  atomic.Int32
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	f.paidAt = f.now
	f.cfg = Config{
		Resource: "https://merchant.example/merchant/text", Payee: "0x" + strings.Repeat("2", 40), AmountMicros: "1000", Capability: Capability,
		PrivateKey: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{17}, ed25519.SeedSize)),
		Now:        func() time.Time { return f.now },
		Execute: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			f.calls.Add(1)
			return TextDigest(ctx, raw)
		},
	}
	f.store = &testStore{rows: map[string]Record{}, events: map[string]string{}, now: f.cfg.Now}
	var err error
	f.svc, err = New(f.cfg, f.store, f.verify)
	if err != nil {
		t.Fatal(err)
	}
	f.quote, err = f.svc.Quote(context.Background(), QuoteRequest{Method: http.MethodPost, Resource: f.cfg.Resource, Request: json.RawMessage(`{"text":"private text\nwith five words"}`)})
	if err != nil {
		t.Fatal(err)
	}
	f.snap = makeSnapshot(t, f.quote, "order-1", json.RawMessage(`{"text":"private text\nwith five words"}`), f.now)
	f.event = PaymentEvent{ChainID: ChainID, TxHash: "0x" + strings.Repeat("a", 64), LogIndex: "2", BlockHash: "0x" + strings.Repeat("b", 64), BlockNumber: "100"}
	return f
}

func makeSnapshot(t *testing.T, q Quote, id string, raw json.RawMessage, now time.Time) Snapshot {
	t.Helper()
	qb, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	i := Intent{Version: Protocol, OrderID: id, AccountID: "account-1", OfferKey: "offer-1", OfferOwner: q.Payee, OfferVersion: "1", OfferContentHash: Digest([]byte("offer")), OfferSnapshotHash: Digest([]byte("snapshot")), Payer: "0x" + strings.Repeat("1", 40), Payee: q.Payee, ChainID: q.ChainID, Asset: q.Asset, AmountMicros: q.AmountMicros, MaxMicros: "2000", Method: q.Method, Resource: q.Resource, RequestDigest: q.RequestDigest, QuoteDigest: Digest(qb), OrderSalt: "0x" + strings.Repeat("c", 64), ValidAfter: strconv.FormatInt(now.Add(-30*time.Second).Unix(), 10), ValidBefore: strconv.FormatInt(q.Deadline.Unix(), 10)}
	b, err := json.Marshal(i)
	if err != nil {
		t.Fatal(err)
	}
	return Snapshot{OrderID: id, IntentHash: Digest(b), CanonicalIntent: b, Payer: i.Payer, Payee: i.Payee, ChainID: i.ChainID, Asset: i.Asset, AmountMicros: i.AmountMicros, Method: i.Method, Resource: i.Resource, RequestDigest: i.RequestDigest, QuoteDigest: i.QuoteDigest, Deadline: q.Deadline, Request: bytes.Clone(raw)}
}

func (f *fixture) verify(_ context.Context, s Snapshot, e PaymentEvent) (PaymentProof, error) {
	return PaymentProof{Event: e, Payer: s.Payer, Payee: s.Payee, Asset: s.Asset, AmountMicros: s.AmountMicros, AuthorizationNonce: s.IntentHash, PaidAt: f.paidAt, AuthorizationConfirmed: true, TransferConfirmed: true}, nil
}

func (f *fixture) accept(t *testing.T) {
	t.Helper()
	r, err := f.svc.Accept(context.Background(), f.snap, f.quote)
	if err != nil || r.State != Accepted {
		t.Fatalf("accept state %s, error %v", r.State, err)
	}
}

func TestProtocolQuoteMutation(t *testing.T) {
	f := newFixture(t)
	public := f.cfg.PrivateKey.Public().(ed25519.PublicKey)
	if err := VerifyQuote(f.quote, public); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Quote){
		"nonce":     func(q *Quote) { q.ID = "0x" + strings.Repeat("f", 64) },
		"method":    func(q *Quote) { q.Method = "GET" },
		"resource":  func(q *Quote) { q.Resource = "https://other.example/text" },
		"request":   func(q *Quote) { q.RequestDigest = Digest([]byte("other")) },
		"chain":     func(q *Quote) { q.ChainID++ },
		"token":     func(q *Quote) { q.Asset = "0x" + strings.Repeat("3", 40) },
		"payee":     func(q *Quote) { q.Payee = "0x" + strings.Repeat("3", 40) },
		"price":     func(q *Quote) { q.AmountMicros = "1001" },
		"deadline":  func(q *Quote) { q.Deadline = q.Deadline.Add(time.Second) },
		"signature": func(q *Quote) { q.Proof = "0x" + strings.Repeat("0", 128) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			q := f.quote
			mutate(&q)
			if !errors.Is(VerifyQuote(q, public), ErrInvalid) {
				t.Fatal("mutation accepted")
			}
		})
	}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{18}, 32)).Public().(ed25519.PublicKey)
	if !errors.Is(VerifyQuote(f.quote, other), ErrInvalid) {
		t.Fatal("wrong signer accepted")
	}
	q2, err := f.svc.Quote(context.Background(), QuoteRequest{Method: "POST", Resource: f.cfg.Resource, Request: f.snap.Request})
	if err != nil || q2.ID == f.quote.ID {
		t.Fatal("challenge nonce reused", err)
	}
}

func TestProtocolCanonicalAndExactSnapshot(t *testing.T) {
	f := newFixture(t)
	pub := f.cfg.PrivateKey.Public().(ed25519.PublicKey)
	if err := ValidateSnapshot(f.snap, f.quote, pub); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Snapshot){
		"hash":           func(s *Snapshot) { s.IntentHash = Digest([]byte("other")) },
		"raw_request":    func(s *Snapshot) { s.Request = json.RawMessage(`{"text":"changed private content"}`) },
		"order":          func(s *Snapshot) { s.OrderID = "other" },
		"payer":          func(s *Snapshot) { s.Payer = s.Payee },
		"payee":          func(s *Snapshot) { s.Payee = s.Payer },
		"chain":          func(s *Snapshot) { s.ChainID++ },
		"asset":          func(s *Snapshot) { s.Asset = s.Payer },
		"amount":         func(s *Snapshot) { s.AmountMicros = "1001" },
		"method":         func(s *Snapshot) { s.Method = "GET" },
		"resource":       func(s *Snapshot) { s.Resource += "/other" },
		"request_digest": func(s *Snapshot) { s.RequestDigest = Digest(nil) },
		"quote_digest":   func(s *Snapshot) { s.QuoteDigest = Digest(nil) },
		"deadline":       func(s *Snapshot) { s.Deadline = s.Deadline.Add(time.Second) },
		"canonical_space": func(s *Snapshot) {
			s.CanonicalIntent = append([]byte(" "), s.CanonicalIntent...)
			s.IntentHash = Digest(s.CanonicalIntent)
		},
		"duplicate_key": func(s *Snapshot) {
			s.CanonicalIntent = bytes.Replace(s.CanonicalIntent, []byte(`"orderId":"order-1"`), []byte(`"orderId":"other","orderId":"order-1"`), 1)
			s.IntentHash = Digest(s.CanonicalIntent)
		},
		"case_alias": func(s *Snapshot) {
			s.CanonicalIntent = bytes.Replace(s.CanonicalIntent, []byte(`"orderId"`), []byte(`"OrderId"`), 1)
			s.IntentHash = Digest(s.CanonicalIntent)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := copyRow(Record{Snapshot: f.snap}).Snapshot
			mutate(&s)
			if !errors.Is(ValidateSnapshot(s, f.quote, pub), ErrInvalid) {
				t.Fatal("mismatch accepted")
			}
		})
	}
	i, err := DecodeIntent(f.snap.CanonicalIntent)
	if err != nil {
		t.Fatal(err)
	}
	i.MaxMicros = "999"
	b, _ := json.Marshal(i)
	if _, err := DecodeIntent(b); !errors.Is(err, ErrInvalid) {
		t.Fatal("over maximum accepted")
	}
	if Digest([]byte("abc")) != "0xba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("SHA256 vector mismatch")
	}
}

func TestProtocolPrivateBounds(t *testing.T) {
	bad := map[string][]byte{
		"unknown_private": []byte(`{"text":"hello","api_key":"do-not-echo"}`), "duplicate": []byte(`{"text":"a","text":"b"}`),
		"case": []byte(`{"Text":"a"}`), "null": []byte(`{"text":null}`), "empty": []byte(`{"text":"  "}`), "number": []byte(`{"text":12}`),
		"array": []byte(`["hello"]`), "trailing": []byte(`{"text":"a"} {}`), "utf8": []byte{'{', '"', 't', 'e', 'x', 't', '"', ':', '"', 0xff, '"', '}'},
		"surrogate_high": []byte(`{"text":"\ud800"}`), "surrogate_low": []byte(`{"text":"\udc00"}`), "control": []byte(`{"text":"a\u0000"}`),
		"large_text": []byte(`{"text":"` + strings.Repeat("x", MaxTextBytes+1) + `"}`), "large_body": []byte(strings.Repeat(" ", MaxRequestBytes) + `{}`),
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRequest(raw); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid request accepted")
			}
		})
	}
	for _, raw := range []string{`{"text":"hello"}`, `{"text":"\ud83d\ude00"}`, `{"text":"line\nline\tlast"}`, `{"text":"literal \\ud800"}`, `{"text":"हैलो world"}`} {
		if _, err := ParseRequest([]byte(raw)); err != nil {
			t.Fatalf("valid control rejected: %v", err)
		}
	}
	f := newFixture(t)
	b, _ := json.Marshal(f.quote)
	if bytes.Contains(b, []byte("private text")) || bytes.Contains(b, []byte("account-1")) {
		t.Fatal("quote leaks private input")
	}
	raw, err := TextDigest(context.Background(), json.RawMessage(`{"text":"one two\nthree"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result TextResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Words != 3 || result.Lines != 2 || result.Bytes != 13 || result.CodePoints != 13 || result.SHA256 != Digest([]byte("one two\nthree")) {
		t.Fatalf("actual computation incorrect: %+v", result)
	}
}

func TestLifecycleIdempotencyAndExpiry(t *testing.T) {
	f := newFixture(t)
	f.accept(t)
	if _, err := f.svc.Accept(context.Background(), f.snap, f.quote); err != nil {
		t.Fatal(err)
	}
	i, _ := DecodeIntent(f.snap.CanonicalIntent)
	i.OrderSalt = "0x" + strings.Repeat("d", 64)
	b, _ := json.Marshal(i)
	other := f.snap
	other.CanonicalIntent = b
	other.IntentHash = Digest(b)
	if _, err := f.svc.Accept(context.Background(), other, f.quote); !errors.Is(err, ErrConflict) {
		t.Fatal("different intent accepted", err)
	}
	f.now = f.now.Add(10 * time.Minute)
	if _, err := f.svc.Accept(context.Background(), f.snap, f.quote); err != nil {
		t.Fatal("existing acceptance expired", err)
	}
	late := makeSnapshot(t, f.quote, "order-new", f.snap.Request, f.paidAt)
	if _, err := f.svc.Accept(context.Background(), late, f.quote); !errors.Is(err, ErrExpired) {
		t.Fatal("fresh expired acceptance allowed", err)
	}
	// A changed listing/config is irrelevant to the previously accepted payment terms.
	cfg := f.cfg
	cfg.AmountMicros = "2000"
	restarted, err := New(cfg, f.store, f.verify)
	if err != nil {
		t.Fatal(err)
	}
	r, err := restarted.Fulfill(context.Background(), f.snap.OrderID, f.snap.IntentHash, f.event)
	if err != nil || r.State != Completed || f.calls.Load() != 1 {
		t.Fatal("valid earlier payment rejected", r.State, err)
	}
	r2, err := restarted.Fulfill(context.Background(), f.snap.OrderID, f.snap.IntentHash, PaymentEvent{})
	if err != nil || !bytes.Equal(r.Result, r2.Result) || f.calls.Load() != 1 {
		t.Fatal("cached result not reused", err)
	}
	if _, err := restarted.Result(context.Background(), f.snap.OrderID, other.IntentHash); !errors.Is(err, ErrConflict) {
		t.Fatal("lookup intent not bound")
	}
	if _, err := restarted.Result(context.Background(), "missing", f.snap.IntentHash); !errors.Is(err, ErrNotFound) {
		t.Fatal("unaccepted lookup succeeded")
	}
}

func TestLifecyclePaymentProof(t *testing.T) {
	mutations := map[string]func(*PaymentProof){
		"authorization_missing": func(p *PaymentProof) { p.AuthorizationConfirmed = false }, "transfer_missing": func(p *PaymentProof) { p.TransferConfirmed = false },
		"nonce": func(p *PaymentProof) { p.AuthorizationNonce = Digest(nil) }, "payer": func(p *PaymentProof) { p.Payer = p.Payee }, "payee": func(p *PaymentProof) { p.Payee = p.Payer },
		"token": func(p *PaymentProof) { p.Asset = p.Payer }, "amount": func(p *PaymentProof) { p.AmountMicros = "999" },
		"chain": func(p *PaymentProof) { p.Event.ChainID++ }, "transaction": func(p *PaymentProof) { p.Event.TxHash = Digest(nil) }, "log": func(p *PaymentProof) { p.Event.LogIndex = "3" }, "block": func(p *PaymentProof) { p.Event.BlockHash = Digest(nil) },
		"after_deadline": func(p *PaymentProof) { p.PaidAt = p.PaidAt.Add(6 * time.Minute) }, "at_valid_after": func(p *PaymentProof) { p.PaidAt = p.PaidAt.Add(-30 * time.Second) },
		"future": func(p *PaymentProof) { p.PaidAt = p.PaidAt.Add(time.Second) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.accept(t)
			f.svc.verify = func(ctx context.Context, s Snapshot, e PaymentEvent) (PaymentProof, error) {
				p, _ := f.verify(ctx, s, e)
				mutate(&p)
				return p, nil
			}
			if _, err := f.svc.Fulfill(context.Background(), f.snap.OrderID, f.snap.IntentHash, f.event); !errors.Is(err, ErrPayment) {
				t.Fatal("bad proof accepted", err)
			}
			r, _ := f.store.Get(context.Background(), f.snap.OrderID)
			if r.State != Accepted || f.calls.Load() != 0 {
				t.Fatal("bad proof claimed service")
			}
		})
	}
	t.Run("pending", func(t *testing.T) {
		f := newFixture(t)
		f.accept(t)
		f.svc.verify = func(context.Context, Snapshot, PaymentEvent) (PaymentProof, error) {
			return PaymentProof{}, ErrPaymentPending
		}
		r, err := f.svc.Fulfill(context.Background(), f.snap.OrderID, f.snap.IntentHash, f.event)
		if r.State != Pending || !errors.Is(err, ErrPaymentPending) || f.calls.Load() != 0 {
			t.Fatal("pending not preserved")
		}
	})
	t.Run("unique_event", func(t *testing.T) {
		f := newFixture(t)
		f.accept(t)
		second := makeSnapshot(t, f.quote, "order-2", f.snap.Request, f.now)
		if _, err := f.svc.Accept(context.Background(), second, f.quote); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Fulfill(context.Background(), f.snap.OrderID, f.snap.IntentHash, f.event); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Fulfill(context.Background(), second.OrderID, second.IntentHash, f.event); !errors.Is(err, ErrConflict) {
			t.Fatal("same payment funded two orders", err)
		}
		if f.calls.Load() != 1 {
			t.Fatal("executed twice")
		}
	})
}

func TestLifecycleConcurrentFulfillment(t *testing.T) {
	f := newFixture(t)
	f.accept(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.svc.execute = func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		if f.calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return TextDigest(ctx, raw)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.svc.Fulfill(context.Background(), f.snap.OrderID, f.snap.IntentHash, f.event)
			if err != nil {
				errs <- err
			} else if r.State != Started && r.State != Completed {
				errs <- fmt.Errorf("state %s", r.State)
			}
		}()
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no executor")
	}
	r, err := f.svc.Result(context.Background(), f.snap.OrderID, f.snap.IntentHash)
	if err != nil || r.State != Started || !View(r).ReconciliationRequired {
		t.Fatal("started lookup claimed completion", err)
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if f.calls.Load() != 1 {
		t.Fatalf("executions=%d", f.calls.Load())
	}
	r, err = f.svc.Result(context.Background(), f.snap.OrderID, f.snap.IntentHash)
	if err != nil || r.State != Completed {
		t.Fatal("no durable final result", err)
	}
}

func TestLifecycleCrashAndLostResponse(t *testing.T) {
	for _, fault := range []string{"before", "after", "claim", "crash", "panic", "cancel", "failure"} {
		t.Run(fault, func(t *testing.T) {
			f := newFixture(t)
			f.accept(t)
			switch fault {
			case "before", "after":
				f.store.finishFault = fault
			case "claim":
				f.store.claimFault = true
			case "crash":
				if _, owns, err := f.store.Claim(context.Background(), f.snap.OrderID, f.snap.IntentHash, f.event); err != nil || !owns {
					t.Fatal(err)
				}
			case "panic":
				f.svc.execute = func(context.Context, json.RawMessage) (json.RawMessage, error) {
					f.calls.Add(1)
					panic("simulated service crash")
				}
			case "failure":
				f.svc.execute = func(context.Context, json.RawMessage) (json.RawMessage, error) {
					f.calls.Add(1)
					return nil, errors.New("private executor diagnostics")
				}
			}
			ctx := context.Background()
			if fault == "cancel" {
				cancelCtx, cancel := context.WithCancel(ctx)
				ctx = cancelCtx
				f.svc.execute = func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
					f.calls.Add(1)
					out, err := TextDigest(ctx, raw)
					cancel()
					return out, err
				}
			}
			first, err := f.svc.Fulfill(ctx, f.snap.OrderID, f.snap.IntentHash, f.event)
			if fault == "claim" && !View(first).Paid {
				t.Fatal("confirmed payment hidden by uncertain claim response")
			}
			if fault == "cancel" {
				if err != nil || first.State != Completed {
					t.Fatal("lost caller cancelled persistence", err)
				}
			} else if fault == "failure" {
				if !errors.Is(err, ErrService) || first.State != Failed {
					t.Fatal("failure state hidden", err)
				}
			} else if fault != "crash" && !errors.Is(err, ErrUnknown) {
				t.Fatal("uncertainty hidden", err)
			}
			if fault == "before" && len(first.Result) != 0 {
				t.Fatal("unpersisted output returned")
			}
			restarted, newErr := New(f.cfg, f.store, f.verify)
			if newErr != nil {
				t.Fatal(newErr)
			}
			before := f.calls.Load()
			r, lookupErr := restarted.Result(context.Background(), f.snap.OrderID, f.snap.IntentHash)
			if lookupErr != nil {
				t.Fatal(lookupErr)
			}
			if (fault == "after" || fault == "cancel") && r.State != Completed {
				t.Fatal("lost response result missing")
			}
			if (fault == "before" || fault == "claim" || fault == "crash") && r.State != Started {
				t.Fatal("crash state not retained")
			}
			if _, err := restarted.Fulfill(context.Background(), f.snap.OrderID, f.snap.IntentHash, f.event); err != nil {
				t.Fatal(err)
			}
			if f.calls.Load() != before {
				t.Fatal("uncertain/final request blindly reran")
			}
		})
	}
}

func TestProtocolConfiguration(t *testing.T) {
	f := newFixture(t)
	for name, mutate := range map[string]func(*Config){"key": func(c *Config) { c.PrivateKey = nil }, "resource": func(c *Config) { c.Resource = "http://merchant.example" }, "price": func(c *Config) { c.AmountMicros = "01" }, "lifetime": func(c *Config) { c.QuoteTTL = 16 * time.Minute }, "capability": func(c *Config) { c.Capability = "other" }} {
		t.Run(name, func(t *testing.T) {
			c := f.cfg
			mutate(&c)
			if _, err := New(c, f.store, f.verify); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid config accepted")
			}
		})
	}
	if _, err := New(f.cfg, nil, f.verify); err == nil {
		t.Fatal("memory fallback")
	}
	var typedNil *testStore
	if _, err := New(f.cfg, typedNil, f.verify); err == nil {
		t.Fatal("typed nil store")
	}
	if _, err := New(f.cfg, f.store, nil); err == nil {
		t.Fatal("unverified payments enabled")
	}
}
