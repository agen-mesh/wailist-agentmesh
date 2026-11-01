package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/google/uuid"
)

type uncertainPurchaseRail struct {
	purchase.Rail
	calls map[string]int
}

func (r uncertainPurchaseRail) Reconcile(_ context.Context, o purchase.Order) (purchase.Settlement, error) {
	r.calls[o.ID]++
	return purchase.Settlement{}, purchase.ErrUncertain
}

type storedPurchaseResult struct{ purchase.Merchant }

type accountPurchaseStore struct {
	purchase.Store
	account string
}

func (s accountPurchaseStore) Get(ctx context.Context, account, id string) (purchase.Order, error) {
	if account != s.account {
		return purchase.Order{}, purchase.ErrNotFound
	}
	return s.Store.Get(ctx, account, id)
}

func (storedPurchaseResult) Lookup(context.Context, purchase.Order) (json.RawMessage, string, error) {
	return json.RawMessage(`{"result":"durable"}`), "completed", nil
}

func purchaseReconcilerFixture(t *testing.T) (*Deps, string) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	account, err := store.CreateUser(t.Context(), uuid.NewString()+"@reconcile.invalid", "unused-hash")
	if err != nil {
		t.Fatal(err)
	}
	service := &purchase.Service{Enabled: true, Store: accountPurchaseStore{Store: store.Purchases(), account: account.ID}, Rail: uncertainPurchaseRail{calls: map[string]int{}}, Merchant: storedPurchaseResult{}, Catalog: purchase.ArkivCatalog{}, Identity: &purchase.WorldIdentity{}, Domain: "https://scratch.invalid", Seal: func(s string) (string, error) { return s, nil }, Open: func(s string) (string, error) { return s, nil }}
	return &Deps{Store: store, Purchases: &PurchaseRuntime{Service: service}}, account.ID
}

func persistReconciliationOrder(t *testing.T, d *Deps, account, id string, state purchase.State) purchase.Order {
	t.Helper()
	now := time.Now().UTC()
	o := purchase.Order{ID: id, AccountID: account, ClientRequestID: uuid.NewString(), State: state, CreatedAt: now, SignatureReleasedAt: &now}
	o.Intent = purchase.Intent{Version: purchase.Protocol, OrderID: id, AccountID: account, Payer: "0x1111111111111111111111111111111111111111", ChainID: purchase.ChainID, Asset: purchase.Asset, AmountMicros: "100"}
	if state == purchase.Paid {
		o.PaymentEvent = &purchase.PaymentEvent{ChainID: purchase.ChainID, TxHash: purchase.Digest([]byte(id)), LogIndex: "1"}
	}
	raw, _ := json.Marshal(o.Intent)
	o.IntentHash = purchase.Digest(raw)
	if _, _, err := d.Store.Purchases().Create(t.Context(), o); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := d.Store.Purchases().Update(context.Background(), account, id, func(current *purchase.Order, _ purchase.Ledger) error {
			current.State = purchase.Cancelled
			current.RelayConfirmed = true
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	})
	return o
}

func TestPurchaseReconciliationAdvancesPastThirtyUncertainOrders(t *testing.T) {
	d, account := purchaseReconcilerFixture(t)
	prefix := uuid.NewString()[:24]
	orders := make([]purchase.Order, 31)
	for i := range orders {
		state := purchase.PaymentUnknown
		if i == 30 {
			state = purchase.Paid
		}
		orders[i] = persistReconciliationOrder(t, d, account, fmt.Sprintf("%s%012x", prefix, i+1), state)
	}
	cursor := d.reconcilePurchaseBatch(t.Context(), prefix+"000000000000", "purchase-reconcile-test")
	if cursor != orders[29].ID {
		t.Fatal("first reconciliation page did not advance its cursor")
	}
	d.reconcilePurchaseBatch(t.Context(), cursor, "purchase-reconcile-test")
	last, err := d.Store.Purchases().Get(t.Context(), account, orders[30].ID)
	if err != nil || last.State != purchase.Completed {
		t.Fatal("uncertain orders starved a confirmed purchase")
	}
	for _, order := range orders[:30] {
		if d.Purchases.Service.Rail.(uncertainPurchaseRail).calls[order.ID] != 1 {
			t.Fatal("uncertain order monopolized later reconciliation pages")
		}
	}
	if cursor := d.reconcilePurchaseBatch(t.Context(), "ffffffff-ffff-ffff-ffff-ffffffffffff", "purchase-reconcile-test"); cursor != "" {
		t.Fatal("exhausted cursor did not wrap")
	}
}

func TestTerminalPurchaseRetainsUnconfirmedRelayRecovery(t *testing.T) {
	d, account := purchaseReconcilerFixture(t)
	prefix := uuid.NewString()[:24]
	o := persistReconciliationOrder(t, d, account, prefix+"000000000001", purchase.Expired)
	if _, err := d.Store.Purchases().Update(t.Context(), account, o.ID, func(current *purchase.Order, _ purchase.Ledger) error {
		current.TxHash = purchase.Digest([]byte(o.ID))
		current.SignedEnvelope = "private-committed-envelope"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertQueued := func(want bool) {
		t.Helper()
		orders, err := d.Store.Purchases().Pending(t.Context(), prefix+"000000000000")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, order := range orders {
			found = found || order.ID == o.ID
		}
		if found != want {
			t.Fatal("terminal relay recovery eligibility changed before confirmed nonce consumption")
		}
	}
	assertQueued(true)
	if _, err := d.Store.Purchases().Update(t.Context(), account, o.ID, func(current *purchase.Order, _ purchase.Ledger) error { current.RelayConfirmed = true; return nil }); err != nil {
		t.Fatal(err)
	}
	assertQueued(false)
}
