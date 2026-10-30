package db_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/agentmesh/backend/internal/purchasesim"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
)

type interruptedJournal struct {
	purchase.Store
	interrupted bool
}

func (s *interruptedJournal) Update(ctx context.Context, account, id string, change func(*purchase.Order, purchase.Ledger) error) (purchase.Order, error) {
	o, err := s.Store.Update(ctx, account, id, change)
	if err == nil && o.SignedEnvelope != "" && !s.interrupted {
		s.interrupted = true
		return o, errors.New("interrupted after durable transaction commit")
	}
	return o, err
}

type missedRelayCaller struct {
	arc.Caller
	miss bool
}

func (c *missedRelayCaller) Call(ctx context.Context, endpoint, method string, params []any, result any) error {
	if method == "eth_sendRawTransaction" && c.miss {
		c.miss = false
		return errors.New("transport ended before RPC accepted transaction")
	}
	return c.Caller.Call(ctx, endpoint, method, params, result)
}

type recoveryMerchant struct {
	purchase.Merchant
	executions map[string]int
}

func (m *recoveryMerchant) Lookup(_ context.Context, o purchase.Order) (json.RawMessage, string, error) {
	if m.executions[o.ID] > 0 {
		return json.RawMessage(`{"result":"stored"}`), "completed", nil
	}
	return nil, "accepted", nil
}
func (m *recoveryMerchant) Fulfill(_ context.Context, o purchase.Order) (json.RawMessage, string, error) {
	m.executions[o.ID]++
	return json.RawMessage(`{"result":"stored"}`), "completed", nil
}

func TestPurchaseRecoversMissedRelayBeforeSecondOrder(t *testing.T) {
	for _, interruption := range []string{"after_commit", "before_RPC_acceptance"} {
		t.Run(interruption, func(t *testing.T) {
			store := testStore(t)
			p := store.Purchases()
			user, err := store.CreateUser(t.Context(), uuid.NewString()+"@recovery.invalid", "unused-hash")
			if err != nil {
				t.Fatal(err)
			}
			payer, err := crypto.GenerateKey()
			if err != nil {
				t.Fatal(err)
			}
			relayer, err := crypto.GenerateKey()
			if err != nil {
				t.Fatal(err)
			}
			address := strings.ToLower(crypto.PubkeyToAddress(payer.PublicKey).Hex())
			challenge := purchase.WalletChallenge{ID: uuid.NewString(), AccountID: user.ID, Address: address, Message: "owned recovery fixture", ExpiresAt: time.Now().Add(time.Minute)}
			if err = p.SaveChallenge(t.Context(), challenge); err != nil {
				t.Fatal(err)
			}
			if _, err = p.LinkWallet(t.Context(), user.ID, challenge.ID, func(purchase.WalletChallenge) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if _, err = p.SetAllowance(t.Context(), user.ID, address, "300"); err != nil {
				t.Fatal(err)
			}
			payee := "0x2222222222222222222222222222222222222222"
			fixture, err := purchasesim.New(purchasesim.Config{Resource: "https://merchant.simulation.invalid/tool", Payee: payee, PriceMicros: "100"})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewTLSServer(fixture.ArcHandler())
			defer server.Close()
			caller := &missedRelayCaller{Caller: arc.HTTPCaller{HTTP: server.Client()}, miss: interruption == "before_RPC_acceptance"}
			rail := &purchase.ArcRail{Caller: caller, RPC: server.URL, Key: relayer, MaxGasWei: big.NewInt(1000000000000000), Confirmations: 2}
			merchant := &recoveryMerchant{executions: map[string]int{}}
			service := &purchase.Service{Enabled: true, Store: p, Rail: rail, Merchant: merchant, Catalog: purchase.ArkivCatalog{}, Identity: &purchase.WorldIdentity{}, Domain: "https://scratch.invalid", Seal: func(s string) (string, error) { return s, nil }, Open: func(s string) (string, error) { return s, nil }}
			makeOrder := func() (purchase.Order, string) {
				now := time.Now().UTC()
				until := now.Add(4 * time.Minute)
				o := purchase.Order{ID: uuid.NewString(), AccountID: user.ID, ClientRequestID: uuid.NewString(), State: purchase.AuthorizationReleased, MerchantAccepted: true, CreatedAt: now, SignatureReleasedAt: &now, IdentityValidUntil: &until}
				o.AuthorizationBlock, err = rail.Anchor(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				o.Intent = purchase.Intent{Version: purchase.Protocol, OrderID: o.ID, AccountID: user.ID, Payer: address, Payee: payee, ChainID: purchase.ChainID, Asset: purchase.Asset, AmountMicros: "100", MaxMicros: "100", OrderSalt: purchase.Digest([]byte(o.ID)), ValidAfter: "0", ValidBefore: strconv.FormatInt(until.Unix(), 10)}
				raw, _ := json.Marshal(o.Intent)
				o.IntentHash = purchase.Digest(raw)
				if _, _, err = p.Create(t.Context(), o); err != nil {
					t.Fatal(err)
				}
				if _, err = p.Update(t.Context(), user.ID, o.ID, func(_ *purchase.Order, l purchase.Ledger) error { return l.Reserve("100") }); err != nil {
					t.Fatal(err)
				}
				a, signErr := arc.SignAuthorization(payer, "USDC", "2", purchase.ChainID, common.HexToAddress(purchase.Asset), arc.Authorization{To: common.HexToAddress(payee), Value: big.NewInt(100), ValidAfter: big.NewInt(0), ValidBefore: big.NewInt(until.Unix()), Nonce: common.HexToHash(o.IntentHash)})
				if signErr != nil {
					t.Fatal(signErr)
				}
				return o, "0x" + hex.EncodeToString(append(append(append([]byte{}, a.R[:]...), a.S[:]...), a.V))
			}
			first, firstSignature := makeOrder()
			second, secondSignature := makeOrder()
			if interruption == "after_commit" {
				service.Store = &interruptedJournal{Store: p}
			}
			if _, err = service.Submit(t.Context(), user.ID, first.ID, firstSignature); err == nil {
				t.Fatal("interruption did not stop submit")
			}
			persisted, err := p.Get(t.Context(), user.ID, first.ID)
			if err != nil || persisted.TxHash == "" || persisted.SignedEnvelope == "" || fixture.Observations().SendAttempts != 0 {
				t.Fatal("missing durable pre-broadcast transaction")
			}
			service.Store = store.Purchases()
			if _, err = service.Submit(t.Context(), user.ID, second.ID, secondSignature); !errors.Is(err, purchase.ErrUncertain) {
				t.Fatal("later order was not blocked behind the missing relayer nonce")
			}
			blocked, err := p.Get(t.Context(), user.ID, second.ID)
			if err != nil || blocked.TxHash != "" || blocked.SignedEnvelope != "" || fixture.Observations().SendAttempts != 0 {
				t.Fatal("later order allocated or dispatched across nonce gap")
			}
			restarted := *service
			recovered, err := restarted.Reconcile(t.Context(), user.ID, first.ID)
			if err != nil || recovered.State != purchase.Completed || recovered.TxHash != persisted.TxHash || recovered.SignedEnvelope != persisted.SignedEnvelope || recovered.PaymentEvent == nil || recovered.PaymentEvent.TxHash != persisted.TxHash {
				t.Fatalf("committed transaction did not recover: state=%s error=%v", recovered.State, err)
			}
			completed, err := restarted.Submit(t.Context(), user.ID, second.ID, secondSignature)
			if err != nil || completed.State != purchase.Completed {
				t.Fatalf("second order cannot proceed after recovery: %s %v", completed.State, err)
			}
			if _, err = restarted.Reconcile(t.Context(), user.ID, first.ID); err != nil {
				t.Fatal(err)
			}
			if fixture.Observations().SendAttempts != 2 || merchant.executions[first.ID] != 1 || merchant.executions[second.ID] != 1 {
				t.Fatal("recovery duplicated a transaction or fulfillment")
			}
			allowance, err := p.Allowance(t.Context(), user.ID, address)
			if err != nil || allowance.ConsumedMicros != "200" || allowance.ReservedMicros != "0" {
				t.Fatal("recovery changed principal accounting")
			}
		})
	}
}
