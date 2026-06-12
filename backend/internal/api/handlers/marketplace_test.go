package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
)

// oneItem is the real Bazaar item shape (resource/serviceName/accepts/extensions).
var oneItem = map[string]any{
	"resource":    "https://example.com/api",
	"serviceName": "TestCo",
	"description": "A test API",
	"tags":        []string{"test", "data"},
	"type":        "data",
	"accepts": []map[string]any{
		{"amount": "1000", "network": "eip155:8453"},
	},
	"extensions": map[string]any{
		"bazaar": map[string]any{
			"schema": map[string]any{
				"properties": map[string]any{
					"input": map[string]any{
						"properties": map[string]any{
							"queryParams": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"query": map[string]any{"type": "string", "description": "Search query"},
								},
								"required": []string{"query"},
							},
						},
					},
				},
			},
		},
	},
}

// fakeBazaarServer returns an httptest.Server serving one Bazaar item using
// the real API shapes: list → "items", search → "resources".
func fakeBazaarServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "search") {
			json.NewEncoder(w).Encode(map[string]any{"resources": []any{oneItem}})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"items": []any{oneItem}})
		}
	}))
}

func TestBazaarList(t *testing.T) {
	fake := fakeBazaarServer(t)
	defer fake.Close()
	t.Setenv("BAZAAR_BASE_URL", fake.URL)

	d := &handlers.Deps{}
	req := httptest.NewRequest(http.MethodGet, "/marketplace/bazaar", nil)
	w := httptest.NewRecorder()
	d.BazaarList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	eps, _ := resp["endpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("want 1 endpoint got %d", len(eps))
	}
	ep, _ := eps[0].(map[string]any)
	if ep["source"] != "bazaar" {
		t.Errorf("want source=bazaar got %v", ep["source"])
	}
	if ep["endpoint"] != "https://example.com/api" {
		t.Errorf("want endpoint URL got %v", ep["endpoint"])
	}
	params, _ := ep["discoveredParams"].([]any)
	if len(params) != 1 {
		t.Errorf("want 1 param got %d", len(params))
	}
}

func TestBazaarSearch(t *testing.T) {
	fake := fakeBazaarServer(t)
	defer fake.Close()
	t.Setenv("BAZAAR_BASE_URL", fake.URL)

	d := &handlers.Deps{}
	req := httptest.NewRequest(http.MethodGet, "/marketplace/bazaar/search?q=weather", nil)
	w := httptest.NewRecorder()
	d.BazaarSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	eps, _ := resp["endpoints"].([]any)
	if len(eps) < 1 {
		t.Error("want at least 1 endpoint")
	}
}

func TestBazaarSearchMissingQ(t *testing.T) {
	d := &handlers.Deps{}
	req := httptest.NewRequest(http.MethodGet, "/marketplace/bazaar/search", nil)
	w := httptest.NewRecorder()
	d.BazaarSearch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestBazaarListChainFamily(t *testing.T) {
	fake := fakeBazaarServer(t)
	defer fake.Close()
	t.Setenv("BAZAAR_BASE_URL", fake.URL)

	d := &handlers.Deps{}
	req := httptest.NewRequest(http.MethodGet, "/marketplace/bazaar", nil)
	w := httptest.NewRecorder()
	d.BazaarList(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	eps, _ := resp["endpoints"].([]any)
	if len(eps) == 0 {
		t.Fatal("want at least 1 endpoint")
	}
	ep, _ := eps[0].(map[string]any)
	if ep["chainFamily"] != "evm" {
		t.Errorf("want chainFamily=evm for Coinbase Bazaar items, got %v", ep["chainFamily"])
	}
}
