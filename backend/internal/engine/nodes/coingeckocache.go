package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// A short shared cache in front of CoinGecko.
//
// The keyless API is rate-limited per calling IP, and every workflow on this
// server shares one IP. A build calls it for resolve_coin, again for each
// test run, and a scheduled morning brief calls it again at the same minute
// as everyone else's. A live build failed its test run on "CoinGecko API
// 429" for exactly that reason.
//
// Sixty seconds, because a price that old is still the price: it is well
// inside what anyone reading a morning brief means by "current". Only
// successful responses are kept. A refusal is never answered from an older
// entry: serving yesterday's price as today's is the failure this layer
// exists to avoid, not a fallback.
//
// Concurrent identical requests share one upstream call. Two nodes in one
// run asking for the same coin are one request, not two.

const (
	coinGeckoCacheTTL = 60 * time.Second
	// coinGeckoCacheMax bounds memory. Past it, expired entries are dropped,
	// and if every entry is still fresh the cache starts over: a burst of
	// distinct queries is not worth holding on to.
	coinGeckoCacheMax = 512
)

type coinGeckoEntry struct {
	body []byte
	at   time.Time
}

type coinGeckoCall struct {
	done chan struct{}
	body []byte
	err  error
}

var coinGeckoCache = struct {
	sync.Mutex
	entries  map[string]coinGeckoEntry
	inFlight map[string]*coinGeckoCall
	now      func() time.Time
}{
	entries:  map[string]coinGeckoEntry{},
	inFlight: map[string]*coinGeckoCall{},
	now:      time.Now,
}

// resetCoinGeckoCache empties the cache. Tests point CoinGecko at a fresh
// server and must not be answered from another test's entries.
func resetCoinGeckoCache() {
	coinGeckoCache.Lock()
	defer coinGeckoCache.Unlock()
	coinGeckoCache.entries = map[string]coinGeckoEntry{}
}

// coinGeckoGet is getAndDecode for CoinGecko, through the cache.
func coinGeckoGet(ctx context.Context, target string) (any, error) {
	body, err := coinGeckoFetch(ctx, target)
	if err != nil {
		return nil, err
	}
	// Decoded per call, never shared: the result is handed to a connector
	// that owns it, and one caller's map must not show another's edits.
	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("CoinGecko: decode response: %w", err)
	}
	return result, nil
}

func coinGeckoFetch(ctx context.Context, target string) ([]byte, error) {
	c := &coinGeckoCache
	c.Lock()
	if e, ok := c.entries[target]; ok && c.now().Sub(e.at) < coinGeckoCacheTTL {
		c.Unlock()
		return e.body, nil
	}
	if call, ok := c.inFlight[target]; ok {
		c.Unlock()
		select {
		case <-call.done:
			return call.body, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &coinGeckoCall{done: make(chan struct{})}
	c.inFlight[target] = call
	c.Unlock()

	// Detached from this caller's cancellation: other callers may be waiting
	// on the same request, and one run being stopped must not fail theirs.
	// The HTTP client's own timeout still bounds it.
	call.body, call.err = getRaw(context.WithoutCancel(ctx), target,
		map[string]string{"Accept": "application/json"}, "CoinGecko")
	if call.err == nil && !json.Valid(call.body) {
		call.body, call.err = nil, fmt.Errorf("CoinGecko: response is not JSON")
	}

	c.Lock()
	delete(c.inFlight, target)
	if call.err == nil {
		if len(c.entries) >= coinGeckoCacheMax {
			now := c.now()
			for k, e := range c.entries {
				if now.Sub(e.at) >= coinGeckoCacheTTL {
					delete(c.entries, k)
				}
			}
			if len(c.entries) >= coinGeckoCacheMax {
				c.entries = map[string]coinGeckoEntry{}
			}
		}
		c.entries[target] = coinGeckoEntry{body: call.body, at: c.now()}
	}
	c.Unlock()
	close(call.done)

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return call.body, call.err
}
