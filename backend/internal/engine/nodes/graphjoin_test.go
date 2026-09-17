package nodes

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

func twoSources() *models.WorkflowGraph {
	return &models.WorkflowGraph{Nodes: []models.WorkflowNode{
		{ID: "n_news", Type: models.NodeTypeAgent},
		{ID: "n_price", Type: models.NodeTypeAgent},
	}}
}

func setFieldsOf(t *testing.T, graph *models.WorkflowGraph) map[string]any {
	t.Helper()
	n := graph.Nodes[len(graph.Nodes)-1]
	var got map[string]any
	if err := json.Unmarshal([]byte(n.Config["setFields"]), &got); err != nil {
		t.Fatalf("stored setFields is not JSON: %q (%v)", n.Config["setFields"], err)
	}
	return got
}

// The live "news and ALGO price" build failed twice writing this as JSON
// text: single quotes first, then a truncated object. As name/value pairs
// the server writes the JSON and neither mistake can happen.
func TestAddNodeWritesSetFieldsFromNameValuePairs(t *testing.T) {
	graph := twoSources()
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "set", "name": "Combine News and Price",
		"config": map[string]any{"setFields": []any{
			map[string]any{"name": "news", "value": "{{ node.n_news }}"},
			map[string]any{"name": "algo_price", "value": "{{ node.n_price }}"},
		}},
	})
	if err != nil {
		t.Fatalf("add_node: %v", err)
	}
	got := setFieldsOf(t, graph)
	if got["news"] != "{{ node.n_news }}" || got["algo_price"] != "{{ node.n_price }}" {
		t.Errorf("setFields = %v", got)
	}
}

// Text that would break JSON by hand (quotes, apostrophes, braces) is
// escaped by the encoder rather than by the model.
func TestAddNodeEscapesSetFieldsValues(t *testing.T) {
	graph := twoSources()
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "set",
		"config": map[string]any{"setFields": []any{
			map[string]any{"name": "headline", "value": `Today's "crypto" brief: {{ node.n_news }}`},
		}},
	})
	if err != nil {
		t.Fatalf("add_node: %v", err)
	}
	if got := setFieldsOf(t, graph)["headline"]; got != `Today's "crypto" brief: {{ node.n_news }}` {
		t.Errorf("headline = %q", got)
	}
}

// The checks that apply to hand-written JSON still apply to the encoded
// form: a reference the engine cannot resolve is refused either way.
func TestAddNodeStillChecksReferencesInSetFieldsPairs(t *testing.T) {
	graph := twoSources()
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "set",
		"config": map[string]any{"setFields": []any{
			map[string]any{"name": "price", "value": "{{ node.n_missing }}"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "n_missing") {
		t.Fatalf("want the unknown reference refused, got %v", err)
	}
}

func TestAddNodeRejectsBadSetFieldsPairs(t *testing.T) {
	bad := map[string]any{
		"empty list":     []any{},
		"missing name":   []any{map[string]any{"value": "x"}},
		"missing value":  []any{map[string]any{"name": "price"}},
		"null value":     []any{map[string]any{"name": "price", "value": nil}},
		"duplicate name": []any{map[string]any{"name": "a", "value": "1"}, map[string]any{"name": "a", "value": "2"}},
		"a number":       float64(3),
	}
	for label, v := range bad {
		graph := twoSources()
		_, err := applyGraphOp(graph, "add_node", map[string]any{
			"type": "tool", "template": "set",
			"config": map[string]any{"setFields": v},
		})
		if err == nil {
			t.Errorf("%s: accepted", label)
		}
		if len(graph.Nodes) != 2 {
			t.Errorf("%s: a node was added despite the error", label)
		}
	}
}

// A JSON string still works, so graphs and prompts written before this
// change keep building.
func TestAddNodeStillAcceptsSetFieldsAsJSONText(t *testing.T) {
	graph := twoSources()
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "set",
		"config": map[string]any{"setFields": `{"news": "{{ node.n_news }}"}`},
	})
	if err != nil {
		t.Fatalf("add_node: %v", err)
	}
	if setFieldsOf(t, graph)["news"] != "{{ node.n_news }}" {
		t.Error("JSON text was not stored as given")
	}
}

// The declared schema is what makes Gemini send pairs at all.
func TestSetFieldsIsDeclaredAsAListOfPairs(t *testing.T) {
	props := catalogKeyUnion("config")
	decl, _ := props["setFields"].(map[string]any)
	if decl["type"] != "ARRAY" {
		t.Fatalf("setFields is declared as %v, want ARRAY", decl["type"])
	}
}
