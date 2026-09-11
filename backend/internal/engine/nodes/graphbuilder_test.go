package nodes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	if n.Type != models.NodeTypeAgent || n.Template != "agent" || n.Name != "Support Agent" || n.SystemPrompt != "Be helpful" {
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

func TestApplyGraphOpAddNodeRespectsExplicitByokKeyMode(t *testing.T) {
	graph := &models.WorkflowGraph{}
	_, err := applyGraphOp(graph, "add_node", map[string]any{
		"type":     "provider",
		"template": "gemini",
		"name":     "Gemini Model",
		"fields":   map[string]any{"keyMode": "byok"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if graph.Nodes[0].KeyMode != "byok" {
		t.Fatalf("want explicit byok preserved, got %q", graph.Nodes[0].KeyMode)
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

	result, err := BuildGraph(context.Background(), "test-key", "add a chat trigger", models.WorkflowGraph{}, nil)
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

	res, err := BuildGraph(context.Background(), "test-key", "build something endless", models.WorkflowGraph{}, nil)
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

	res, err := BuildGraph(context.Background(), "k", "build a phone search agent", models.WorkflowGraph{}, nil)
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
		Type:     models.NodeTypeTool,
		Template: "http",
		Config:   map[string]string{"httpBodyTemplate": "old"},
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
	if graph.Nodes[0].Config["httpBodyTemplate"] != "old" {
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

func TestBuildSystemPromptListsEveryToolTemplate(t *testing.T) {
	// The prompt advertised 4 of the 12 templates executeTool implements, so
	// the builder could not reach the other 8. Asserted here because this is
	// a list that silently rots every time a template is added.
	//
	// Scoped to the "- tool:" line and matched as whole comma-separated
	// tokens, not with a bare strings.Contains over the whole prompt: "set",
	// "http", "template" and "xml" are all substrings of words the prompt
	// uses elsewhere ("settable", "httpBodyTemplate", "Template id"), so a
	// Contains check would pass for templates the list does not actually
	// name -- the precise failure this test exists to catch.
	var list string
	for _, line := range strings.Split(buildSystemPrompt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- tool:") {
			list = strings.TrimPrefix(strings.TrimSpace(line), "- tool:")
			break
		}
	}
	if list == "" {
		t.Fatal(`no "- tool:" line in the system prompt`)
	}
	// The line may end with prose after the template list; cut at the first "(".
	if i := strings.Index(list, "("); i >= 0 {
		list = list[:i]
	}
	named := map[string]bool{}
	for _, tok := range strings.Split(list, ",") {
		named[strings.TrimSpace(tok)] = true
	}
	for _, tmpl := range []string{
		"http", "calc", "set", "json_extract", "crypto", "datetime",
		"xml", "template", "html_extract", "markdown", "quickchart", "websearch",
	} {
		if !named[tmpl] {
			t.Errorf("the tool template list does not name %q (got %v)", tmpl, named)
		}
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
		ID:     "p1",
		Type:   models.NodeTypeProvider,
		APIKey: "__enc__",
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
	if _, err := BuildGraph(context.Background(), "k", "now add an email step", models.WorkflowGraph{}, history); err != nil {
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

	res, err := BuildGraph(context.Background(), "k", "hello", models.WorkflowGraph{}, nil)
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
	if _, err := BuildGraph(context.Background(), "k", "go", models.WorkflowGraph{}, history); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(body, "BAD-ROLE") {
		t.Fatal("a turn with an invalid role was replayed")
	}
	if !strings.Contains(body, "keep me") {
		t.Fatal("a valid turn was dropped")
	}
}
