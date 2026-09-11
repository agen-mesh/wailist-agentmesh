package nodes

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/bazaar"
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
	// A builder-made agent keeps its instructions and gains the no-guessing
	// rule (see TestBuilderAgentsAreToldNeverToInventValues).
	if n.Type != models.NodeTypeAgent || n.Template != "agent" || n.Name != "Support Agent" ||
		!strings.HasPrefix(n.SystemPrompt, "Be helpful") || !strings.Contains(n.SystemPrompt, agentAnswerGuard) {
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

	result, err := BuildGraph(context.Background(), BuildRequest{APIKey: "test-key", Message: "add a chat trigger", Graph: models.WorkflowGraph{}, History: nil})
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

	res, err := BuildGraph(context.Background(), BuildRequest{APIKey: "test-key", Message: "build something endless", Graph: models.WorkflowGraph{}, History: nil})
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

	res, err := BuildGraph(context.Background(), BuildRequest{APIKey: "k", Message: "build a phone search agent", Graph: models.WorkflowGraph{}, History: nil})
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

func TestApplyGraphOpAddNodeSetsAllowedConfigKey(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type":     "tool",
		"template": "http",
		"name":     "Weather API",
		"fields":   map[string]any{"url": "https://api.example.com/v1/weather", "method": "POST"},
		"config":   map[string]any{"httpBodyTemplate": `{"city":"{{ result }}"}`},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := graph.Nodes[0].Config["httpBodyTemplate"]
	if got != `{"city":"{{ result }}"}` {
		t.Fatalf("httpBodyTemplate not set, got %q", got)
	}
}

// The redaction that keeps credentials away from Gemini
// (redactNodesForBuildAgent) is only worth anything if the model cannot
// write them back. A rejected call, not a silently ignored key: the model
// must learn to disclose the credential instead of trying to set it.
func TestApplyGraphOpRejectsSecretBearingConfigKeys(t *testing.T) {
	for _, key := range []string{"httpHeadersJSON", "httpBasicUser", "httpBasicPass", "apiKey"} {
		graph := &models.WorkflowGraph{}
		_, err := applyGraphOp(graph, "add_node", map[string]any{
			"type":     "tool",
			"template": "http",
			"config":   map[string]any{key: "secret-value"},
		})
		if err == nil {
			t.Fatalf("config key %q should be rejected", key)
		}
		if !strings.Contains(err.Error(), "description") {
			t.Fatalf("the error for %q should tell the model to disclose it on the description instead, got: %v", key, err)
		}
	}
}

func TestApplyGraphOpUpdateNodeMergesConfig(t *testing.T) {
	graph := &models.WorkflowGraph{Nodes: []models.WorkflowNode{{
		ID:       "n1",
		Type:     models.NodeTypeAction,
		Template: "slack",
		Config:   map[string]string{"slackChannel": "C0123"},
	}}}
	_, err := applyGraphOp(graph, "update_node", map[string]any{
		"id":     "n1",
		"config": map[string]any{"messageTemplate": "hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Merge, not replace: an update that sets one key must not wipe a key
	// the user configured by hand in the Inspector.
	if graph.Nodes[0].Config["slackChannel"] != "C0123" {
		t.Fatal("update_node replaced Config instead of merging into it")
	}
	if graph.Nodes[0].Config["messageTemplate"] != "hello" {
		t.Fatal("update_node did not apply the new config key")
	}
}

func TestApplyGraphOpRejectsNonStringConfigValue(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type":     "tool",
		"template": "http",
		"config":   map[string]any{"httpBodyTemplate": 42},
	})
	if err == nil {
		t.Fatal("expected an error for a non-string config value")
	}
}

// The node section of the prompt is generated from the catalog, so every
// template the canvas offers must appear -- this is what the old hand-typed
// list got wrong (4 of 12 tools, 24 of 42 connectors, a cron trigger that
// never existed).
func TestBuildSystemPromptListsEveryCatalogTemplate(t *testing.T) {
	for _, typ := range NodeCatalogData().Types {
		for _, tpl := range typ.Templates {
			if !strings.Contains(buildSystemPrompt, "\n  "+tpl.ID+" ") {
				t.Errorf("system prompt does not list %s/%s", typ.Type, tpl.ID)
			}
		}
	}
}

func TestBuildSystemPromptHasNoCronAndExplainsSchedules(t *testing.T) {
	// What matters is that no catalog entry offers one; the prompt is
	// expected to MENTION cron, to say there is no such trigger.
	if strings.Contains(buildSystemPrompt, "\n  cron ") || strings.Contains(buildSystemPrompt, "\n  schedule ") {
		t.Fatal("the node catalog in the prompt offers a cron/schedule trigger")
	}
	if !strings.Contains(buildSystemPrompt, "Schedule") || !strings.Contains(buildSystemPrompt, "UTC") {
		t.Fatal("the prompt must say schedules are set on the Workflows page, in UTC")
	}
}

// The same guarantee through the other door. nodeFieldSetters used to map
// "apiKey" straight onto node.APIKey, so update_node(fields={apiKey:...}) on
// a provider holding a real key handed encryptField a plain value, which it
// encrypted over the top of the user's real credential. The model has never
// seen that key (the graph it gets is redacted) -- anything it writes there
// is invented.
func TestApplyGraphOpRejectsAPIKeyInFields(t *testing.T) {
	graph := &models.WorkflowGraph{Nodes: []models.WorkflowNode{{
		ID:       "p1",
		Type:     models.NodeTypeProvider,
		Template: "gemini",
		APIKey:   "__enc__",
	}}}
	_, err := applyGraphOp(graph, "update_node", map[string]any{
		"id":     "p1",
		"fields": map[string]any{"apiKey": "sk-invented-by-the-model"},
	})
	if err == nil {
		t.Fatal("expected update_node to reject an apiKey field")
	}
	if graph.Nodes[0].APIKey != "__enc__" {
		t.Fatalf("the stored key sentinel must be untouched, got %q", graph.Nodes[0].APIKey)
	}
}

func TestBuildGraphReplaysPriorTurns(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	history := []BuildTurn{
		{Role: "user", Text: "the phone must have 16GB RAM"},
		{Role: "model", Text: "Noted."},
	}
	if _, err := BuildGraph(context.Background(), BuildRequest{APIKey: "k", Message: "now add an email step", Graph: models.WorkflowGraph{}, History: history}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(body, "16GB RAM") {
		t.Fatal("prior user turn was not replayed into the request")
	}
	if !strings.Contains(body, "now add an email step") {
		t.Fatal("the current message is missing from the request")
	}
	// The graph snapshot must ride with the CURRENT turn, not the oldest one:
	// replaying a stale graph as the first user turn would have the model
	// reasoning about nodes that no longer exist.
	if strings.Index(body, "16GB RAM") > strings.Index(body, "Current graph") {
		t.Fatal("the graph snapshot must come after the replayed history")
	}
}

func TestBuildGraphWithNoHistoryStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	res, err := BuildGraph(context.Background(), BuildRequest{APIKey: "k", Message: "hello", Graph: models.WorkflowGraph{}, History: nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Reply != "ok" {
		t.Fatalf("want reply ok, got %q", res.Reply)
	}
}

// Stored history is replayed verbatim into Gemini's contents, where a role
// other than user/model is rejected by the API and fails the whole build.
// Skipped rather than trusted: the DB CHECK guards writes today, but this is
// the boundary where a bad row would otherwise take down every later turn.
func TestBuildGraphSkipsMalformedHistoryTurns(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	history := []BuildTurn{
		{Role: "system", Text: "BAD-ROLE"},
		{Role: "user", Text: "   "},
		{Role: "user", Text: "keep me"},
	}
	if _, err := BuildGraph(context.Background(), BuildRequest{APIKey: "k", Message: "go", Graph: models.WorkflowGraph{}, History: history}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(body, "BAD-ROLE") {
		t.Fatal("a turn with an invalid role was replayed")
	}
	if !strings.Contains(body, "keep me") {
		t.Fatal("a valid turn was dropped")
	}
}

func TestAddNodeRejectsCronTrigger(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{"type": "trigger", "template": "cron"})
	if err == nil {
		t.Fatal("a cron trigger does not exist and must be rejected")
	}
	for _, want := range []string{"manual", "chat", "webhook"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error should list the real triggers, got: %v", err)
		}
	}
}

func TestAddNodeCreatesStateAndGoogleNodes(t *testing.T) {
	graph := &models.WorkflowGraph{}
	if _, err := applyGraphOp(graph, "add_node", map[string]any{"type": "state", "template": "set"}); err != nil {
		t.Fatalf("state node: %v", err)
	}
	if _, err := applyGraphOp(graph, "add_node", map[string]any{"type": "google", "template": "gmail_list"}); err != nil {
		t.Fatalf("google node: %v", err)
	}
}

// A "Write State" node with no stateOp silently runs get; a Tendril node with
// no tendrilAction fails outright. The palette sets both on drop.
func TestAddNodeAppliesCatalogPresets(t *testing.T) {
	graph := &models.WorkflowGraph{}
	applyGraphOp(graph, "add_node", map[string]any{"type": "state", "template": "set"})
	applyGraphOp(graph, "add_node", map[string]any{"type": "tendril", "template": "tendril_rent"})
	applyGraphOp(graph, "add_node", map[string]any{"type": "provider", "template": "gemini"})
	if graph.Nodes[0].StateOp != "set" {
		t.Fatalf("state/set must preset stateOp=set, got %q", graph.Nodes[0].StateOp)
	}
	if graph.Nodes[1].TendrilAction != "rent" || graph.Nodes[1].TendrilHours != "1" {
		t.Fatalf("tendril_rent presets not applied: %+v", graph.Nodes[1])
	}
	if graph.Nodes[2].Model != "gemini-2.5-flash" {
		t.Fatalf("provider/gemini must preset its default model, got %q", graph.Nodes[2].Model)
	}
}

// The bug from the Nifty-50 build: the path had nowhere to go.
func TestAddNodeSetsTemplateSpecificConfig(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "json_extract",
		"config": map[string]any{"jsonPath": "data.0.lastPrice"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if graph.Nodes[0].Config["jsonPath"] != "data.0.lastPrice" {
		t.Fatalf("jsonPath not set: %+v", graph.Nodes[0].Config)
	}
}

func TestAddNodeRejectsSettingsTheTemplateDoesNotHave(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "http",
		"config": map[string]any{"jsonPath": "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "httpBodyTemplate") {
		t.Fatalf("want a rejection listing http's real settings, got: %v", err)
	}
	_, err = applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "http",
		"fields": map[string]any{"bogusField": "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "url") {
		t.Fatalf("want a rejection listing http's real fields, got: %v", err)
	}
}

// A connector credential is disclosed, never set -- and the error points at
// where the user gets it, so the reply can too.
func TestAddNodeRejectsConnectorCredentialWithItsSource(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "action", "template": "slack",
		"config": map[string]any{"slackWebhookURL": "https://hooks.slack.com/x"},
	})
	if err == nil {
		t.Fatal("a Slack webhook URL is a credential and must not be settable")
	}
	if !strings.Contains(err.Error(), "api.slack.com") {
		t.Fatalf("the error should say where the credential comes from, got: %v", err)
	}
}

func TestAddNodeRejectsGoogleAccountConnection(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "google", "template": "gmail_list",
		"config": map[string]any{"oauthCredentialID": "whatever"},
	})
	if err == nil {
		t.Fatal("the Google account is linked by the user, never set by the builder")
	}
}

func TestUpdateNodeTemplateChangeReappliesPresets(t *testing.T) {
	graph := &models.WorkflowGraph{}
	applyGraphOp(graph, "add_node", map[string]any{"type": "state", "template": "get"})
	id := graph.Nodes[0].ID
	if _, err := applyGraphOp(graph, "update_node", map[string]any{"id": id, "template": "set"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if graph.Nodes[0].StateOp != "set" {
		t.Fatalf("changing state get->set must move stateOp too, got %q", graph.Nodes[0].StateOp)
	}
}

// x402 nodes are custom endpoints, not catalog templates -- the pasted-URL
// path must keep working.
func TestAddNodeTool402StillAcceptsEndpointFields(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool402", "template": "custom",
		"fields": map[string]any{"endpoint": "https://x402.example.com/data", "method": "GET"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if graph.Nodes[0].Endpoint != "https://x402.example.com/data" {
		t.Fatalf("endpoint not set: %+v", graph.Nodes[0])
	}
	// tool402 calls node.Endpoint and nothing else, so "url" -- which the old
	// prompt told the model to set -- must be refused rather than stored
	// somewhere the executor never reads.
	if _, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool402", "template": "custom",
		"fields": map[string]any{"url": "https://x402.example.com/data"},
	}); err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("a tool402 url must be rejected in favour of endpoint, got: %v", err)
	}
}

func TestDescribeNodeReturnsTemplateDetail(t *testing.T) {
	out, err := describeNode("tool", "json_extract")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "jsonPath") {
		t.Fatalf("describe_node should detail jsonPath, got %s", out)
	}
	if _, err := describeNode("trigger", "cron"); err == nil {
		t.Fatal("describe_node must reject a template that does not exist")
	}
}

func sampleX402() bazaar.Resource {
	return bazaar.Resource{
		ID: "res-stocks-1", URL: "https://stocks.example.com/v1/index", Method: "GET",
		Description: "Live stock market index prices", Host: "stocks.example.com",
		AmountMicros: 5000, Asset: "31566704", Network: "algorand-mainnet",
		Params:        []bazaar.Param{{Name: "symbol", Type: "string", Required: true, Description: "index symbol"}},
		OutputExample: `{"symbol":"NIFTY","price":24812.3}`,
		SettleCount:   42,
	}
}

// The same mapping the frontend's resourceToNode applies when a Bazaar card
// is added to a canvas (frontend/src/lib/bazaar.ts), so a builder-added
// endpoint is indistinguishable from one the user added by hand.
func TestX402NodeFromResourceMatchesTheBazaarMapping(t *testing.T) {
	n := x402NodeFromResource(sampleX402(), "n_1", 0, 0, "")
	if n.Type != models.NodeTypeTool402 {
		t.Fatalf("type = %q", n.Type)
	}
	// tool402 calls node.Endpoint, never node.URL.
	if n.Endpoint != "https://stocks.example.com/v1/index" || n.URL != "" {
		t.Fatalf("endpoint/url wrong: endpoint=%q url=%q", n.Endpoint, n.URL)
	}
	if n.Method != "GET" || n.Price != "0.005" || n.Unit != "call" {
		t.Fatalf("method/price/unit wrong: %+v", n)
	}
	if n.Provider != "stocks.example.com" || n.Name != "stocks.example.com" {
		t.Fatalf("an unsupported entry is named after its host: %+v", n)
	}
	if len(n.DiscoveredParams) != 1 || n.DiscoveredParams[0].Name != "symbol" || !n.DiscoveredParams[0].Required {
		t.Fatalf("params not carried over: %+v", n.DiscoveredParams)
	}
	// Catalog examples are placeholders, never usable values.
	if v, ok := n.ParamDefaults["symbol"]; !ok || v != "" {
		t.Fatalf("param defaults must be seeded empty: %+v", n.ParamDefaults)
	}
}

// search_x402 -> add_x402_node through the real tool loop: the model never
// types a URL or a price; it picks an id and the server fills in the rest
// from the catalog.
func TestBuildGraphSearchesAndAddsX402Node(t *testing.T) {
	turn := 0
	var searchReply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		turn++
		w.Header().Set("Content-Type", "application/json")
		switch turn {
		case 1:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"search_x402","args":{"query":"stock index prices"}}}]}}]}`)
		case 2:
			searchReply = string(body)
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"add_x402_node","args":{"id":"res-stocks-1"}}}]}}]}`)
		default:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"Added the stock index endpoint (0.005 USDC per call)."}]}}]}`)
		}
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	res, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "fetch NSE index prices",
		X402Catalog: func(context.Context) ([]bazaar.Resource, error) {
			return []bazaar.Resource{sampleX402()}, nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"res-stocks-1", "0.005", "USDC", "symbol"} {
		if !strings.Contains(searchReply, want) {
			t.Fatalf("search result sent to the model is missing %q", want)
		}
	}
	var x402 *models.WorkflowNode
	for i := range res.Graph.Nodes {
		if res.Graph.Nodes[i].Type == models.NodeTypeTool402 {
			x402 = &res.Graph.Nodes[i]
		}
	}
	if x402 == nil || x402.Endpoint != "https://stocks.example.com/v1/index" {
		t.Fatalf("want a tool402 node on the catalog endpoint, got %+v", res.Graph.Nodes)
	}
}

func TestAddX402NodeRejectsAnIDNotInTheCatalog(t *testing.T) {
	cat := func(context.Context) ([]bazaar.Resource, error) { return []bazaar.Resource{sampleX402()}, nil }
	x := newX402Session(cat)
	graph := &models.WorkflowGraph{}
	if _, err := x.add(context.Background(), graph, map[string]any{"id": "invented-by-the-model"}); err == nil {
		t.Fatal("an id that is not in the catalog must be rejected")
	}
	if len(graph.Nodes) != 0 {
		t.Fatal("nothing may be added for a rejected id")
	}
}

func TestX402ToolsWithoutACatalogFailSoftly(t *testing.T) {
	x := newX402Session(nil)
	if _, err := x.search(context.Background(), map[string]any{"query": "weather"}); err == nil {
		t.Fatal("with no catalog configured, search_x402 must report that rather than succeed empty")
	}
}

// From the live Nifty-50 build: the model set keyMode=byok unprompted and
// then told the user to paste a Gemini key. The platform key needs nothing
// from the user, and anyone who does want their own key switches the mode in
// the Inspector, where they paste it -- so the builder never sets byok.
func TestAddNodeRejectsByokKeyMode(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "provider", "template": "gemini",
		"fields": map[string]any{"keyMode": "byok"},
	})
	if err == nil || !strings.Contains(err.Error(), "Inspector") {
		t.Fatalf("byok must be rejected with where the user switches it, got: %v", err)
	}
	if _, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "provider", "template": "gemini",
		"fields": map[string]any{"keyMode": "platform"},
	}); err != nil {
		t.Fatalf("platform must stay settable: %v", err)
	}
}

// Also from the live build: model "gemini-pro", a retired name that 404s.
func TestAddNodeRejectsAModelTheEngineDoesNotKnow(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "provider", "template": "gemini",
		"fields": map[string]any{"model": "gemini-pro"},
	})
	if err == nil || !strings.Contains(err.Error(), "gemini-2.5-flash") {
		t.Fatalf("an unknown model must be rejected with the real ones listed, got: %v", err)
	}
	if _, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "provider", "template": "gemini",
		"fields": map[string]any{"model": "gemini-2.5-pro"},
	}); err != nil {
		t.Fatalf("a known model must be accepted: %v", err)
	}
}

// And: jsonPath "$.data[0].lastPrice". walkPath splits on dots only, so that
// fails on the "$" segment at run time. Reject it with the form that works.
func TestAddNodeRejectsJSONPathSyntaxWithTheDotPathThatWorks(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "json_extract",
		"config": map[string]any{"jsonPath": "$.data[0].lastPrice"},
	})
	if err == nil || !strings.Contains(err.Error(), "data.0.lastPrice") {
		t.Fatalf("want a rejection suggesting data.0.lastPrice, got: %v", err)
	}
	if _, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "json_extract",
		"config": map[string]any{"jsonPath": "data.0.lastPrice"},
	}); err != nil {
		t.Fatalf("a dot path must be accepted: %v", err)
	}
}

func TestJSONPathToDotPath(t *testing.T) {
	for in, want := range map[string]string{
		"$.data[0].lastPrice":     "data.0.lastPrice",
		"$['data'][2]['price']":   "data.2.price",
		"data.items.0.name":       "data.items.0.name",
		"$.records.data[10].last": "records.data.10.last",
	} {
		if got := toDotPath(in); got != want {
			t.Errorf("toDotPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// The compact catalog lists key names; a key's expected FORMAT is what the
// model got wrong, so a short example rides along where the catalog has one.
func TestBuildSystemPromptShowsSettingExamples(t *testing.T) {
	if !strings.Contains(buildSystemPrompt, "jsonPath (e.g. data.items.0.name)") {
		t.Fatal("the catalog line for json_extract should show a jsonPath example")
	}
}

// From the live Slack build: the provider was correctly on the platform key,
// yet the reply told the user to paste a Gemini API key -- because the
// catalog line and the add_node result both listed apiKey under "user
// supplies". The builder can never set byok, so a builder-made provider
// never needs a key, and must not be described as needing one.
func TestProviderIsNotDescribedAsNeedingAKey(t *testing.T) {
	for _, line := range strings.Split(buildSystemPrompt, "\n") {
		// The NOTE may mention apiKey to say it is NOT needed; what must go is
		// listing it as something the user supplies.
		if strings.HasPrefix(line, "  gemini ") && strings.Contains(line, "user supplies: apiKey") {
			t.Fatalf("the gemini catalog line still lists apiKey as user-supplied: %s", line)
		}
	}
	graph := &models.WorkflowGraph{}
	res, err := applyGraphOp(graph, "add_node", map[string]any{"type": "provider", "template": "gemini"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res, "apiKey") || strings.Contains(res, "credential") {
		t.Fatalf("adding a platform-key provider must not ask for credentials: %s", res)
	}
}

// From the same failed run: Combine Prices used {{n_<id>.output}}. The engine
// reads {{ node.<id> }}; anything else is left in the text verbatim, so the
// step would have produced literal braces instead of the prices.
func TestAddNodeRejectsTemplateReferencesTheEngineCannotResolve(t *testing.T) {
	graph := &models.WorkflowGraph{Nodes: []models.WorkflowNode{
		{ID: "n_111", Type: models.NodeTypeTool, Template: "json_extract"},
	}}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "set",
		"config": map[string]any{"setFields": `{"price": "{{n_111.output}}"}`},
	})
	if err == nil || !strings.Contains(err.Error(), "{{ node.n_111 }}") {
		t.Fatalf("want a rejection suggesting {{ node.n_111 }}, got: %v", err)
	}
	_, err = applyGraphOp(graph, "add_node", map[string]any{
		"type": "tool", "template": "set",
		"config": map[string]any{"setFields": `{"price": "{{ node.n_999 }}"}`},
	})
	if err == nil || !strings.Contains(err.Error(), "n_999") {
		t.Fatalf("a reference to a node that does not exist must be rejected, got: %v", err)
	}
	for _, ok := range []string{
		`{{ result }}`, `{{ result.data.price }}`, `{{ input }}`,
		`{{ node.n_111 }}`, `{{node.n_111.price}}`, `{{ state.lastPrice }}`,
	} {
		if _, err := applyGraphOp(graph, "add_node", map[string]any{
			"type": "action", "template": "slack",
			"config": map[string]any{"messageTemplate": "Price: " + ok},
		}); err != nil {
			t.Fatalf("%s is a supported reference and must be accepted: %v", ok, err)
		}
	}
}

// fetch_url lets the builder call an API before wiring it, so it can see
// whether the endpoint answers at all and read the real JSON shape instead of
// guessing a path. The failed run guessed both, on an endpoint answering 429.
func TestFetchURLReportsStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocked" {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"symbol":"NIFTY 50","lastPrice":24812.3}]}`)
	}))
	defer srv.Close()
	// Restore the package's test-wide permissive validator, not nil: nil
	// switches every later test in the package back to production dialing,
	// which refuses the 127.0.0.1 test servers they all use.
	SetURLValidatorForTest(func(string) error { return nil })
	defer SetURLValidatorForTest(func(string) error { return nil })

	out := fetchURL(context.Background(), srv.URL+"/quote")
	for _, want := range []string{`"status":200`, "lastPrice", "24812.3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("fetch_url result missing %q: %s", want, out)
		}
	}
	blocked := fetchURL(context.Background(), srv.URL+"/blocked")
	if !strings.Contains(blocked, `"status":429`) {
		t.Fatalf("a refused request must report its status, got: %s", blocked)
	}
	if got := fetchURL(context.Background(), "file:///etc/passwd"); !strings.Contains(got, "error") {
		t.Fatalf("a non-http URL must be refused, got: %s", got)
	}
}

func TestGraphToolDeclsIncludesFetchURL(t *testing.T) {
	for _, d := range graphToolDecls() {
		if d.Name == "fetch_url" {
			return
		}
	}
	t.Fatal("expected a fetch_url declaration")
}

// A live build of "fetch nifty 50 and sensex prices and give me a summary"
// ran past the frontend proxy's 120s limit. The proxy cut the request, the
// build's context was cancelled mid-loop, and everything built so far was
// thrown away. The builder now stops starting rounds once its time budget is
// spent and returns what it has, like the round cap does.
func TestBuildGraphStopsAtItsTimeBudgetAndKeepsWhatItBuilt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"add_node","args":{"type":"tool","template":"calc"}}}]}}]}`)
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	start := time.Now()
	res, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "build something slow",
		TimeBudget: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("running out of time must not be an error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the budget was not honoured: took %v", elapsed)
	}
	if len(res.Graph.Nodes) == 0 || len(res.Graph.Nodes) >= maxBuildIterations {
		t.Fatalf("want the partial graph built before the budget ran out, got %d nodes", len(res.Graph.Nodes))
	}
	if !strings.Contains(strings.ToLower(res.Reply), "time") {
		t.Fatalf("the reply should say it ran out of time, got: %q", res.Reply)
	}
}

// If a single model call is still in flight when the hard deadline hits,
// the nodes already built must still come back, not an error.
func TestBuildGraphHardDeadlineMidCallKeepsWhatItBuilt(t *testing.T) {
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn++
		if turn > 1 {
			time.Sleep(2 * time.Second) // outlives the hard deadline below
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"add_node","args":{"type":"tool","template":"calc"}}}]}}]}`)
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	res, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "x", TimeBudget: 300 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("a deadline mid-call must not be an error when work exists: %v", err)
	}
	if len(res.Graph.Nodes) != 1 {
		t.Fatalf("want the one node built before the stalled call, got %d", len(res.Graph.Nodes))
	}
}

// The run that 404'd: the builder called fetch_url on one URL, then wired a
// different one it never checked. The prompt asked it to verify first; it
// did not. So the builder now probes every static GET url it sets on an http
// node itself, and refuses a URL a run would fail on.
func buildOnce(t *testing.T, url string) (BuildGraphResult, string) {
	t.Helper()
	turn := 0
	var secondRequest string
	gem := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		turn++
		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"add_node","args":{"type":"tool","template":"http","fields":{"url":"`+url+`","method":"GET"}}}}]}}]}`)
			return
		}
		if turn == 2 {
			secondRequest = string(body)
		}
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"done"}]}}]}`)
	}))
	t.Cleanup(gem.Close)
	SetGeminiBaseURL(gem.URL)
	t.Cleanup(func() { SetGeminiBaseURL("https://generativelanguage.googleapis.com") })
	SetURLValidatorForTest(func(string) error { return nil })
	t.Cleanup(func() { SetURLValidatorForTest(func(string) error { return nil }) })

	res, err := BuildGraph(context.Background(), BuildRequest{APIKey: "k", Message: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res, secondRequest
}

func probeTarget(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"data":{"nifty":24812.3}}`)
		case "/auth":
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			http.Error(w, "The page could not be found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBuilderRefusesAnHTTPNodeOnAURLThatFails(t *testing.T) {
	srv := probeTarget(t)
	res, sent := buildOnce(t, srv.URL+"/missing")
	for _, n := range res.Graph.Nodes {
		if n.Type == models.NodeTypeTool && n.Template == "http" {
			t.Fatalf("an http node on a 404 url must not be added: %+v", n)
		}
	}
	// A phrase only the probe produces: the prompt itself is in this request.
	if !strings.Contains(sent, "answered HTTP 404") {
		t.Fatalf("the model must be told the url answered 404, got: %s", sent)
	}
	// A live build then burned its whole time budget hunting for another
	// free API -- none works for index prices from a server. The refusal
	// must point at the fallback that does: a websearch tool on the agent.
	if !strings.Contains(sent, "attach a websearch tool") {
		t.Fatalf("the refusal should offer the websearch-tool fallback, got: %s", sent)
	}
}

func TestBuilderAddsAWorkingHTTPNodeAndShowsItsResponse(t *testing.T) {
	srv := probeTarget(t)
	res, sent := buildOnce(t, srv.URL+"/ok")
	if len(res.Graph.Nodes) != 1 || res.Graph.Nodes[0].URL != srv.URL+"/ok" {
		t.Fatalf("a url answering 200 must be added: %+v", res.Graph.Nodes)
	}
	// The real body goes back to the model so any jsonPath comes from it.
	if !strings.Contains(sent, "24812.3") {
		t.Fatalf("the verified response body should reach the model, got: %s", sent)
	}
}

// 401/403 means the endpoint is real but wants a credential the user adds in
// the Inspector -- add it, and say so.
func TestBuilderAddsAnAuthRequiredHTTPNodeWithAWarning(t *testing.T) {
	srv := probeTarget(t)
	res, sent := buildOnce(t, srv.URL+"/auth")
	if len(res.Graph.Nodes) != 1 {
		t.Fatalf("an endpoint answering 401 must still be added: %+v", res.Graph.Nodes)
	}
	if !strings.Contains(sent, "requires authentication (HTTP 401)") {
		t.Fatalf("the model must be told the endpoint needs a credential, got: %s", sent)
	}
}

// The chat shows what the builder is doing, step by step, instead of a bare
// spinner. OnProgress receives a snapshot each time a step starts or ends:
// the finished steps so far, and what is happening right now.
func TestBuildGraphReportsReadableProgress(t *testing.T) {
	target := probeTarget(t)
	turn := 0
	gem := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		// The web_search sub-request is not a builder turn: answer it before
		// counting, or it shifts every scripted turn after it.
		if strings.Contains(string(body), "google_search") {
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"Nifty is 24812."}]}}]}`)
			return
		}
		turn++
		switch {
		case turn == 1:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"web_search","args":{"query":"nifty 50 price api"}}}]}}]}`)
		case turn == 2:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[`+
				`{"functionCall":{"name":"add_node","args":{"type":"trigger","template":"manual","name":"Start"}}},`+
				`{"functionCall":{"name":"add_node","args":{"type":"tool","template":"http","name":"Fetch Nifty","fields":{"url":"`+target.URL+`/ok"}}}},`+
				`{"functionCall":{"name":"add_node","args":{"type":"tool","template":"http","name":"Dead API","fields":{"url":"`+target.URL+`/gone"}}}}`+
				`]}}]}`)
		case turn == 3:
			// Edge between the two nodes added last round, found by name.
			var ids []string
			for _, m := range regexp.MustCompile(`added node (n_\d+)`).FindAllStringSubmatch(string(body), -1) {
				ids = append(ids, m[1])
			}
			if len(ids) < 2 {
				io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"done"}]}}]}`)
				return
			}
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"add_edge","args":{"from":"`+ids[0]+`","to":"`+ids[1]+`"}}}]}}]}`)
		default:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"done"}]}}]}`)
		}
	}))
	defer gem.Close()
	SetGeminiBaseURL(gem.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")
	SetURLValidatorForTest(func(string) error { return nil })
	defer SetURLValidatorForTest(func(string) error { return nil })

	var last BuildProgress
	var sawCurrent []string
	_, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "x",
		OnProgress: func(p BuildProgress) {
			last = p
			if p.Current != "" {
				sawCurrent = append(sawCurrent, p.Current)
			}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	labels := make([]string, len(last.Steps))
	for i, s := range last.Steps {
		labels[i] = s.Status + " | " + s.Label + " | " + s.Detail
	}
	joined := strings.Join(labels, "\n")
	for _, want := range []string{
		`done | Searched the web for “nifty 50 price api”`,
		`done | Added Manual Trigger “Start”`,
		`done | Added HTTP Request “Fetch Nifty”`,
		`error | Couldn't add HTTP Request “Dead API”`,
		`HTTP 404`,
		`done | Connected “Start” → “Fetch Nifty”`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("progress is missing %q; got:\n%s", want, joined)
		}
	}
	if len(sawCurrent) == 0 || !strings.Contains(strings.Join(sawCurrent, "\n"), "Searching the web for") {
		t.Fatalf("the in-flight step should be reported while it runs, saw: %v", sawCurrent)
	}
	if last.Current != "" {
		t.Fatalf("nothing should be in flight once the build returns, got %q", last.Current)
	}
}

// Review finding: when the final audit sent the model back to fix the graph
// and the time budget ran out on that extra round, the model's real summary
// -- credentials to add, x402 costs -- was replaced by "I ran out of time".
func TestBuildGraphKeepsTheRealReplyWhenTheAuditRoundRunsOutOfTime(t *testing.T) {
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn++
		w.Header().Set("Content-Type", "application/json")
		switch turn {
		case 1: // a lone trigger: the audit will flag it as unconnected
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"add_node","args":{"type":"trigger","template":"manual"}}}]}}]}`)
		case 2: // the real answer arrives late, using up the budget
			time.Sleep(180 * time.Millisecond)
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"REAL SUMMARY: add your Slack webhook."}]}}]}`)
		default:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"repaired"}]}}]}`)
		}
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	res, err := BuildGraph(context.Background(), BuildRequest{APIKey: "k", Message: "x", TimeBudget: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Reply, "REAL SUMMARY") {
		t.Fatalf("the model's own reply must survive a budget stop on the audit round, got: %q", res.Reply)
	}
}

// Review finding: a failed web_search showed Gemini's raw error body
// ("LLM API 429: {...}") in the chat's progress list.
func TestWebSearchFailureShowsNoRawUpstreamError(t *testing.T) {
	step := finishedStep(&models.WorkflowGraph{}, "web_search", map[string]any{"query": "q"},
		map[string]any{"error": `websearch: LLM API 429: {"error":{"code":429,"message":"Resource exhausted"}}`})
	if step.Status != "error" {
		t.Fatalf("want an error step, got %+v", step)
	}
	if strings.Contains(step.Detail, "LLM API") || strings.Contains(step.Detail, "{") {
		t.Fatalf("raw upstream error text must not reach the chat: %q", step.Detail)
	}
}

// A question-only turn changes nothing, so it must not spend a repair round
// on the user's own unfinished graph -- that round is where a parked node got
// removed without anyone asking.
func TestBuildGraphQuestionTurnLeavesTheUsersGraphAlone(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"It posts the summary."}]}}]}`)
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	graph := models.WorkflowGraph{Nodes: []models.WorkflowNode{
		{ID: "t1", Type: models.NodeTypeTrigger, Template: "manual"},
		{ID: "parked", Type: models.NodeTypeTool, Template: "json_extract"},
	}}
	res, err := BuildGraph(context.Background(), BuildRequest{APIKey: "k", Message: "what does the slack node send?", Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("a question needs one model call and no repair round, got %d", calls)
	}
	if len(res.Graph.Nodes) != 2 {
		t.Fatalf("the user's graph must come back untouched, got %+v", res.Graph.Nodes)
	}
}

// Review finding: fetched pages and search results reach the builder, and
// update_node could point a node that holds the user's credentials at another
// server while those credentials stayed in place.
func TestUpdateNodeRefusesToRedirectACredentialedNode(t *testing.T) {
	httpNode := func(secrets map[string]string) models.WorkflowNode {
		return models.WorkflowNode{ID: "h1", Type: models.NodeTypeTool, Template: "http", URL: "https://api.example.com/v1", Secrets: secrets}
	}
	graphqlNode := models.WorkflowNode{
		ID: "g1", Type: models.NodeTypeAction, Template: "graphql",
		Config:  map[string]string{"graphqlEndpoint": "https://api.example.com/graphql"},
		Secrets: map[string]string{"graphqlAuthHeader": "__enc__"},
	}
	cases := []struct {
		name    string
		node    models.WorkflowNode
		args    map[string]any
		refused bool
	}{
		{"url on a node with stored headers", httpNode(map[string]string{"httpHeadersJSON": "__enc__"}),
			map[string]any{"id": "h1", "fields": map[string]any{"url": "https://attacker.example/steal"}}, true},
		{"url on a node with no credentials", httpNode(nil),
			map[string]any{"id": "h1", "fields": map[string]any{"url": "https://other.example.com/v2"}}, false},
		{"same url again", httpNode(map[string]string{"httpHeadersJSON": "__enc__"}),
			map[string]any{"id": "h1", "fields": map[string]any{"url": "https://api.example.com/v1"}}, false},
		{"rename a credentialed node", httpNode(map[string]string{"httpHeadersJSON": "__enc__"}),
			map[string]any{"id": "h1", "name": "Fetch prices"}, false},
		{"graphql endpoint with an auth header", graphqlNode,
			map[string]any{"id": "g1", "config": map[string]any{"graphqlEndpoint": "https://attacker.example/graphql"}}, true},
		{"graphql query with an auth header", graphqlNode,
			map[string]any{"id": "g1", "config": map[string]any{"graphqlQuery": "{ viewer { login } }"}}, false},
		{"template change on a credentialed node", graphqlNode,
			map[string]any{"id": "g1", "template": "slack"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := c.node
			n.Config = maps.Clone(c.node.Config)
			graph := &models.WorkflowGraph{Nodes: []models.WorkflowNode{n}}
			_, err := applyGraphOp(graph, "update_node", c.args)
			if c.refused {
				if err == nil || !strings.Contains(err.Error(), "credentials") {
					t.Fatalf("want a refusal naming the credentials, got %v", err)
				}
				if got := graph.Nodes[0]; got.URL != c.node.URL || got.Template != c.node.Template ||
					got.Config["graphqlEndpoint"] != c.node.Config["graphqlEndpoint"] {
					t.Fatalf("a refused update must change nothing, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("want this update allowed, got %v", err)
			}
		})
	}
}

// Review finding: the tools edit the graph in place, and the graph BuildGraph
// was handed shared its edge array and every node's Config map with the
// caller's. remove_edge on the first edge rewrote the caller's edges to
// [e2 e2], and a settings update showed through too -- so BuildWorkflow's
// "did the graph change during the build" check compared the stored graph
// against one the build itself had edited, and refused to save.
func TestBuildGraphLeavesTheCallersGraphUntouched(t *testing.T) {
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn++
		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			io.WriteString(w, `{"candidates":[{"content":{"parts":[`+
				`{"functionCall":{"name":"remove_edge","args":{"id":"e1"}}},`+
				`{"functionCall":{"name":"update_node","args":{"id":"s1","config":{"slackChannel":"#new"}}}}`+
				`]}}]}`)
			return
		}
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"done"}]}}]}`)
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	caller := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "t1", Type: models.NodeTypeTrigger, Template: "manual"},
			{ID: "s1", Type: models.NodeTypeAction, Template: "slack", Config: map[string]string{"slackChannel": "#old"}},
			{ID: "end1", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "t1", To: "s1", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "e2", From: "s1", To: "end1", Kind: models.EdgeKindFlow, ToPort: "in"},
		},
	}
	res, err := BuildGraph(context.Background(), BuildRequest{APIKey: "k", Message: "unhook slack and use #new", Graph: caller})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Graph.Edges) != 1 || res.Graph.Edges[0].ID != "e2" || res.Graph.Nodes[1].Config["slackChannel"] != "#new" {
		t.Fatalf("the build's own result is wrong: %+v", res.Graph)
	}
	if caller.Edges[0].ID != "e1" || caller.Edges[1].ID != "e2" {
		t.Fatalf("the caller's edges were rewritten: %+v", caller.Edges)
	}
	if got := caller.Nodes[1].Config["slackChannel"]; got != "#old" {
		t.Fatalf("the caller's node settings were changed: slackChannel=%q", got)
	}
}

// wiredAgentGraph is a complete, runnable graph: trigger -> agent -> end with
// a provider on the agent. Nothing for the audit to flag.
func wiredAgentGraph() models.WorkflowGraph {
	return models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "t", Type: models.NodeTypeTrigger, Template: "manual", Name: "Start"},
			{ID: "a", Type: models.NodeTypeAgent, Template: "agent", Name: "Answer"},
			{ID: "p", Type: models.NodeTypeProvider, Template: "gemini", KeyMode: "platform"},
			{ID: "e", Type: models.NodeTypeEnd, Template: "done"},
		},
		Edges: []models.WorkflowEdge{
			{ID: "1", From: "t", To: "a", Kind: models.EdgeKindFlow, ToPort: "in"},
			{ID: "2", From: "p", To: "a", Kind: models.EdgeKindAttach, ToPort: "model"},
			{ID: "3", From: "a", To: "e", Kind: models.EdgeKindFlow, ToPort: "in"},
		},
	}
}

// scriptedGemini answers each builder request from a script and records
// every request body it received.
func scriptedGemini(t *testing.T, script []string) *[]string {
	t.Helper()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "application/json")
		i := len(bodies) - 1
		if i >= len(script) {
			i = len(script) - 1
		}
		io.WriteString(w, script[i])
	}))
	t.Cleanup(srv.Close)
	SetGeminiBaseURL(srv.URL)
	t.Cleanup(func() { SetGeminiBaseURL("https://generativelanguage.googleapis.com") })
	return &bodies
}

const (
	callUpdateAgent = `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"update_node","args":{"id":"a","fields":{"systemPrompt":"Report the MYRAD price."}}}}]}}]}`
	callTestRun     = `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"test_run","args":{}}}]}}]}`
)

func text(s string) string {
	return `{"candidates":[{"content":{"parts":[{"text":"` + s + `"}]}}]}`
}

// The user asked that the builder make "a workflow which does run and gives
// the desired answer". So it may not answer until the graph as it now stands
// has been test-run.
func TestBuildGraphMustTestTheWorkflowBeforeAnswering(t *testing.T) {
	bodies := scriptedGemini(t, []string{callUpdateAgent, text("done"), callTestRun, text("Tested: MYRAD is $0.00021.")})
	runs := 0
	res, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "myrad price", Graph: wiredAgentGraph(),
		TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) DryRunResult {
			runs++
			return DryRunResult{Answer: "MYRAD is $0.00021.", FinalOutput: "MYRAD is $0.00021."}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runs != 1 {
		t.Fatalf("want exactly one test run, got %d", runs)
	}
	if len(*bodies) < 3 || !strings.Contains((*bodies)[2], "run test_run") {
		t.Fatalf("an untested answer must be sent back with an instruction to test")
	}
	if res.Reply != "Tested: MYRAD is $0.00021." {
		t.Fatalf("want the reply given after testing, got %q", res.Reply)
	}
}

// The user's failure: a lookup returned {} and the workflow was declared done.
func TestBuildGraphSendsTheModelBackWhenTheTestProducesNothing(t *testing.T) {
	bodies := scriptedGemini(t, []string{callTestRun, text("done"), text("still done")})
	_, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "myrad price", Graph: wiredAgentGraph(),
		TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) DryRunResult {
			return DryRunResult{Empty: true, Steps: []DryRunStep{{Name: "CoinGecko Price", Status: "empty", Output: "{}"}}}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(*bodies, "\n")
	if !strings.Contains(joined, "did not produce a real answer") || !strings.Contains(joined, "CoinGecko Price") {
		t.Fatalf("an empty test result must be sent back naming the step that returned nothing")
	}
}

// The agent that answered {} with an invented $0.00123456 had copied an
// example value out of its own instructions.
func TestBuilderAgentsAreToldNeverToInventValues(t *testing.T) {
	graph := &models.WorkflowGraph{}
	if _, err := applyGraphOp(graph, "add_node", map[string]any{
		"type": "agent", "template": "agent",
		"fields": map[string]any{"systemPrompt": "Report the MYRAD price."},
	}); err != nil {
		t.Fatal(err)
	}
	sp := graph.Nodes[0].SystemPrompt
	if !strings.HasPrefix(sp, "Report the MYRAD price.") || !strings.Contains(sp, "Never guess") {
		t.Fatalf("the agent must keep its instructions and gain the no-guessing rule, got %q", sp)
	}
	// Updating the prompt again must not stack the rule twice.
	applyGraphOp(graph, "update_node", map[string]any{"id": graph.Nodes[0].ID, "fields": map[string]any{"systemPrompt": sp}})
	if strings.Count(graph.Nodes[0].SystemPrompt, "Never guess") != 1 {
		t.Fatalf("the rule must appear once, got %q", graph.Nodes[0].SystemPrompt)
	}
}

// A retest still wrote "For example, '... is $0.00001266 USD.'" into an
// agent's instructions -- the prompt rule alone was ignored, and that exact
// pattern is what an agent handed {} repeats as a real price.
func TestBuilderRejectsExampleValuesInAgentInstructions(t *testing.T) {
	for _, prompt := range []string{
		"Report the price. For example, 'The price is $0.00001266 USD.'",
		"Say the price, e.g. 0.00123456 USD",
		"Respond like: ₹45.20",
	} {
		graph := &models.WorkflowGraph{}
		_, err := applyGraphOp(graph, "add_node", map[string]any{
			"type": "agent", "template": "agent", "fields": map[string]any{"systemPrompt": prompt},
		})
		if err == nil || !strings.Contains(err.Error(), "example") {
			t.Errorf("instructions with an example value must be rejected: %q (err %v)", prompt, err)
		}
	}
	for _, ok := range []string{
		"State the coin's USD price in one sentence, under 30 words.",
		// A threshold is an instruction, not an example -- it must stay legal.
		"Only report it if the price is above $100.",
	} {
		graph := &models.WorkflowGraph{}
		if _, err := applyGraphOp(graph, "add_node", map[string]any{
			"type": "agent", "template": "agent", "fields": map[string]any{"systemPrompt": ok},
		}); err != nil {
			t.Fatalf("ordinary instructions must be accepted: %q: %v", ok, err)
		}
	}
}

// The same retest ended with the agent's "answer" being a JSON block, and the
// gate accepted it because it was not empty.
func TestBuildGraphSendsBackAnAnswerThatIsRawData(t *testing.T) {
	bodies := scriptedGemini(t, []string{callTestRun, text("done"), text("done again")})
	_, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "myrad price", Graph: wiredAgentGraph(),
		TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) DryRunResult {
			return DryRunResult{Answer: "```json\n{\"myrad\":{\"usd\":0.0002}}\n```", FinalOutput: "{}"}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.Join(*bodies, "\n"), "raw data") {
		t.Fatal("an answer that is just JSON must be sent back as not an answer")
	}
}

// The reply that said "Here's the test run output:" and then nothing. The
// user must always see what the test actually produced.
func TestBuildGraphReplyAlwaysIncludesTheTestedAnswer(t *testing.T) {
	scriptedGemini(t, []string{callTestRun, text("I built it. Here is the test run output:")})
	res, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "myrad price", Graph: wiredAgentGraph(),
		TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) DryRunResult {
			return DryRunResult{Answer: "MYRAD is trading at 0.00021 USD.", FinalOutput: "MYRAD is trading at 0.00021 USD."}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Reply, "MYRAD is trading at 0.00021 USD.") {
		t.Fatalf("the reply must include the tested answer, got %q", res.Reply)
	}
}

// Review finding: every turn was forced through a test run, even one that
// changed nothing -- "what does this node do?" really ran the user's agents
// and, on an unfinished graph, pushed the builder to rework it unasked.
func TestBuildGraphDoesNotTestAWorkflowItDidNotChange(t *testing.T) {
	bodies := scriptedGemini(t, []string{text("That agent writes the answer.")})
	runs := 0
	res, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "what does the agent do?", Graph: wiredAgentGraph(),
		TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) DryRunResult {
			runs++
			return DryRunResult{Failed: true, Error: "unfinished"}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runs != 0 || len(*bodies) != 1 {
		t.Fatalf("an unchanged workflow must not be test-run or sent back: runs=%d requests=%d", runs, len(*bodies))
	}
	if res.Reply != "That agent writes the answer." {
		t.Fatalf("want the model's answer as is, got %q", res.Reply)
	}
}

// Review finding: when the build stopped after the gate had sent back an
// untested "Done", that "Done" was what the user got, with no hint that
// nothing had been checked.
func TestBuildGraphSaysSoWhenItStopsBeforeTesting(t *testing.T) {
	// The model edits, claims done, and then keeps editing until the rounds
	// run out, never testing.
	scriptedGemini(t, []string{callUpdateAgent, text("Done, your workflow reports the MYRAD price."), callUpdateAgent})
	res, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "myrad price", Graph: wiredAgentGraph(),
		TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) DryRunResult {
			t.Fatal("the model never asked for a test run")
			return DryRunResult{}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Reply, "Done, your workflow reports the MYRAD price.") {
		t.Fatalf("the model's own reply should still be kept, got %q", res.Reply)
	}
	if !strings.Contains(res.Reply, "not been test-run") {
		t.Fatalf("the reply must say the workflow was not tested, got %q", res.Reply)
	}
}

// A test run that could not check part of the workflow (a paid or sending
// step feeds it) is not a failure to fix -- the builder must not be pushed to
// rework a correct workflow, and the user is told what went unchecked.
func TestBuildGraphDoesNotSendBackAnUnverifiableTest(t *testing.T) {
	bodies := scriptedGemini(t, []string{callUpdateAgent, callTestRun, text("Built it.")})
	res, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "paid quote", Graph: wiredAgentGraph(),
		TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) DryRunResult {
			return DryRunResult{Unverified: true, Steps: []DryRunStep{
				{Name: "Paid Quote", Status: "simulated", Reason: "a paid x402 call"},
				{Name: "Extract Price", Status: "unverified", Reason: "its input comes from a simulated step"},
			}}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*bodies) != 3 {
		t.Fatalf("an unverifiable test must not send the model back, got %d requests", len(*bodies))
	}
	if !strings.Contains(res.Reply, "Built it.") || !strings.Contains(res.Reply, "Extract Price") {
		t.Fatalf("the reply must say which step went unchecked, got %q", res.Reply)
	}
}

// Test runs execute real agents and fetches, so one build may not run them
// without limit however often the model asks.
func TestBuildGraphCapsTestRunsPerBuild(t *testing.T) {
	scriptedGemini(t, []string{callTestRun})
	runs := 0
	_, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "myrad price", Graph: wiredAgentGraph(),
		TestRun: func(ctx context.Context, g models.WorkflowGraph, input string) DryRunResult {
			runs++
			return DryRunResult{Answer: "MYRAD is $0.00021."}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runs != maxTestRuns {
		t.Fatalf("want test runs capped at %d, got %d", maxTestRuns, runs)
	}
}
