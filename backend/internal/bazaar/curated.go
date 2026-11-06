package bazaar

import (
	"net/url"
	"strings"
)

// Curated is AgentMesh's own registry of officially-supported x402
// providers. On Whitechain there are no partner consoles yet, so it is empty
// and every entry comes from the facilitator's live discovery catalog via
// Merge. Add an entry here only for a provider with a dedicated console.
func Curated() []Resource {
	return []Resource{}
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
			// Console has to travel with the curation, not just the
			// provider name: without this, a Prism or Tendril endpoint
			// that later appears in the upstream catalog would be badged
			// Supported but lose its console key, and its Bazaar card
			// would silently fall back to dropping a canvas node the
			// endpoint's real input shape cannot be expressed in.
			r.Console = c.Console
			// The PAYMENT QUOTE comes from the registry, not the catalog,
			// whenever the registry declares one.
			//
			// This is the one place the "registry curates, catalog supplies
			// facts" split does not hold, and it was found the hard way:
			// Prism's code-review endpoints appeared in the upstream catalog
			// listing 25500 micros, while their live 402 challenges (probed
			// 2026-09-05) declare 100000 and 200000. Keeping the catalog's
			// number meant the Bazaar quoting $0.0255 for a call the console
			// charges $0.20 for -- a price the user is shown and then not
			// billed.
			//
			// Between a mirrored third-party listing and an amount we probed
			// off the endpoint itself and will settle against, the probe wins.
			// Only overwrite what the registry actually declares, so an entry
			// that carries no quote (a curated URL with no verified price)
			// still inherits the catalog's.
			//
			// Live telemetry -- SettleCount, LastSeen, the entry's own ID --
			// is untouched: that genuinely is the catalog's to supply.
			if c.AmountMicros > 0 {
				r.AmountMicros = c.AmountMicros
			}
			if c.PayTo != "" {
				r.PayTo = c.PayTo
			}
			if c.Asset != "" {
				r.Asset = c.Asset
			}
			if c.Network != "" {
				r.Network = c.Network
				// Testnet was derived from the CATALOG's network
				// (catalog.go:207), so overriding Network without it leaves the
				// two disagreeing: a curated entry that upstream happens to
				// list on testnet would merge out priced and payable on
				// mainnet while ResourceCard/EndpointRow still render a
				// "testnet" pill over it.
				r.Testnet = r.Network == WhitechainTestnet
			}
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
