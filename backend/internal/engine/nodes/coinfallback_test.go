package nodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// backups stands up CoinGecko (refusing), Coinbase, the Coinbase exchange
// and CoinPaprika. Coinbase knows only the pairs in spot; CoinPaprika
// answers any known id with the quotes in paprika.
type backups struct {
	coinbaseHits, paprikaHits, candleHits atomic.Int64
}

// OfflinePriceBackup is a base URL nothing listens on (the discard port).
// Tests restore the price backups to it rather than to the real APIs.
const OfflinePriceBackup = "http://127.0.0.1:9"

func restoreOfflineBackups() {
	SetPriceFallbackBasesForTest(OfflinePriceBackup, OfflinePriceBackup, OfflinePriceBackup)
}

func refusingCoinGecko(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"status":{"error_code":429}}`, http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	SetURLValidatorForTest(func(string) error { return nil })
	t.Cleanup(func() { SetURLValidatorForTest(func(string) error { return nil }) })
	SetCoinGeckoAPIBaseForTest(srv.URL)
	t.Cleanup(func() { SetCoinGeckoAPIBaseForTest("") })
}

func standUpBackups(t *testing.T, spot map[string]string, paprika map[string]map[string]float64, candles string) *backups {
	t.Helper()
	b := &backups{}
	cb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.coinbaseHits.Add(1)
		pair := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/prices/"), "/spot")
		amount, ok := spot[pair]
		if !ok {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			return
		}
		base, cur, _ := strings.Cut(pair, "-")
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"amount": amount, "base": base, "currency": cur}})
	}))
	ex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.candleHits.Add(1)
		if candles == "" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(candles))
	}))
	pp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.paprikaHits.Add(1)
		id := strings.TrimPrefix(r.URL.Path, "/tickers/")
		quotes, ok := paprika[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		out := map[string]any{}
		for cur, p := range quotes {
			out[cur] = map[string]any{"price": p}
		}
		json.NewEncoder(w).Encode(map[string]any{"id": id, "quotes": out})
	}))
	t.Cleanup(cb.Close)
	t.Cleanup(ex.Close)
	t.Cleanup(pp.Close)
	SetPriceFallbackBasesForTest(cb.URL, ex.URL, pp.URL)
	t.Cleanup(restoreOfflineBackups)
	return b
}

func spotNode(ids, currencies string) models.WorkflowNode {
	cfg := map[string]string{"cgIDs": ids}
	if currencies != "" {
		cfg["cgCurrencies"] = currencies
	}
	return models.WorkflowNode{Type: models.NodeTypeAction, Template: "coingecko", Config: cfg}
}

// The live failure: CoinGecko answered 429. The price still arrives, in the
// shape the workflow already reads, and says where it came from.
func TestSpotPriceFallsBackToCoinbase(t *testing.T) {
	refusingCoinGecko(t)
	standUpBackups(t, map[string]string{"ALGO-USD": "0.089365"}, nil, "")
	out, err := fetchCoinGecko(context.Background(), spotNode("algorand", ""), emptyRunContext{})
	if err != nil {
		t.Fatalf("fetchCoinGecko: %v", err)
	}
	m := out.(map[string]any)
	algo, _ := m["algorand"].(map[string]any)
	if algo["usd"] != 0.089365 {
		t.Errorf("algorand.usd = %v, want 0.089365", algo["usd"])
	}
	if m["source"] != "coinbase" {
		t.Errorf("source = %v, want coinbase", m["source"])
	}
	if !strings.Contains(m["note"].(string), "CoinGecko was unavailable") {
		t.Errorf("note = %v", m["note"])
	}
	// The jsonPath a builder wrote for CoinGecko still reads it.
	if v, err := walkPath(out, "algorand.usd"); err != nil || v != 0.089365 {
		t.Errorf("algorand.usd via walkPath = %v, %v", v, err)
	}
}

// Coinbase has no INR pair; CoinPaprika fills it in, and both are named.
func TestSpotPriceUsesCoinPaprikaForWhatCoinbaseLacks(t *testing.T) {
	refusingCoinGecko(t)
	standUpBackups(t,
		map[string]string{"ALGO-USD": "0.0894"},
		map[string]map[string]float64{"algo-algorand": {"INR": 7.45}},
		"")
	out, err := fetchCoinGecko(context.Background(), spotNode("algorand", "usd,inr"), emptyRunContext{})
	if err != nil {
		t.Fatalf("fetchCoinGecko: %v", err)
	}
	m := out.(map[string]any)
	algo := m["algorand"].(map[string]any)
	if algo["usd"] != 0.0894 || algo["inr"] != 7.45 {
		t.Errorf("algorand = %v", algo)
	}
	if m["source"] != "coinbase+coinpaprika" {
		t.Errorf("source = %v", m["source"])
	}
}

// A coin outside the verified table is never priced by guessing its ticker:
// two coins can share one, and pricing the wrong asset is the failure
// resolve_coin exists to stop. It is listed as unavailable.
func TestSpotPriceFallbackNamesWhatItCouldNotPrice(t *testing.T) {
	refusingCoinGecko(t)
	b := standUpBackups(t, map[string]string{"ALGO-USD": "0.0894", "MYRAD-USD": "1"}, nil, "")
	out, err := fetchCoinGecko(context.Background(), spotNode("algorand,myrad", ""), emptyRunContext{})
	if err != nil {
		t.Fatalf("fetchCoinGecko: %v", err)
	}
	m := out.(map[string]any)
	if _, ok := m["myrad"]; ok {
		t.Error("an unverified coin was priced from a guessed ticker")
	}
	if got, _ := m["unavailable"].([]string); len(got) != 1 || got[0] != "myrad" {
		t.Errorf("unavailable = %v, want [myrad]", m["unavailable"])
	}
	if b.coinbaseHits.Load() != 1 {
		t.Errorf("coinbase was asked %d times, want once (ALGO only)", b.coinbaseHits.Load())
	}
}

// Nothing priceable: CoinGecko's own error stands, rather than an empty
// success.
func TestSpotPriceKeepsTheOriginalErrorWhenNothingCanBePriced(t *testing.T) {
	refusingCoinGecko(t)
	standUpBackups(t, nil, nil, "")
	_, err := fetchCoinGecko(context.Background(), spotNode("myrad", ""), emptyRunContext{})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("want CoinGecko's 429, got %v", err)
	}
	_, err = fetchCoinGecko(context.Background(), spotNode("algorand", ""), emptyRunContext{})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("with every backup down, want CoinGecko's 429, got %v", err)
	}
}

// A backup answering for some other pair is not trusted.
func TestCoinbaseSpotRejectsAnAnswerForAnotherPair(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"amount":"100","base":"BTC","currency":"USD"}}`))
	}))
	defer srv.Close()
	SetURLValidatorForTest(func(string) error { return nil })
	defer SetURLValidatorForTest(func(string) error { return nil })
	SetPriceFallbackBasesForTest(srv.URL, "", "")
	defer restoreOfflineBackups()
	if _, err := coinbaseSpot(context.Background(), "ALGO", "usd"); err == nil {
		t.Fatal("a BTC price was accepted as ALGO")
	}
}

// A healthy CoinGecko is never second-guessed and the backups are not asked.
func TestSpotPriceDoesNotTouchTheBackupsWhenCoinGeckoAnswers(t *testing.T) {
	var status atomic.Int32
	cgStub(t, &status, 0)
	b := standUpBackups(t, map[string]string{"ALGO-USD": "999"}, nil, "")
	out, err := fetchCoinGecko(context.Background(), spotNode("algorand", ""), emptyRunContext{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.(map[string]any)["source"]; ok {
		t.Error("a CoinGecko answer was labelled as a backup")
	}
	if b.coinbaseHits.Load()+b.paprikaHits.Load() != 0 {
		t.Error("the backups were called although CoinGecko answered")
	}
}

// Price history from Coinbase candles, in the same summary shape, oldest
// first, using each candle's close.
func TestPriceHistoryFallsBackToCoinbaseCandles(t *testing.T) {
	refusingCoinGecko(t)
	// Newest first, as Coinbase sends them: [time, low, high, open, close, volume].
	standUpBackups(t, nil, nil, `[[1789603200,0.08,0.10,0.09,0.0896,1],[1789516800,0.08,0.09,0.088,0.0889,1],[1789430400,0.08,0.099,0.096,0.0887,1]]`)
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "coingecko_history",
		Config: map[string]string{"cgID": "algorand", "cgDays": "3"}}
	out, err := fetchCoinGeckoHistory(context.Background(), node, emptyRunContext{})
	if err != nil {
		t.Fatalf("fetchCoinGeckoHistory: %v", err)
	}
	m := out.(map[string]any)
	if m["first"] != 0.0887 || m["last"] != 0.0896 {
		t.Errorf("first/last = %v/%v, want the oldest and newest closes", m["first"], m["last"])
	}
	if m["high"] != 0.0896 || m["low"] != 0.0887 {
		t.Errorf("high/low = %v/%v", m["high"], m["low"])
	}
	if m["source"] != "coinbase" {
		t.Errorf("source = %v", m["source"])
	}
	pts := m["points"].([]map[string]any)
	if len(pts) != 3 || pts[0]["time"] != time.Unix(1789430400, 0).UTC().Format(time.RFC3339) {
		t.Errorf("points = %v", pts)
	}
}

// Hourly candles cover 12 days in one request, daily ones 300. Outside that
// (or "max") the backup declines and CoinGecko's error stands.
func TestHistoryFallbackPicksTheGranularityAndDeclinesWhatItCannotCover(t *testing.T) {
	var gotQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery.Store(r.URL.RawQuery)
		w.Write([]byte(`[[1789603200,0.08,0.10,0.09,0.0896,1]]`))
	}))
	defer srv.Close()
	SetURLValidatorForTest(func(string) error { return nil })
	defer SetURLValidatorForTest(func(string) error { return nil })
	SetPriceFallbackBasesForTest("", srv.URL, "")
	defer restoreOfflineBackups()
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	if _, ok := historyFallback(context.Background(), "algorand", "usd", "7", now); !ok {
		t.Fatal("7 days was declined")
	}
	if q := gotQuery.Load().(string); !strings.Contains(q, "granularity=3600") {
		t.Errorf("7 days asked for %q, want hourly", q)
	}
	if _, ok := historyFallback(context.Background(), "algorand", "usd", "90", now); !ok {
		t.Fatal("90 days was declined")
	}
	if q := gotQuery.Load().(string); !strings.Contains(q, "granularity=86400") {
		t.Errorf("90 days asked for %q, want daily", q)
	}
	for _, days := range []string{"max", "365", "0"} {
		if _, ok := historyFallback(context.Background(), "algorand", "usd", days, now); ok {
			t.Errorf("days=%s was accepted", days)
		}
	}
	if _, ok := historyFallback(context.Background(), "myrad", "usd", "7", now); ok {
		t.Error("an unverified coin was served")
	}
}
