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

func TestExecuteAgentOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "Hello from mock"}},
			},
		})
	}))
	defer srv.Close()

	node := models.WorkflowNode{ID: "a1", Type: models.NodeTypeAgent, SystemPrompt: "Be helpful"}
	provider := models.WorkflowNode{ID: "p1", Type: models.NodeTypeProvider, Template: "openai", APIKey: "test-key", Model: "gpt-4o"}
	attach := models.AttachConfig{Provider: &provider}

	rc := engine.NewRunContext("run1", []byte(`{"message":"hello"}`))
	nodes.SetOpenAIBaseURL(srv.URL)

	result, err := nodes.ExecuteAgent(context.Background(), node, attach, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Hello from mock" {
		t.Fatalf("want 'Hello from mock' got %q", result)
	}
}
