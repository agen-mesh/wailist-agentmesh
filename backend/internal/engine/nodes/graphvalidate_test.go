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

// The exact shape of the failed Nifty/Sensex run: "Fetch Sensex" was never
// connected to "Extract Sensex Price". Nothing flowed into the extract step,
// so the engine ran it FIRST, beside the trigger, on an empty input -- it
// failed in 3ms and dead-lettered the run before either fetch happened.
// Every node was connected to something, so the orphan check missed it.
func TestAuditGraphReportsAStepNothingFlowsInto(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			gn("start", models.NodeTypeTrigger),
			gn("fetchN", models.NodeTypeTool), gn("extractN", models.NodeTypeTool),
			gn("fetchS", models.NodeTypeTool), gn("extractS", models.NodeTypeTool),
			gn("combine", models.NodeTypeTool),
			gn("agent", models.NodeTypeAgent), gn("model", models.NodeTypeProvider),
		},
		Edges: []models.WorkflowEdge{
			{ID: "1", From: "start", To: "fetchN", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "2", From: "fetchN", To: "extractN", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "3", From: "start", To: "fetchS", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "4", From: "extractN", To: "combine", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "5", From: "extractS", To: "combine", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "6", From: "combine", To: "agent", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "7", From: "model", To: "agent", Kind: models.EdgeKindAttach, ToPort: "model"},
		},
	}
	joined := strings.Join(auditGraph(g), " ")
	if !strings.Contains(joined, `"extractS"`) {
		t.Fatalf("want a finding naming extractS, got: %s", joined)
	}
	if strings.Contains(joined, `"model"`) {
		t.Fatalf("a provider attached to an agent is not a flow step and must not be flagged: %s", joined)
	}
}

// A tool attached only to an agent's tools port is called by the agent, not
// run as a flow step, so it is never "unreached".
func TestAuditGraphDoesNotFlagAnAgentAttachedTool(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			gn("t1", models.NodeTypeTrigger), gn("a1", models.NodeTypeAgent),
			gn("p1", models.NodeTypeProvider), gn("search", models.NodeTypeTool),
		},
		Edges: []models.WorkflowEdge{
			{ID: "1", From: "t1", To: "a1", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "2", From: "p1", To: "a1", Kind: models.EdgeKindAttach, ToPort: "model"},
			{ID: "3", From: "search", To: "a1", Kind: models.EdgeKindAttach, ToPort: "tools"},
		},
	}
	if got := auditGraph(g); len(got) != 0 {
		t.Fatalf("want no findings, got %v", got)
	}
}

// Review finding: only a self-loop was rejected. A->B->A passed every check,
// released build mode, and then failed every run with "cycle detected".
func TestValidateEdgeRejectsAnEdgeThatClosesALoop(t *testing.T) {
	g := graphWith(gn("t", models.NodeTypeTrigger), gn("a", models.NodeTypeTool), gn("b", models.NodeTypeTool))
	g.Edges = []models.WorkflowEdge{
		{ID: "1", From: "t", To: "a", Kind: models.EdgeKindFlow, ToPort: "in"},
		{ID: "2", From: "a", To: "b", Kind: models.EdgeKindFlow, ToPort: "in"},
	}
	if _, err := validateEdge(g, "b", "a", "flow", ""); err == nil || !strings.Contains(err.Error(), "loop") {
		t.Fatalf("an edge closing a loop must be rejected, got: %v", err)
	}
}

// A loop drawn by hand on the canvas never passes through validateEdge, so
// the audit must catch one that already exists.
func TestAuditGraphReportsALoop(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{gn("t", models.NodeTypeTrigger), gn("a", models.NodeTypeTool), gn("b", models.NodeTypeTool)},
		Edges: []models.WorkflowEdge{
			{ID: "1", From: "t", To: "a", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "2", From: "a", To: "b", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "3", From: "b", To: "a", Kind: models.EdgeKindFlow, ToPort: "in"},
		},
	}
	if !strings.Contains(strings.Join(auditGraph(g), " "), "loop") {
		t.Fatalf("want a finding about the loop, got %v", auditGraph(g))
	}
}

// Review finding: the repair round used to get every finding on the graph,
// so a turn that only asked a question would have the builder quietly delete
// or rewire a node the user had parked unconnected. Only what the turn itself
// broke is handed back.
func TestNewAuditFindingsIgnoresWhatTheUserLeft(t *testing.T) {
	before := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{gn("t1", models.NodeTypeTrigger), gn("parked", models.NodeTypeTool)},
	}
	baseline := auditGraph(before)
	if len(baseline) == 0 {
		t.Fatal("setup: the parked node should be a finding on its own")
	}
	if got := newAuditFindings(baseline, before); len(got) != 0 {
		t.Fatalf("an unchanged graph has nothing new to repair, got %v", got)
	}

	after := before
	after.Nodes = append(append([]models.WorkflowNode{}, before.Nodes...), gn("added", models.NodeTypeTool))
	got := newAuditFindings(baseline, after)
	if len(got) != 1 || !strings.Contains(got[0], `"added"`) {
		t.Fatalf("want only the node this turn added, got %v", got)
	}
}

// The loop finding names whichever edge closes the loop first, which an
// unrelated edit can shift. The loop the user already had is still not new.
func TestNewAuditFindingsMatchesAnExistingLoopByKind(t *testing.T) {
	baseline := []string{loopFindingPrefix + ` through node "a" -- the engine cannot run a loop`}
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{gn("t1", models.NodeTypeTrigger), gn("a", models.NodeTypeAction), gn("b", models.NodeTypeAction)},
		Edges: []models.WorkflowEdge{
			{ID: "e0", From: "t1", To: "a", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "e1", From: "b", To: "a", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "e2", From: "a", To: "b", Kind: models.EdgeKindFlow, ToPort: "in"},
		},
	}
	for _, f := range newAuditFindings(baseline, g) {
		if strings.HasPrefix(f, loopFindingPrefix) {
			t.Fatalf("a loop that was already there must not count as new: %v", f)
		}
	}
}

// The user: "sometimes the agent generates workflows where there are no
// agent" -- and the run's output was raw JSON or {}, not an answer.
func TestAuditGraphReportsAWorkflowWithNoAgent(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{gn("t", models.NodeTypeTrigger), gn("cg", models.NodeTypeAction), gn("e", models.NodeTypeEnd)},
		Edges: []models.WorkflowEdge{
			{ID: "1", From: "t", To: "cg", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "2", From: "cg", To: "e", Kind: models.EdgeKindFlow, ToPort: "in"},
		},
	}
	if !strings.Contains(strings.Join(auditGraph(g), " "), "no agent") {
		t.Fatalf("want a finding that the workflow has no agent, got %v", auditGraph(g))
	}
}
