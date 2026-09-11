package nodes

import (
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

func graphWith(nodes ...models.WorkflowNode) *models.WorkflowGraph {
	return &models.WorkflowGraph{Nodes: nodes}
}

func gn(id string, t models.NodeType) models.WorkflowNode {
	return models.WorkflowNode{ID: id, Type: t, Template: "x"}
}

func TestValidateEdgeAttachProviderToAgent(t *testing.T) {
	g := graphWith(gn("p1", models.NodeTypeProvider), gn("a1", models.NodeTypeAgent))
	port, err := validateEdge(g, "p1", "a1", "attach", "model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != "model" {
		t.Fatalf("want port model, got %q", port)
	}
}

func TestValidateEdgeRejectsBackwardsAttach(t *testing.T) {
	g := graphWith(gn("p1", models.NodeTypeProvider), gn("a1", models.NodeTypeAgent))
	_, err := validateEdge(g, "a1", "p1", "attach", "model")
	if err == nil {
		t.Fatal("expected error for agent -> provider attach")
	}
	// The message is fed back to the model as a functionResponse, so it must
	// say how to fix it, not just that it is wrong.
	if !strings.Contains(err.Error(), "provider") {
		t.Fatalf("error should name the correct source type, got: %v", err)
	}
}

func TestValidateEdgeDefaultsAttachPortBySourceType(t *testing.T) {
	g := graphWith(
		gn("p1", models.NodeTypeProvider),
		gn("t1", models.NodeTypeTool),
		gn("a1", models.NodeTypeAgent),
	)
	port, err := validateEdge(g, "p1", "a1", "attach", "")
	if err != nil || port != "model" {
		t.Fatalf("provider attach should default to model, got %q err=%v", port, err)
	}
	port, err = validateEdge(g, "t1", "a1", "attach", "")
	if err != nil || port != "tools" {
		t.Fatalf("tool attach should default to tools, got %q err=%v", port, err)
	}
}

func TestValidateEdgeRejectsProviderOnToolsPort(t *testing.T) {
	g := graphWith(gn("p1", models.NodeTypeProvider), gn("a1", models.NodeTypeAgent))
	if _, err := validateEdge(g, "p1", "a1", "attach", "tools"); err == nil {
		t.Fatal("expected error: a provider attaches to model, not tools")
	}
}

func TestValidateEdgeRejectsAttachToNonAgent(t *testing.T) {
	g := graphWith(gn("p1", models.NodeTypeProvider), gn("e1", models.NodeTypeEnd))
	if _, err := validateEdge(g, "p1", "e1", "attach", "model"); err == nil {
		t.Fatal("expected error: only an agent has attach ports")
	}
}

func TestValidateEdgeFlowDefaultsToInPort(t *testing.T) {
	g := graphWith(gn("t1", models.NodeTypeTrigger), gn("a1", models.NodeTypeAgent))
	port, err := validateEdge(g, "t1", "a1", "flow", "")
	if err != nil || port != "in" {
		t.Fatalf("want in, got %q err=%v", port, err)
	}
}

func TestValidateEdgeRejectsProviderAsFlowSource(t *testing.T) {
	g := graphWith(gn("p1", models.NodeTypeProvider), gn("e1", models.NodeTypeEnd))
	if _, err := validateEdge(g, "p1", "e1", "flow", ""); err == nil {
		t.Fatal("expected error: a provider is attach-only, never a flow step")
	}
}

func TestValidateEdgeRejectsFlowIntoTrigger(t *testing.T) {
	g := graphWith(gn("a1", models.NodeTypeAgent), gn("t1", models.NodeTypeTrigger))
	if _, err := validateEdge(g, "a1", "t1", "flow", ""); err == nil {
		t.Fatal("expected error: nothing flows into a trigger")
	}
}

func TestAuditGraphCleanGraphHasNoFindings(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			gn("t1", models.NodeTypeTrigger),
			gn("a1", models.NodeTypeAgent),
			gn("p1", models.NodeTypeProvider),
			gn("e1", models.NodeTypeEnd),
		},
		Edges: []models.WorkflowEdge{
			{ID: "e_1", From: "t1", To: "a1", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "e_2", From: "p1", To: "a1", Kind: models.EdgeKindAttach, ToPort: "model"},
			{ID: "e_3", From: "a1", To: "e1", Kind: models.EdgeKindFlow, ToPort: "in"},
		},
	}
	if got := auditGraph(g); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

func TestAuditGraphReportsAgentWithNoModel(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{gn("t1", models.NodeTypeTrigger), gn("a1", models.NodeTypeAgent)},
		Edges: []models.WorkflowEdge{
			{ID: "e_1", From: "t1", To: "a1", Kind: models.EdgeKindFlow, ToPort: "in"},
		},
	}
	findings := auditGraph(g)
	if len(findings) == 0 || !strings.Contains(strings.Join(findings, " "), "a1") {
		t.Fatalf("want a finding naming a1, got %v", findings)
	}
}

func TestAuditGraphReportsMissingTrigger(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{gn("a1", models.NodeTypeAgent), gn("p1", models.NodeTypeProvider)},
		Edges: []models.WorkflowEdge{
			{ID: "e_2", From: "p1", To: "a1", Kind: models.EdgeKindAttach, ToPort: "model"},
		},
	}
	if got := auditGraph(g); len(got) == 0 {
		t.Fatal("want a finding about the missing trigger")
	}
}

func TestAuditGraphReportsOrphanNode(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			gn("t1", models.NodeTypeTrigger),
			gn("a1", models.NodeTypeAgent),
			gn("p1", models.NodeTypeProvider),
			gn("orphan", models.NodeTypeAction),
		},
		Edges: []models.WorkflowEdge{
			{ID: "e_1", From: "t1", To: "a1", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "e_2", From: "p1", To: "a1", Kind: models.EdgeKindAttach, ToPort: "model"},
		},
	}
	findings := auditGraph(g)
	if !strings.Contains(strings.Join(findings, " "), "orphan") {
		t.Fatalf("want a finding naming the orphan node, got %v", findings)
	}
}

// Every node here is "connected" -- the agent to its provider -- so the
// orphan check passes, yet nothing the trigger starts ever reaches the agent.
// isGraphRunnable (frontend) refuses to release build mode for this graph, so
// if the audit called it clean the builder would declare it finished and the
// user would be stuck.
func TestAuditGraphReportsAgentUnreachableFromTrigger(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			gn("t1", models.NodeTypeTrigger),
			gn("e1", models.NodeTypeEnd),
			gn("a1", models.NodeTypeAgent),
			gn("p1", models.NodeTypeProvider),
		},
		Edges: []models.WorkflowEdge{
			{ID: "x1", From: "t1", To: "e1", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "x2", From: "p1", To: "a1", Kind: models.EdgeKindAttach, ToPort: "model"},
		},
	}
	findings := auditGraph(g)
	if !strings.Contains(strings.Join(findings, " "), "reach") {
		t.Fatalf("want a finding about the agent being unreachable, got %v", findings)
	}
}

// The builder's API-backed shape: the agent is reached through tool steps.
// Must stay clean -- flagging it would send the model to "repair" a graph
// that already works.
func TestAuditGraphAgentReachedThroughToolsIsClean(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			gn("t1", models.NodeTypeTrigger),
			gn("h1", models.NodeTypeTool),
			gn("a1", models.NodeTypeAgent),
			gn("p1", models.NodeTypeProvider),
		},
		Edges: []models.WorkflowEdge{
			{ID: "x1", From: "t1", To: "h1", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "x2", From: "h1", To: "a1", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "x3", From: "p1", To: "a1", Kind: models.EdgeKindAttach, ToPort: "model"},
		},
	}
	if got := auditGraph(g); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}
