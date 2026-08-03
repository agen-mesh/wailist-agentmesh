package bazaar

import "testing"

func TestCuratedEntriesAreWellFormed(t *testing.T) {
	for _, c := range Curated() {
		if c.URL == "" || c.ID == "" || c.Provider == "" {
			t.Errorf("curated entry missing id/url/provider: %+v", c)
		}
		if !c.Supported {
			t.Errorf("curated entry %s must have Supported=true", c.ID)
		}
		if c.Method == "" {
			t.Errorf("curated entry %s must declare an HTTP method", c.ID)
		}
	}
}

func TestMergeMarksMatchingCatalogEntrySupported(t *testing.T) {
	curatedURL := Curated()[0].URL
	catalog := []Resource{
		{ID: "cat1", URL: curatedURL, Method: "GET", SettleCount: 3},
		{ID: "cat2", URL: "https://unrelated.example/x", Method: "GET"},
	}
	got := Merge(catalog)

	var matched, unrelated *Resource
	for i := range got {
		switch got[i].URL {
		case curatedURL:
			matched = &got[i]
		case "https://unrelated.example/x":
			unrelated = &got[i]
		}
	}
	if matched == nil {
		t.Fatal("catalog entry matching a curated URL disappeared")
	}
	if !matched.Supported {
		t.Error("catalog entry matching a curated URL must be marked Supported")
	}
	// Real catalog telemetry must survive the merge — it is not in our registry.
	if matched.SettleCount != 3 {
		t.Errorf("SettleCount = %d, want 3 (live data must not be overwritten)", matched.SettleCount)
	}
	if unrelated == nil || unrelated.Supported {
		t.Error("unrelated catalog entry must stay unsupported")
	}
}

func TestMergeAppendsCuratedEntriesAbsentFromCatalog(t *testing.T) {
	// Tendril is genuinely absent from the live catalog, so a merge over an
	// empty catalog must still surface every curated entry.
	got := Merge(nil)
	if len(got) != len(Curated()) {
		t.Fatalf("want %d curated entries with an empty catalog, got %d", len(Curated()), len(got))
	}
	for _, r := range got {
		if !r.Supported {
			t.Errorf("entry %s appended from the registry must be Supported", r.ID)
		}
	}
}

func TestMergeDoesNotDuplicate(t *testing.T) {
	curatedURL := Curated()[0].URL
	got := Merge([]Resource{{ID: "cat1", URL: curatedURL, Method: "GET"}})
	count := 0
	for _, r := range got {
		if r.URL == curatedURL {
			count++
		}
	}
	if count != 1 {
		t.Errorf("curated URL appears %d times, want exactly 1", count)
	}
}
