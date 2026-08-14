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
