package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
)

var fakeMerchant = map[string]any{
	"id":            "merchant-1",
	"name":          "Weather Co",
	"description":   "Real-time weather APIs",
	"website":       "https://weatherco.example.com",
	"logo":          "",
	"categories":    []string{"data"},
	"resourceCount": 1,
}

var fakeResource = map[string]any{
	"id":          "resource-1",
	"resourceUrl": "https://api.weatherco.example.com/weather",
	"method":      "GET",
	"description": "Get weather by city",
	"mimeType":    "application/json",
	"merchantId":  "merchant-1",
	"accepts": []map[string]any{
		{"scheme": "exact", "network": "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=", "amount": "1000", "payTo": "ALGO_ADDR_HERE"},
	},
	"discoveryInfo": map[string]any{
		"input": map[string]any{
			"method":      "GET",
			"queryParams": map[string]any{"city": "San Francisco"},
		},
		"output": map[string]any{"example": map[string]any{"temperature": 18.5}},
	},
	"verifyCount": 0,
	"settleCount": 0,
}

func fakeGoPlausibleServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "merchants") {
			json.NewEncoder(w).Encode(map[string]any{
				"x402Version": 2,
				"items":       []any{fakeMerchant},
				"pagination":  map[string]any{"limit": 200, "offset": 0, "total": 1},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"x402Version": 2,
			"items":       []any{fakeResource},
			"pagination":  map[string]any{"limit": 50, "offset": 0, "total": 1},
		})
	}))
}

func TestGoplausibleList(t *testing.T) {
	fake := fakeGoPlausibleServer(t)
	defer fake.Close()
	t.Setenv("GOPLAUSIBLE_BASE_URL", fake.URL)

	d := &handlers.Deps{}
	req := httptest.NewRequest(http.MethodGet, "/marketplace/goplausible", nil)
	w := httptest.NewRecorder()
	d.GoplausibleList(w, req)

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
	if ep["source"] != "goplausible" {
		t.Errorf("want source=goplausible got %v", ep["source"])
	}
	if ep["chainFamily"] != "avm" {
		t.Errorf("want chainFamily=avm got %v", ep["chainFamily"])
	}
	if ep["endpoint"] != "https://api.weatherco.example.com/weather" {
		t.Errorf("want endpoint URL got %v", ep["endpoint"])
	}
	if ep["name"] != "Weather Co" {
		t.Errorf("want name=Weather Co (from merchant) got %v", ep["name"])
	}
	if ep["price"] != "0.0010" {
		t.Errorf("want price=0.0010 got %v", ep["price"])
	}
}

func TestGoplausibleListEmpty(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"x402Version": 2,
			"items":       []any{},
			"pagination":  map[string]any{"limit": 50, "offset": 0, "total": 0},
		})
	}))
	defer fake.Close()
	t.Setenv("GOPLAUSIBLE_BASE_URL", fake.URL)

	d := &handlers.Deps{}
	req := httptest.NewRequest(http.MethodGet, "/marketplace/goplausible", nil)
	w := httptest.NewRecorder()
	d.GoplausibleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	eps, _ := resp["endpoints"].([]any)
	// When catalog is empty the handler falls back to seeded example endpoints.
	if len(eps) < 1 {
		t.Errorf("want seeded example endpoints, got %d", len(eps))
	}
	ep0, _ := eps[0].(map[string]any)
	if ep0["source"] != "goplausible" {
		t.Errorf("want source=goplausible got %v", ep0["source"])
	}
	if ep0["chainFamily"] != "avm" {
		t.Errorf("want chainFamily=avm got %v", ep0["chainFamily"])
	}
}
