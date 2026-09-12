package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentmesh/backend/internal/bazaar"
	"github.com/agentmesh/backend/internal/respond"
)

// defaultBazaarBaseURL is GoPlausible's facilitator. Used when Deps leaves
// BazaarBaseURL empty, so a missing env var degrades to the real catalog
// rather than to no catalog at all.
const defaultBazaarBaseURL = "https://facilitator.goplausible.xyz"

// bazaarCacheTTL bounds how stale the mirrored catalog can be. The upstream
// changes on the order of hours (entries are added when a merchant first
// settles), so a short TTL would spend a full ~8-page crawl to learn nothing.
const bazaarCacheTTL = 15 * time.Minute

// bazaarCrawlTimeout bounds runCatalogFetch's whole sequential crawl -- see
// that function's doc comment for the sizing rationale.
const bazaarCrawlTimeout = 90 * time.Second

// bazaarRetryBackoff bounds how often a failed refresh is retried once the
// cache has expired (or, on a cold start, once no fetch has ever succeeded).
// Without this, every request during an upstream outage re-attempts the full
// ~8-page crawl instead of getting a fast stale-cache hit or a fast error.
// A var, not a const, so tests can shrink it instead of sleeping 30s.
var bazaarRetryBackoff = 30 * time.Second

// bazaarPageDefault/Max bound one page of results. The frontend's infinite
// scroll asks for 30 at a time.
const (
	bazaarPageDefault = 30
	bazaarPageMax     = 100
)

// bazaarCache memoises the whole merged catalog. The catalog is ~780 entries,
// small enough to hold entirely and slice per request — which is also why
// search runs here rather than upstream, where there is no search parameter.
type bazaarCache struct {
	mu              sync.Mutex
	items           []bazaar.Resource
	fetchedAt       time.Time
	lastAttemptedAt time.Time
	lastErr         error
	// inflight is non-nil while a refresh is in progress. Callers that arrive
	// while a refresh is already running wait on this channel (with their own
	// request context honoured) instead of starting a second concurrent crawl
	// or blocking the shared mutex for the crawl's full duration.
	inflight chan struct{}
}

// bazaarHTTPClient has its own timeout because a full crawl is several
// sequential upstream requests.
var bazaarHTTPClient = &http.Client{Timeout: 30 * time.Second}

func (d *Deps) bazaarBaseURL() string {
	if d.BazaarBaseURL != "" {
		return d.BazaarBaseURL
	}
	return defaultBazaarBaseURL
}

// catalog returns the merged catalog, refreshing it if the cache has expired.
//
// The refresh itself runs on a detached context with its own 30s timeout,
// not any single caller's request context: this is a shared resource, and
// the fetch must not abort just because the caller who happened to trigger
// it disconnected. A caller waiting on an in-flight refresh, however, DOES
// honour its own request context — it stops waiting (and releases its
// handler goroutine) the moment its own connection goes away, without
// affecting the shared refresh other callers are still waiting on.
func (d *Deps) catalog(ctx context.Context) ([]bazaar.Resource, error) {
	d.bazaarCache.mu.Lock()
	if d.bazaarCache.items != nil && time.Since(d.bazaarCache.fetchedAt) < bazaarCacheTTL {
		items := d.bazaarCache.items
		d.bazaarCache.mu.Unlock()
		return items, nil
	}
	// inflight is checked BEFORE the backoff check below, not after: a
	// caller that arrives after a crawl has started but before it finishes
	// must wait on that crawl. Checking backoff first would let it take the
	// backoff branch instead (lastAttemptedAt was just set by whoever
	// started the crawl) and read items/lastErr while both are still nil on
	// a cold start -- a spurious empty "200 {items: [], total: 0}" that
	// looks like a real, if unfortunate, page state instead of "still
	// loading."
	ch := d.bazaarCache.inflight
	if ch == nil {
		// No refresh running — only now does the backoff apply: a prior
		// attempt failed recently, so serve the stale cache instead of
		// re-running the full crawl on every request until it clears. This
		// applies even on a cold start (no successful fetch yet, items ==
		// nil): checking lastAttemptedAt rather than requiring items != nil
		// is what makes the backoff actually engage during a from-boot
		// outage, instead of every single request racing to start its own
		// crawl.
		if !d.bazaarCache.lastAttemptedAt.IsZero() && time.Since(d.bazaarCache.lastAttemptedAt) < bazaarRetryBackoff {
			items, err := d.bazaarCache.items, d.bazaarCache.lastErr
			d.bazaarCache.mu.Unlock()
			if items != nil {
				return items, nil
			}
			return nil, err
		}
		// Start one on a detached goroutine so it is never at the mercy of
		// whichever caller happens to be the one that triggers it. Every
		// caller, this one included, only ever *waits* on it below via
		// select, so every caller (not just followers) honours its own ctx
		// and can bail out early without affecting the shared fetch.
		ch = make(chan struct{})
		d.bazaarCache.inflight = ch
		d.bazaarCache.lastAttemptedAt = time.Now()
		go d.runCatalogFetch(ch)
	}
	d.bazaarCache.mu.Unlock()

	select {
	case <-ch:
		d.bazaarCache.mu.Lock()
		items, err := d.bazaarCache.items, d.bazaarCache.lastErr
		d.bazaarCache.mu.Unlock()
		if items != nil {
			return items, nil
		}
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// runCatalogFetch performs one crawl and publishes the result into the
// shared cache, independent of any caller's request context. It always
// closes done, even on failure, so every caller waiting in catalog's select
// is released.
//
// Budget is 90s, not 30s: FetchAll pages strictly sequentially (one real
// client.Do call at a time -- see its own doc comment for why a concurrent
// version was tried and reverted), and at today's ~780-entry catalog that's
// ~8 real pages, comfortably inside even 30s under normal latency. But if
// the upstream slows to a few seconds per page, the sequential total alone
// can approach or exceed a tight budget and the whole cache refresh fails
// outright, falling into the backoff/stale-serve path. 90s gives real
// headroom for a slow-but-not-dead upstream without gambling on an
// out-of-range query the upstream might not tolerate.
func (d *Deps) runCatalogFetch(done chan struct{}) {
	fetchCtx, cancel := context.WithTimeout(context.Background(), bazaarCrawlTimeout)
	defer cancel()
	fetched, fetchErr := bazaar.FetchAll(fetchCtx, bazaarHTTPClient, d.bazaarBaseURL())

	d.bazaarCache.mu.Lock()
	if fetchErr != nil {
		d.bazaarCache.lastErr = fetchErr
		// Keep whatever's already cached (possibly nil) — a transient
		// upstream blip should not empty a page that was working a moment
		// ago.
	} else {
		merged := bazaar.Merge(fetched)
		d.bazaarCache.items = merged
		d.bazaarCache.fetchedAt = time.Now()
		d.bazaarCache.lastErr = nil
	}
	d.bazaarCache.inflight = nil
	d.bazaarCache.mu.Unlock()
	close(done)
}

// forceCatalogRefresh starts a new crawl if one is not already running,
// regardless of whether the cache is still within its TTL. catalog() itself
// never does this -- a request that finds a fresh cache should never pay for
// a crawl no one asked for -- so this exists only for WarmBazaarCache, which
// deliberately refreshes AHEAD of expiry.
func (d *Deps) forceCatalogRefresh() {
	d.bazaarCache.mu.Lock()
	if d.bazaarCache.inflight != nil {
		// Already refreshing -- a real request's catalog() call beat the
		// timer to it, or the previous tick's crawl is still running past
		// its own interval. Do not start a second, concurrent crawl.
		d.bazaarCache.mu.Unlock()
		return
	}
	ch := make(chan struct{})
	d.bazaarCache.inflight = ch
	d.bazaarCache.lastAttemptedAt = time.Now()
	d.bazaarCache.mu.Unlock()
	go d.runCatalogFetch(ch)
}

// WarmBazaarCache keeps the merged catalog cache populated ahead of expiry,
// so that in the steady state a real GET /bazaar/resources practically never
// pays for the crawl catalog() would otherwise have to run inline the
// instant the cache goes stale -- up to bazaarCrawlTimeout (90s) of dead
// time for whichever request happens to land right after the 15-minute TTL
// lapses. Call once, from main, right after Deps is constructed; ctx
// cancellation (server shutdown) stops the loop.
//
// The first refresh is fired here too, on its own goroutine (via
// forceCatalogRefresh, not awaited) rather than run inline, so server
// startup itself is never blocked on the initial ~780-entry crawl
// succeeding -- the very first request or two after boot may still pay for
// it, same as before this existed, but every one after that should not.
func (d *Deps) WarmBazaarCache(ctx context.Context) {
	d.forceCatalogRefresh()

	// Refresh before the TTL actually lapses, not exactly at it: the crawl
	// itself can take up to bazaarCrawlTimeout, so ticking at the TTL would
	// still leave a window where the cache goes stale mid-refresh and a
	// request lands in it. Subtracting the crawl's own worst case keeps that
	// window closed; the 1-minute floor is just so a future edit to either
	// constant can't accidentally produce a zero or negative ticker interval.
	interval := bazaarCacheTTL - bazaarCrawlTimeout
	if interval < time.Minute {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.forceCatalogRefresh()
			}
		}
	}()
}

// BazaarResources serves one page of the mirrored x402 catalog.
func (d *Deps) BazaarResources(w http.ResponseWriter, r *http.Request) {
	all, err := d.catalog(r.Context())
	if err != nil {
		// A caller that navigated away mid-request surfaces here as its own
		// ctx.Err() (see catalog's <-ctx.Done() arm), which is a client
		// disconnect, not an upstream catalog failure -- logging it as one
		// misattributes the cause, and during a real outage the retry
		// backoff returns the cached error fast enough that every single
		// request would log a line rather than one per actual crawl.
		if !errors.Is(err, context.Canceled) {
			log.Printf("bazaar: catalog unavailable: %v", err)
		}
		respond.Error(w, http.StatusBadGateway, "could not reach the x402 catalog")
		return
	}

	// Computed over the full, unfiltered catalog so it reports a stable
	// total regardless of the q/supported filters applied below — a future
	// "N supported providers" badge should not change with every keystroke
	// in the search box.
	supported := 0
	for _, it := range all {
		if it.Supported {
			supported++
		}
	}

	items := all
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		// Fuzzy, not a substring filter: FuzzyMatch also RANKS a match, and a
		// query reorders items by that score (best first) rather than
		// leaving them in the catalog's own settle-count order underneath a
		// filter -- see that function's doc comment for why ranking is the
		// point. This is why q and sort are mutually exclusive below: a
		// query already answers "what order", and letting a client-picked
		// sort override match quality would be worse than not sorting at
		// all when what someone typed is right there scoring every result.
		type scored struct {
			r     bazaar.Resource
			score int
		}
		matched := make([]scored, 0, len(items))
		for _, it := range items {
			if score, ok := bazaar.BestFieldScore(it, q); ok {
				matched = append(matched, scored{it, score})
			}
		}
		sort.SliceStable(matched, func(i, j int) bool { return matched[i].score > matched[j].score })
		filtered := make([]bazaar.Resource, len(matched))
		for i, m := range matched {
			filtered[i] = m.r
		}
		items = filtered
	} else {
		items = sortItems(items, r.URL.Query().Get("sort"))
	}

	// supported=1/true keeps only endorsed entries (the pinned "Supported"
	// section); supported=0/false excludes them (the paged grid, so a card
	// never renders twice under contradictory copy). Any other value or an
	// absent param leaves items untouched, matching the doc below. A curated
	// entry with zero catalog matches (e.g. Tendril) only ever gets pulled in
	// via Merge's "not present in the catalog" append, which can land far
	// past any page-size cutoff by settle count — so this filter must run
	// over the full merged set, not a slice of it, for supported=1 to find
	// it at all.
	switch r.URL.Query().Get("supported") {
	case "1", "true":
		items = filterSupported(items, true)
	case "0", "false":
		items = filterSupported(items, false)
	}

	offset := clampAtoi(r.URL.Query().Get("offset"), 0, 0, len(items))
	limit := clampAtoi(r.URL.Query().Get("limit"), bazaarPageDefault, 1, bazaarPageMax)

	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	page := items[offset:end]
	if page == nil {
		page = []bazaar.Resource{}
	}

	respond.JSON(w, http.StatusOK, map[string]any{
		"items":          page,
		"total":          len(items),
		"offset":         offset,
		"limit":          limit,
		"supportedCount": supported,
	})
}

// filterSupported returns only entries whose Supported flag matches want.
func filterSupported(items []bazaar.Resource, want bool) []bazaar.Resource {
	filtered := make([]bazaar.Resource, 0, len(items))
	for _, it := range items {
		if it.Supported == want {
			filtered = append(filtered, it)
		}
	}
	return filtered
}

// sortItems returns items reordered per the sort param, always as a COPY --
// items may be (or be sliced from) the shared catalog cache itself, and
// sort.SliceStable mutates in place, so sorting the cache's own backing
// array here would corrupt the order every other concurrent request (and
// the next TTL cycle's cached copy) sees.
//
// "settles" (or anything unrecognised) is the crawl's own order and needs no
// copy at all -- FetchAll already sorts most-settled-first once, at crawl
// time, so every request agrees on what "default" means without repeating
// the sort per request.
func sortItems(items []bazaar.Resource, by string) []bazaar.Resource {
	switch by {
	case "name":
		out := append([]bazaar.Resource(nil), items...)
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(displayName(out[i])) < strings.ToLower(displayName(out[j]))
		})
		return out
	case "recent":
		out := append([]bazaar.Resource(nil), items...)
		// LastSeen is an upstream ISO-8601 timestamp string; lexical order
		// on that format IS chronological order, so a plain string compare
		// is correct here without parsing it into a time.Time first.
		sort.SliceStable(out, func(i, j int) bool { return out[i].LastSeen > out[j].LastSeen })
		return out
	case "price":
		out := append([]bazaar.Resource(nil), items...)
		sort.SliceStable(out, func(i, j int) bool { return out[i].AmountMicros < out[j].AmountMicros })
		return out
	default:
		return items
	}
}

// displayName is the same fallback ResourceCard/EndpointRow use on the
// frontend: a supported entry's own Provider name, or the bare Host for a
// community listing that has none.
func displayName(r bazaar.Resource) string {
	if r.Provider != "" {
		return r.Provider
	}
	return r.Host
}

// clampAtoi parses a query integer, falling back to def and clamping to
// [min,max] so a hand-edited URL cannot slice out of range.
func clampAtoi(raw string, def, min, max int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
