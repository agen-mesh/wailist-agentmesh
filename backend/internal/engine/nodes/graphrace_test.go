package nodes

import (
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

func flow(from, to string) models.WorkflowEdge {
	return models.WorkflowEdge{ID: from + "-" + to, From: from, To: to, Kind: models.EdgeKindFlow}
}

func attachModel(from, to string) models.WorkflowEdge {
	return models.WorkflowEdge{ID: from + "~" + to, From: from, To: to, Kind: models.EdgeKindAttach, ToPort: "model"}
}

func racy(graph models.WorkflowGraph) []string { return racyInputFindings(graph) }

// The live "crypto news and ALGO price" build, as it was when the test run
// answered with the price and no news.
func TestRaceFlagsTwoChainsRunningSideBySide(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "trig", Type: models.NodeTypeTrigger, Template: "manual"},
			{ID: "rss", Type: models.NodeTypeAction, Template: "rss", Config: map[string]string{"rssUrl": "https://example.com/feed"}},
			{ID: "price", Type: models.NodeTypeAction, Template: "coingecko", Config: map[string]string{"cgIDs": "algorand"}},
			{ID: "news_agent", Type: models.NodeTypeAgent},
			{ID: "price_agent", Type: models.NodeTypeAgent},
			{ID: "m1", Type: models.NodeTypeProvider},
			{ID: "m2", Type: models.NodeTypeProvider},
			{ID: "combine", Type: models.NodeTypeTool, Template: "set",
				Config: map[string]string{"setFields": `{"news":"{{ node.news_agent }}","price":"{{ node.price_agent }}"}`}},
			{ID: "end", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			flow("trig", "rss"), flow("trig", "price"),
			flow("rss", "news_agent"), flow("price", "price_agent"),
			attachModel("m1", "news_agent"), attachModel("m2", "price_agent"),
			flow("news_agent", "combine"), flow("price_agent", "combine"),
			flow("combine", "end"),
		},
	}
	got := strings.Join(racy(g), "\n")
	for _, want := range []string{`"news_agent"`, `"price_agent"`} {
		if !strings.Contains(got, "step "+want) {
			t.Errorf("want a finding for %s, got:\n%s", want, got)
		}
	}
	// The combine step names both sources and the end reads a single
	// predecessor with nothing beside it: neither is at risk.
	for _, fine := range []string{`step "combine"`, `step "end"`} {
		if strings.Contains(got, fine) {
			t.Errorf("%s is safe but was flagged:\n%s", fine, got)
		}
	}
}

// The pattern the finding recommends: parallel fetches are fine, because a
// fetch ignores its input; the join names each source; one chain after it.
func TestRaceAcceptsFetchThenCombineThenOneChain(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "trig", Type: models.NodeTypeTrigger, Template: "manual"},
			{ID: "rss", Type: models.NodeTypeAction, Template: "rss", Config: map[string]string{"rssUrl": "https://example.com/feed"}},
			{ID: "price", Type: models.NodeTypeAction, Template: "coingecko", Config: map[string]string{"cgIDs": "algorand"}},
			{ID: "combine", Type: models.NodeTypeTool, Template: "set",
				Config: map[string]string{"setFields": `{"news":"{{ node.rss }}","price":"{{ node.price }}"}`}},
			{ID: "agent", Type: models.NodeTypeAgent},
			{ID: "m", Type: models.NodeTypeProvider},
			{ID: "end", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			flow("trig", "rss"), flow("trig", "price"),
			flow("rss", "combine"), flow("price", "combine"),
			flow("combine", "agent"), attachModel("m", "agent"), flow("agent", "end"),
		},
	}
	if got := racy(g); len(got) != 0 {
		t.Fatalf("a safe graph was flagged:\n%s", strings.Join(got, "\n"))
	}
}

func TestRaceAcceptsASingleChain(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "trig", Type: models.NodeTypeTrigger, Template: "manual"},
			{ID: "price", Type: models.NodeTypeAction, Template: "coingecko", Config: map[string]string{"cgIDs": "algorand"}},
			{ID: "agent", Type: models.NodeTypeAgent},
			{ID: "m", Type: models.NodeTypeProvider},
			{ID: "end", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			flow("trig", "price"), flow("price", "agent"), attachModel("m", "agent"), flow("agent", "end"),
		},
	}
	if got := racy(g); len(got) != 0 {
		t.Fatalf("a single chain was flagged:\n%s", strings.Join(got, "\n"))
	}
}

// An end with two inputs reads only one of them.
func TestRaceFlagsAStepWithTwoImplicitInputs(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "trig", Type: models.NodeTypeTrigger, Template: "manual"},
			{ID: "a", Type: models.NodeTypeAgent},
			{ID: "m", Type: models.NodeTypeProvider},
			{ID: "end", Type: models.NodeTypeEnd},
			{ID: "price", Type: models.NodeTypeAction, Template: "coingecko", Config: map[string]string{"cgIDs": "algorand"}},
		},
		Edges: []models.WorkflowEdge{
			flow("trig", "price"), flow("price", "a"), attachModel("m", "a"),
			flow("a", "end"), flow("price", "end"),
		},
	}
	got := strings.Join(racy(g), "\n")
	if !strings.Contains(got, `step "end"`) {
		t.Fatalf("an end fed by two steps must be flagged, got:\n%s", got)
	}
}

// A read connector whose settings use {{ }} does read its input, so it is
// judged like any other implicit reader.
func TestRaceTreatsATemplatedFetchAsAReader(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "trig", Type: models.NodeTypeTrigger, Template: "manual"},
			{ID: "x", Type: models.NodeTypeAction, Template: "coingecko", Config: map[string]string{"cgIDs": "algorand"}},
			{ID: "y", Type: models.NodeTypeAction, Template: "coingecko", Config: map[string]string{"cgIDs": "bitcoin"}},
			{ID: "z", Type: models.NodeTypeAction, Template: "rss", Config: map[string]string{"rssUrl": "{{ result }}"}},
		},
		Edges: []models.WorkflowEdge{flow("trig", "x"), flow("trig", "y"), flow("x", "z")},
	}
	if got := strings.Join(racy(g), "\n"); !strings.Contains(got, `step "z"`) {
		t.Fatalf("a templated fetch beside a parallel branch must be flagged, got:\n%s", got)
	}
}

// Wired into the audit the builder already runs before it replies.
func TestAuditIncludesRaceFindings(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "trig", Type: models.NodeTypeTrigger, Template: "manual"},
			{ID: "a", Type: models.NodeTypeAgent},
			{ID: "b", Type: models.NodeTypeAgent},
			{ID: "m", Type: models.NodeTypeProvider},
		},
		Edges: []models.WorkflowEdge{
			flow("trig", "a"), flow("trig", "b"), attachModel("m", "a"), attachModel("m", "b"),
		},
	}
	if got := strings.Join(auditGraph(g), "\n"); !strings.Contains(got, "wrong branch") {
		t.Fatalf("auditGraph did not report the race:\n%s", got)
	}
}
