package bazaar

import "time"

// Trust grades how much evidence a catalog entry carries that it still works.
//
// The x402 Bazaar is permissionless: anyone can publish an entry, nothing
// removes one when the service behind it dies, and the description is
// whatever the publisher typed. Over a third of the live catalog has not been
// paid in more than a month. Relevance alone therefore picks dead endpoints
// as happily as live ones, and the consequence is not a 404 -- it is a real
// payment for nothing.
//
// The catalog carries two fields that are evidence rather than advertising,
// because they are counted by the facilitator rather than written by the
// publisher: settleCount (how many times somebody actually paid this
// endpoint), lastSeen (when that last happened) and firstSeen (when the
// facilitator first observed it). Trust is those
// combined, and it is deliberately coarse: four tiers, so it can break ties
// between comparable matches without letting a popular endpoint outrank one
// that genuinely answers the question.
type Trust int

// Ordered worst to best so a plain > comparison ranks them.
const (
	// TrustStale means nobody has paid this endpoint in staleAfter. A high
	// settleCount does not rescue it: the count is cumulative over the
	// entry's whole life, so a service that worked for months and then went
	// away keeps every settlement it ever earned.
	TrustStale Trust = iota
	// TrustNew means recent but barely paid, or undated. Not a mark against
	// the endpoint, just an absence of evidence either way.
	TrustNew
	// TrustActive means paid by real callers, recently enough to believe it
	// still answers.
	TrustActive
	// TrustProven means paid heavily and paid in the last few days.
	TrustProven
)

func (t Trust) String() string {
	switch t {
	case TrustProven:
		return "proven"
	case TrustActive:
		return "active"
	case TrustNew:
		return "new"
	default:
		return "stale"
	}
}

// The thresholds, chosen against a real sample of the live catalog (467
// entries, 2026-09-17): 36% had not been paid in over 30 days, 60% had been
// paid fewer than 5 times, and 10% had 50 or more settlements. So staleAfter
// separates a real third of the catalog rather than a rounding error, and
// provenSettles names the top tenth rather than everything with a pulse.
const (
	staleAfter    = 30 * 24 * time.Hour
	provenWithin  = 7 * 24 * time.Hour
	provenSettles = 50
	activeSettles = 5

	// provenHistory is how long an entry must have been in the catalog
	// before a high settle count counts as a track record. Nothing stops a
	// publisher from paying its own endpoint, and a thousand settlements
	// earned in a day is a different claim from a thousand earned over
	// months. An entry too young for this is capped at TrustActive: still
	// good evidence, just not yet proven.
	provenHistory = 14 * 24 * time.Hour
)

// firstPaid parses an entry's firstSeen, reporting false the same way
// lastPaid does.
func firstPaid(r Resource) (time.Time, bool) {
	if r.FirstSeen == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, r.FirstSeen)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// lastPaid parses an entry's lastSeen. Reports false when the catalog gave
// no timestamp or gave one we cannot read, which is treated as no evidence
// rather than as an old date.
func lastPaid(r Resource) (time.Time, bool) {
	if r.LastSeen == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, r.LastSeen)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// DaysSinceLastPaid is how many whole days ago this endpoint was last paid,
// or -1 when the catalog does not say. Reported to the builder alongside the
// tier, because "last paid 112 days ago" is a fact the model can act on
// where a bare label is something it has to take on faith.
func DaysSinceLastPaid(r Resource, now time.Time) int {
	t, ok := lastPaid(r)
	if !ok {
		return -1
	}
	age := now.Sub(t)
	if age < 0 {
		// Our clock and the facilitator's disagree. Not an error worth
		// surfacing, and certainly not a reason to call the entry stale.
		return 0
	}
	return int(age / (24 * time.Hour))
}

// TrustOf grades one entry as of now.
func TrustOf(r Resource, now time.Time) Trust {
	t, ok := lastPaid(r)
	if !ok {
		return TrustNew
	}
	age := now.Sub(t)
	if age < 0 {
		age = 0
	}
	switch {
	case age > staleAfter:
		return TrustStale
	case r.SettleCount >= provenSettles && age <= provenWithin:
		if first, ok := firstPaid(r); !ok || now.Sub(first) < provenHistory {
			return TrustActive
		}
		return TrustProven
	case r.SettleCount >= activeSettles:
		return TrustActive
	default:
		return TrustNew
	}
}
