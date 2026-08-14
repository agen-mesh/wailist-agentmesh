package nodes_test

import (
	"context"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestSetToolBuildsObjectFromTemplates(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"what is the weather"`))
	rc.Set("n1", map[string]any{"city": "Kolkata", "temp": 31.5})

	node := models.WorkflowNode{
		ID: "s1", Type: models.NodeTypeTool, Template: "set",
		Config: map[string]string{
			"setFields": `{"place":"{{ node.n1.city }}","reading":"{{ node.n1.temp }}C","asked":"{{ input }}"}`,
		},
	}
	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want map output so downstream {{ node.s1.field }} works, got %T", out)
	}
	if got["place"] != "Kolkata" {
		t.Errorf("place: got %v", got["place"])
	}
	if got["reading"] != "31.5C" {
		t.Errorf("reading: got %v", got["reading"])
	}
	if got["asked"] != "what is the weather" {
		t.Errorf("asked: got %v", got["asked"])
	}
}

func TestSetToolErrorsOnInvalidJSON(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{
		ID: "s1", Type: models.NodeTypeTool, Template: "set",
		Config: map[string]string{"setFields": `{"a": }`},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for malformed setFields, got nil")
	}
}

func TestSetToolErrorsWhenUnconfigured(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{ID: "s1", Type: models.NodeTypeTool, Template: "set"}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error when setFields is unset, got nil")
	}
}

func TestJSONExtractPullsNestedValue(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", `{"data":{"items":[{"name":"first"},{"name":"second"}]},"ok":true}`)

	cases := []struct {
		name, path string
		want       any
	}{
		{"nested object", "data.items.0.name", "first"},
		{"array index", "data.items.1.name", "second"},
		{"bool at root", "ok", true},
		{"whole subtree", "data", map[string]any{"items": []any{
			map[string]any{"name": "first"}, map[string]any{"name": "second"},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := models.WorkflowNode{
				ID: "j1", Type: models.NodeTypeTool, Template: "json_extract",
				Config: map[string]string{"jsonPath": tc.path},
			}
			got, err := nodes.ExecuteTool(context.Background(), node, rc)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "whole subtree" {
				m, ok := got.(map[string]any)
				if !ok || len(m) != 1 {
					t.Fatalf("want the data subtree, got %#v", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestJSONExtractErrorsOnMissingPath(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", `{"a":1}`)
	node := models.WorkflowNode{
		ID: "j1", Type: models.NodeTypeTool, Template: "json_extract",
		Config: map[string]string{"jsonPath": "a.b.c"},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for a path that does not exist, got nil")
	}
}

func TestJSONExtractErrorsOnNonJSONInput(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "this is not json")
	node := models.WorkflowNode{
		ID: "j1", Type: models.NodeTypeTool, Template: "json_extract",
		Config: map[string]string{"jsonPath": "a"},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for non-JSON input, got nil")
	}
}
