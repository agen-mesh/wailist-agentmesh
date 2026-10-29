package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/merchant"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/agentmesh/backend/internal/world"
	"github.com/google/uuid"
)

func journalFixture(t *testing.T) (*db.Store, *db.PurchaseStore, string, string) {
	t.Helper()
	store := testStore(t)
	user, err := store.CreateUser(t.Context(), uuid.NewString()+"@purchase.invalid", "unused-test-hash")
	if err != nil {
		t.Fatal(err)
	}
	address := "0x" + purchase.Digest([]byte(user.ID))[2:42]
	p := store.Purchases()
	challenge := purchase.WalletChallenge{ID: uuid.NewString(), AccountID: user.ID, Address: address, Message: "database fixture", ExpiresAt: time.Now().Add(time.Minute)}
	if err = p.SaveChallenge(t.Context(), challenge); err != nil {
		t.Fatal(err)
	}
	if _, err = p.LinkWallet(t.Context(), user.ID, challenge.ID, func(purchase.WalletChallenge) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return store, p, user.ID, address
}
func journalOrder(t *testing.T, p *db.PurchaseStore, account, address string) purchase.Order {
	t.Helper()
	o := purchase.Order{ID: uuid.NewString(), AccountID: account, ClientRequestID: uuid.NewString(), State: purchase.Approved, MerchantAccepted: true, CreatedAt: time.Now().UTC()}
	o.Intent = purchase.Intent{Version: purchase.Protocol, OrderID: o.ID, AccountID: account, Payer: address, Payee: "0x2222222222222222222222222222222222222222", ChainID: purchase.ChainID, Asset: purchase.Asset, AmountMicros: "100", MaxMicros: "100", OrderSalt: purchase.Digest([]byte(o.ID)), ValidBefore: strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)}
	raw, err := json.Marshal(o.Intent)
	if err != nil {
		t.Fatal(err)
	}
	o.IntentHash = purchase.Digest(raw)
	stored, created, err := p.Create(t.Context(), o)
	if err != nil || !created {
		t.Fatalf("create: %v created=%v", err, created)
	}
	return stored
}

func TestPurchaseJournalConcurrentReservationAndOwnerIsolation(t *testing.T) {
	_, p, account, address := journalFixture(t)
	if _, err := p.SetAllowance(t.Context(), account, address, "150"); err != nil {
		t.Fatal(err)
	}
	first, second := journalOrder(t, p, account, address), journalOrder(t, p, account, address)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, o := range []purchase.Order{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Update(t.Context(), account, o.ID, func(current *purchase.Order, l purchase.Ledger) error {
				if err := l.Reserve(current.Intent.AmountMicros); err != nil {
					return err
				}
				current.State = purchase.AuthorizationReleased
				return nil
			})
			if err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("reservation winners=%d", wins.Load())
	}
	a, err := p.Allowance(t.Context(), account, address)
	if err != nil || a.ReservedMicros != "100" || a.AvailableMicros != "50" {
		t.Fatalf("allowance=%+v err=%v", a, err)
	}
	if _, err = p.Get(t.Context(), "different-account", first.ID); !errors.Is(err, purchase.ErrNotFound) {
		t.Fatal("cross-account read accepted")
	}
	if _, err = p.SetAllowance(t.Context(), account, address, "99"); !errors.Is(err, purchase.ErrConflict) {
		t.Fatal("reduced below reservation")
	}
}

func TestPurchaseJournalRollbackAndEventUniqueness(t *testing.T) {
	_, p, account, address := journalFixture(t)
	if _, err := p.SetAllowance(t.Context(), account, address, "300"); err != nil {
		t.Fatal(err)
	}
	a, b := journalOrder(t, p, account, address), journalOrder(t, p, account, address)
	for _, o := range []purchase.Order{a, b} {
		if _, err := p.Update(t.Context(), account, o.ID, func(current *purchase.Order, l purchase.Ledger) error { return l.Reserve(current.Intent.AmountMicros) }); err != nil {
			t.Fatal(err)
		}
	}
	injected := errors.New("injected journal failure")
	if _, err := p.Update(t.Context(), account, a.ID, func(o *purchase.Order, l purchase.Ledger) error {
		_, err := l.RelayNonce(address, 7)
		if err != nil {
			return err
		}
		o.TxHash = "uncommitted"
		return injected
	}); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	if _, err := p.Update(t.Context(), account, a.ID, func(o *purchase.Order, l purchase.Ledger) error {
		n, err := l.RelayNonce(address, 7)
		if err != nil {
			return err
		}
		if n != 7 {
			return fmt.Errorf("nonce advanced after rollback: %d", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e := purchase.PaymentEvent{ChainID: purchase.ChainID, TxHash: purchase.Digest([]byte(uuid.NewString())), LogIndex: "1", BlockHash: purchase.Digest([]byte("block")), BlockNumber: "42"}
	consume := func(o *purchase.Order, l purchase.Ledger) error {
		if err := l.BindEvent(e); err != nil {
			return err
		}
		if err := l.Consume(o.Intent.AmountMicros); err != nil {
			return err
		}
		o.PaymentEvent = &e
		o.State = purchase.Paid
		return nil
	}
	if _, err := p.Update(t.Context(), account, a.ID, consume); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Update(t.Context(), account, b.ID, consume); err == nil {
		t.Fatal("one event funded two orders")
	}
	allowance, err := p.Allowance(t.Context(), account, address)
	if err != nil || allowance.ConsumedMicros != "100" || allowance.ReservedMicros != "100" {
		t.Fatalf("duplicate changed allowance: %+v %v", allowance, err)
	}
	if _, err = p.Update(t.Context(), account, a.ID, func(o *purchase.Order, _ purchase.Ledger) error { o.Intent.AmountMicros = "1"; return nil }); !errors.Is(err, purchase.ErrConflict) {
		t.Fatal("immutable terms changed")
	}
}

func TestPurchaseMerchantClaimSurvivesRestartWithoutSecondExecution(t *testing.T) {
	store, _, _, _ := journalFixture(t)
	m := store.MerchantPurchases()
	id := uuid.NewString()
	snapshot := merchant.Snapshot{OrderID: id, IntentHash: purchase.Digest([]byte(id)), CanonicalIntent: json.RawMessage(`{}`), Request: json.RawMessage(`{"text":"scratch"}`)}
	if _, err := m.Accept(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	e := merchant.PaymentEvent{ChainID: purchase.ChainID, TxHash: purchase.Digest([]byte(id + "payment")), LogIndex: "2"}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimed, err := m.Claim(context.Background(), id, snapshot.IntentHash, e)
			if err != nil {
				t.Error(err)
			}
			if claimed {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("fulfillment claims=%d", wins.Load())
	}
	restarted := store.MerchantPurchases()
	record, claimed, err := restarted.Claim(t.Context(), id, snapshot.IntentHash, e)
	if err != nil || claimed || record.State != merchant.Started {
		t.Fatalf("restart reexecuted: %v %v %s", err, claimed, record.State)
	}
	if _, err = restarted.Finish(t.Context(), id, snapshot.IntentHash, merchant.Outcome{State: merchant.Completed, Result: json.RawMessage(`{"digest":"saved"}`)}); err != nil {
		t.Fatal(err)
	}
	_, claimed, err = restarted.Claim(t.Context(), id, snapshot.IntentHash, e)
	if err != nil || claimed {
		t.Fatal("completed result was replayed")
	}
}

func TestPurchaseIdentityConsumptionOwnershipAndCancellation(t *testing.T) {
	store, p, account, address := journalFixture(t)
	identities := store.PurchaseIdentities("01234567890123456789012345678901")
	makeAttempt := func(account, address string) (purchase.Order, world.Attempt, world.Evidence) {
		t.Helper()
		o := journalOrder(t, p, account, address)
		if _, err := p.Update(t.Context(), account, o.ID, func(current *purchase.Order, _ purchase.Ledger) error {
			current.State = purchase.AwaitingIdentity
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		binding, err := identities.Binding(t.Context(), account, o.ID, o.IntentHash, world.SandboxIssuer)
		if err != nil {
			t.Fatal(err)
		}
		a := world.Attempt{ID: uuid.NewString(), StateHash: purchase.Digest([]byte(uuid.NewString())), Binding: binding, Issuer: world.SandboxIssuer, Environment: world.Sandbox, Nonce: "private-nonce", CodeVerifier: "private-verifier", CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
		e := world.Evidence{AttemptID: a.ID, Binding: binding, Issuer: world.SandboxIssuer, Subject: "unique-subject-" + account, AuthenticatedAt: now, ValidUntil: now.Add(time.Minute)}
		if err = identities.Create(t.Context(), a); err != nil {
			t.Fatal(err)
		}
		return o, a, e
	}
	o, a, e := makeAttempt(account, address)
	wrong := a.Binding
	wrong.AccountID = "another-account"
	if _, err := identities.Consume(t.Context(), a.StateHash, wrong, time.Now()); err == nil {
		t.Fatal("attempt crossed accounts")
	}
	stored, err := identities.Consume(t.Context(), a.StateHash, a.Binding, time.Now())
	if err != nil || stored.Nonce != a.Nonce || stored.CodeVerifier != a.CodeVerifier {
		t.Fatal("encrypted attempt did not round trip")
	}
	if _, err = identities.Consume(t.Context(), a.StateHash, a.Binding, time.Now()); err == nil {
		t.Fatal("attempt consumed twice")
	}
	if err = identities.Complete(t.Context(), a.ID, e, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = identities.Complete(t.Context(), a.ID, e, time.Now()); err == nil {
		t.Fatal("evidence completed twice")
	}
	verified, err := p.Get(t.Context(), account, o.ID)
	if err != nil || verified.State != purchase.AwaitingApproval || verified.ApprovedAt != nil || verified.IdentityVerifiedAt == nil {
		t.Fatal("identity automatically approved payment or was lost")
	}
	if err = identities.Cancel(t.Context(), a.ID, a.Binding, time.Now()); err != nil {
		t.Fatal(err)
	}
	verified, err = p.Get(t.Context(), account, o.ID)
	if err != nil || verified.State != purchase.AwaitingIdentity || verified.IdentityVerifiedAt != nil {
		t.Fatal("cancel did not revoke pre-sign evidence")
	}
	if err = identities.Complete(t.Context(), a.ID, e, time.Now()); err == nil {
		t.Fatal("cancelled evidence restored")
	}
	other, err := store.CreateUser(t.Context(), uuid.NewString()+"@purchase.invalid", "unused")
	if err != nil {
		t.Fatal(err)
	}
	_, second, secondEvidence := makeAttempt(other.ID, address)
	if _, err = identities.Consume(t.Context(), second.StateHash, second.Binding, time.Now()); err != nil {
		t.Fatal(err)
	}
	secondEvidence.Subject = e.Subject
	if err = identities.Complete(t.Context(), second.ID, secondEvidence, time.Now()); err == nil {
		t.Fatal("World subject linked to two accounts")
	}
	missing := a
	missing.ID = uuid.NewString()
	missing.StateHash = uuid.NewString()
	missing.Binding.OrderID = uuid.NewString()
	if err = identities.Create(t.Context(), missing); err == nil {
		t.Fatal("identity attempt created without an owned order")
	}
}
