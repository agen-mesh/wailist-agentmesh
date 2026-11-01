package purchasesim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/arkiv"
	"github.com/agentmesh/backend/internal/merchant"
)

type handlerTransport struct{ handler http.Handler }

func (h handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w.Result(), nil
}

func offerClient(t *testing.T, f *Fixture) *arkiv.Client {
	t.Helper()
	c, err := arkiv.NewClientWithHTTP("https://arkiv.simulation.example", func(context.Context, string) error { return nil }, &http.Client{Transport: handlerTransport{f.ArkivHandler()}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestArkivProductionReaderAndRevalidation(t *testing.T) {
	for _, mode := range []string{"none", "changed", "changed_owner", "changed_version", "changed_payload", "expired", "native_expired", "utc_expired", "empty"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := testFixture(t)
			f.config.Now = time.Now
			f.Reset()
			c := offerClient(t, f)
			page, err := c.Query(context.Background(), arkiv.QueryOptions{Capability: merchant.Capability, MaxPriceMicros: 1000000})
			if err != nil || len(page.Offers) != 1 || page.Rejected != 0 {
				t.Fatalf("valid fixture rejected: %v", err)
			}
			accepted := page.Offers[0]
			if accepted.EntityKey != f.OfferKey() || accepted.Owner != f.config.Payee || accepted.PriceMicros != 1000000 || accepted.Payload.Endpoint != f.config.Resource || accepted.ExpiresAtBlock == accepted.ValidUntil || accepted.Capability != merchant.Capability {
				t.Fatal("offer snapshot terms mismatch")
			}
			setFaults(t, f, Faults{Arkiv: mode})
			current, err := c.Revalidate(context.Background(), accepted)
			switch mode {
			case "none":
				if err != nil || current.SnapshotHash != accepted.SnapshotHash {
					t.Fatal("unchanged offer rejected")
				}
			case "changed", "changed_version", "changed_payload":
				if !errors.Is(err, arkiv.ErrChanged) {
					t.Fatalf("changed offer accepted: %v", err)
				}
			case "changed_owner":
				if !errors.Is(err, arkiv.ErrInvalidOffer) {
					t.Fatalf("owner/payee mismatch accepted: %v", err)
				}
			case "utc_expired":
				if !errors.Is(err, arkiv.ErrExpired) {
					t.Fatalf("UTC deadline accepted: %v", err)
				}
			default:
				if !errors.Is(err, arkiv.ErrNotFound) {
					t.Fatalf("native expired or empty offer accepted: %v", err)
				}
			}
			if f.Observations().ArkivQueries != 1 {
				t.Fatal("revalidation did not query fixture")
			}
		})
	}
}

type queryResult struct {
	Data  []map[string]json.RawMessage `json:"data"`
	Block string                       `json:"blockNumber"`
}

func TestArkivRabdiFiltersProjectionAndLatestOffset(t *testing.T) {
	f, _ := testFixture(t)
	h := f.ArkivHandler()
	if result[string](t, rpc(t, h, "eth_chainId")) != hexQuantity(ArkivChainID) {
		t.Fatal("wrong Arkiv chain")
	}
	head := result[string](t, rpc(t, h, "eth_blockNumber"))
	projection := map[string]any{"key": true, "owner": true, "expiresAt": true, "updatedAt": true, "attributes": true}
	q := func(expression string, options map[string]any) queryResult {
		return result[queryResult](t, rpc(t, h, "arkiv_query", expression, options))
	}
	page := q(arkiv.DiscoveryQuery+" ORDER BY $key ASC", map[string]any{"select": projection, "offset": "0x0", "limit": "0x1"})
	if len(page.Data) != 1 || page.Block != head {
		t.Fatal("exact discovery query missing row")
	}
	var attrs []attribute
	if json.Unmarshal(page.Data[0]["attributes"], &attrs) != nil || len(attrs) != 12 {
		t.Fatal("typed attributes missing")
	}
	payloadValue := func(row map[string]json.RawMessage) string {
		var values []attribute
		if err := json.Unmarshal(row["attributes"], &values); err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			if value.Name == "payload" && value.Type == "bytes" {
				return value.Value
			}
		}
		t.Fatal("payload bytes attribute missing")
		return ""
	}
	originalPayload := payloadValue(page.Data[0])
	if _, exists := page.Data[0]["payload"]; exists {
		t.Fatal("fixture retained the retired top-level payload field")
	}
	if len(q(arkiv.DiscoveryQuery, map[string]any{"offset": "0x1"}).Data) != 0 {
		t.Fatal("offset repeated the first entity")
	}
	for _, filter := range []string{"project = str('wrong')", "settlement_chain = u64(1)", "settlement_chain = str('5042002')", "price_micros < u64(1000000)", "capability = str('other')", "$key = key(0x0000000000000000000000000000000000000000000000000000000000000000)"} {
		if len(q(filter, nil).Data) != 0 {
			t.Fatalf("incorrect filter ignored: %s", filter)
		}
	}
	for _, filter := range []string{"*", "price_micros <= u64(1000000)", "price_micros >= u64(1000000)", "$key = key(" + f.OfferKey() + ")", "$owner = addr(" + f.config.Payee + ")"} {
		if len(q(filter, nil).Data) != 1 {
			t.Fatalf("valid filter rejected: %s", filter)
		}
	}
	for _, filter := range []string{"project='agentmesh-mumbai'", "price_micros = u64(-1)", "price_micros = u64(18446744073709551616)", "* OR *", "project = str('x') OR status = str('active')"} {
		requireError(t, rpc(t, h, "arkiv_query", filter))
	}
	for _, options := range []map[string]any{{"limit": "0x0"}, {"limit": "0x33"}, {"offset": "invalid"}, {"atBlock": head}, {"cursor": "not-present"}, {"select": map[string]any{"payload": true}}, {"select": map[string]any{"contentType": true}}, {"select": map[string]any{"unknown": true}}, {"ignored": true}} {
		requireError(t, rpc(t, h, "arkiv_query", "*", options))
	}
	only := q("*", map[string]any{"select": map[string]any{"key": true, "attributes": map[string]bool{"price_micros": true}}})
	if len(only.Data[0]) != 2 {
		t.Fatal("projection ignored")
	}
	if json.Unmarshal(only.Data[0]["attributes"], &attrs) != nil || len(attrs) != 1 || attrs[0].Name != "price_micros" {
		t.Fatal("attribute projection ignored")
	}
	setFaults(t, f, Faults{Arkiv: "changed_payload"})
	current := q("*", map[string]any{"select": projection})
	if payloadValue(current.Data[0]) == originalPayload {
		t.Fatal("latest query retained the old payload")
	}
	requireError(t, rpc(t, h, "arkiv_query", "*", map[string]any{"select": projection, "atBlock": head}))
	if err := f.AdvanceBlocks(0, 300); err != nil {
		t.Fatal(err)
	}
	if len(q("*", nil).Data) != 0 {
		t.Fatal("native expiry did not follow the current block")
	}
	if len(q("$key = key("+f.OfferKey()+")", map[string]any{"select": projection}).Data) != 0 {
		t.Fatal("expired entity returned by its key")
	}
	requireError(t, rpc(t, h, "arkiv_getEntity", f.OfferKey(), head))
	requireError(t, rpc(t, h, "arkiv_createEntity", map[string]string{"payload": "0x"}))
}
