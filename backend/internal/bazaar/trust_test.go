package bazaar

import (
	"testing"
	"time"
)

// at builds an entry last paid days ago, with a track record long enough
// that only recency and settle count decide its tier.
func at(days int, settles int) Resource {
	r := Resource{
		SettleCount: settles,
		LastSeen:    trustNow.AddDate(0, 0, -days).Format(time.RFC3339),
	}
	r.FirstSeen = trustNow.AddDate(0, 0, -days-90).Format(time.RFC3339)
	return r
}

var trustNow = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

func TestTrustOfGradesOnPaymentsAndRecency(t *testing.T) {
	cases := []struct {
		name string
		r    Resource
		want Trust
	}{
		{"paid often and paid recently", at(1, 400), TrustProven},
		{"paid a few times this week", at(2, 9), TrustActive},
		{"paid often but not for months", at(120, 400), TrustStale},
		{"paid once, recently", at(1, 1), TrustNew},
		{"paid twice, five weeks ago", at(35, 2), TrustStale},
		{"heavily paid but last seen three weeks ago", at(21, 400), TrustActive},
	}
	for _, c := range cases {
		if got := TrustOf(c.r, trustNow); got != c.want {
			t.Errorf("%s: TrustOf = %s, want %s", c.name, got, c.want)
		}
	}
}

// An entry the catalog never dated cannot be called proven on settle count
// alone: settleCount is cumulative over the entry's whole life, so a long-
// dead endpoint keeps the total it earned while it worked.
func TestTrustOfWillNotPromoteAnUndatedEntry(t *testing.T) {
	r := Resource{SettleCount: 5000}
	if got := TrustOf(r, trustNow); got != TrustNew {
		t.Errorf("TrustOf with no lastSeen = %s, want %s", got, TrustNew)
	}
	r.LastSeen = "not a timestamp"
	if got := TrustOf(r, trustNow); got != TrustNew {
		t.Errorf("TrustOf with an unparseable lastSeen = %s, want %s", got, TrustNew)
	}
}

// Nothing stops a publisher paying its own endpoint, so a big settle count
// earned in a couple of days is not the track record it looks like.
func TestTrustOfWillNotCallAYoungEntryProven(t *testing.T) {
	r := at(1, 400)
	r.FirstSeen = trustNow.AddDate(0, 0, -2).Format(time.RFC3339)
	if got := TrustOf(r, trustNow); got != TrustActive {
		t.Errorf("TrustOf = %s, want %s", got, TrustActive)
	}
	r.FirstSeen = ""
	if got := TrustOf(r, trustNow); got != TrustActive {
		t.Errorf("TrustOf with no firstSeen = %s, want %s", got, TrustActive)
	}
}

// A clock skew between us and the facilitator must not make a live endpoint
// look like it was paid in the future and therefore skip a tier it has not
// earned -- a negative age is still "recent", nothing more.
func TestTrustOfToleratesAFutureTimestamp(t *testing.T) {
	r := Resource{SettleCount: 1, LastSeen: trustNow.Add(time.Hour).Format(time.RFC3339)}
	if got := TrustOf(r, trustNow); got != TrustNew {
		t.Errorf("TrustOf = %s, want %s", got, TrustNew)
	}
}

func TestDaysSinceLastPaidReportsUnknownAsNegative(t *testing.T) {
	if got := DaysSinceLastPaid(Resource{}, trustNow); got >= 0 {
		t.Errorf("DaysSinceLastPaid with no lastSeen = %d, want negative", got)
	}
	if got := DaysSinceLastPaid(at(12, 3), trustNow); got != 12 {
		t.Errorf("DaysSinceLastPaid = %d, want 12", got)
	}
}
