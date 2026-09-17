package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Backup price sources for when CoinGecko refuses.
//
// The keyless CoinGecko API is limited per calling IP and this whole server
// is one IP, so the cache in coingeckocache.go cuts repeat calls but cannot
// stop the first one being refused. When it is, spot prices come from
// Coinbase (a real exchange price) and then CoinPaprika, and price history
// from Coinbase's public candles. Every answer that did not come from
// CoinGecko says where it came from.
//
// Only coins in knownCoins are covered. A backup that guessed the symbol for
// an arbitrary CoinGecko id would price the wrong asset whenever two coins
// share a ticker, which is exactly the substitution resolve_coin exists to
// stop. A coin outside the table keeps CoinGecko's own error.

// knownCoin is one coin's identity on each backup. All of these were checked
// live against CoinGecko, Coinbase and CoinPaprika on 2026-09-17.
type knownCoin struct {
	// Symbol is the Coinbase base currency, e.g. ALGO in ALGO-USD.
	Symbol string
	// Paprika is the CoinPaprika coin id.
	Paprika string
}

// knownCoins is keyed by CoinGecko id.
var knownCoins = map[string]knownCoin{
	"algorand":                {"ALGO", "algo-algorand"},
	"bitcoin":                 {"BTC", "btc-bitcoin"},
	"ethereum":                {"ETH", "eth-ethereum"},
	"usd-coin":                {"USDC", "usdc-usd-coin"},
	"tether":                  {"USDT", "usdt-tether"},
	"solana":                  {"SOL", "sol-solana"},
	"ripple":                  {"XRP", "xrp-xrp"},
	"cardano":                 {"ADA", "ada-cardano"},
	"dogecoin":                {"DOGE", "doge-dogecoin"},
	"binancecoin":             {"BNB", "bnb-binance-coin"},
	"polygon-ecosystem-token": {"POL", "pol-polygon-ecosystem-token"},
	"chainlink":               {"LINK", "link-chainlink"},
	"avalanche-2":             {"AVAX", "avax-avalanche"},
	"litecoin":                {"LTC", "ltc-litecoin"},
	"polkadot":                {"DOT", "dot-polkadot"},
	"tron":                    {"TRX", "trx-tron"},
}

// Base URLs, swappable in tests.
var (
	coinbaseAPIBase         = "https://api.coinbase.com/v2"
	coinbaseExchangeAPIBase = "https://api.exchange.coinbase.com"
	coinPaprikaAPIBase      = "https://api.coinpaprika.com/v1"
)

// SetPriceFallbackBasesForTest points the backups at test servers. Blank
// restores the real APIs. Call only from tests.
func SetPriceFallbackBasesForTest(coinbase, exchange, paprika string) {
	coinbaseAPIBase = firstNonEmpty(coinbase, "https://api.coinbase.com/v2")
	coinbaseExchangeAPIBase = firstNonEmpty(exchange, "https://api.exchange.coinbase.com")
	coinPaprikaAPIBase = firstNonEmpty(paprika, "https://api.coinpaprika.com/v1")
}

func firstNonEmpty(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// coinbaseSpot is one Coinbase spot price, e.g. ALGO in USD.
func coinbaseSpot(ctx context.Context, symbol, currency string) (float64, error) {
	pair := url.PathEscape(strings.ToUpper(symbol) + "-" + strings.ToUpper(currency))
	b, err := getRaw(ctx, coinbaseAPIBase+"/prices/"+pair+"/spot", map[string]string{"Accept": "application/json"}, "Coinbase")
	if err != nil {
		return 0, err
	}
	var body struct {
		Data struct {
			Amount   string `json:"amount"`
			Base     string `json:"base"`
			Currency string `json:"currency"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return 0, fmt.Errorf("Coinbase: decode response: %w", err)
	}
	// Checked, not assumed: a price for some other pair is not this price.
	if !strings.EqualFold(body.Data.Base, symbol) || !strings.EqualFold(body.Data.Currency, currency) {
		return 0, fmt.Errorf("Coinbase: answered for %s-%s, not %s-%s", body.Data.Base, body.Data.Currency, symbol, currency)
	}
	return strconv.ParseFloat(body.Data.Amount, 64)
}

// paprikaQuotes is CoinPaprika's price for one coin in each currency it has.
func paprikaQuotes(ctx context.Context, paprikaID string, currencies []string) (map[string]float64, error) {
	upper := make([]string, len(currencies))
	for i, c := range currencies {
		upper[i] = strings.ToUpper(c)
	}
	q := url.Values{"quotes": {strings.Join(upper, ",")}}
	b, err := getRaw(ctx, coinPaprikaAPIBase+"/tickers/"+url.PathEscape(paprikaID)+"?"+q.Encode(),
		map[string]string{"Accept": "application/json"}, "CoinPaprika")
	if err != nil {
		return nil, err
	}
	var body struct {
		ID     string `json:"id"`
		Quotes map[string]struct {
			Price float64 `json:"price"`
		} `json:"quotes"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return nil, fmt.Errorf("CoinPaprika: decode response: %w", err)
	}
	if body.ID != paprikaID {
		return nil, fmt.Errorf("CoinPaprika: answered for %q, not %q", body.ID, paprikaID)
	}
	out := map[string]float64{}
	for _, c := range currencies {
		if qt, ok := body.Quotes[strings.ToUpper(c)]; ok {
			out[c] = qt.Price
		}
	}
	return out, nil
}

// spotFallback prices ids in currencies from the backups, in CoinGecko's
// /simple/price shape ({"algorand": {"usd": 0.089}}) so every jsonPath the
// builder wrote still reads it, plus source and, when needed, unavailable.
// Reports false when it could price nothing.
func spotFallback(ctx context.Context, idList, currencyList string) (map[string]any, bool) {
	ids, currencies := splitList(idList), splitList(currencyList)
	if len(ids) == 0 || len(currencies) == 0 {
		return nil, false
	}
	var (
		mu          sync.Mutex
		wg          sync.WaitGroup
		out         = map[string]any{}
		sources     = map[string]bool{}
		unavailable []string
	)
	for _, id := range ids {
		coin, known := knownCoins[id]
		if !known {
			mu.Lock()
			unavailable = append(unavailable, id)
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(id string, coin knownCoin) {
			defer wg.Done()
			prices := map[string]any{}
			var missing []string
			for _, cur := range currencies {
				if p, err := coinbaseSpot(ctx, coin.Symbol, cur); err == nil {
					prices[cur] = p
					mu.Lock()
					sources["coinbase"] = true
					mu.Unlock()
				} else {
					missing = append(missing, cur)
				}
			}
			if len(missing) > 0 {
				if quotes, err := paprikaQuotes(ctx, coin.Paprika, missing); err == nil {
					for cur, p := range quotes {
						prices[cur] = p
						mu.Lock()
						sources["coinpaprika"] = true
						mu.Unlock()
					}
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(prices) == 0 {
				unavailable = append(unavailable, id)
				return
			}
			out[id] = prices
		}(id, coin)
	}
	wg.Wait()
	if len(out) == 0 {
		return nil, false
	}
	names := make([]string, 0, len(sources))
	for s := range sources {
		names = append(names, s)
	}
	sort.Strings(names)
	out["source"] = strings.Join(names, "+")
	out["note"] = "CoinGecko was unavailable, so these prices come from " + strings.Join(names, " and ") + "."
	if len(unavailable) > 0 {
		sort.Strings(unavailable)
		out["unavailable"] = unavailable
	}
	return out, true
}

// Coinbase candle granularities, and the most candles one request returns.
const (
	candleHour     = 3600
	candleDay      = 86400
	maxCandlesUsed = 300
)

// historyFallback returns CoinGecko market_chart's {"prices": [[ms, price]]}
// shape from Coinbase's public candles, oldest first, using each candle's
// close. Reports false for a coin, currency or range it cannot serve.
func historyFallback(ctx context.Context, id, currency, days string, now time.Time) (map[string]any, bool) {
	coin, known := knownCoins[strings.ToLower(id)]
	if !known {
		return nil, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(days))
	if err != nil || n < 1 {
		return nil, false // "max" and friends: more than the candles reach
	}
	granularity := candleHour
	if n*24 > maxCandlesUsed {
		granularity = candleDay
	}
	if n*candleDay/granularity > maxCandlesUsed {
		return nil, false
	}
	product := url.PathEscape(strings.ToUpper(coin.Symbol) + "-" + strings.ToUpper(currency))
	q := url.Values{
		"granularity": {strconv.Itoa(granularity)},
		"start":       {now.Add(-time.Duration(n) * 24 * time.Hour).UTC().Format(time.RFC3339)},
		"end":         {now.UTC().Format(time.RFC3339)},
	}
	b, err := getRaw(ctx, coinbaseExchangeAPIBase+"/products/"+product+"/candles?"+q.Encode(),
		map[string]string{"Accept": "application/json"}, "Coinbase")
	if err != nil {
		return nil, false
	}
	var candles [][]float64
	if err := json.Unmarshal(b, &candles); err != nil {
		return nil, false
	}
	prices := make([][]float64, 0, len(candles))
	for _, c := range candles {
		// [time, low, high, open, close, volume]
		if len(c) >= 5 {
			prices = append(prices, []float64{c[0] * 1000, c[4]})
		}
	}
	if len(prices) == 0 {
		return nil, false
	}
	sort.Slice(prices, func(i, j int) bool { return prices[i][0] < prices[j][0] })
	pts := make([]any, len(prices))
	for i, p := range prices {
		pts[i] = []any{p[0], p[1]}
	}
	return map[string]any{"prices": pts}, true
}

func logPriceFallback(what string, cause error) {
	log.Printf("price fallback: %s (CoinGecko: %v)", what, cause)
}
