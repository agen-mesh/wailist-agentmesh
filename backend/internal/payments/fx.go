package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func fetchINRToUSDFromURL(ctx context.Context, url string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := fxHTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fx: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fx: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	var parsed struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, fmt.Errorf("fx: parse response: %w", err)
	}
	rate, ok := parsed.Rates["USD"]
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
