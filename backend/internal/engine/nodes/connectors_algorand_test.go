package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// These tests live in package nodes to reach the unexported fetcher, so they
// use emptyRunContext rather than engine.NewRunContext: engine imports nodes,
// and an in-package test importing engine would be an import cycle.

func TestAlgorandAccountFlattensTheHoldings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v2/accounts/ABCDEF") {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"address":     "ABCDEF",
			"amount":      2500000,
			"min-balance": 100000,
			"assets":      []map[string]any{{"asset-id": 31566704, "amount": 1500000}},
		})
	}))
	defer srv.Close()
	SetAlgorandBases(srv.URL)
	defer SetAlgorandBases("")

	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_account",
		Config: map[string]string{"algoAddress": "ABCDEF"}}
	out, err := fetchAlgorandAccount(context.Background(), node, emptyRunContext{})
	if err != nil {
		t.Fatalf("fetchAlgorandAccount: %v", err)
	}
	m := out.(map[string]any)
	// Whole ALGO, not microalgos: every agent that has ever been handed
	// microalgos has reported them as ALGO.
	if m["algo"] != 2.5 {
		t.Errorf("algo = %v, want 2.5", m["algo"])
	}
	if m["minBalance"] != 0.1 {
		t.Errorf("minBalance = %v, want 0.1", m["minBalance"])
	}
	if m["address"] != "ABCDEF" {
		t.Errorf("address = %v, want ABCDEF", m["address"])
	}
	assets, ok := m["assets"].([]map[string]any)
	if !ok || len(assets) != 1 {
		t.Fatalf("assets = %#v", m["assets"])
	}
	if assets[0]["assetId"] != int64(31566704) || assets[0]["amount"] != int64(1500000) {
		t.Errorf("asset = %#v", assets[0])
	}
}

// A fresh wallet has no assets. This must be an empty list, never nil: nil
// marshals to null and an agent describes null as "unknown" rather than
// "none".
func TestAlgorandAccountWithNoAssetsReturnsAnEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"address": "ABCDEF", "amount": 0, "min-balance": 100000})
	}))
	defer srv.Close()
	SetAlgorandBases(srv.URL)
	defer SetAlgorandBases("")

	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_account",
		Config: map[string]string{"algoAddress": "ABCDEF"}}
	out, err := fetchAlgorandAccount(context.Background(), node, emptyRunContext{})
	if err != nil {
		t.Fatalf("fetchAlgorandAccount: %v", err)
	}
	assets, ok := out.(map[string]any)["assets"].([]map[string]any)
	if !ok {
		t.Fatalf("assets is %T, want a slice", out.(map[string]any)["assets"])
	}
	if assets == nil {
		t.Error("assets is nil; it must be an empty slice so it marshals to [] not null")
	}
	if len(assets) != 0 {
		t.Errorf("got %d assets", len(assets))
	}
}

func TestAlgorandAccountSkipsWithoutAnAddress(t *testing.T) {
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_account"}
	out, err := fetchAlgorandAccount(context.Background(), node, emptyRunContext{})
	if !errors.Is(err, ErrActionSkipped) {
		t.Fatalf("err = %v, want ErrActionSkipped", err)
	}
	if out != "algorand_skipped_no_address" {
		t.Errorf("skip code = %v", out)
	}
}

func TestAlgorandAccountFailsClosedWithNoAlgodConfigured(t *testing.T) {
	SetAlgorandBases("")
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_account",
		Config: map[string]string{"algoAddress": "ABCDEF"}}
	_, err := fetchAlgorandAccount(context.Background(), node, emptyRunContext{})
	if err == nil {
		t.Fatal("no error with algod unconfigured")
	}
	if !strings.Contains(err.Error(), "ALGOD_URL") {
		t.Errorf("the error does not name the setting, got: %v", err)
	}
}
