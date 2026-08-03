package handlers

import (
	"net/http"
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
	mu        sync.Mutex
	items     []bazaar.Resource
	fetchedAt time.Time
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
func (d *Deps) catalog(r *http.Request) ([]bazaar.Resource, error) {
	d.bazaarCache.mu.Lock()
	defer d.bazaarCache.mu.Unlock()
	if d.bazaarCache.items != nil && time.Since(d.bazaarCache.fetchedAt) < bazaarCacheTTL {
		return d.bazaarCache.items, nil
	}
	fetched, err := bazaar.FetchAll(r.Context(), bazaarHTTPClient, d.bazaarBaseURL())
	if err != nil {
		// Serve stale rather than nothing: a transient upstream blip should
		// not empty a page that was working a moment ago.
		if d.bazaarCache.items != nil {
			return d.bazaarCache.items, nil
		}
		return nil, err
	}
	merged := bazaar.Merge(fetched)
	d.bazaarCache.items = merged
	d.bazaarCache.fetchedAt = time.Now()
	return merged, nil
}

// BazaarResources serves one page of the mirrored x402 catalog.
func (d *Deps) BazaarResources(w http.ResponseWriter, r *http.Request) {
	items, err := d.catalog(r)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "could not reach the x402 catalog")
		return
	}

	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); q != "" {
		filtered := make([]bazaar.Resource, 0, len(items))
		for _, it := range items {
			hay := strings.ToLower(it.URL + " " + it.Description + " " + it.Provider + " " + it.Host)
			if strings.Contains(hay, q) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	offset := clampAtoi(r.URL.Query().Get("offset"), 0, 0, len(items))
	limit := clampAtoi(r.URL.Query().Get("limit"), bazaarPageDefault, 1, bazaarPageMax)

	supported := 0
	for _, it := range items {
		if it.Supported {
			supported++
		}
	}

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
