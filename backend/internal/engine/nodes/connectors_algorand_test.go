package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// These tests live in package nodes to reach the unexported fetcher, so they
// use emptyRunContext rather than engine.NewRunContext: engine imports nodes,
// and an in-package test importing engine would be an import cycle.

// algodStub serves one account body verbatim (raw, so numbers past 2^53
// reach the connector exactly as algod would send them) and answers
// /v2/assets/{id} from assets, 404 for any id it does not know. It counts
// the asset lookups it received.
func algodStub(t *testing.T, account string, assets map[string]string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var lookups atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/accounts/"):
			w.Write([]byte(account))
		case strings.HasPrefix(r.URL.Path, "/v2/assets/"):
			lookups.Add(1)
			body, ok := assets[strings.TrimPrefix(r.URL.Path, "/v2/assets/")]
			if !ok {
				http.Error(w, `{"message":"asset does not exist"}`, http.StatusNotFound)
				return
			}
			w.Write([]byte(body))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	SetAlgorandBases(srv.URL)
	t.Cleanup(func() { SetAlgorandBases("") })
	return srv, &lookups
}

func readAlgorandAccount(t *testing.T) map[string]any {
	t.Helper()
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_account",
		Config: map[string]string{"algoAddress": "ABCDEF"}}
	out, err := fetchAlgorandAccount(context.Background(), node, emptyRunContext{})
	if err != nil {
		t.Fatalf("fetchAlgorandAccount: %v", err)
	}
	return out.(map[string]any)
}

func TestAlgorandAccountFlattensTheHoldings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/accounts/ABCDEF":
			json.NewEncoder(w).Encode(map[string]any{
				"address":     "ABCDEF",
				"amount":      2500000,
				"min-balance": 100000,
				"assets":      []map[string]any{{"asset-id": 31566704, "amount": 1500000}},
			})
		case "/v2/assets/31566704":
			json.NewEncoder(w).Encode(map[string]any{"index": 31566704, "params": map[string]any{
				"decimals": 6, "unit-name": "USDC", "name": "USDC",
			}})
		default:
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	SetAlgorandBases(srv.URL)
	defer SetAlgorandBases("")

	m := readAlgorandAccount(t)
	// Whole ALGO, not microalgos: every agent that has ever been handed
	// microalgos has reported them as ALGO. The exact integer travels beside
	// it, the same shape every asset holding uses.
	if m["algo"] != "2.5" {
		t.Errorf("algo = %#v, want \"2.5\"", m["algo"])
	}
	if m["algoMicro"] != uint64(2500000) {
		t.Errorf("algoMicro = %#v, want 2500000", m["algoMicro"])
	}
	if m["minBalance"] != "0.1" {
		t.Errorf("minBalance = %#v, want \"0.1\"", m["minBalance"])
	}
	if m["minBalanceMicro"] != uint64(100000) {
		t.Errorf("minBalanceMicro = %#v, want 100000", m["minBalanceMicro"])
	}
	if m["address"] != "ABCDEF" {
		t.Errorf("address = %v, want ABCDEF", m["address"])
	}
	if m["unresolvedAssets"] != 0 {
		t.Errorf("unresolvedAssets = %v, want 0", m["unresolvedAssets"])
	}
	assets, ok := m["assets"].([]map[string]any)
	if !ok || len(assets) != 1 {
		t.Fatalf("assets = %#v", m["assets"])
	}
	want := map[string]any{
		"assetId": uint64(31566704), "amountBaseUnits": uint64(1500000),
		"decimals": uint64(6), "unitName": "USDC", "name": "USDC",
		// 1.5 USDC, not 1,500,000 USDC.
		"amount": "1.5",
	}
	for k, v := range want {
		if assets[0][k] != v {
			t.Errorf("asset[%s] = %#v, want %#v", k, assets[0][k], v)
		}
	}
}

// algod amounts are uint64. Decoding through float64 rounds 2^53+1 down and
// fails outright on anything past the int64 range, so both must come back
// exact.
func TestAlgorandAccountKeepsLargeAmountsExact(t *testing.T) {
	algodStub(t, `{"address":"ABCDEF","amount":0,"min-balance":100000,"assets":[
		{"asset-id":1,"amount":18446744073709551615},
		{"asset-id":2,"amount":9007199254740993}]}`,
		map[string]string{
			"1": `{"index":1,"params":{"decimals":0,"unit-name":"BIG","name":"Big"}}`,
			"2": `{"index":2,"params":{"decimals":0,"unit-name":"ODD","name":"Odd"}}`,
		})
	assets := readAlgorandAccount(t)["assets"].([]map[string]any)
	if len(assets) != 2 {
		t.Fatalf("got %d assets", len(assets))
	}
	if got := assets[0]["amountBaseUnits"]; got != uint64(18446744073709551615) {
		t.Errorf("max uint64 came back as %v", got)
	}
	if got := assets[0]["amount"]; got != "18446744073709551615" {
		t.Errorf("max uint64 amount = %v", got)
	}
	if got := assets[1]["amountBaseUnits"]; got != uint64(9007199254740993) {
		t.Errorf("2^53+1 came back as %v", got)
	}
	if got := assets[1]["amount"]; got != "9007199254740993" {
		t.Errorf("2^53+1 amount = %v", got)
	}
}

// The headline balance is the field most likely to be on screen, and a whale
// or exchange wallet is past what float64 holds exactly: ALGO supply is 10^16
// microalgos and float64 stops being exact at about 9.007e15.
func TestAlgorandAccountKeepsALargeAlgoBalanceExact(t *testing.T) {
	algodStub(t, `{"address":"ABCDEF","amount":9007199254740993,"min-balance":0,"assets":[]}`, nil)
	m := readAlgorandAccount(t)
	// 9007199254740993 microalgos is 9,007,199,254.740993 ALGO.
	if m["algo"] != "9007199254.740993" {
		t.Errorf("algo = %#v, want \"9007199254.740993\"", m["algo"])
	}
	if m["algoMicro"] != uint64(9007199254740993) {
		t.Errorf("algoMicro = %#v, want 9007199254740993", m["algoMicro"])
	}
	if m["minBalance"] != "0" {
		t.Errorf("minBalance = %#v, want \"0\"", m["minBalance"])
	}
	if m["minBalanceMicro"] != uint64(0) {
		t.Errorf("minBalanceMicro = %#v, want 0", m["minBalanceMicro"])
	}
}

// A lookup that fails must leave that holding in base units only, count it,
// and not cost the caller the rest of the read.
func TestAlgorandAccountSurvivesAFailedAssetLookup(t *testing.T) {
	algodStub(t, `{"address":"ABCDEF","amount":1000000,"min-balance":100000,"assets":[
		{"asset-id":7,"amount":42},
		{"asset-id":8,"amount":1500000}]}`,
		map[string]string{
			"8": `{"index":8,"params":{"decimals":6,"unit-name":"USDC","name":"USDC"}}`,
		})
	m := readAlgorandAccount(t)
	if m["unresolvedAssets"] != 1 {
		t.Errorf("unresolvedAssets = %v, want 1", m["unresolvedAssets"])
	}
	assets := m["assets"].([]map[string]any)
	if len(assets) != 2 {
		t.Fatalf("got %d assets", len(assets))
	}
	if assets[0]["amountBaseUnits"] != uint64(42) {
		t.Errorf("unresolved asset lost its base units: %#v", assets[0])
	}
	for _, k := range []string{"amount", "decimals", "unitName", "name"} {
		if _, ok := assets[0][k]; ok {
			t.Errorf("unresolved asset carries %q: %#v", k, assets[0])
		}
	}
	if assets[1]["amount"] != "1.5" {
		t.Errorf("resolved asset amount = %v, want 1.5", assets[1]["amount"])
	}
}

// Past the cap, holdings are listed but not looked up, and the count says so.
func TestAlgorandAccountResolvesAtMostTheCap(t *testing.T) {
	const holdings = 25
	var parts []string
	lookup := map[string]string{}
	for i := 1; i <= holdings; i++ {
		parts = append(parts, fmt.Sprintf(`{"asset-id":%d,"amount":%d}`, i, i))
		lookup[fmt.Sprint(i)] = `{"params":{"decimals":0,"unit-name":"T","name":"T"}}`
	}
	_, lookups := algodStub(t, `{"address":"ABCDEF","amount":0,"min-balance":0,"assets":[`+strings.Join(parts, ",")+`]}`, lookup)

	m := readAlgorandAccount(t)
	if got := lookups.Load(); got != maxAlgorandAssetLookups {
		t.Errorf("made %d asset lookups, want %d", got, maxAlgorandAssetLookups)
	}
	if m["unresolvedAssets"] != holdings-maxAlgorandAssetLookups {
		t.Errorf("unresolvedAssets = %v, want %d", m["unresolvedAssets"], holdings-maxAlgorandAssetLookups)
	}
	assets := m["assets"].([]map[string]any)
	if len(assets) != holdings {
		t.Fatalf("got %d assets, want all %d listed", len(assets), holdings)
	}
	resolved := 0
	for _, a := range assets {
		if _, ok := a["amount"]; ok {
			resolved++
		}
	}
	if resolved != maxAlgorandAssetLookups {
		t.Errorf("resolved %d holdings, want exactly %d", resolved, maxAlgorandAssetLookups)
	}
}

func TestFormatBaseUnits(t *testing.T) {
	tests := []struct {
		v        uint64
		decimals uint64
		want     string
	}{
		{1500000, 6, "1.5"},
		{1, 6, "0.000001"},
		{0, 6, "0"},
		{100, 2, "1"},
		{123, 0, "123"},
		{1000000, 6, "1"},
		{18446744073709551615, 19, "1.8446744073709551615"},
		{5, 19, "0.0000000000000000005"},
	}
	for _, tt := range tests {
		if got := formatBaseUnits(tt.v, tt.decimals); got != tt.want {
			t.Errorf("formatBaseUnits(%d, %d) = %q, want %q", tt.v, tt.decimals, got, tt.want)
		}
	}
}

// A fresh wallet has no assets. This must be an empty list, never nil: nil
// marshals to null and an agent describes null as "unknown" rather than
// "none".
func TestAlgorandAccountWithNoAssetsReturnsAnEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"address": "ABCDEF", "amount": 0, "min-balance": 100000})
	}))
	defer srv.Close()
	SetAlgorandBases(srv.URL)
	defer SetAlgorandBases("")

	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_account",
		Config: map[string]string{"algoAddress": "ABCDEF"}}
	out, err := fetchAlgorandAccount(context.Background(), node, emptyRunContext{})
	if err != nil {
		t.Fatalf("fetchAlgorandAccount: %v", err)
	}
	assets, ok := out.(map[string]any)["assets"].([]map[string]any)
	if !ok {
		t.Fatalf("assets is %T, want a slice", out.(map[string]any)["assets"])
	}
	if assets == nil {
		t.Error("assets is nil; it must be an empty slice so it marshals to [] not null")
	}
	if len(assets) != 0 {
		t.Errorf("got %d assets", len(assets))
	}
}

func TestAlgorandAccountSkipsWithoutAnAddress(t *testing.T) {
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_account"}
	out, err := fetchAlgorandAccount(context.Background(), node, emptyRunContext{})
	if !errors.Is(err, ErrActionSkipped) {
		t.Fatalf("err = %v, want ErrActionSkipped", err)
	}
	if out != "algorand_skipped_no_address" {
		t.Errorf("skip code = %v", out)
	}
}

func TestAlgorandAccountFailsClosedWithNoAlgodConfigured(t *testing.T) {
	SetAlgorandBases("")
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_account",
		Config: map[string]string{"algoAddress": "ABCDEF"}}
	_, err := fetchAlgorandAccount(context.Background(), node, emptyRunContext{})
	if err == nil {
		t.Fatal("no error with algod unconfigured")
	}
	if !strings.Contains(err.Error(), "ALGOD_URL") {
		t.Errorf("the error does not name the setting, got: %v", err)
	}
}
