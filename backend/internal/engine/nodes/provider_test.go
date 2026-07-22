package nodes_test

import (
	"context"
	"encoding/json"
	"fmt"
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

	result, err := nodes.ExecuteAgent(context.Background(), node, attach, models.AgentWallet{}, nil, rc, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Hello from mock" {
		t.Fatalf("want 'Hello from mock' got %q", result)
	}
}

func TestExecuteAgentOpenAIBillsAttachedHTTPTool(t *testing.T) {
	toolSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":"tool ran"}`))
	}))
	defer toolSrv.Close()

	callCount := 0
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{{
						"id":       "call_1",
						"type":     "function",
						"function": map[string]any{"name": "search_tool", "arguments": "{}"},
					}},
				}}},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "done"}}},
		})
	}))
	defer llmSrv.Close()

	node := models.WorkflowNode{ID: "a1", Type: models.NodeTypeAgent}
	provider := models.WorkflowNode{ID: "p1", Type: models.NodeTypeProvider, Template: "openai", APIKey: "test-key", Model: "gpt-4o"}
	tool := models.WorkflowNode{ID: "t1", Name: "search_tool", Type: models.NodeTypeTool, Template: "http", URL: toolSrv.URL, Method: "GET"}
	attach := models.AttachConfig{Provider: &provider, Tools: []models.WorkflowNode{tool}}

	rc := engine.NewRunContext("run1", []byte(`{"message":"hello"}`))
	nodes.SetOpenAIBaseURL(llmSrv.URL)
	nodes.SetURLValidatorForTest(func(string) error { return nil })
	defer nodes.SetURLValidatorForTest(func(string) error { return nil })

	result, err := nodes.ExecuteAgent(context.Background(), node, attach, models.AgentWallet{}, nil, rc, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("want a map result (tool was billed), got %T: %v", result, result)
	}
	ids, ok := m["billedFlatFeeNodeIds"].([]string)
	if !ok || len(ids) != 1 || ids[0] != "t1" {
		t.Fatalf("want billedFlatFeeNodeIds [t1], got %v", m["billedFlatFeeNodeIds"])
	}
}

func TestExecuteAgentBlocksAttachedToolWhenBalanceCheckFails(t *testing.T) {
	var toolHits int
	toolSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		toolHits++
		w.Write([]byte(`{"result":"tool ran"}`))
	}))
	defer toolSrv.Close()

	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{{
					"id":       "call_1",
					"type":     "function",
					"function": map[string]any{"name": "search_tool", "arguments": "{}"},
				}},
			}}},
		})
	}))
	defer llmSrv.Close()

	node := models.WorkflowNode{ID: "a1", Type: models.NodeTypeAgent}
	provider := models.WorkflowNode{ID: "p1", Type: models.NodeTypeProvider, Template: "openai", APIKey: "test-key", Model: "gpt-4o"}
	tool := models.WorkflowNode{ID: "t1", Name: "search_tool", Type: models.NodeTypeTool, Template: "http", URL: toolSrv.URL, Method: "GET"}
	attach := models.AttachConfig{Provider: &provider, Tools: []models.WorkflowNode{tool}}

	rc := engine.NewRunContext("run1", []byte(`{"message":"hello"}`))
	nodes.SetOpenAIBaseURL(llmSrv.URL)
	nodes.SetURLValidatorForTest(func(string) error { return nil })
	defer nodes.SetURLValidatorForTest(func(string) error { return nil })

	checkBalance := func(ctx context.Context, amount int64) error {
		return fmt.Errorf("insufficient credits: balance 0 micros, need %d micros", amount)
	}

	result, err := nodes.ExecuteAgent(context.Background(), node, attach, models.AgentWallet{}, nil, rc, checkBalance, nil)
	if err == nil {
		t.Fatalf("want error from failed balance check, got result %v", result)
	}
	if toolHits != 0 {
		t.Fatalf("want zero requests to the tool server (blocked before execution), got %d", toolHits)
	}
}

func TestExecuteAgentOpenAIPlatformKeyUsesPlatformKeyAndReportsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer platform-secret" {
			t.Errorf("want platform key in Authorization header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "platform hello"}},
			},
			"usage": map[string]any{"prompt_tokens": 42, "completion_tokens": 7},
		})
	}))
	defer srv.Close()

	node := models.WorkflowNode{ID: "a1", Type: models.NodeTypeAgent}
	provider := models.WorkflowNode{ID: "p1", Type: models.NodeTypeProvider, Template: "openai", KeyMode: "platform", Model: "gpt-4.1"}
	attach := models.AttachConfig{Provider: &provider}
	platformKeys := map[string]string{"openai": "platform-secret"}

	rc := engine.NewRunContext("run1", []byte(`{"message":"hello"}`))
	nodes.SetOpenAIBaseURL(srv.URL)

	result, err := nodes.ExecuteAgent(context.Background(), node, attach, models.AgentWallet{}, nil, rc, nil, platformKeys)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("want map[string]any result, got %T: %v", result, result)
	}
	if m["message"] != "platform hello" {
		t.Fatalf("message = %v, want 'platform hello'", m["message"])
	}
	usage, ok := m["platformKeyUsage"].(map[string]any)
	if !ok {
		t.Fatalf("missing platformKeyUsage in result: %v", m)
	}
	if usage["tier"] != "standard" {
		t.Fatalf("tier = %v, want standard", usage["tier"])
	}
	if usage["model"] != "gpt-4.1" {
		t.Fatalf("model = %v, want gpt-4.1", usage["model"])
	}
	if usage["tokensIn"] != 42 {
		t.Fatalf("tokensIn = %v, want 42", usage["tokensIn"])
	}
	if usage["tokensOut"] != 7 {
		t.Fatalf("tokensOut = %v, want 7", usage["tokensOut"])
	}
}

func TestExecuteAgentOpenAIPlatformKeyMissingErrors(t *testing.T) {
	node := models.WorkflowNode{ID: "a1", Type: models.NodeTypeAgent}
	provider := models.WorkflowNode{ID: "p1", Type: models.NodeTypeProvider, Template: "openai", KeyMode: "platform", Model: "gpt-4.1"}
	attach := models.AttachConfig{Provider: &provider}
	rc := engine.NewRunContext("run1", []byte(`{"message":"hello"}`))

	_, err := nodes.ExecuteAgent(context.Background(), node, attach, models.AgentWallet{}, nil, rc, nil, map[string]string{})
	if err == nil {
		t.Fatal("want error when platform key not configured for template, got nil")
	}
}

func TestExecuteAgentByokUnaffectedByPlatformKeysParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer byok-key" {
			t.Errorf("want byok key in Authorization header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "byok hello"}}},
		})
	}))
	defer srv.Close()

	node := models.WorkflowNode{ID: "a1", Type: models.NodeTypeAgent}
	provider := models.WorkflowNode{ID: "p1", Type: models.NodeTypeProvider, Template: "openai", APIKey: "byok-key", Model: "gpt-4o"}
	attach := models.AttachConfig{Provider: &provider}

	rc := engine.NewRunContext("run1", []byte(`{"message":"hello"}`))
	nodes.SetOpenAIBaseURL(srv.URL)

	result, err := nodes.ExecuteAgent(context.Background(), node, attach, models.AgentWallet{}, nil, rc, nil, map[string]string{"openai": "should-not-be-used"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "byok hello" {
		t.Fatalf("result = %v, want plain string 'byok hello' (unwrapped, unchanged from pre-platform-key behavior)", result)
	}
}
