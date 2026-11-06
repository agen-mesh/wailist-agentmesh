package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/bazaar"
)

const bzMainnet = "eip155:1874"

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
						"asset": "0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7", "payTo": "P",
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

// TestCatalogFollowerWaitsOnInflightCrawlInsteadOfTakingBackoffBranch guards
// against checking the backoff window before the inflight channel: a
// follower arriving while a crawl is already running (and still within the
// backoff window that crawl's own start just set) must wait on that crawl,
// not read items/lastErr while both are still nil on a cold start and
// return a spurious empty "200 {items: [], total: 0}".
func TestCatalogFollowerWaitsOnInflightCrawlInsteadOfTakingBackoffBranch(t *testing.T) {
	orig := bazaarRetryBackoff
	bazaarRetryBackoff = 5 * time.Second
	defer func() { bazaarRetryBackoff = orig }()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		items := []map[string]any{}
		if r.URL.Query().Get("offset") == "0" {
			items = append(items, map[string]any{
				"id": "r0", "resourceUrl": "https://h0.example/api", "method": "GET",
				"accepts": []any{map[string]any{
					"network": bzMainnet, "amount": "5000", "asset": "0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7", "payTo": "P",
				}},
			})
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer srv.Close()
	d := &Deps{BazaarBaseURL: srv.URL}

	firstDone := make(chan struct{})
	go func() {
		d.catalog(context.Background())
		close(firstDone)
	}()
	time.Sleep(20 * time.Millisecond) // let the first caller actually start the crawl

	secondResult := make(chan []bazaar.Resource, 1)
	go func() {
		items, _ := d.catalog(context.Background())
		secondResult <- items
	}()

	select {
	case items := <-secondResult:
		t.Fatalf("second caller returned early with %d items instead of waiting for the in-flight crawl", len(items))
	case <-time.After(50 * time.Millisecond):
		// Still waiting on the shared crawl, as expected.
	}

	close(release)
	<-firstDone
	items := <-secondResult
	// Merge() also appends the curated registry's own entries, so the total
	// isn't exactly 1 -- what matters is that the fetched item made it
	// through, proving the second caller saw the real crawl result rather
	// than a spurious pre-completion nil/nil.
	found := false
	for _, r := range items {
		if r.ID == "r0" {
			found = true
		}
	}
	if !found {
		t.Errorf("want the second caller to see the real crawl result (item r0 present), got %d items: %+v", len(items), items)
	}
}

func TestCatalogConcurrentColdRequestsShareOneCrawlAndHonourCancellation(t *testing.T) {
	var hits int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		<-release // block the whole crawl until the test says go
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}})
	}))
	defer srv.Close()
	d := &Deps{BazaarBaseURL: srv.URL}

	// A caller whose own context is cancelled must not wait for the shared
	// crawl to finish — it should return as soon as its ctx is done, freeing
	// its handler goroutine/connection instead of blocking for the full
	// crawl duration alongside every other waiter.
	cancelledCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := d.catalog(cancelledCtx)
		done <- err
	}()
	// Give the first goroutine time to actually start the crawl (acquire
	// inflight) before a second concurrent caller arrives.
	time.Sleep(20 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.catalog(context.Background())
		}()
	}

	cancel()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled caller returned err=%v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled caller did not return promptly — it's blocking on the shared crawl")
	}

	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("upstream hit %d times across 4 concurrent cold-start callers, want exactly 1 shared crawl", got)
	}
}

// TestForceCatalogRefreshRefetchesEvenWhenCacheIsFresh is what WarmBazaarCache
// actually depends on: catalog() itself is a no-op while the cache is still
// within its TTL (correctly -- a real request should never pay for a crawl no
// one asked for), so the background warmer needs a way to force one anyway,
// ahead of expiry. This pins that forceCatalogRefresh does exactly that.
// TestForceCatalogRefreshDoesNotStartASecondConcurrentCrawl guards the other
// half of the same contract: the background warmer's own ticker firing while
// a real request's catalog() call (or a previous tick) is already mid-crawl
// must join that crawl, not start a second one against the same upstream.
func TestForceCatalogRefreshDoesNotStartASecondConcurrentCrawl(t *testing.T) {
	var hits int32
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		<-block // held open until the test releases it
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer srv.Close()
	d := &Deps{BazaarBaseURL: srv.URL}

	// inflight is set synchronously, before the crawl's goroutine is even
	// spawned (see forceCatalogRefresh), so the second call below is
	// guaranteed to observe it -- no sleep-based race to make this
	// deterministic.
	d.forceCatalogRefresh()
	d.forceCatalogRefresh()

	close(block)

	d.bazaarCache.mu.Lock()
	ch := d.bazaarCache.inflight
	d.bazaarCache.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("crawl did not finish in time")
		}
	}

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("forceCatalogRefresh started %d concurrent crawls, want 1", got)
	}
}
