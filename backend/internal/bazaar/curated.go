package bazaar

import (
	"net/http"
	"net/url"
	"strings"
)

// Curated is AgentMesh's own registry of officially-supported x402 providers.
//
// It is deliberately NOT derived from the GoPlausible catalog. Verified live
// 2026-08-03: of the launch providers, only CANIX402 appears in
// /discovery/resources at all (14 entries) — Tendril and Prism return zero
// matches across all 779 entries despite Tendril sitting at rank 4 on the
// challenge leaderboard. A "supported" list built by filtering catalog data
// would silently omit them.
//
// Params here are hand-authored, not scraped: the whole point of the supported
// tier is that a user fills labelled fields instead of hand-writing JSON.
func Curated() []Resource {
	return []Resource{
		{
			ID:          "curated:tendril-run",
			URL:         "https://tendrilregister.007575.xyz/x402/run",
			Method:      http.MethodPost,
			Provider:    "Tendril",
			Host:        "tendrilregister.007575.xyz",
			Description: "Run a Python script on rented compute and get its stdout back. No lease needed — Tendril picks the machine, runs the job in a throwaway sandbox, and destroys it. Requires a positive Tendril credit balance on the paying wallet.",
			Network:     AlgorandMainnet,
			Asset:       "31566704",
			PayTo:       "ZIK7QQE7ZX446TW3PN7PQ5UDZNTY7JI5RYNTIU3LPEYBOSTVWI6PTNSWKI",
			// Flat gate fee only. Execution time is billed separately from a
			// Tendril-side credit balance keyed to the paying wallet address.
			AmountMicros: 10000,
			Supported:    true,
			Params: []Param{{
				Name:        "payload",
				Type:        "string",
				Required:    true,
				Description: "Python source to execute. Its stdout is returned as `result`.",
			}},
		},
		{
			ID:          "curated:canix-quotes",
			URL:         "https://canix402-api.compx.io/execution/quotes",
			Method:      http.MethodPost,
			Provider:    "CANIX402",
			Host:        "canix402-api.compx.io",
			Description: "Algorand DeFi execution quotes across supported protocols.",
			Network:     AlgorandMainnet,
			Asset:       "31566704",
			Supported:   true,
			// No hand-authored params for this one yet (see Merge) — explicit
			// []Param{} rather than the nil zero value, which marshals as JSON
			// null and crashes the frontend grid.
			Params: []Param{},
		},
	}
}

// normalizeURLForMatch makes two URLs comparable for Merge's purposes: it
// lowercases the scheme and host (case-insensitive per RFC 3986) and trims
// exactly one trailing slash from the path, so a catalog entry differing from
// a curated URL only by trailing-slash or scheme/host case still matches
// instead of silently producing a duplicate row for the same real endpoint.
// It is used only for comparison — Resource.URL itself is never rewritten.
func normalizeURLForMatch(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if len(u.Path) > 1 {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}
	return u.String()
}

// Merge annotates catalog entries that match a curated URL as Supported, and
// appends curated entries the catalog does not list at all.
//
// Live telemetry on a matched entry (settle counts, last seen) is preserved —
// the registry supplies curation, the catalog supplies facts.
func Merge(catalog []Resource) []Resource {
	curated := Curated()
	byURL := make(map[string]Resource, len(curated))
	for _, c := range curated {
		byURL[normalizeURLForMatch(c.URL)] = c
	}

	out := make([]Resource, 0, len(catalog)+len(curated))
	matched := make(map[string]bool, len(curated))
	// Tracks curated URLs already emitted as a Supported=true row. If the
	// upstream catalog lists two distinct entries under the same resourceUrl
	// (re-registration under a new catalog id, for example), only the first
	// becomes a Supported row — otherwise the same real endpoint renders
	// twice in the pinned section.
	emitted := make(map[string]bool, len(curated))
	// Tracks non-curated URLs already appended. Unlike the curated case
	// above, a plain community entry has no Supported badge to de-duplicate
	// against, so without this a re-registered non-curated provider (same
	// real resourceUrl, new catalog id) would render as two indistinguishable
	// duplicate cards in the community grid rather than one row with a
	// double-flagged badge. catalog is sorted by settle count descending
	// (FetchAll), so keeping only the first occurrence keeps whichever
	// registration is the most established.
	seenUnsupportedURL := make(map[string]bool, len(catalog))
	for _, r := range catalog {
		key := normalizeURLForMatch(r.URL)
		if c, ok := byURL[key]; ok {
			// !emitted[key], not a `continue` on emitted[key]: a second
			// catalog entry under an already-emitted curated URL must still
			// appear in out (with its own id/SettleCount/LastSeen, just not
			// re-flagged Supported) -- curated re-registrations are real
			// data, not a duplicate to drop.
			if !emitted[key] {
				emitted[key] = true
				matched[key] = true
				r.Supported = true
				r.Provider = c.Provider
				// A hand-authored description and param set are strictly
				// better than the publisher's own, which is why the entry
				// is curated.
				if c.Description != "" {
					r.Description = c.Description
				}
				if len(c.Params) > 0 {
					r.Params = c.Params
				}
				if c.Method != "" {
					r.Method = c.Method
				}
			}
			out = append(out, r)
			continue
		}
		if seenUnsupportedURL[key] {
			continue
		}
		seenUnsupportedURL[key] = true
		out = append(out, r)
	}
	for _, c := range curated {
		if !matched[normalizeURLForMatch(c.URL)] {
			out = append(out, c)
		}
	}
	return out
}
