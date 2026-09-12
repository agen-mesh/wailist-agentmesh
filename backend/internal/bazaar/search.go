package bazaar

import (
	"sort"
	"strings"
)

// BestFieldScore reports whether q fuzzy-matches r in AT LEAST ONE of its own
// fields, taking the best score among the fields that do.
//
// Matched per field, not against one string the caller joins first: a query
// like "tendril" is short enough that concatenating Provider+Host+URL+
// Description into one haystack routinely lets it complete as a scattered
// subsequence spanning several UNRELATED fields (a 't' from the URL, an 'e'
// from the description, and so on) -- a real false positive this caught in
// review, matching a Prism entry on a search for "tendril". Checking each
// field on its own means every letter of the query has to come from the
// SAME field, which is what "this actually matches" should mean.
func BestFieldScore(r Resource, q string) (int, bool) {
	best := 0
	matched := false
	for _, field := range [...]string{r.Provider, r.Host, r.URL, r.Description} {
		if score, ok := FuzzyMatch(field, q); ok && (!matched || score > best) {
			best = score
			matched = true
		}
	}
	return best, matched
}

// searchNoise are words that appear in nearly every URL or description (or
// carry no meaning at all), so matching one says nothing about relevance.
var searchNoise = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true, "that": true,
	"this": true, "api": true, "apis": true, "get": true, "http": true, "https": true,
	"www": true, "com": true, "endpoint": true, "x402": true, "any": true, "some": true,
}

// Search ranks canvas-addable resources against a natural-language query and
// returns at most limit of them, best first.
//
// Word by word, unlike the Bazaar page's single-string match: a person types
// "tendril" into a search box, but the chat builder asks things like "live
// NSE stock index prices", and a whole sentence essentially never fuzzy-
// matches as one subsequence. Each meaningful word is scored with
// BestFieldScore; an entry matching more of the words ranks first, then by
// total score, then by how often it has actually been paid for.
//
// Console-backed entries (Prism, HelixBox) are skipped: they open a dedicated
// page rather than becoming a canvas node, so they are never something the
// builder can add.
func Search(items []Resource, query string, limit int) []Resource {
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) >= 3 && !searchNoise[w] {
			words = append(words, w)
		}
	}
	if len(words) == 0 {
		return nil
	}
	type scored struct {
		r            Resource
		words, total int
	}
	var hits []scored
	for _, r := range items {
		if r.Console != "" {
			continue
		}
		var s scored
		s.r = r
		for _, w := range words {
			// A word only counts if it matched with real contiguity. Short
			// words complete as scattered subsequences by chance -- "nse"
			// "matches" news.example.com as n..s..e with a score of 1, where
			// a whole word scores several points per letter. The Bazaar
			// page's search box has no such floor: a person typing one word
			// wants typo tolerance, while a sentence from the builder has
			// enough short words to drown the real hits in noise.
			if score, ok := BestFieldScore(r, w); ok && score >= len(w) {
				s.words++
				s.total += score
			}
		}
		if s.words > 0 {
			hits = append(hits, s)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].words != hits[j].words {
			return hits[i].words > hits[j].words
		}
		if hits[i].total != hits[j].total {
			return hits[i].total > hits[j].total
		}
		return hits[i].r.SettleCount > hits[j].r.SettleCount
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Resource, len(hits))
	for i, h := range hits {
		out[i] = h.r
	}
	return out
}
