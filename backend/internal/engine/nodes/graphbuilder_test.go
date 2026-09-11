package nodes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

func TestApplyGraphOpAddNode(t *testing.T) {
	graph := &models.WorkflowGraph{}
	result, err := applyGraphOp(graph, "add_node", map[string]any{
		"type":     "agent",
		"template": "agent",
		"name":     "Support Agent",
		"fields": map[string]any{
			"systemPrompt": "Be helpful",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(graph.Nodes))
	}
	n := graph.Nodes[0]
	if n.Type != models.NodeTypeAgent || n.Template != "agent" || n.Name != "Support Agent" || n.SystemPrompt != "Be helpful" {
		t.Fatalf("unexpected node: %+v", n)
	}
	if result == "" {
		t.Fatal("expected non-empty result string")
	}
}

func TestApplyGraphOpAddNodeInvalidType(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{"type": "bogus", "template": "x"})
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestApplyGraphOpAddNodeDefaultsProviderToPlatformKeyMode(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type":     "provider",
		"template": "gemini",
		"name":     "Gemini Model",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if graph.Nodes[0].KeyMode != "platform" {
		t.Fatalf("want KeyMode defaulted to platform, got %q", graph.Nodes[0].KeyMode)
	}
}

func TestApplyGraphOpAddNodeRespectsExplicitByokKeyMode(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type":     "provider",
		"template": "gemini",
		"name":     "Gemini Model",
		"fields":   map[string]any{"keyMode": "byok"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if graph.Nodes[0].KeyMode != "byok" {
		t.Fatalf("want explicit byok preserved, got %q", graph.Nodes[0].KeyMode)
	}
}

func TestApplyGraphOpAddNodeNonProviderKeyModeUnset(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type":     "agent",
		"template": "agent",
		"name":     "An Agent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if graph.Nodes[0].KeyMode != "" {
		t.Fatalf("KeyMode default should only apply to provider nodes, got %q", graph.Nodes[0].KeyMode)
	}
}

func TestApplyGraphOpAddNodeFieldTypeMismatch(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type":     "provider",
		"template": "gemini",
		"name":     "Test Provider",
		"fields": map[string]any{
			"model": 42,
		},
	})
	if err == nil {
		t.Fatal("expected error for non-string field value")
	}
}

func TestApplyGraphOpUpdateNode(t *testing.T) {
	graph := &models.WorkflowGraph{Nodes: []models.WorkflowNode{{ID: "n_1", Type: models.NodeTypeProvider, Template: "gemini"}}}
	_, err := applyGraphOp(graph, "update_node", map[string]any{
		"id":     "n_1",
		"fields": map[string]any{"model": "gemini-2.5-flash", "keyMode": "platform"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if graph.Nodes[0].Model != "gemini-2.5-flash" || graph.Nodes[0].KeyMode != "platform" {
		t.Fatalf("update did not apply: %+v", graph.Nodes[0])
	}
}

func TestApplyGraphOpUpdateNodeNotFound(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "update_node", map[string]any{"id": "missing"})
	if err == nil {
		t.Fatal("expected error for missing node")
	}
}

func TestApplyGraphOpRemoveNode(t *testing.T) {
	graph := &models.WorkflowGraph{
		Nodes: []models.WorkflowNode{{ID: "n_1"}, {ID: "n_2"}},
		Edges: []models.WorkflowEdge{{ID: "e_1", From: "n_1", To: "n_2"}},
	}
	_, err := applyGraphOp(graph, "remove_node", map[string]any{"id": "n_1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Nodes) != 1 || graph.Nodes[0].ID != "n_2" {
		t.Fatalf("node not removed: %+v", graph.Nodes)
	}
	if len(graph.Edges) != 0 {
		t.Fatalf("edge referencing removed node should be gone: %+v", graph.Edges)
	}
}

func TestApplyGraphOpAddEdge(t *testing.T) {
	// Typed nodes, not bare ids: an attach edge is only legal provider ->
	// agent now (validateEdge), which is the shape this test always meant to
	// describe -- the untyped fixture it used to carry would have been
	// dropped by BuildAttachMap at run time.
	graph := &models.WorkflowGraph{Nodes: []models.WorkflowNode{
		{ID: "n_1", Type: models.NodeTypeProvider},
		{ID: "n_2", Type: models.NodeTypeAgent},
	}}
	_, err := applyGraphOp(graph, "add_edge", map[string]any{"from": "n_1", "to": "n_2", "kind": "attach", "toPort": "model"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Edges) != 1 || graph.Edges[0].Kind != models.EdgeKindAttach || graph.Edges[0].ToPort != "model" {
		t.Fatalf("edge not added correctly: %+v", graph.Edges)
	}
}

func TestApplyGraphOpAddEdgeMissingNode(t *testing.T) {
	graph := &models.WorkflowGraph{Nodes: []models.WorkflowNode{{ID: "n_1"}}}
	_, err := applyGraphOp(graph, "add_edge", map[string]any{"from": "n_1", "to": "missing"})
	if err == nil {
		t.Fatal("expected error for dangling edge reference")
	}
}

func TestApplyGraphOpRemoveEdge(t *testing.T) {
	graph := &models.WorkflowGraph{Edges: []models.WorkflowEdge{{ID: "e_1"}}}
	_, err := applyGraphOp(graph, "remove_edge", map[string]any{"id": "e_1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Edges) != 0 {
		t.Fatalf("edge not removed: %+v", graph.Edges)
	}
}

func TestApplyGraphOpUnknownTool(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "delete_everything", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestBuildGraphAddsNodeThenReturnsReply(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{
					{"content": map[string]any{"parts": []map[string]any{
						{"functionCall": map[string]any{
							"name": "add_node",
							"args": map[string]any{"type": "trigger", "template": "chat", "name": "On Chat"},
						}},
					}}},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{
					{"text": "Added a chat trigger node."},
				}}},
			},
		})
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	result, err := BuildGraph(context.Background(), "test-key", "add a chat trigger", models.WorkflowGraph{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "Added a chat trigger node." {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
	if len(result.Graph.Nodes) != 1 || result.Graph.Nodes[0].Template != "chat" {
		t.Fatalf("unexpected graph: %+v", result.Graph.Nodes)
	}
	// Three, not two: the graph this builds is a single unconnected trigger,
	// so auditGraph reports an orphan and spends one extra round giving the
	// model a chance to repair before answering. The stub replies with the
	// same text either way, so the assertions above are unaffected.
	if callCount != 3 {
		t.Fatalf("expected 3 calls (2 + one audit repair round), got %d", callCount)
	}
}

// Running out of rounds must not throw the graph away. This used to return
// an error, which made BuildWorkflow skip its save entirely -- every node
// built across all the rounds discarded, on exactly the requests that
// produced the most work. Replaces the old TestBuildGraphIterationCap, which
// asserted that error.
func TestBuildGraphOutOfIterationsKeepsWhatItBuilt(t *testing.T) {
	// Always answer with another add_node call, so the loop can never
	// terminate on its own and must hit the cap.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{
					{"functionCall": map[string]any{
						"name": "add_node",
						"args": map[string]any{"type": "tool", "template": "calc"},
					}},
				}}},
			},
		})
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	res, err := BuildGraph(context.Background(), "test-key", "build something endless", models.WorkflowGraph{})
	if err != nil {
		t.Fatalf("running out of rounds must not be an error: %v", err)
	}
	if len(res.Graph.Nodes) != maxBuildIterations {
		t.Fatalf("want the %d nodes it built kept, got %d", maxBuildIterations, len(res.Graph.Nodes))
	}
	if res.Reply == "" {
		t.Fatal("an unfinished build still needs a reply saying so")
	}
}

func TestGraphToolDeclsIncludesWebSearch(t *testing.T) {
	var found bool
	for _, d := range graphToolDecls() {
		if d.Name == "web_search" {
			found = true
			props, _ := d.Parameters["properties"].(map[string]any)
			if _, ok := props["query"]; !ok {
				t.Fatal("web_search must take a query parameter")
			}
		}
	}
	if !found {
		t.Fatal("expected a web_search declaration")
	}
}

// web_search is not a graph mutation, so applyGraphOp must not claim it --
// routing it there would return "unknown graph tool" to the model and the
// search would silently never happen.
func TestApplyGraphOpDoesNotHandleWebSearch(t *testing.T) {
	graph := &models.WorkflowGraph{}
	if _, err := applyGraphOp(graph, "web_search", map[string]any{"query": "x"}); err == nil {
		t.Fatal("expected applyGraphOp to reject web_search")
	}
}

func TestBuildGraphRunsWebSearchAndKeepsGoing(t *testing.T) {
	// Turn 1: the model asks to search. Turn 2: having "read" the result, it
	// adds a node. Turn 3: it answers. Asserting the search result actually
	// reaches the model matters -- a tool whose output is dropped is worse
	// than no tool, because the model will cite it.
	turn := 0
	var sawSearchResult bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "google_search") {
			// This is the webSearch sub-call, not the builder loop.
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"The Pixel 10 has 16GB RAM."}]}}]}`)
			return
		}
		if strings.Contains(string(body), "Pixel 10 has 16GB") {
			sawSearchResult = true
		}
		turn++
		w.Header().Set("Content-Type", "application/json")
		switch turn {
		case 1:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"web_search","args":{"query":"pixel 10 specs"}}}]}}]}`)
		case 2:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"add_node","args":{"type":"agent","template":"agent","name":"Phone Search"}}}]}}]}`)
		default:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"Built it."}]}}]}`)
		}
	}))
	defer srv.Close()

	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	res, err := BuildGraph(context.Background(), "k", "build a phone search agent", models.WorkflowGraph{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawSearchResult {
		t.Fatal("the search result never reached the model")
	}
	if len(res.Graph.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(res.Graph.Nodes))
	}
}
