package nodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveCoinReturnsRankedMatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != "aerodrome" {
			t.Errorf("query = %q, want aerodrome", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"coins": []map[string]any{
			{"id": "aerodrome-finance", "symbol": "aero", "name": "Aerodrome Finance", "market_cap_rank": 102},
		}})
	}))
	defer srv.Close()
	SetCoinGeckoAPIBaseForTest(srv.URL)
	defer SetCoinGeckoAPIBaseForTest("")

	got, err := resolveCoin(context.Background(), "aerodrome")
	if err != nil {
		t.Fatalf("resolveCoin: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	if got[0].ID != "aerodrome-finance" || got[0].Symbol != "AERO" || got[0].Rank != 102 {
		t.Errorf("match = %+v", got[0])
	}
}

// The case that started this: a token nobody lists. An empty result is an
// answer, not a failure -- the builder has to be able to tell the user.
func TestResolveCoinOnAnUnlistedTokenReturnsNoMatchesAndNoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"coins": []any{}})
	}))
	defer srv.Close()
	SetCoinGeckoAPIBaseForTest(srv.URL)
	defer SetCoinGeckoAPIBaseForTest("")

	got, err := resolveCoin(context.Background(), "myriad")
	if err != nil {
		t.Fatalf("an unlisted token must not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d matches for an unlisted token", len(got))
	}
}

func TestResolveCoinRequiresAQuery(t *testing.T) {
	if _, err := resolveCoin(context.Background(), "   "); err == nil {
		t.Fatal("an empty query was accepted")
	}
}

// CoinGecko lists every coin whose name contains the query; only the first
// few go back to the model.
func TestResolveCoinCapsTheMatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		coins := make([]map[string]any, 0, 20)
		for i := 0; i < 20; i++ {
			coins = append(coins, map[string]any{"id": "coin-" + string(rune('a'+i)), "symbol": "c", "name": "Coin"})
		}
		json.NewEncoder(w).Encode(map[string]any{"coins": coins})
	}))
	defer srv.Close()
	SetCoinGeckoAPIBaseForTest(srv.URL)
	defer SetCoinGeckoAPIBaseForTest("")

	got, err := resolveCoin(context.Background(), "coin")
	if err != nil {
		t.Fatalf("resolveCoin: %v", err)
	}
	if len(got) != maxCoinMatches {
		t.Fatalf("got %d matches, want %d", len(got), maxCoinMatches)
	}
}

// resolve_coin through the build dispatcher: matches come back with their
// ids, and an empty result is reported as not_listed rather than an error.
func TestResolveCoinThroughTheBuildDispatcher(t *testing.T) {
	var coins []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"coins": coins})
	}))
	defer srv.Close()
	SetCoinGeckoAPIBaseForTest(srv.URL)
	defer SetCoinGeckoAPIBaseForTest("")

	call := func(q string) string {
		res := dispatchBuildCall(context.Background(), nil, geminiFuncCall{
			name: "resolve_coin", args: map[string]any{"query": q},
		}, "k", nil, nil, &testTracker{})
		text, _ := res["result"].(string)
		return text
	}

	coins = []map[string]any{{"id": "bitcoin", "symbol": "btc", "name": "Bitcoin", "market_cap_rank": 1}}
	if got := call("bitcoin"); !containsAll(got, `"id":"bitcoin"`, "use the id field") {
		t.Errorf("match result = %q", got)
	}

	coins = []map[string]any{}
	got := call("myriad")
	if !containsAll(got, "not_listed:", "myriad") {
		t.Errorf("unlisted result = %q", got)
	}
	step := finishedStep(nil, "resolve_coin", map[string]any{"query": "myriad"}, map[string]any{"result": got})
	if step.Status != "error" {
		t.Errorf("an unlisted token shows as %q, want error", step.Status)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
