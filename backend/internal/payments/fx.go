package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"
)

// Plausible bounds for 1 INR in USD. INR/USD has stayed within roughly 60-140 over any
// realistic window; these bounds (~7-200 INR/USD) are deliberately wide so routine market
// movement never trips them — they exist only to catch a broken/inverted upstream value
// before it gets locked into a ledger row and drives how much a user is credited.
const (
	minPlausibleINRToUSD = 0.005
	maxPlausibleINRToUSD = 0.15
)

var fxHTTPClient = &http.Client{Timeout: 5 * time.Second}

// fetchINRToUSD is swappable in tests via SetFetchRateForTest.
var fetchINRToUSD = liveFetchINRToUSD

const fxAPIURL = "https://open.er-api.com/v6/latest/INR"

func liveFetchINRToUSD(ctx context.Context) (float64, error) {
	return fetchINRToUSDFromURL(ctx, fxAPIURL)
}

// LiveFetchINRToUSDForTest exercises the real parsing/bounds-checking logic against an
// arbitrary URL (e.g. an httptest.Server). Call only from tests.
func LiveFetchINRToUSDForTest(ctx context.Context, url string) (float64, error) {
	return fetchINRToUSDFromURL(ctx, url)
}

// fetchRatesFromURL performs the GET both rate fetchers share: request, status
// check, a bounded read, and the one response shape this upstream returns.
//
// The two callers keep their own validation and their own caching strategy --
// those genuinely differ, and the comment below explains why they must stay
// apart. Only this boilerplate was duplicated, which meant a change to the
// timeout, size limit or parsing had to be made twice by hand.
//
// prefix keeps the callers' error messages distinct ("fx:" for the billing
// path, "fx table:" for display), so an incident log-grep can still tell which
// fetch failed.
func fetchRatesFromURL(ctx context.Context, url, prefix string) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", prefix, err)
	}
	resp, err := fxHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", prefix, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: unexpected status %d", prefix, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", prefix, err)
	}
	var parsed struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%s: parse response: %w", prefix, err)
	}
	return parsed.Rates, nil
}

func fetchINRToUSDFromURL(ctx context.Context, url string) (float64, error) {
	rates, err := fetchRatesFromURL(ctx, url, "fx")
	if err != nil {
		return 0, err
	}
	rate, ok := rates["USD"]
	if !ok || rate <= 0 {
		return 0, fmt.Errorf("fx: no USD rate in response")
	}
	if rate < minPlausibleINRToUSD || rate > maxPlausibleINRToUSD {
		return 0, fmt.Errorf("fx: rate %v outside plausible bounds [%v, %v]", rate, minPlausibleINRToUSD, maxPlausibleINRToUSD)
	}
	return rate, nil
}

// FetchINRToUSDRate returns the current conversion rate where 1 INR = rate USD.
func FetchINRToUSDRate(ctx context.Context) (float64, error) {
	return fetchINRToUSD(ctx)
}

// SetFetchRateForTest overrides the rate fetcher used by FetchINRToUSDRate. Pass nil to reset
// to the live implementation. Call only from tests.
func SetFetchRateForTest(fn func(context.Context) (float64, error)) {
	if fn == nil {
		fetchINRToUSD = liveFetchINRToUSD
	} else {
		fetchINRToUSD = fn
	}
}

// ---------------------------------------------------------------------------
// Display rate table
//
// Deliberately separate from FetchINRToUSDRate above, and NOT a refactor of it.
// That function locks a rate into a credit_ledger row, so it must stay uncached
// and fail closed — a stale rate there is a real financial error. This one only
// decides what number a user reads on screen, where a rate a few hours old is
// fine and an outage should degrade rather than break.
// ---------------------------------------------------------------------------

const fxTableAPIURL = "https://open.er-api.com/v6/latest/USD"

// rateTableTTL matches the upstream's publishing cadence: open.er-api.com
// refreshes once a day (its own time_next_update_utc is ~24h out), so anything
// shorter re-fetches identical bytes. Half a day keeps us close to each refresh
// without hammering a free endpoint.
const rateTableTTL = 12 * time.Hour

// RateTable is a USD-based snapshot of the currencies the UI can render.
type RateTable struct {
	Base      string             `json:"base"`
	Rates     map[string]float64 `json:"rates"`
	FetchedAt time.Time          `json:"fetchedAt"`
}

var (
	rateTableMu     sync.RWMutex
	cachedRateTable *RateTable
	// Held across the upstream call so a cold cache produces one request, not
	// one per waiting caller. Never taken while holding rateTableMu.
	rateFetchMu sync.Mutex
)

var fetchRateTable = liveFetchRateTable

// SetFetchRateTableForTest overrides the table fetcher and clears the cache.
// Pass nil to reset to the live implementation. Call only from tests.
func SetFetchRateTableForTest(fn func(context.Context) (map[string]float64, error)) {
	rateTableMu.Lock()
	defer rateTableMu.Unlock()
	cachedRateTable = nil
	if fn == nil {
		fetchRateTable = liveFetchRateTable
	} else {
		fetchRateTable = fn
	}
}

// currentFetcher reads the swappable fetcher under the same lock that guards
// writes to it, so the test seam cannot race a concurrent FetchRateTable.
func currentFetcher() func(context.Context) (map[string]float64, error) {
	rateTableMu.RLock()
	defer rateTableMu.RUnlock()
	return fetchRateTable
}

// freshTable returns the cached table when it is still within its TTL.
func freshTable() *RateTable {
	rateTableMu.RLock()
	defer rateTableMu.RUnlock()
	if cachedRateTable != nil && time.Since(cachedRateTable.FetchedAt) < rateTableTTL {
		return cachedRateTable
	}
	return nil
}

func staleTable() *RateTable {
	rateTableMu.RLock()
	defer rateTableMu.RUnlock()
	return cachedRateTable
}

// FetchRateTable returns rates for `wanted`, keyed by currency code, with USD as
// the base. Cached for rateTableTTL.
//
// On an upstream failure it serves the last good table however old it is, and
// only errors when there has never been one. Showing a slightly stale rate beats
// showing nothing; showing a rate from an unknown source would be worse than
// both, which is why there is no hardcoded fallback here.
func FetchRateTable(ctx context.Context, wanted []string) (RateTable, error) {
	if cached := freshTable(); cached != nil {
		return subsetTable(*cached, wanted), nil
	}

	// One fetch per cold cache, not one per caller. Without this, N concurrent
	// requests on an empty or expired cache each hit the upstream — measurably
	// 20-for-20 in testing — against a free API that publishes once a day.
	rateFetchMu.Lock()
	defer rateFetchMu.Unlock()

	// Another goroutine may have refreshed the cache while we waited for the
	// lock, in which case there is nothing left to do.
	if cached := freshTable(); cached != nil {
		return subsetTable(*cached, wanted), nil
	}

	rates, err := currentFetcher()(ctx)
	if err != nil {
		if cached := staleTable(); cached != nil {
			return subsetTable(*cached, wanted), nil
		}
		return RateTable{}, err
	}

	fresh := RateTable{Base: "USD", Rates: rates, FetchedAt: time.Now().UTC()}
	rateTableMu.Lock()
	cachedRateTable = &fresh
	rateTableMu.Unlock()

	return subsetTable(fresh, wanted), nil
}

// subsetTable narrows a full upstream response to the codes the UI offers, so
// the endpoint never ships 160-odd rates a client cannot select.
func subsetTable(full RateTable, wanted []string) RateTable {
	out := RateTable{Base: full.Base, FetchedAt: full.FetchedAt, Rates: make(map[string]float64, len(wanted))}
	for _, code := range wanted {
		if r, ok := full.Rates[code]; ok {
			out.Rates[code] = r
		}
	}
	return out
}

func liveFetchRateTable(ctx context.Context) (map[string]float64, error) {
	return fetchRateTableFromURL(ctx, fxTableAPIURL)
}

// LiveFetchRateTableForTest exercises the real parsing and validation against an
// arbitrary URL (e.g. an httptest.Server). Call only from tests.
func LiveFetchRateTableForTest(ctx context.Context, url string) (map[string]float64, error) {
	return fetchRateTableFromURL(ctx, url)
}

func fetchRateTableFromURL(ctx context.Context, url string) (map[string]float64, error) {
	rates, err := fetchRatesFromURL(ctx, url, "fx table")
	if err != nil {
		return nil, err
	}
	// USD must be exactly 1 in a USD-based response. If it isn't, the upstream
	// changed base or inverted its rates, and every number derived from this
	// table would be wrong in a way no per-currency bound would catch.
	if usd, ok := rates["USD"]; !ok || usd != 1 {
		return nil, fmt.Errorf("fx table: response is not USD-based (USD=%v)", rates["USD"])
	}
	// Drop anything non-positive or non-finite rather than letting it reach a
	// division and render NaN or Infinity on screen.
	clean := make(map[string]float64, len(rates))
	for code, rate := range rates {
		if rate > 0 && !math.IsInf(rate, 0) && !math.IsNaN(rate) {
			clean[code] = rate
		}
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("fx table: no usable rates in response")
	}
	return clean, nil
}

// --- Cached rate -----------------------------------------------------------

// The rate is consulted on every /payments/providers request, which the
// billing page and checkout panel both hit on mount. Fetching live each time
// put a third-party host on the critical path of a page load, and made a brief
// outage at that host disable Stripe, PayPal and NOWPayments simultaneously on
// a fully configured deployment.
//
// So: serve from cache for fxCacheTTL, and on a failed refresh keep serving
// the last good rate rather than reporting none. A slightly stale rate is a
// far better failure than "all USD gateways are Coming soon". Only a cold
// cache (no rate ever fetched) can still fail.
const (
	fxCacheTTL = 10 * time.Minute
	// How long a stale rate may still be served after refreshes start
	// failing. Beyond this the rate is too old to price a real charge with,
	// and reporting no rate is the honest answer.
	fxStaleGrace = 24 * time.Hour
)

var fxCache struct {
	sync.Mutex
	rate      float64
	fetchedAt time.Time
}

// CachedINRToUSDRate returns the INR->USD rate, refreshing at most once per
// fxCacheTTL. On a refresh failure it falls back to the last good value while
// that value is younger than fxStaleGrace, so a transient upstream outage does
// not take the USD checkout options offline.
//
// The returned bool reports whether the value is stale (a refresh failed and
// this is the fallback), so callers can log it without having to guess.
func CachedINRToUSDRate(ctx context.Context) (rate float64, stale bool, err error) {
	fxCache.Lock()
	defer fxCache.Unlock()

	if fxCache.rate > 0 && time.Since(fxCache.fetchedAt) < fxCacheTTL {
		return fxCache.rate, false, nil
	}

	// Not ctx: this fills a cache shared by every concurrent caller, so one
	// caller's request being cancelled must not fail the refresh for the
	// others waiting on the same lock. fxHTTPClient's own 5s timeout still
	// bounds the call.
	fresh, err := fetchINRToUSD(context.Background())
	if err == nil {
		fxCache.rate = fresh
		fxCache.fetchedAt = time.Now()
		return fresh, false, nil
	}

	if fxCache.rate > 0 && time.Since(fxCache.fetchedAt) < fxStaleGrace {
		return fxCache.rate, true, nil
	}
	return 0, false, err
}

// ResetFXCacheForTest clears the cached rate. Call only from tests.
func ResetFXCacheForTest() {
	fxCache.Lock()
	defer fxCache.Unlock()
	fxCache.rate, fxCache.fetchedAt = 0, time.Time{}
}
