package purchase

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryJournal struct {
	Store
	mu                 sync.Mutex
	o                  Order
	reserved, consumed int
	nonce              uint64
	fail               bool
}
type memoryLedger struct {
	reserved, consumed int
	nonce              uint64
}

func (l *memoryLedger) Reserve(string) error {
	if l.reserved+l.consumed >= 1 {
		return ErrDenied
	}
	l.reserved++
	return nil
}
func (l *memoryLedger) Consume(string) error {
	if l.reserved != 1 {
		return ErrConflict
	}
	l.reserved--
	l.consumed++
	return nil
}
func (l *memoryLedger) Release(string) error {
	if l.reserved != 1 {
		return ErrConflict
	}
	l.reserved--
	return nil
}
func (l *memoryLedger) BindEvent(PaymentEvent) error { return nil }
func (l *memoryLedger) RelayNonce(string, uint64) (uint64, error) {
	n := l.nonce
	l.nonce++
	return n, nil
}
func (m *memoryJournal) Get(_ context.Context, a, id string) (Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a != m.o.AccountID || id != m.o.ID {
		return Order{}, ErrNotFound
	}
	return m.o, nil
}
func (m *memoryJournal) Update(_ context.Context, a, id string, fn func(*Order, Ledger) error) (Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return Order{}, errors.New("injected durable write failure")
	}
	if a != m.o.AccountID || id != m.o.ID {
		return Order{}, ErrNotFound
	}
	o := m.o
	l := &memoryLedger{m.reserved, m.consumed, m.nonce}
	if err := fn(&o, l); err != nil {
		return m.o, err
	}
	m.o = o
	m.reserved = l.reserved
	m.consumed = l.consumed
	m.nonce = l.nonce
	return o, nil
}

type testCatalog struct {
	Catalog
	err error
}

func (c testCatalog) Revalidate(context.Context, Offer) error { return c.err }

type testIdentity struct{ Identity }
type testMerchant struct {
	Merchant
	calls    atomic.Int32
	mu       sync.Mutex
	complete bool
}

func (m *testMerchant) Lookup(context.Context, Order) (json.RawMessage, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.complete {
		return json.RawMessage(`{"digest":"confirmed"}`), "completed", nil
	}
	return nil, "accepted", nil
}
func (m *testMerchant) Fulfill(context.Context, Order) (json.RawMessage, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.complete {
		m.calls.Add(1)
		m.complete = true
	}
	return json.RawMessage(`{"digest":"confirmed"}`), "completed", nil
}

type testRail struct {
	mu      sync.Mutex
	sent    atomic.Int32
	journal *memoryJournal
	event   *PaymentEvent
	expired bool
	lost    bool
}

func (r *testRail) Check(context.Context) error            { return nil }
func (r *testRail) Anchor(context.Context) (string, error) { return "0x100", nil }
func (r *testRail) IsEOA(context.Context, string) error    { return nil }
func (r *testRail) Prepare(context.Context, Order, string) (Prepared, error) {
	return Prepared{Relayer: "0x3333333333333333333333333333333333333333", Sign: func(uint64) (string, string, error) { return "signed-bytes", "local-hash", nil }}, nil
}
func (r *testRail) Broadcast(_ context.Context, raw, hash string) error {
	r.journal.mu.Lock()
	persisted := r.journal.o.TxHash == hash && r.journal.o.SignedEnvelope != "" && r.journal.o.State == Submitting
	r.journal.mu.Unlock()
	if !persisted {
		return errors.New("dispatch preceded journal")
	}
	r.sent.Add(1)
	if r.lost {
		return errors.New("response lost")
	}
	return nil
}
func (r *testRail) Reconcile(context.Context, Order) (Settlement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Settlement{Event: r.event, ExpiredUnused: r.expired}, nil
}
func serviceFixture(t *testing.T) (*Service, *memoryJournal, *testRail, *testMerchant) {
	t.Helper()
	o := protocolOrder(t)
	now := o.CreatedAt
	o.IdentityVerifiedAt = &now
	until := now.Add(5 * time.Minute)
	o.IdentityValidUntil = &until
	o.ApprovedAt = &now
	o.State = Approved
	o.MerchantAccepted = true
	j := &memoryJournal{o: o}
	r := &testRail{journal: j}
	m := &testMerchant{}
	s := &Service{Enabled: true, Store: j, Catalog: testCatalog{}, Merchant: m, Identity: testIdentity{}, Rail: r, Domain: "https://app.example", Seal: func(s string) (string, error) { return "sealed:" + s, nil }, Open: func(s string) (string, error) { return s, nil }, Now: func() time.Time { return now }}
	return s, j, r, m
}

func TestReleaseReservesOnceAndCancellationNeverRevokes(t *testing.T) {
	s, j, _, _ := serviceFixture(t)
	ctx := context.Background()
	account, id := j.o.AccountID, j.o.ID
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ReleaseAuthorization(ctx, account, id)
			if err != nil && !errors.Is(err, ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if j.reserved != 1 || j.o.SignatureReleasedAt == nil || j.o.AuthorizationBlock != "0x100" {
		t.Fatal("release was not durably reserved")
	}
	if _, err := s.Cancel(ctx, j.o.AccountID, j.o.ID); !errors.Is(err, ErrUncertain) {
		t.Fatalf("post-release cancellation: %v", err)
	}
}
func TestLostBroadcastAndRestartNeverSendTwice(t *testing.T) {
	s, j, r, m := serviceFixture(t)
	ctx := context.Background()
	account, id := j.o.AccountID, j.o.ID
	o, err := s.ReleaseAuthorization(ctx, account, id)
	if err != nil {
		t.Fatal(err)
	}
	sig := signOrder(t, o)
	r.lost = true
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = s.Submit(ctx, account, id, sig) }()
	}
	wg.Wait()
	if r.sent.Load() != 1 || j.o.State != PaymentUnknown || j.o.TxHash != "local-hash" {
		t.Fatalf("lost response state: sent %d state %s", r.sent.Load(), j.o.State)
	}
	restarted := *s
	if _, err = restarted.Reconcile(ctx, account, id); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if m.calls.Load() != 0 {
		t.Fatal("pending payment fulfilled")
	}
	r.mu.Lock()
	r.event = &PaymentEvent{ChainID: ChainID, TxHash: "third-party-hash", LogIndex: "4", BlockHash: "confirmed-block", BlockNumber: "260"}
	r.mu.Unlock()
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := restarted.Reconcile(ctx, account, id); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if r.sent.Load() != 1 || m.calls.Load() != 1 || j.reserved != 0 || j.consumed != 1 || j.o.State != Completed {
		t.Fatalf("reconciliation repeated effects: sent %d fulfilled %d reserve %d consumed %d state %s", r.sent.Load(), m.calls.Load(), j.reserved, j.consumed, j.o.State)
	}
}
func TestJournalFailurePreventsDispatch(t *testing.T) {
	s, j, r, _ := serviceFixture(t)
	o, err := s.ReleaseAuthorization(context.Background(), j.o.AccountID, j.o.ID)
	if err != nil {
		t.Fatal(err)
	}
	j.fail = true
	if _, err = s.Submit(context.Background(), o.AccountID, o.ID, signOrder(t, o)); err == nil {
		t.Fatal("injected persistence failure accepted")
	}
	if r.sent.Load() != 0 {
		t.Fatal("broadcast without journal")
	}
}
func TestExpiredUnusedReleasesReservationWithoutRefund(t *testing.T) {
	s, j, r, m := serviceFixture(t)
	o, err := s.ReleaseAuthorization(context.Background(), j.o.AccountID, j.o.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.expired = true
	o, err = s.Reconcile(context.Background(), o.AccountID, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o.State != Expired || j.reserved != 0 || j.consumed != 0 || m.calls.Load() != 0 {
		t.Fatal("unused expiration changed principal")
	}
}
func TestDisabledOrChangedOfferCannotRelease(t *testing.T) {
	s, j, r, _ := serviceFixture(t)
	s.Enabled = false
	if _, err := s.ReleaseAuthorization(context.Background(), j.o.AccountID, j.o.ID); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	s.Enabled = true
	s.Catalog = testCatalog{err: errors.New("owner changed")}
	if _, err := s.ReleaseAuthorization(context.Background(), j.o.AccountID, j.o.ID); err == nil {
		t.Fatal("changed offer accepted")
	}
	if j.reserved != 0 || r.sent.Load() != 0 {
		t.Fatal("denied order changed funds")
	}
}
