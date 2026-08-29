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
//
// Prism (https://prism-99h2.onrender.com/resume-screen-accurate) is the one
// launch provider named above that still has no entry here, and that's
// deliberate on two independent grounds, not an oversight:
//
//  1. Unlike Tendril, we have no verified static payTo/amount for Prism to
//     hand-author — engine/nodes/tool402.go discovers its challenge live, per
//     call, from the Payment-Required header rather than a fixed quote.
//     Guessing one would risk routing a real payment to the wrong address,
//     exactly the hazard frontend/src/lib/bazaar.ts's MOCK_RESOURCES comment
//     already refuses to take for the same provider.
//  2. Prism's real body isn't expressible as labelled params anyway: it needs
//     a nested files array (base64 file content alongside a text field) built
//     via BodyMode-JSON + a body template (see
//     TestBuildTargetRequestJSONBodyModeProducesPrismShape), while Resource's
//     Param and the frontend's discoveredParams pipeline only ever render a
//     flat list of plain-text inputs — there is no file-upload kind in either.
//     A curated entry today, even with a trustworthy payTo, would let a user
//     add "Prism" from the Bazaar expecting it to work and get a silently
//     broken request (or the $0.25 charge-then-"No files provided" failure
//     TestBuildTargetRequestJSONBodyModeProducesPrismShape exists to prevent).
//
// Making Prism a real Bazaar entry needs Param/discoveredParams to support a
// file kind and a body-template shape first — a model change, not a one-line
// registry addition.
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
	// One row per real endpoint URL, curated or not. The upstream catalog
	// can list the same resourceUrl under two different ids (a
	// re-registration), and emitting both puts the same endpoint on the
	// page twice.
	//
	// Applied to curated matches too, not just community ones: there the
	// duplicate is actually worse. Only the first row gets Supported=true,
	// so the second is served under supported=0 -- and since BazaarPage
	// fetches the pinned section with supported=1 and the grid with
	// supported=0, one endpoint would appear in BOTH, pinned with the
	// curated description and again in the grid with the publisher's own.
	// That is precisely the "same card twice under contradictory copy"
	// case this whole merge exists to prevent, so treating curated
	// re-registrations as data worth keeping traded one duplicate for a
	// strictly more confusing one.
	//
	// catalog is sorted by settle count descending (FetchAll), so keeping
	// the first occurrence keeps the most established registration along
	// with its own id/SettleCount/LastSeen.
	seenURL := make(map[string]bool, len(catalog))
	for _, r := range catalog {
		key := normalizeURLForMatch(r.URL)
		if seenURL[key] {
			continue
		}
		seenURL[key] = true
		if c, ok := byURL[key]; ok {
			matched[key] = true
			r.Supported = true
			r.Provider = c.Provider
			// A hand-authored description and param set are strictly better
			// than the publisher's own, which is why the entry is curated.
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
	}
	for _, c := range curated {
		if !matched[normalizeURLForMatch(c.URL)] {
			out = append(out, c)
		}
	}
	return out
}
