package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/config"
	"github.com/agentmesh/backend/internal/purchase"
)

type discoveryOnly struct{ purchase.Catalog }

func (discoveryOnly) List(context.Context, string, string) ([]purchase.Offer, error) {
	return []purchase.Offer{{Key: "observed-offer"}}, nil
}

func TestArkivDiscoveryRemainsAvailableWithoutWorldCheckout(t *testing.T) {
	d := &Deps{}
	c := config.LoadPurchase(func(key string) string {
		if key == "ARKIV_RPC_URL" {
			return "https://rpc.example"
		}
		return ""
	})
	if err := d.SetupPurchases(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if d.Purchases.Service != nil || d.Purchases.PublicConfig["enabled"] != false || d.Purchases.PublicConfig["arkivAvailable"] != true || d.Purchases.PublicConfig["worldAvailable"] != false {
		t.Fatal("checkout and discovery capabilities collapsed")
	}
	d.Purchases.Catalog = discoveryOnly{}
	d.Purchases.ValidateEndpoint = func(context.Context, string) error { return nil }
	request := func(method, path, body string) *http.Request {
		return httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(context.WithValue(t.Context(), CtxUserID, "account"))
	}
	rr := httptest.NewRecorder()
	d.PurchaseOffers(rr, request("GET", "/purchases/offers", ""))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "observed-offer") {
		t.Fatal("read surface blocked")
	}
	rr = httptest.NewRecorder()
	d.PurchasePreflight(rr, request("POST", "/purchases/offers/preflight", `{"endpoint":"https://merchant.example/tool"}`))
	if rr.Code != 200 {
		t.Fatal("provider preflight blocked")
	}
	rr = httptest.NewRecorder()
	d.PurchaseCreate(rr, request("POST", "/purchases", `{"worldVerified":true,"operator":true}`))
	if rr.Code != 503 {
		t.Fatal("body flags enabled protected checkout")
	}
}

func TestPurchaseBodyRejectsForgedAuthorityFields(t *testing.T) {
	for _, body := range []string{`{"signature":"x","operator":true}`, `{"signature":"x","accountId":"another"}`, `{"signature":"x","worldVerified":true}`} {
		var target struct {
			Signature string `json:"signature"`
		}
		rr := httptest.NewRecorder()
		if purchaseBody(rr, httptest.NewRequest("POST", "/purchases/order/submit", strings.NewReader(body)), &target) || rr.Code != 400 {
			t.Fatal("unknown authority field accepted")
		}
	}
}
