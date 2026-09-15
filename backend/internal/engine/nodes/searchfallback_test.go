package nodes

import (
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// liveDataGraph is a trigger feeding a read connector feeding an agent that
// has a provider but no tools: the shape a price-lookup build produces.
func liveDataGraph() *models.WorkflowGraph {
	return &models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "t", Type: models.NodeTypeTrigger, Template: "manual"},
			{ID: "f", Type: models.NodeTypeAction, Template: "coingecko", Name: "Price"},
			{ID: "a", Type: models.NodeTypeAgent, Template: "agent", Name: "Reporter"},
			{ID: "p", Type: models.NodeTypeProvider, Template: "gemini"},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "t", To: "f", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "f", To: "a", Kind: models.EdgeKindFlow},
			{ID: "e3", From: "p", To: "a", Kind: models.EdgeKindAttach, ToPort: "model"},
		},
	}
}

func searchToolAttachedTo(g *models.WorkflowGraph, agentID string) (models.WorkflowNode, bool) {
	for _, n := range g.Nodes {
		if n.Type != models.NodeTypeTool || n.Template != "websearch" {
			continue
		}
		for _, e := range g.Edges {
			if e.From == n.ID && e.To == agentID && e.Kind == models.EdgeKindAttach && e.ToPort == "tools" {
				return n, true
			}
		}
	}
	return models.WorkflowNode{}, false
}

func TestEnsureSearchFallbackAttachesToALiveDataAgent(t *testing.T) {
	g := liveDataGraph()
	if !ensureSearchFallback(g) {
		t.Fatal("ensureSearchFallback reported no change, want one")
	}
	if _, ok := searchToolAttachedTo(g, "a"); !ok {
		t.Fatal("no websearch tool attached to the agent's tools port")
	}
	if len(g.Nodes) != 5 || len(g.Edges) != 4 {
		t.Errorf("graph is %d nodes / %d edges, want 5 / 4 (one node, one edge added)", len(g.Nodes), len(g.Edges))
	}
}

func TestEnsureSearchFallbackIsIdempotent(t *testing.T) {
	g := liveDataGraph()
	ensureSearchFallback(g)
	nodes, edges := len(g.Nodes), len(g.Edges)
	if ensureSearchFallback(g) {
		t.Error("a second call reported a change")
	}
	if len(g.Nodes) != nodes || len(g.Edges) != edges {
		t.Errorf("a second call changed the graph: %d/%d -> %d/%d nodes/edges", nodes, edges, len(g.Nodes), len(g.Edges))
	}
}

func TestEnsureSearchFallbackLeavesAGraphAlone(t *testing.T) {
	tests := []struct {
		name  string
		graph func() *models.WorkflowGraph
	}{
		{
			name: "no live source, only a send",
			graph: func() *models.WorkflowGraph {
				g := liveDataGraph()
				g.Nodes[1] = models.WorkflowNode{ID: "f", Type: models.NodeTypeAction, Template: "slack", Name: "Notify"}
				return g
			},
		},
		{
			// A read, but not a live one: nothing here can go down, so a
			// search tool would only invite searches nobody asked for.
			name: "only pure computation",
			graph: func() *models.WorkflowGraph {
				g := liveDataGraph()
				g.Nodes[1] = models.WorkflowNode{ID: "f", Type: models.NodeTypeTool, Template: "calc", Name: "Sum", URL: "1+1"}
				return g
			},
		},
		{
			name: "an http POST is a send, not a source",
			graph: func() *models.WorkflowGraph {
				g := liveDataGraph()
				g.Nodes[1] = models.WorkflowNode{ID: "f", Type: models.NodeTypeTool, Template: "http", Name: "Post", Method: "POST"}
				return g
			},
		},
		{
			name: "no agent to give it to",
			graph: func() *models.WorkflowGraph {
				return &models.WorkflowGraph{
					Nodes: []models.WorkflowNode{
						{ID: "t", Type: models.NodeTypeTrigger, Template: "manual"},
						{ID: "f", Type: models.NodeTypeAction, Template: "coingecko", Name: "Price"},
					},
					Edges: []models.WorkflowEdge{{ID: "e1", From: "t", To: "f", Kind: models.EdgeKindFlow}},
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := tt.graph()
			nodes, edges := len(g.Nodes), len(g.Edges)
			if ensureSearchFallback(g) {
				t.Error("reported a change, want none")
			}
			if len(g.Nodes) != nodes || len(g.Edges) != edges {
				t.Errorf("graph changed: %d/%d -> %d/%d nodes/edges", nodes, edges, len(g.Nodes), len(g.Edges))
			}
		})
	}
}

// TestEnsureSearchFallbackCountsAnHttpGet: the http template is a live source
// only when it reads, which IsDegradable decides by method.
func TestEnsureSearchFallbackCountsAnHttpGet(t *testing.T) {
	g := liveDataGraph()
	g.Nodes[1] = models.WorkflowNode{ID: "f", Type: models.NodeTypeTool, Template: "http", Name: "Fetch", URL: "https://example.com", Method: "GET"}
	if !ensureSearchFallback(g) {
		t.Fatal("an http GET feeding an agent got no search fallback")
	}
	if _, ok := searchToolAttachedTo(g, "a"); !ok {
		t.Fatal("no websearch tool attached to the agent's tools port")
	}
}
