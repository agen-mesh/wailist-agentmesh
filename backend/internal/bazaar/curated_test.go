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

func TestMergeDoesNotDuplicateWhenTwoCatalogEntriesShareOneCuratedURL(t *testing.T) {
	// Regression: if the upstream catalog (re-)registers the same real
	// endpoint under two distinct catalog ids, both used to match the same
	// curated URL and both got appended as separate Supported=true rows --
	// the real endpoint rendered twice in the pinned section.
	//
	// Exactly one row survives. An earlier fix kept the second entry as an
	// unsupported row on the grounds that a re-registration is real data,
	// but that was worse, not better: BazaarPage fetches the pinned
	// section with supported=1 and the grid with supported=0, so the same
	// endpoint appeared in BOTH -- pinned with the curated description and
	// again in the grid with the publisher's own. catalog is sorted by
	// settle count descending, so the surviving row is the most
	// established registration.
	curatedURL := Curated()[0].URL
	catalog := []Resource{
		{ID: "cat1", URL: curatedURL, Method: "GET", SettleCount: 5},
		{ID: "cat2-reregistered", URL: curatedURL, Method: "GET", SettleCount: 3},
	}
	got := Merge(catalog)

	total, supportedCount := 0, 0
	var kept Resource
	for _, r := range got {
		if r.URL != curatedURL {
			continue
		}
		total++
		kept = r
		if r.Supported {
			supportedCount++
		}
	}
	if total != 1 {
		t.Errorf("want exactly 1 row for the shared curated URL, got %d", total)
	}
	if supportedCount != 1 {
		t.Errorf("want the surviving row flagged Supported, got %d supported of %d", supportedCount, total)
	}
	if kept.ID != "cat1" {
		t.Errorf("want the higher-settle-count registration kept, got id %q", kept.ID)
	}
}

// TestMergeDedupesNonCuratedCatalogEntriesSharingOneURL guards the
// non-curated counterpart of TestMergeDoesNotDuplicateWhenTwoCatalogEntriesShareOneCuratedURL:
// two catalog entries under different ids that share a real resourceUrl
// which ISN'T a curated one. Unlike the curated case, a plain community
// entry has no Supported badge to de-duplicate against, so without this
// fix both would render as indistinguishable duplicate cards in the
// community grid. Only the first (higher settle count -- FetchAll's own
// sort order) is kept.
func TestMergeDedupesNonCuratedCatalogEntriesSharingOneURL(t *testing.T) {
	const sharedURL = "https://re-registered.example.com/api"
	catalog := []Resource{
		{ID: "cat1", URL: sharedURL, Method: "GET", SettleCount: 9},
		{ID: "cat2-reregistered", URL: sharedURL, Method: "GET", SettleCount: 2},
	}
	got := Merge(catalog)

	count := 0
	var kept Resource
	for _, r := range got {
		if r.URL == sharedURL {
			count++
			kept = r
		}
	}
	if count != 1 {
		t.Fatalf("want exactly 1 row for the shared non-curated URL, got %d", count)
	}
	if kept.ID != "cat1" {
		t.Errorf("want the higher-settle-count entry kept, got id %q", kept.ID)
	}
	if kept.Supported {
		t.Errorf("want a non-curated URL to stay unsupported, got Supported=true")
	}
}

func TestMergeMatchesURLWithTrailingSlashVariant(t *testing.T) {
	curatedURL := Curated()[0].URL
	catalog := []Resource{
		{ID: "cat1", URL: curatedURL + "/", Method: "GET", SettleCount: 9},
	}
	got := Merge(catalog)

	count := 0
	var matched *Resource
	for i := range got {
		// Both the trailing-slash catalog URL and the bare curated URL must
		// resolve to ONE row, not two.
		if got[i].URL == curatedURL || got[i].URL == curatedURL+"/" {
			count++
			matched = &got[i]
		}
	}
	if count != 1 {
		t.Fatalf("want exactly 1 row for the trailing-slash variant, got %d", count)
	}
	if !matched.Supported {
		t.Error("trailing-slash variant must still be marked Supported")
	}
	if matched.SettleCount != 9 {
		t.Errorf("SettleCount = %d, want 9 (catalog telemetry must survive)", matched.SettleCount)
	}
}
