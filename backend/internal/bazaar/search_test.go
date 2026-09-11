package bazaar

import "testing"

func res(id, host, desc string, settles int) Resource {
	return Resource{ID: id, Host: host, URL: "https://" + host + "/v1", Description: desc, SettleCount: settles}
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
