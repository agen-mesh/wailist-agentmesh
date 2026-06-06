package nodes_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestX402FreeEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":"free response"}`))
	}))
	defer srv.Close()
	node := models.WorkflowNode{ID: "x1", Type: models.NodeTypeTool402, Endpoint: srv.URL}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteTool402(context.Background(), node, rc, models.AgentWallet{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]any)
	if !ok || m["data"] != "free response" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestX402ParseQuote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Payment-Required", `{"price":"0.001","unit":"call","network":"algorand-testnet","recipient":"ALGO123"}`)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()
	price, err := nodes.QuoteX402(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if price["price"] != "0.001" {
		t.Fatalf("want price 0.001 got %v", price["price"])
	}
}
