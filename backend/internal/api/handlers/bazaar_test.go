package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/agentmesh/backend/internal/bazaar"
)

const bzMainnet = "algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8="

// fakeCatalog serves n synthetic resources on the first page and counts how
// often the upstream was hit, so caching can be asserted.
func fakeCatalog(n int, hits *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		items := []map[string]any{}
		// Everything fits on page 0 (n stays well under the 100 page limit),
		// so any later offset correctly returns an empty page.
		if r.URL.Query().Get("offset") == "0" {
			for i := 0; i < n; i++ {
				id := fmt.Sprintf("r%d", i)
				items = append(items, map[string]any{
					"id":          id,
					"resourceUrl": fmt.Sprintf("https://h%d.example/api", i),
					"method":      "GET",
					"description": "synthetic resource",
					"accepts": []any{map[string]any{
						"network": bzMainnet, "amount": "5000",
						"asset": "31566704", "payTo": "P",
					}},
					// Descending, so the handler's settle-count ordering is
					// observable: r0 must come first.
					"settleCount": n - i,
				})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
}

func TestBazaarResourcesPages(t *testing.T) {
	var hits int32
	srv := fakeCatalog(30, &hits)
	defer srv.Close()
	d := &Deps{BazaarBaseURL: srv.URL}

	rec := httptest.NewRecorder()
	d.BazaarResources(rec, httptest.NewRequest(http.MethodGet, "/bazaar/resources?offset=0&limit=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []bazaar.Resource `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Items) != 10 {
		t.Errorf("first page has %d items, want 10", len(page.Items))
	}
	// 30 synthetic + however many curated entries the registry appends.
	if page.Total < 30 {
		t.Errorf("Total = %d, want at least 30", page.Total)
	}

	rec2 := httptest.NewRecorder()
	d.BazaarResources(rec2, httptest.NewRequest(http.MethodGet, "/bazaar/resources?offset=10&limit=10", nil))
	var page2 struct {
		Items []bazaar.Resource `json:"items"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &page2)
	if len(page2.Items) != 10 {
		t.Fatalf("second page has %d items, want 10", len(page2.Items))
	}
	if page.Items[0].ID == page2.Items[0].ID {
		t.Error("second page repeated the first page's first item")
	}
}

func TestBazaarResourcesCachesUpstream(t *testing.T) {
	var hits int32
	srv := fakeCatalog(5, &hits)
	defer srv.Close()
	d := &Deps{BazaarBaseURL: srv.URL}

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		d.BazaarResources(rec, httptest.NewRequest(http.MethodGet, "/bazaar/resources", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}
	// One crawl is two upstream calls at most (page 0 plus the short-page
	// stop); three handler calls must not multiply that.
	if got := atomic.LoadInt32(&hits); got > 2 {
		t.Errorf("upstream hit %d times across 3 cached requests, want <= 2", got)
	}
}

func TestBazaarResourcesSearchFilters(t *testing.T) {
	var hits int32
	srv := fakeCatalog(5, &hits)
	defer srv.Close()
	d := &Deps{BazaarBaseURL: srv.URL}

	rec := httptest.NewRecorder()
	d.BazaarResources(rec, httptest.NewRequest(http.MethodGet, "/bazaar/resources?q=tendril", nil))
	var page struct {
		Items []bazaar.Resource `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &page)
	for _, it := range page.Items {
		if it.Provider != "Tendril" {
			t.Errorf("q=tendril returned unrelated entry %s", it.URL)
		}
	}
}

func TestBazaarResourcesUpstreamFailureIsBadGateway(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	d := &Deps{BazaarBaseURL: srv.URL}

	rec := httptest.NewRecorder()
	d.BazaarResources(rec, httptest.NewRequest(http.MethodGet, "/bazaar/resources", nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}
