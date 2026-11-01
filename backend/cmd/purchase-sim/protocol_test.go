package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/arkiv"
	"github.com/agentmesh/backend/internal/merchant"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/agentmesh/backend/internal/purchasesim"
	"github.com/google/uuid"
)

type quoteOnlyStore struct{ merchant.Store }

func TestFixtureOfferMatchesMerchantAndCanonicalIntent(t *testing.T) {
	const payee = "0x1111111111111111111111111111111111111111"
	const payer = "0x2222222222222222222222222222222222222222"
	fixture, err := purchasesim.New(purchasesim.Config{Resource: simulationResource, Payee: payee, PriceMicros: "10000"})
	if err != nil {
		t.Fatal(err)
	}
	policy := func(_ context.Context, target string) error {
		if target != simulationResource {
			return purchase.ErrDenied
		}
		return nil
	}
	rpc := httptest.NewTLSServer(fixture.ArkivHandler())
	defer rpc.Close()
	client, err := arkiv.NewClientWithHTTP(rpc.URL, policy, rpc.Client())
	if err != nil {
		t.Fatal(err)
	}
	catalog := purchase.ArkivCatalog{Client: client}
	offer, err := catalog.Get(t.Context(), fixture.OfferKey())
	if err != nil {
		t.Fatal(err)
	}
	if offer.Capability != merchant.Capability {
		t.Fatalf("fixture capability %q, merchant requires %q", offer.Capability, merchant.Capability)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := merchant.New(merchant.Config{Resource: simulationResource, Payee: payee, AmountMicros: "10000", Capability: merchant.Capability, PrivateKey: private}, quoteOnlyStore{}, func(context.Context, merchant.Snapshot, merchant.PaymentEvent) (merchant.PaymentProof, error) {
		return merchant.PaymentProof{}, merchant.ErrPaymentPending
	})
	if err != nil {
		t.Fatal(err)
	}
	bearer := "private-local-protocol-test-credential"
	handler, err := merchant.NewHandler(service, bearer)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	adapter := &purchase.MerchantClient{Client: server.Client(), Policy: policy, BaseURL: server.URL + "/merchant", Resource: simulationResource, Bearer: bearer, PublicKey: public, Payee: payee, PriceMicros: "10000"}
	request := json.RawMessage(`{"text":"controlled boundary regression"}`)
	quote, err := adapter.Quote(t.Context(), offer, request)
	if err != nil {
		t.Fatal(err)
	}
	o := purchase.Order{ID: uuid.NewString(), AccountID: uuid.NewString(), Offer: offer, Quote: quote, Request: request}
	o.Intent, o.IntentHash, err = purchase.NewIntent(o, payer, "10000", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(o.Intent)
	if err != nil {
		t.Fatal(err)
	}
	rawQuote, err := json.Marshal(quote)
	if err != nil {
		t.Fatal(err)
	}
	var merchantQuote merchant.Quote
	if err = json.Unmarshal(rawQuote, &merchantQuote); err != nil {
		t.Fatal(err)
	}
	snapshot := merchant.Snapshot{OrderID: o.ID, IntentHash: o.IntentHash, CanonicalIntent: canonical, Payer: payer, Payee: payee, ChainID: purchase.ChainID, Asset: purchase.Asset, AmountMicros: "10000", Method: "POST", Resource: simulationResource, RequestDigest: o.Intent.RequestDigest, QuoteDigest: o.Intent.QuoteDigest, Deadline: purchase.Deadline(o), Request: request}
	if err = merchant.ValidateSnapshot(snapshot, merchantQuote, public); err != nil {
		t.Fatal(err)
	}
}
