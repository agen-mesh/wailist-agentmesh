package nodes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestHubSpotAction_CreatesNote(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	nodes.SetHubSpotAPIBaseForTest(srv.URL)
	defer nodes.SetHubSpotAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "hs1", Type: models.NodeTypeAction, Template: "hubspot",
		Secrets: map[string]string{"hubspotAPIKey": "pat-na1-xxx"},
	}
	rc := engine.NewRunContext("r1", []byte(`"follow up with lead"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "hubspot_note_created" {
		t.Errorf("want 'hubspot_note_created', got %v", result)
	}
	if gotAuth != "Bearer pat-na1-xxx" {
		t.Errorf("want bearer auth, got %q", gotAuth)
	}
	props, _ := gotBody["properties"].(map[string]any)
	if props["hs_note_body"] != "follow up with lead" {
		t.Errorf("want note body from message, got %v", gotBody)
	}
}

func TestHubSpotAction_SkipsWhenNoAPIKey(t *testing.T) {
	node := models.WorkflowNode{
		ID: "hs2", Type: models.NodeTypeAction, Template: "hubspot",
	}
	rc := engine.NewRunContext("r1", []byte(`"follow up with lead"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "hubspot_skipped_no_api_key" {
		t.Errorf("want 'hubspot_skipped_no_api_key', got %v", result)
	}
}
