package payments

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func serveJSON(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A USD-based response must have USD == 1. Anything else means the upstream
// changed base or inverted its rates, and every derived figure would be wrong in
// a way no per-currency bound would catch — so it is rejected outright.
func TestFetchRateTableRejectsNonUSDBase(t *testing.T) {
	cases := map[string]string{
		"USD missing":  `{"rates":{"EUR":0.86,"INR":95.25}}`,
		"USD inverted": `{"rates":{"USD":0.0105,"EUR":0.0090}}`,
		"USD zero":     `{"rates":{"USD":0,"EUR":0.86}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := serveJSON(t, body)
			if _, err := LiveFetchRateTableForTest(context.Background(), srv.URL); err == nil {
				t.Fatal("want an error for a non-USD-based response, got nil")
			}
		})
	}
}

// Non-finite and non-positive rates are dropped rather than passed through,
// because they reach a division on the frontend and render NaN or Infinity.
func TestFetchRateTableDropsUnusableRates(t *testing.T) {
	srv := serveJSON(t, `{"rates":{"USD":1,"EUR":0.86,"BAD":0,"WORSE":-3}}`)

	rates, err := LiveFetchRateTableForTest(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rates["BAD"]; ok {
		t.Error("a zero rate should have been dropped")
	}
	if _, ok := rates["WORSE"]; ok {
		t.Error("a negative rate should have been dropped")
	}
	if rates["EUR"] != 0.86 {
		t.Errorf("want EUR 0.86, got %v", rates["EUR"])
	}
}

func TestFetchRateTableRejectsMalformedBody(t *testing.T) {
	srv := serveJSON(t, `{"rates":`)
	if _, err := LiveFetchRateTableForTest(context.Background(), srv.URL); err == nil {
		t.Fatal("want a parse error, got nil")
	}
}

func TestFetchRateTableRejectsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	if _, err := LiveFetchRateTableForTest(context.Background(), srv.URL); err == nil {
		t.Fatal("want an error on a 500, got nil")
	}
}

// The response is narrowed to the codes the UI can actually select, so the
// endpoint never ships 160-odd rates a client has no use for.
func TestFetchRateTableReturnsOnlyRequestedCurrencies(t *testing.T) {
	SetFetchRateTableForTest(func(context.Context) (map[string]float64, error) {
		return map[string]float64{"USD": 1, "EUR": 0.86, "INR": 95.25, "ZWL": 322}, nil
	})
	t.Cleanup(func() { SetFetchRateTableForTest(nil) })

	table, err := FetchRateTable(context.Background(), []string{"USD", "EUR"})
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Rates) != 2 {
		t.Fatalf("want 2 rates, got %d: %v", len(table.Rates), table.Rates)
	}
	if _, ok := table.Rates["ZWL"]; ok {
		t.Error("unrequested currency leaked into the response")
	}
	if table.Base != "USD" {
		t.Errorf("want base USD, got %q", table.Base)
	}
	if table.FetchedAt.IsZero() {
		t.Error("fetchedAt should be set so clients can judge staleness")
	}
}

// A second call inside the TTL must not hit the upstream again — the source
// publishes once a day, so re-fetching is pure waste.
func TestFetchRateTableCachesWithinTTL(t *testing.T) {
	calls := 0
	SetFetchRateTableForTest(func(context.Context) (map[string]float64, error) {
		calls++
		return map[string]float64{"USD": 1, "EUR": 0.86}, nil
	})
	t.Cleanup(func() { SetFetchRateTableForTest(nil) })

	for i := 0; i < 3; i++ {
		if _, err := FetchRateTable(context.Background(), []string{"EUR"}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("want 1 upstream call across 3 requests, got %d", calls)
	}
}

// Display tolerates a stale rate; it does not tolerate a blank screen. Once a
// good table exists, an upstream outage serves it rather than failing.
func TestFetchRateTableServesStaleOnUpstreamFailure(t *testing.T) {
	fail := false
	SetFetchRateTableForTest(func(context.Context) (map[string]float64, error) {
		if fail {
			return nil, errors.New("upstream down")
		}
		return map[string]float64{"USD": 1, "EUR": 0.86}, nil
	})
	t.Cleanup(func() { SetFetchRateTableForTest(nil) })

	if _, err := FetchRateTable(context.Background(), []string{"EUR"}); err != nil {
		t.Fatal(err)
	}

	// Age the cache past its TTL so the next call is forced to re-fetch.
	rateTableMu.Lock()
	cachedRateTable.FetchedAt = time.Now().Add(-2 * rateTableTTL)
	rateTableMu.Unlock()

	fail = true
	table, err := FetchRateTable(context.Background(), []string{"EUR"})
	if err != nil {
		t.Fatalf("want the stale table served, got error %v", err)
	}
	if table.Rates["EUR"] != 0.86 {
		t.Errorf("want the last good rate, got %v", table.Rates["EUR"])
	}
}

// A cold cache under concurrent load must produce one upstream call, not one
// per caller. Before the fetch lock this measured 20-for-20 against a free API
// that publishes once a day — the exact shape of request that gets an app
// rate-limited.
func TestFetchRateTableCollapsesConcurrentColdFetches(t *testing.T) {
	var calls int64
	SetFetchRateTableForTest(func(context.Context) (map[string]float64, error) {
		atomic.AddInt64(&calls, 1)
		// Long enough that every goroutine is genuinely in flight together;
		// without it the first call could finish before the others start and
		// the test would pass for the wrong reason.
		time.Sleep(50 * time.Millisecond)
		return map[string]float64{"USD": 1, "EUR": 0.86}, nil
	})
	t.Cleanup(func() { SetFetchRateTableForTest(nil) })

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := FetchRateTable(context.Background(), []string{"EUR"}); err != nil {
				t.Errorf("concurrent fetch: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("want 1 upstream call for 20 concurrent cold requests, got %d", got)
	}
}

// With no cache to fall back on there is nothing honest to show, so it errors
// rather than inventing a rate.
func TestFetchRateTableErrorsWhenNoCacheAndUpstreamFails(t *testing.T) {
	SetFetchRateTableForTest(func(context.Context) (map[string]float64, error) {
		return nil, errors.New("upstream down")
	})
	t.Cleanup(func() { SetFetchRateTableForTest(nil) })

	if _, err := FetchRateTable(context.Background(), []string{"EUR"}); err == nil {
		t.Fatal("want an error with no cache and a failing upstream, got nil")
	}
}
