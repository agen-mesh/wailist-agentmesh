package nodes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestExecuteAgentOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing or wrong auth header, got %q", r.Header.Get("Authorization"))
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

	result, err := nodes.ExecuteAgent(context.Background(), node, attach, models.AgentWallet{}, nil, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Hello from mock" {
		t.Fatalf("want 'Hello from mock' got %q", result)
	}
}

// TestResolveAPIKeyOwnKey verifies that when UseOurKey=false (default), the node's own
// API key is used even if an env var is set.
func TestResolveAPIKeyOwnKey(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "env-key")
	defer os.Unsetenv("OPENAI_API_KEY")

	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "ok"}},
			},
		})
	}))
	defer srv.Close()
	nodes.SetOpenAIBaseURL(srv.URL)

	node := models.WorkflowNode{ID: "a1", Type: models.NodeTypeAgent}
	provider := models.WorkflowNode{
		ID: "p1", Type: models.NodeTypeProvider, Template: "openai",
		APIKey: "node-key", Model: "gpt-4o",
		UseOurKey: false, // explicit: use node key
	}
	rc := engine.NewRunContext("r1", []byte(`"hello"`))
	nodes.ExecuteAgent(context.Background(), node, models.AttachConfig{Provider: &provider}, models.AgentWallet{}, nil, rc) //nolint:errcheck

	if capturedAuth != "Bearer node-key" {
		t.Fatalf("expected node key, got %q", capturedAuth)
	}
}

// TestResolveAPIKeyPlatformFallback verifies that when UseOurKey=true, the env var
// is used instead of the node's key.
func TestResolveAPIKeyPlatformFallback(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "platform-env-key")
	defer os.Unsetenv("OPENAI_API_KEY")

	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "ok"}},
			},
		})
	}))
	defer srv.Close()
	nodes.SetOpenAIBaseURL(srv.URL)

	node := models.WorkflowNode{ID: "a1", Type: models.NodeTypeAgent}
	provider := models.WorkflowNode{
		ID: "p1", Type: models.NodeTypeProvider, Template: "openai",
		APIKey:    "node-key",
		Model:     "gpt-4o",
		UseOurKey: true, // use platform key
	}
	rc := engine.NewRunContext("r1", []byte(`"hello"`))
	nodes.ExecuteAgent(context.Background(), node, models.AttachConfig{Provider: &provider}, models.AgentWallet{}, nil, rc) //nolint:errcheck

	if capturedAuth != "Bearer platform-env-key" {
		t.Fatalf("expected platform env key, got %q", capturedAuth)
	}
}

// TestResolveAPIKeyEmptyNodeKeyFallback verifies that an empty node API key falls
// back to the platform env var (supports the "use ours by default" UX).
func TestResolveAPIKeyEmptyNodeKeyFallback(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "gemini-platform-key")
	defer os.Unsetenv("GEMINI_API_KEY")

	var capturedAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAPIKey = r.Header.Get("x-goog-api-key")
		// Return minimal valid Gemini response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": "ok"}}}},
			},
		})
	}))
	defer srv.Close()

	// Point Gemini URL to our mock — we can't override it directly like OpenAI,
	// so we test the key resolution in isolation via the OpenAI path for simplicity.
	// The key resolution logic is provider-template-agnostic, so testing on OpenAI
	// is sufficient for the fallback behaviour.
	nodes.SetOpenAIBaseURL(srv.URL)
	os.Setenv("OPENAI_API_KEY", "openai-fallback")
	defer os.Unsetenv("OPENAI_API_KEY")

	var capturedAuth string
	srvOAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "ok"}},
			},
		})
	}))
	defer srvOAI.Close()
	nodes.SetOpenAIBaseURL(srvOAI.URL)

	node := models.WorkflowNode{ID: "a1", Type: models.NodeTypeAgent}
	provider := models.WorkflowNode{
		ID:      "p1",
		Type:    models.NodeTypeProvider,
		Template: "openai",
		APIKey:  "", // empty — should fall back to env
		Model:   "gpt-4o",
	}
	rc := engine.NewRunContext("r1", []byte(`"hello"`))
	nodes.ExecuteAgent(context.Background(), node, models.AttachConfig{Provider: &provider}, models.AgentWallet{}, nil, rc) //nolint:errcheck

	if capturedAuth != "Bearer openai-fallback" {
		t.Fatalf("expected openai-fallback from env, got %q", capturedAuth)
	}
	_ = capturedAPIKey // only used in gemini branch, silences unused warning
}
