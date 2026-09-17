package bazaar

import (
	"strings"
	"testing"
	"time"
)

func res(id, host, desc string, settles int) Resource {
	return Resource{ID: id, Host: host, URL: "https://" + host + "/v1", Description: desc, SettleCount: settles,
		FirstSeen: trustNow.AddDate(0, 0, -200).Format(time.RFC3339)}
}

// The builder asks in plain language. FuzzyMatch wants one short word -- a
// whole sentence almost never matches as one subsequence -- so Search matches
// word by word and ranks entries matching MORE of the words first.
func TestSearchMatchesNaturalLanguageWordByWord(t *testing.T) {
	items := []Resource{
		res("a", "weather.example.com", "Current weather forecast for any city", 5),
		res("b", "stocks.example.com", "Live stock market index prices", 3),
		res("c", "news.example.com", "Top headlines", 9),
	}
	got := Search(items, "live stock index prices for NSE", 10)
	if len(got) == 0 || got[0].ID != "b" {
		t.Fatalf("want the stock endpoint first, got %+v", ids(got))
	}
	for _, r := range got {
		if r.ID == "c" {
			t.Fatal("an entry matching none of the words must not be returned")
		}
	}
}

func TestSearchRanksMoreMatchedWordsFirst(t *testing.T) {
	items := []Resource{
		res("one", "a.example.com", "weather data", 100),
		res("two", "b.example.com", "weather forecast data", 1),
	}
	got := Search(items, "weather forecast", 10)
	if len(got) != 2 || got[0].ID != "two" {
		t.Fatalf("matching both words must outrank matching one, however popular: %v", ids(got))
	}
}

// Console-backed entries (Prism, HelixBox) open a dedicated page, not a
// canvas node, so the builder must never be offered them as nodes.
func TestSearchSkipsConsoleEntries(t *testing.T) {
	c := res("p", "prism.example.com", "code review", 50)
	c.Console = "prism"
	got := Search([]Resource{c, res("x", "x.example.com", "code review bot", 1)}, "code review", 10)
	if len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("console entries must be excluded: %v", ids(got))
	}
}

func TestSearchHonoursLimitAndIgnoresNoiseWords(t *testing.T) {
	var items []Resource
	for i := 0; i < 20; i++ {
		items = append(items, res(string(rune('a'+i)), "w.example.com", "weather", i))
	}
	if got := Search(items, "weather", 5); len(got) != 5 {
		t.Fatalf("want 5, got %d", len(got))
	}
	// "api" and "the" appear in nearly every URL and description; on their
	// own they must not match everything.
	if got := Search(items, "the api", 5); len(got) != 0 {
		t.Fatalf("noise words alone must match nothing, got %d", len(got))
	}
}

func ids(rs []Resource) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

// The live catalog holds hundreds of entries that mention Algorand, so an
// Algorand query has to be won by the words beside "algorand", not by that
// word alone.
func TestSearchPrefersAnAccountLookupForAnAccountQuery(t *testing.T) {
	items := []Resource{
		{
			ID: "noise", Provider: "Otto", Host: "algorand.ottoai.services",
			URL:         "https://algorand.ottoai.services/trending-pools",
			Description: "Trending DEX pools right now on Algorand and other chains",
			Network:     AlgorandMainnet,
		},
		{
			ID: "account", Provider: "AlgorandIndexer", Host: "api.algorand-indexer.xyz",
			URL:         "https://api.algorand-indexer.xyz/account/ABC",
			Description: "Algorand account lookup: balance, status, participation, and auth-addr for a single address",
			Network:     AlgorandMainnet,
		},
	}
	got := Search(items, "algorand account balance for an address", 5)
	if len(got) == 0 {
		t.Fatal("no results")
	}
	if got[0].ID != "account" {
		t.Errorf("best match is %q, want the account lookup", got[0].ID)
	}
}

func TestSearchFindsAnASAQuery(t *testing.T) {
	items := []Resource{
		{
			ID: "asa", Provider: "Blueprints", Host: "api.blueprintstech.org",
			URL:         "https://api.blueprintstech.org/asa/verify",
			Description: "Algorand ASA and NFT verification with supply, control-address, and metadata checks",
			Network:     AlgorandMainnet,
		},
		{
			ID: "other", Provider: "Otto", Host: "algorand.ottoai.services",
			URL:         "https://algorand.ottoai.services/equities",
			Description: "Registry of tokenized US equities on Robinhood Chain",
			Network:     AlgorandMainnet,
		},
	}
	got := Search(items, "ASA supply and metadata", 5)
	if len(got) == 0 || got[0].ID != "asa" {
		t.Fatalf("best match is %+v, want the ASA verifier", got)
	}
}

// A dead endpoint is not a 404, it is a real payment for nothing. Among
// entries that matched as many of the query's words, one nobody has paid in
// months ranks below every entry that is still being paid -- even when the
// stale one is worded better and scores higher.
func TestSearchAtDemotesStaleEntriesBelowLiveOnes(t *testing.T) {
	const query = "algorand transaction history"
	// Leading match, so it takes FuzzyMatch's early-position bonus on every
	// word and outscores the live entry, which buries the same words.
	stale := res("stale", "a.example.com", "Algorand transaction history for an address", 900)
	stale.LastSeen = trustNow.AddDate(0, 0, -200).Format(time.RFC3339)
	live := res("live", "b.example.com", "Endpoint returning the full Algorand transaction history for an address", 6)
	live.LastSeen = trustNow.AddDate(0, 0, -1).Format(time.RFC3339)

	if TrustOf(stale, trustNow) != TrustStale || TrustOf(live, trustNow) != TrustActive {
		t.Fatalf("fixture is wrong: stale=%s live=%s", TrustOf(stale, trustNow), TrustOf(live, trustNow))
	}
	// The premise of this test: without the stale rule, the stale entry wins
	// on score. If this ever stops holding the test proves nothing.
	if staleScore, liveScore := queryScore(stale, query), queryScore(live, query); staleScore <= liveScore {
		t.Fatalf("fixture is wrong: stale scores %d, live scores %d -- the stale entry must score higher", staleScore, liveScore)
	}

	got := SearchAt([]Resource{stale, live}, query, 5, trustNow)
	if len(got) != 2 || got[0].ID != "live" {
		t.Fatalf("a still-paid endpoint must outrank a better-worded stale one: %v", ids(got))
	}
}

// queryScore is the total BestFieldScore Search would give r, used to assert
// a fixture really is scored the way a test claims.
func queryScore(r Resource, query string) int {
	total := 0
	for _, w := range strings.Fields(query) {
		if score, ok := BestFieldScore(r, w); ok && score >= len(w) {
			total += score
		}
	}
	return total
}

// Relevance still comes first: trust breaks ties between comparable matches,
// it does not let a popular endpoint answer a question it does not answer.
func TestSearchAtKeepsRelevanceAboveTrust(t *testing.T) {
	popular := res("popular", "a.example.com", "Algorand account balance lookup", 5000)
	popular.LastSeen = trustNow.Format(time.RFC3339)
	relevant := res("relevant", "b.example.com", "Algorand transaction history for an address", 1)
	relevant.LastSeen = trustNow.Format(time.RFC3339)

	got := SearchAt([]Resource{popular, relevant}, "algorand transaction history", 5, trustNow)
	if len(got) == 0 || got[0].ID != "relevant" {
		t.Fatalf("the entry matching more of the query must win: %v", ids(got))
	}
}

// Two entries that match the query identically and are both live: the one
// real callers have actually paid for goes first.
func TestSearchAtPrefersTheProvenOfTwoLiveEntries(t *testing.T) {
	quiet := res("quiet", "a.example.com", "Algorand transaction history for an address", 1)
	quiet.LastSeen = trustNow.AddDate(0, 0, -2).Format(time.RFC3339)
	proven := res("proven", "b.example.com", "Algorand transaction history for an address", 400)
	proven.LastSeen = trustNow.AddDate(0, 0, -1).Format(time.RFC3339)

	got := SearchAt([]Resource{quiet, proven}, "algorand transaction history", 5, trustNow)
	if len(got) != 2 || got[0].ID != "proven" {
		t.Fatalf("want the proven endpoint first: %v", ids(got))
	}
}

// Stale is a demotion, never a filter: if every match is stale, the builder
// still gets to see them and say so, rather than being told nothing exists.
func TestSearchAtStillReturnsStaleEntriesWhenNothingElseMatches(t *testing.T) {
	stale := res("stale", "a.example.com", "Algorand transaction history for an address", 900)
	stale.LastSeen = trustNow.AddDate(0, 0, -200).Format(time.RFC3339)
	got := SearchAt([]Resource{stale}, "algorand transaction history", 5, trustNow)
	if len(got) != 1 || got[0].ID != "stale" {
		t.Fatalf("stale entries must still be returned: %v", ids(got))
	}
}
