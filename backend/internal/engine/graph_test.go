package engine_test

import (
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/models"
)

func TestTopologicalSort(t *testing.T) {
	nodes := []models.WorkflowNode{
		{ID: "n4", Type: models.NodeTypeEnd},
		{ID: "n3", Type: models.NodeTypeAction},
		{ID: "n1", Type: models.NodeTypeTrigger},
		{ID: "n2", Type: models.NodeTypeAgent},
	}
	edges := []models.WorkflowEdge{
		{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
		{ID: "e2", From: "n2", To: "n3", Kind: models.EdgeKindFlow},
		{ID: "e3", From: "n3", To: "n4", Kind: models.EdgeKindFlow},
		{ID: "e4", From: "p1", To: "n2", Kind: models.EdgeKindAttach, ToPort: "model"},
	}

	levels, err := engine.TopologicalSort(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 4 {
		t.Fatalf("want 4 levels got %d", len(levels))
	}
	if levels[0][0].ID != "n1" {
		t.Fatalf("first node should be trigger, got %s", levels[0][0].ID)
	}
	if levels[3][0].ID != "n4" {
		t.Fatalf("last node should be end, got %s", levels[3][0].ID)
	}
}

func TestCycleDetected(t *testing.T) {
	nodes := []models.WorkflowNode{{ID: "a"}, {ID: "b"}}
	edges := []models.WorkflowEdge{
		{ID: "e1", From: "a", To: "b", Kind: models.EdgeKindFlow},
		{ID: "e2", From: "b", To: "a", Kind: models.EdgeKindFlow},
	}
	_, err := engine.TopologicalSort(nodes, edges)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestBuildAttachMap(t *testing.T) {
	nodes := []models.WorkflowNode{
		{ID: "provider1", Type: models.NodeTypeProvider, Template: "openai"},
		{ID: "tool1", Type: models.NodeTypeTool, Template: "http"},
		{ID: "agent1", Type: models.NodeTypeAgent},
	}
	edges := []models.WorkflowEdge{
		{ID: "e1", From: "provider1", To: "agent1", Kind: models.EdgeKindAttach, ToPort: "model"},
		{ID: "e2", From: "tool1", To: "agent1", Kind: models.EdgeKindAttach, ToPort: "tools"},
	}
	m := engine.BuildAttachMap(nodes, edges)
	cfg, ok := m["agent1"]
	if !ok {
		t.Fatal("no attach config for agent1")
	}
	if cfg.Provider == nil || cfg.Provider.ID != "provider1" {
		t.Fatal("provider not attached")
	}
	if len(cfg.Tools) != 1 || cfg.Tools[0].ID != "tool1" {
		t.Fatal("tools not attached")
	}
}
