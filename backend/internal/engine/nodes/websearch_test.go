package nodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// fakeRC is a minimal RunContexter for tests in this (internal) package that
// need one but don't care about Set/Get/ToolOutputs bookkeeping.
type fakeRC struct{ message string }

func (f *fakeRC) Message() string             { return f.message }
func (f *fakeRC) LastOutput() any             { return f.message }
func (f *fakeRC) UserInput() string           { return f.message }
func (f *fakeRC) ToolOutputs() map[string]any { return nil }
func (f *fakeRC) Set(string, any)             {}
func (f *fakeRC) Get(string) (any, bool)      { return nil, false }
func (f *fakeRC) OutputOrder() []string       { return nil }

func TestWebSearchReturnsAnswerAndSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{"parts": []map[string]any{
						{"text": "The answer is 42."},
					}},
					"groundingMetadata": map[string]any{
						"groundingChunks": []map[string]any{
							{"web": map[string]any{"uri": "https://example.com/a", "title": "Example A"}},
							{"web": map[string]any{"uri": ""}}, // no uri -- must be skipped
						},
					},
				},
			},
		})
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	result, err := webSearch(context.Background(), "what is the answer", "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if m["answer"] != "The answer is 42." {
		t.Fatalf("unexpected answer: %v", m["answer"])
	}
	sources, ok := m["sources"].([]map[string]string)
	if !ok || len(sources) != 1 || sources[0]["uri"] != "https://example.com/a" {
		t.Fatalf("unexpected sources: %+v", m["sources"])
	}
}

func TestWebSearchRejectsEmptyQuery(t *testing.T) {
	if _, err := webSearch(context.Background(), "   ", "test-key"); err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestWebSearchRejectsMissingKey(t *testing.T) {
	if _, err := webSearch(context.Background(), "hello", ""); err == nil {
		t.Fatal("expected error for missing platform Gemini key")
	}
}

func TestWebsearchQueryPrefersLLMArgOverMessage(t *testing.T) {
	rc := &fakeRC{message: "original run message"}
	got := websearchQuery(models.WorkflowNode{Template: "websearch"}, map[string]any{"query": "llm chosen query"}, rc)
	if got != "llm chosen query" {
		t.Fatalf("want LLM arg, got %q", got)
	}
}

func TestWebsearchQueryFallsBackToMessageWhenNoArg(t *testing.T) {
	rc := &fakeRC{message: "original run message"}
	got := websearchQuery(models.WorkflowNode{Template: "websearch"}, nil, rc)
	if got != "original run message" {
		t.Fatalf("want fallback to rc.Message(), got %q", got)
	}
}

func TestExecuteToolWebsearchUsesPlatformKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": "ok"}}}},
			},
		})
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	SetPlatformKeys(map[string]string{"gemini": "test-key"})
	defer SetPlatformKeys(nil)

	node := models.WorkflowNode{ID: "t1", Type: models.NodeTypeTool, Template: "websearch"}
	rc := &fakeRC{message: "fallback query"}
	result, err := ExecuteToolWithArgs(context.Background(), node, rc, map[string]any{"query": "real query"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok || m["answer"] != "ok" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExecuteToolWebsearchWithoutPlatformKeyFails(t *testing.T) {
	SetPlatformKeys(nil)
	node := models.WorkflowNode{ID: "t1", Type: models.NodeTypeTool, Template: "websearch"}
	rc := &fakeRC{message: "fallback query"}
	if _, err := ExecuteTool(context.Background(), node, rc); err == nil {
		t.Fatal("expected error when no platform Gemini key is configured")
	}
}

// A standalone Web Search node had no way to say what to search for: the
// catalog gave tool/websearch zero fields, so the builder wired one into the
// flow and it failed at run time with "query is required" -- a manual trigger
// carries no message for it to fall back to.
func TestWebsearchQueryPrefersTheAgentThenTheNodeThenTheUpstream(t *testing.T) {
	configured := models.WorkflowNode{
		Type: models.NodeTypeTool, Template: "websearch",
		Config: map[string]string{"searchQuery": "top tokens on Base by volume"},
	}
	bare := models.WorkflowNode{Type: models.NodeTypeTool, Template: "websearch"}
	rc := &fakeRC{message: "upstream text"}

	// An agent calling the tool supplies the query as a function argument,
	// and that always wins: it is the question actually being asked.
	if got := websearchQuery(configured, map[string]any{"query": "from the agent"}, rc); got != "from the agent" {
		t.Errorf("agent argument ignored, got %q", got)
	}
	// A flow node with no agent uses its own configured query.
	if got := websearchQuery(configured, nil, rc); got != "top tokens on Base by volume" {
		t.Errorf("configured query ignored, got %q", got)
	}
	// With neither, the previous step's output, which is the old behaviour.
	if got := websearchQuery(bare, nil, rc); got != "upstream text" {
		t.Errorf("upstream fallback broken, got %q", got)
	}
	// A blank configured query is not a query.
	blank := models.WorkflowNode{
		Type: models.NodeTypeTool, Template: "websearch",
		Config: map[string]string{"searchQuery": "   "},
	}
	if got := websearchQuery(blank, nil, rc); got != "upstream text" {
		t.Errorf("a blank searchQuery should fall through, got %q", got)
	}
}

// The configured query resolves {{ }} references like every other setting,
// so a node can search for whatever the step before it produced.
func TestWebsearchQueryResolvesTemplateReferences(t *testing.T) {
	node := models.WorkflowNode{
		Type: models.NodeTypeTool, Template: "websearch",
		Config: map[string]string{"searchQuery": "price of {{ result }}"},
	}
	if got := websearchQuery(node, nil, &fakeRC{message: "ALGO"}); got != "price of ALGO" {
		t.Errorf("template not resolved, got %q", got)
	}
}
