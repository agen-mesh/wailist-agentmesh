package engine

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func dn(id string, t models.NodeType, template string) models.WorkflowNode {
	return models.WorkflowNode{ID: id, Type: t, Template: template, Name: id}
}

func flow(from, to string) models.WorkflowEdge {
	return models.WorkflowEdge{ID: from + "-" + to, From: from, To: to, Kind: models.EdgeKindFlow, ToPort: "in"}
}

func dataServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/empty" {
			io.WriteString(w, `{}`)
			return
		}
		io.WriteString(w, `{"data":{"price":1.5}}`)
	}))
	t.Cleanup(srv.Close)
	nodes.SetURLValidatorForTest(func(string) error { return nil })
	t.Cleanup(func() { nodes.SetURLValidatorForTest(func(string) error { return nil }) })
	return srv
}

func TestDryRunRunsReadOnlyStepsForReal(t *testing.T) {
	srv := dataServer(t)
	fetch := dn("fetch", models.NodeTypeTool, "http")
	fetch.URL = srv.URL + "/quote"
	extract := dn("extract", models.NodeTypeTool, "json_extract")
	extract.Config = map[string]string{"jsonPath": "data.price"}
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{dn("t", models.NodeTypeTrigger, "manual"), fetch, extract, dn("e", models.NodeTypeEnd, "done")},
		Edges: []models.WorkflowEdge{flow("t", "fetch"), flow("fetch", "extract"), flow("extract", "e")},
	}
	res := DryRun(context.Background(), g, "", nil)
	if res.Failed || res.Empty || res.Error != "" {
		t.Fatalf("want a clean run, got %+v", res)
	}
	if res.FinalOutput != "1.5" {
		t.Fatalf("want the extracted price as the final output, got %q", res.FinalOutput)
	}
}

// The user's case: a lookup that "succeeds" with {}.
func TestDryRunFlagsAStepThatReturnsNothing(t *testing.T) {
	srv := dataServer(t)
	fetch := dn("fetch", models.NodeTypeTool, "http")
	fetch.URL = srv.URL + "/empty"
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{dn("t", models.NodeTypeTrigger, "manual"), fetch, dn("e", models.NodeTypeEnd, "done")},
		Edges: []models.WorkflowEdge{flow("t", "fetch"), flow("fetch", "e")},
	}
	res := DryRun(context.Background(), g, "", nil)
	if !res.Empty {
		t.Fatalf("an empty {} result must be flagged, got %+v", res)
	}
	var fetchStep nodes.DryRunStep
	for _, s := range res.Steps {
		if s.NodeID == "fetch" {
			fetchStep = s
		}
	}
	if fetchStep.Status != "empty" {
		t.Fatalf("the step that returned {} must be marked empty, got %+v", fetchStep)
	}
}

func TestDryRunSimulatesStepsWithSideEffects(t *testing.T) {
	slack := dn("slack", models.NodeTypeAction, "slack")
	// A real webhook would be called if this were executed; the test server
	// fails the test if anything reaches it.
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	slack.Secrets = map[string]string{"slackWebhookURL": srv.URL}
	slack.Config = map[string]string{"messageTemplate": "Price: {{ input }}"}
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{dn("t", models.NodeTypeTrigger, "chat"), slack, dn("e", models.NodeTypeEnd, "done")},
		Edges: []models.WorkflowEdge{flow("t", "slack"), flow("slack", "e")},
	}
	res := DryRun(context.Background(), g, "hello", nil)
	if hit {
		t.Fatal("a dry run must never send the Slack message")
	}
	for _, s := range res.Steps {
		if s.NodeID == "slack" {
			if s.Status != "simulated" || s.Reason == "" {
				t.Fatalf("slack must be simulated with a reason, got %+v", s)
			}
			if !strings.Contains(s.Output, "Price: hello") {
				t.Fatalf("a simulated send should show what it would send, got %q", s.Output)
			}
			return
		}
	}
	t.Fatal("no slack step in the result")
}

func TestDryRunReportsTheAgentsAnswer(t *testing.T) {
	gem := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"MYRAD is $0.00021."}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}`)
	}))
	defer gem.Close()
	nodes.SetGeminiBaseURL(gem.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	provider := dn("p", models.NodeTypeProvider, "gemini")
	provider.KeyMode = "platform"
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{dn("t", models.NodeTypeTrigger, "manual"), dn("a", models.NodeTypeAgent, "agent"), provider, dn("e", models.NodeTypeEnd, "done")},
		Edges: []models.WorkflowEdge{flow("t", "a"), flow("a", "e"),
			{ID: "pa", From: "p", To: "a", Kind: models.EdgeKindAttach, ToPort: "model"}},
	}
	res := DryRun(context.Background(), g, "", map[string]string{"gemini": "k"})
	if res.Failed {
		t.Fatalf("unexpected failure: %+v", res)
	}
	if res.Answer != "MYRAD is $0.00021." {
		t.Fatalf("want the agent's reply as the answer, got %q", res.Answer)
	}
}

func TestDryRunReportsALoop(t *testing.T) {
	g := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{dn("t", models.NodeTypeTrigger, "manual"), dn("a", models.NodeTypeTool, "calc"), dn("b", models.NodeTypeTool, "calc")},
		Edges: []models.WorkflowEdge{flow("t", "a"), flow("a", "b"), flow("b", "a")},
	}
	if res := DryRun(context.Background(), g, "", nil); res.Error == "" || !res.Failed {
		t.Fatalf("a loop must fail the dry run, got %+v", res)
	}
}
