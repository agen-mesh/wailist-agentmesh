package db_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/merchant"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type durableMerchantFixture struct {
	store    *db.Store
	orders   *db.PurchaseStore
	order    purchase.Order
	snapshot merchant.Snapshot
	quote    merchant.Quote
	cfg      merchant.Config
	service  *merchant.Service
	calls    atomic.Int32
}

func newDurableMerchantFixture(t *testing.T) *durableMerchantFixture {
	t.Helper()
	store, orders, account, payer := journalFixture(t)
	f := &durableMerchantFixture{store: store, orders: orders}
	now := time.Now().UTC().Truncate(time.Second)
	f.cfg = merchant.Config{Resource: "https://merchant.example/tool", Payee: "0x" + strings.Repeat("2", 40), AmountMicros: "10000", Capability: merchant.Capability,
		PrivateKey: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{19}, ed25519.SeedSize)), Now: func() time.Time { return now },
		Execute: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			f.calls.Add(1)
			return merchant.TextDigest(ctx, raw)
		},
	}
	f.service = f.restart(t)
	request, err := purchase.CanonicalRequest(json.RawMessage(`{"text":"Mumbai café <offer> & quote \"yes\"\nsecond line"}`))
	if err != nil {
		t.Fatal(err)
	}
	f.quote, err = f.service.Quote(t.Context(), merchant.QuoteRequest{Method: "POST", Resource: f.cfg.Resource, Request: request})
	if err != nil {
		t.Fatal(err)
	}
	quoteJSON, err := json.Marshal(f.quote)
	if err != nil {
		t.Fatal(err)
	}
	var quote purchase.Quote
	if err := json.Unmarshal(quoteJSON, &quote); err != nil {
		t.Fatal(err)
	}
	o := purchase.Order{ID: uuid.NewString(), AccountID: account, ClientRequestID: uuid.NewString(), State: purchase.AwaitingIdentity, CreatedAt: now, Request: request, Quote: quote,
		Offer: purchase.Offer{Key: "offer-1", Owner: f.cfg.Payee, Payee: f.cfg.Payee, Version: "1", ContentHash: purchase.Digest([]byte("content")), SnapshotHash: purchase.Digest([]byte("snapshot")), Capability: merchant.Capability, Endpoint: f.cfg.Resource, Method: "POST", ChainID: purchase.ChainID, Asset: purchase.Asset, PriceMicros: f.cfg.AmountMicros, ValidUntil: now.Add(time.Hour), ExpiresAtBlock: "100", ObservedBlock: "1"},
	}
	o.Intent, o.IntentHash, err = purchase.NewIntent(o, payer, "20000", now)
	if err != nil {
		t.Fatal(err)
	}
	f.order = o
	canonical, err := json.Marshal(o.Intent)
	if err != nil {
		t.Fatal(err)
	}
	i := o.Intent
	f.snapshot = merchant.Snapshot{OrderID: o.ID, IntentHash: o.IntentHash, CanonicalIntent: canonical, Payer: i.Payer, Payee: i.Payee, ChainID: i.ChainID, Asset: i.Asset, AmountMicros: i.AmountMicros, Method: i.Method, Resource: i.Resource, RequestDigest: i.RequestDigest, QuoteDigest: i.QuoteDigest, Deadline: f.quote.Deadline, Request: bytes.Clone(request)}
	if err := merchant.ValidateSnapshot(f.snapshot, f.quote, f.cfg.PrivateKey.Public().(ed25519.PublicKey)); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *durableMerchantFixture) restart(t *testing.T) *merchant.Service {
	t.Helper()
	s, err := merchant.New(f.cfg, f.store.MerchantPurchases(), func(_ context.Context, s merchant.Snapshot, e merchant.PaymentEvent) (merchant.PaymentProof, error) {
		return merchant.PaymentProof{Event: e, Payer: s.Payer, Payee: s.Payee, Asset: s.Asset, AmountMicros: s.AmountMicros, AuthorizationNonce: s.IntentHash, PaidAt: f.cfg.Now(), AuthorizationConfirmed: true, TransferConfirmed: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPurchaseJSONBCommittedBytes(t *testing.T) {
	t.Run("purchase reload and merchant HTTP acceptance", func(t *testing.T) {
		f := newDurableMerchantFixture(t)
		if _, created, err := f.orders.Create(t.Context(), f.order); err != nil || !created {
			t.Fatalf("create: %v created=%v", err, created)
		}
		loaded, err := f.orders.Get(t.Context(), f.order.AccountID, f.order.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(loaded.Request, f.order.Request) {
			t.Fatal("purchase JSONB changed committed request bytes")
		}
		bearer := strings.Repeat("b", 32)
		h, err := merchant.NewHandler(f.service, bearer)
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(h)
		defer server.Close()
		client := purchase.MerchantClient{Client: server.Client(), Policy: func(context.Context, string) error { return nil }, BaseURL: server.URL + "/merchant", Resource: f.cfg.Resource, Bearer: bearer, PublicKey: f.cfg.PrivateKey.Public().(ed25519.PublicKey)}
		if err := client.Accept(t.Context(), loaded); err != nil {
			t.Fatalf("reloaded merchant acceptance: %v", err)
		}
		if _, err := f.orders.Update(t.Context(), loaded.AccountID, loaded.ID, func(o *purchase.Order, _ purchase.Ledger) error { o.MerchantAccepted = true; return nil }); err != nil {
			t.Fatal(err)
		}
		reloaded, err := f.orders.ByRequest(t.Context(), loaded.AccountID, loaded.ClientRequestID)
		if err != nil || !bytes.Equal(reloaded.Request, f.order.Request) {
			t.Fatalf("updated request changed: %v", err)
		}
		if err := client.Accept(t.Context(), reloaded); err != nil {
			t.Fatalf("duplicate acceptance: %v", err)
		}
		changed := reloaded
		changed.Request = json.RawMessage(`{"text":"changed"}`)
		if err := client.Accept(t.Context(), changed); err == nil {
			t.Fatal("changed request accepted under old digest")
		}
		if f.calls.Load() != 0 {
			t.Fatal("acceptance executed merchant")
		}
	})
	t.Run("merchant acceptance fulfillment and restarted lookup", func(t *testing.T) {
		f := newDurableMerchantFixture(t)
		if _, err := f.service.Accept(t.Context(), f.snapshot, f.quote); err != nil {
			t.Fatalf("signed snapshot acceptance: %v", err)
		}
		loaded, err := f.store.MerchantPurchases().Get(t.Context(), f.order.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(loaded.Snapshot.CanonicalIntent, f.snapshot.CanonicalIntent) || !bytes.Equal(loaded.Snapshot.Request, f.snapshot.Request) {
			t.Fatal("merchant JSONB changed committed bytes")
		}
		restarted := f.restart(t)
		if _, err := restarted.Accept(t.Context(), f.snapshot, f.quote); err != nil {
			t.Fatalf("duplicate acceptance: %v", err)
		}
		event := merchant.PaymentEvent{ChainID: merchant.ChainID, TxHash: purchase.Digest([]byte(f.order.ID)), LogIndex: "2", BlockHash: purchase.Digest([]byte("block")), BlockNumber: "100"}
		completed, err := restarted.Fulfill(t.Context(), f.order.ID, f.order.IntentHash, event)
		if err != nil || completed.State != merchant.Completed {
			t.Fatalf("fulfillment state=%s: %v", completed.State, err)
		}
		result, err := f.restart(t).Result(t.Context(), f.order.ID, f.order.IntentHash)
		if err != nil || !bytes.Equal(result.Result, completed.Result) {
			t.Fatalf("cached result changed: %v", err)
		}
		if _, err := f.restart(t).Fulfill(t.Context(), f.order.ID, f.order.IntentHash, event); err != nil {
			t.Fatal(err)
		}
		if f.calls.Load() != 1 {
			t.Fatalf("executions=%d", f.calls.Load())
		}
	})
}

func TestPurchaseJSONBLegacyCommitmentRecovery(t *testing.T) {
	f := newDurableMerchantFixture(t)
	if _, _, err := f.orders.Create(t.Context(), f.order); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(t.Context(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	oldOrder, err := json.Marshal(f.order)
	if err != nil {
		t.Fatal(err)
	}
	oldMerchant, err := json.Marshal(merchant.Record{Snapshot: f.snapshot, State: merchant.Accepted, AcceptedAt: f.cfg.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(t.Context(), `UPDATE purchase_orders SET payload=$1 WHERE id=$2`, string(oldOrder), f.order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(t.Context(), `INSERT INTO purchase_merchant_orders(order_id,intent_hash,payload,state) VALUES($1,$2,$3,$4)`, f.order.ID, f.order.IntentHash, string(oldMerchant), merchant.Accepted); err != nil {
		t.Fatal(err)
	}
	restored, err := f.orders.Get(t.Context(), f.order.AccountID, f.order.ID)
	if err != nil || !bytes.Equal(restored.Request, f.order.Request) || restored.IntentHash != f.order.IntentHash {
		t.Fatalf("legacy purchase recovery changed commitment: %v", err)
	}
	accepted, err := f.restart(t).Accept(t.Context(), f.snapshot, f.quote)
	if err != nil || !bytes.Equal(accepted.Snapshot.CanonicalIntent, f.snapshot.CanonicalIntent) || !bytes.Equal(accepted.Snapshot.Request, f.snapshot.Request) {
		t.Fatalf("legacy merchant recovery changed commitment: %v", err)
	}
	if _, err = f.orders.Update(t.Context(), f.order.AccountID, f.order.ID, func(o *purchase.Order, _ purchase.Ledger) error { o.MerchantAccepted = true; return nil }); err != nil {
		t.Fatal(err)
	}
	var version int
	if err = conn.QueryRow(t.Context(), `SELECT (payload->>'storageVersion')::int FROM purchase_orders WHERE id=$1`, f.order.ID).Scan(&version); err != nil || version != 1 {
		t.Fatalf("legacy update did not preserve bytes: %v", err)
	}
	t.Run("encoded request mutation", func(t *testing.T) {
		if _, err := conn.Exec(t.Context(), `UPDATE purchase_orders SET payload=jsonb_set(payload,'{requestBytes}','"e30="'::jsonb) WHERE id=$1`, f.order.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.orders.Get(t.Context(), f.order.AccountID, f.order.ID); err == nil {
			t.Fatal("altered encoded request accepted")
		}
	})
	t.Run("legacy request mutation", func(t *testing.T) {
		if _, err := conn.Exec(t.Context(), `UPDATE purchase_orders SET payload=jsonb_set($1::jsonb,'{request}','{"text":"changed"}'::jsonb) WHERE id=$2`, string(oldOrder), f.order.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.orders.Get(t.Context(), f.order.AccountID, f.order.ID); err == nil {
			t.Fatal("altered legacy request accepted")
		}
		if _, err := conn.Exec(t.Context(), `UPDATE purchase_merchant_orders SET payload=jsonb_set($1::jsonb,'{snapshot,request}','{"text":"changed"}'::jsonb) WHERE order_id=$2`, string(oldMerchant), f.order.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.MerchantPurchases().Get(t.Context(), f.order.ID); err == nil {
			t.Fatal("altered legacy merchant request accepted")
		}
	})
	t.Run("legacy quote mutation", func(t *testing.T) {
		if _, err := conn.Exec(t.Context(), `UPDATE purchase_orders SET payload=jsonb_set($1::jsonb,'{quote,proof}','"changed"'::jsonb) WHERE id=$2`, string(oldOrder), f.order.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.orders.Get(t.Context(), f.order.AccountID, f.order.ID); err == nil {
			t.Fatal("altered quote accepted")
		}
	})
	t.Run("legacy intent mutation", func(t *testing.T) {
		if _, err := conn.Exec(t.Context(), `UPDATE purchase_merchant_orders SET payload=jsonb_set($1::jsonb,'{snapshot,canonicalIntent,maxMicros}','"30000"'::jsonb) WHERE order_id=$2`, string(oldMerchant), f.order.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.MerchantPurchases().Get(t.Context(), f.order.ID); err == nil {
			t.Fatal("altered legacy intent accepted")
		}
	})
	t.Run("legacy result cannot recover original bytes", func(t *testing.T) {
		if _, err := conn.Exec(t.Context(), `UPDATE purchase_merchant_orders SET payload=jsonb_set($1::jsonb,'{result}','{"result":"old"}'::jsonb) WHERE order_id=$2`, string(oldMerchant), f.order.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.MerchantPurchases().Get(t.Context(), f.order.ID); err == nil {
			t.Fatal("unrecoverable legacy result accepted")
		}
	})
}
