package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
)

// fakeBazaarServer returns an httptest.Server serving one Bazaar resource.
func fakeBazaarServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"resources": []map[string]any{
				{
					"id":          "abc123",
					"url":         "https://example.com/api",
					"name":        "Test API",
					"description": "A test API",
					"provider":    "TestCo",
					"price":       map[string]any{"amount": "0.005", "currency": "USDC", "network": "base-mainnet"},
					"tags":        []string{"test", "data"},
					"category":    "data",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{"type": "string", "description": "Search query"},
						},
						"required": []string{"query"},
					},
				},
			},
		})
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
