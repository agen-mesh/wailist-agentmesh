package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

// buildWorkflowReq issues POST /workflows/{id}/build as userID.
func buildWorkflowReq(d *handlers.Deps, workflowID, userID, message string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"message": message})
	req := withURLParam(httptest.NewRequest(http.MethodPost, "/workflows/"+workflowID+"/build", bytes.NewReader(body)), "id", workflowID)
	d.BuildWorkflow(rec, withUser(req, userID))
	return rec
}

func TestBuildWorkflowAddsNode(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()

	user, err := d.Store.CreateUser(ctx, "wf-build-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Build Me", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	// Mock Gemini: first response is a functionCall for add_node (driving the
	// meta-agent's tool-calling loop), second is the final text reply -- the
	// same two-step shape as TestBuildGraphAddsNodeThenReturnsReply in
	// engine/nodes/graphbuilder_test.go.
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
	nodes.SetGeminiBaseURL(srv.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	rec := buildWorkflowReq(d, wf.ID, user.ID, "add a chat trigger")
	if rec.Code != http.StatusOK {
		t.Fatalf("build got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Reply    string `json:"reply"`
		Workflow struct {
			Nodes []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Template string `json:"template"`
				Name     string `json:"name"`
			} `json:"nodes"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Reply != "Added a chat trigger node." {
		t.Fatalf("unexpected reply: %q", out.Reply)
	}
	if len(out.Workflow.Nodes) != 1 {
		t.Fatalf("expected 1 node in response.workflow.nodes, got %d: %+v", len(out.Workflow.Nodes), out.Workflow.Nodes)
	}
	if got := out.Workflow.Nodes[0]; got.Type != "trigger" || got.Template != "chat" {
		t.Fatalf("expected added trigger/chat node, got type=%q template=%q", got.Type, got.Template)
	}
}

// TestBuildWorkflowPreservesUnrelatedSecret verifies the security-critical
// round trip the handler relies on: an existing node's real API key must
// never be sent to the meta-agent, and an untouched node's encrypted DB
// value must survive a build-mode edit byte-for-byte even when that edit
// only adds an unrelated node elsewhere in the graph.
func TestBuildWorkflowPreservesUnrelatedSecret(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()

	user, err := d.Store.CreateUser(ctx, "wf-build-secret-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Secret Build", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	// Seed a node with a real API key through the same save path
	// UpdateWorkflow uses, mirroring TestAPIKeyEncryption's setup style.
	seedBody, _ := json.Marshal(map[string]any{
		"name": "Secret Build",
		"nodes": []map[string]any{
			{"id": "n1", "type": "provider", "template": "gemini", "apiKey": "my-real-secret-key-456"},
		},
		"edges": []any{},
	})
	seedReq := withURLParam(httptest.NewRequest(http.MethodPut, "/workflows/"+wf.ID, bytes.NewReader(seedBody)), "id", wf.ID)
	seedRec := httptest.NewRecorder()
	d.UpdateWorkflow(seedRec, withUser(seedReq, user.ID))
	if seedRec.Code != http.StatusOK {
		t.Fatalf("seed: want 200 got %d (body: %s)", seedRec.Code, seedRec.Body.String())
	}

	rawBefore, err := d.Store.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	var encryptedBefore string
	for _, n := range rawBefore.Nodes {
		if n.ID == "n1" {
			encryptedBefore = n.APIKey
		}
	}
	if !strings.HasPrefix(encryptedBefore, "enc:") {
		t.Fatalf("seed: want encrypted blob (enc: prefix), got %q", encryptedBefore)
	}

	// Mock Gemini adds an unrelated trigger node without ever referencing n1.
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
					{"text": "Added an unrelated trigger node."},
				}}},
			},
		})
	}))
	defer srv.Close()
	nodes.SetGeminiBaseURL(srv.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	rec := buildWorkflowReq(d, wf.ID, user.ID, "add an unrelated trigger")
	if rec.Code != http.StatusOK {
		t.Fatalf("build got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var out struct {
		Workflow struct {
			Nodes []struct {
				ID     string `json:"id"`
				Type   string `json:"type"`
				APIKey string `json:"apiKey"`
			} `json:"nodes"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	// (a) The HTTP response must never contain the plaintext or raw
	// ciphertext for n1's API key -- only the mask sentinel.
	found := false
	for _, n := range out.Workflow.Nodes {
		if n.ID != "n1" {
			continue
		}
		found = true
		if n.APIKey != handlers.EncSentinel {
			t.Errorf("response: n1 apiKey want sentinel %q, got %q", handlers.EncSentinel, n.APIKey)
		}
	}
	if !found {
		t.Fatal("n1 missing from build response -- unrelated edit should not have removed it")
	}

	// (b) The DB's encrypted blob for n1 must be byte-for-byte unchanged --
	// proving the round trip didn't corrupt or blank the real secret.
	rawAfter, err := d.Store.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	var encryptedAfter string
	for _, n := range rawAfter.Nodes {
		if n.ID == "n1" {
			encryptedAfter = n.APIKey
		}
	}
	if encryptedAfter != encryptedBefore {
		t.Fatalf("secret round trip: DB blob changed; was %q, now %q", encryptedBefore, encryptedAfter)
	}
}

func TestBuildWorkflowRequiresPlatformKey(t *testing.T) {
	d := testDeps(t)
	ctx := context.Background()

	user, err := d.Store.CreateUser(ctx, "wf-build-nokey-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "No Key", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	rec := buildWorkflowReq(d, wf.ID, user.ID, "add a trigger")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestBuildWorkflowOtherUserGets404(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()

	owner, err := d.Store.CreateUser(ctx, "wf-build-owner-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	other, err := d.Store.CreateUser(ctx, "wf-build-other-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Not Yours", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	rec := buildWorkflowReq(d, wf.ID, other.ID, "add a trigger")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other user got %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestBuildWorkflowRecordsBothTurnsAndReplaysThem(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()

	user, err := d.Store.CreateUser(ctx, "wf-hist-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Remember Me", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	// Always answer with plain text: this test is about what goes INTO the
	// request and what lands in the history, not about tool calling.
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{
					{"text": "Noted."},
				}}},
			},
		})
	}))
	defer srv.Close()
	nodes.SetGeminiBaseURL(srv.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	if rec := buildWorkflowReq(d, wf.ID, user.ID, "the phone must have 16GB RAM"); rec.Code != http.StatusOK {
		t.Fatalf("first build got %d (body: %s)", rec.Code, rec.Body.String())
	}

	msgs, err := d.Store.GetBuildMessages(ctx, wf.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want the user turn and the model turn recorded, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Text != "the phone must have 16GB RAM" {
		t.Fatalf("user turn wrong: %+v", msgs[0])
	}
	if msgs[1].Role != "model" || msgs[1].Text != "Noted." {
		t.Fatalf("model turn wrong: %+v", msgs[1])
	}

	// The second turn must carry the first one into the request -- this is
	// the whole point of the history, and it is the assertion that would
	// catch the handler loading history but forgetting to pass it on.
	if rec := buildWorkflowReq(d, wf.ID, user.ID, "now add an email step"); rec.Code != http.StatusOK {
		t.Fatalf("second build got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(lastBody, "16GB RAM") {
		t.Fatal("the first turn was not replayed into the second request")
	}
	if !strings.Contains(lastBody, "now add an email step") {
		t.Fatal("the current message is missing from the second request")
	}
}

// A build that fails must leave the history untouched. Recording the user's
// message up front would leave an unanswered question behind, and the next
// build would replay it with no reply after it.
func TestBuildWorkflowFailedBuildRecordsNothing(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()

	user, err := d.Store.CreateUser(ctx, "wf-histfail-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Fails", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream exploded", http.StatusInternalServerError)
	}))
	defer srv.Close()
	nodes.SetGeminiBaseURL(srv.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	if rec := buildWorkflowReq(d, wf.ID, user.ID, "this will fail"); rec.Code == http.StatusOK {
		t.Fatalf("expected the build to fail, got 200")
	}
	msgs, err := d.Store.GetBuildMessages(ctx, wf.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("a failed build must record nothing, got %d turns: %+v", len(msgs), msgs)
	}
}

// BuildWorkflow must hand the builder the real Bazaar catalog. Every nodes-
// level x402 test injects its own loader, so a handler that forgot to wire
// d.catalog would pass all of them while search_x402 failed on every real
// build. This drives the handler end to end: a fake Bazaar upstream, a fake
// model that searches and adds, and a saved workflow checked for the node.
func TestBuildWorkflowSearchesTheBazaarAndSavesTheX402Node(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()

	bazaarSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := []map[string]any{}
		if r.URL.Query().Get("offset") == "0" {
			items = append(items, map[string]any{
				"id":          "idx-feed",
				"resourceUrl": "https://indexfeed.example/v1/quote",
				"method":      "GET",
				"description": "Live stock index quotes",
				"accepts": []any{map[string]any{
					"network": "algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=",
					"amount":  "5000", "asset": "31566704", "payTo": "P",
				}},
				"settleCount": 12,
			})
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer bazaarSrv.Close()
	d.BazaarBaseURL = bazaarSrv.URL

	user, err := d.Store.CreateUser(ctx, "wf-x402-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Index feed", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	idRe := regexp.MustCompile(`\\"id\\":\\"([^\\"]+)\\"`)
	turn := 0
	geminiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		turn++
		w.Header().Set("Content-Type", "application/json")
		switch turn {
		case 1:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"search_x402","args":{"query":"stock index quotes"}}}]}}]}`)
		case 2:
			m := idRe.FindSubmatch(body)
			if m == nil {
				t.Errorf("search_x402 returned no result to the model: %s", body)
				io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"nothing found"}]}}]}`)
				return
			}
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"add_x402_node","args":{"id":"`+string(m[1])+`"}}}]}}]}`)
		default:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"Added the index feed."}]}}]}`)
		}
	}))
	defer geminiSrv.Close()
	nodes.SetGeminiBaseURL(geminiSrv.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	if rec := buildWorkflowReq(d, wf.ID, user.ID, "fetch live index quotes"); rec.Code != http.StatusOK {
		t.Fatalf("build got %d (body: %s)", rec.Code, rec.Body.String())
	}
	saved, err := d.Store.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range saved.Nodes {
		if n.Type == "tool402" && n.Endpoint == "https://indexfeed.example/v1/quote" && n.Price == "0.005" {
			return
		}
	}
	t.Fatalf("the saved workflow has no tool402 node for the catalog endpoint: %+v", saved.Nodes)
}

// buildProgressReq issues GET /workflows/{id}/build/progress as userID.
func buildProgressReq(d *handlers.Deps, workflowID, userID, buildID string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodGet, "/workflows/"+workflowID+"/build/progress?buildId="+buildID, nil), "id", workflowID)
	d.BuildWorkflowProgress(rec, withUser(req, userID))
	return rec
}

// The chat polls this while a build runs, to show each step as it happens
// instead of a bare spinner.
func TestBuildWorkflowProgressIsPollableByItsOwnerOnly(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()

	user, err := d.Store.CreateUser(ctx, "wf-prog-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	other, err := d.Store.CreateUser(ctx, "wf-prog-other-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Progress", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn++
		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"add_node","args":{"type":"trigger","template":"manual","name":"Start"}}}]}}]}`)
			return
		}
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"Added a trigger."}]}}]}`)
	}))
	defer srv.Close()
	nodes.SetGeminiBaseURL(srv.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	buildID := "build-" + strings.ReplaceAll(randSuffix(t), ".", "") // the id format allows no dots
	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"message": "add a trigger", "buildId": buildID})
	req := withURLParam(httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/build", bytes.NewReader(body)), "id", wf.ID)
	d.BuildWorkflow(rec, withUser(req, user.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("build got %d: %s", rec.Code, rec.Body.String())
	}

	var got struct {
		Steps []struct {
			Label  string `json:"label"`
			Status string `json:"status"`
		} `json:"steps"`
		Done bool `json:"done"`
	}
	pr := buildProgressReq(d, wf.ID, user.ID, buildID)
	if pr.Code != http.StatusOK {
		t.Fatalf("progress got %d: %s", pr.Code, pr.Body.String())
	}
	json.Unmarshal(pr.Body.Bytes(), &got)
	if !got.Done {
		t.Fatal("a finished build must report done")
	}
	found := false
	for _, s := range got.Steps {
		if strings.Contains(s.Label, "Added Manual Trigger “Start”") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the owner should see the build's steps, got %+v", got.Steps)
	}

	// Someone else polling the same build id sees nothing.
	var theirs struct {
		Steps []any `json:"steps"`
		Done  bool  `json:"done"`
	}
	json.Unmarshal(buildProgressReq(d, wf.ID, other.ID, buildID).Body.Bytes(), &theirs)
	if len(theirs.Steps) != 0 || theirs.Done {
		t.Fatalf("another user must not see this build's progress, got %+v", theirs)
	}

	if bad := buildProgressReq(d, wf.ID, user.ID, "../../etc"); bad.Code != http.StatusBadRequest {
		t.Fatalf("a malformed build id must be rejected, got %d", bad.Code)
	}
}

// Review finding: the build runs detached from the request and used to save
// the graph it read at the start, overwriting any edit made meanwhile (a
// reload and an edit, another tab, a second build). It now refuses to save
// over a workflow that changed while it was building.
func TestBuildWorkflowDoesNotOverwriteAnEditMadeDuringTheBuild(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()

	user, err := d.Store.CreateUser(ctx, "wf-race-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Race", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn++
		if turn == 1 {
			// The user edits the canvas while the builder is mid-build.
			edit := models.WorkflowGraph{Nodes: []models.WorkflowNode{{ID: "user-node", Type: models.NodeTypeTrigger, Template: "manual", Name: "Mine"}}}
			if _, err := d.Store.UpdateWorkflow(context.Background(), wf.ID, "Race", edit); err != nil {
				t.Errorf("simulated edit failed: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"add_node","args":{"type":"trigger","template":"chat","name":"Builder's"}}}]}}]}`)
			return
		}
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"done"}]}}]}`)
	}))
	defer srv.Close()
	nodes.SetGeminiBaseURL(srv.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	rec := buildWorkflowReq(d, wf.ID, user.ID, "add a chat trigger")
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409 when the workflow changed mid-build, got %d: %s", rec.Code, rec.Body.String())
	}
	saved, err := d.Store.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Nodes) != 1 || saved.Nodes[0].ID != "user-node" {
		t.Fatalf("the user's edit must survive, got %+v", saved.Nodes)
	}
	if msgs, _ := d.Store.GetBuildMessages(ctx, wf.ID, 20); len(msgs) != 0 {
		t.Fatalf("a build that was not saved must not be remembered, got %d turns", len(msgs))
	}
}

// Review finding: the conflict check compared updated_at, which a deploy, a
// rename and a dragged node all bump without touching anything the builder
// edited -- so clicking "Deploy now" mid-build threw the whole build away.
// Only a change to the graph itself is a conflict; a node dragged meanwhile
// keeps the position it was dragged to.
func TestBuildWorkflowSavesOverADeployAndADragDuringTheBuild(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()

	user, err := d.Store.CreateUser(ctx, "wf-deploy-race-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Deploy Race", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })
	start := models.WorkflowGraph{Nodes: []models.WorkflowNode{{ID: "user-node", Type: models.NodeTypeTrigger, Template: "manual", Name: "Mine", X: 10, Y: 10}}}
	if _, err := d.Store.UpdateWorkflow(ctx, wf.ID, "Deploy Race", start); err != nil {
		t.Fatal(err)
	}

	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn++
		if turn == 1 {
			// Mid-build: the user drags their node, then deploys.
			moved := models.WorkflowGraph{Nodes: []models.WorkflowNode{{ID: "user-node", Type: models.NodeTypeTrigger, Template: "manual", Name: "Mine", X: 500, Y: 300}}}
			if _, err := d.Store.UpdateWorkflow(context.Background(), wf.ID, "Deploy Race", moved); err != nil {
				t.Errorf("simulated drag failed: %v", err)
			}
			if err := d.Store.SetWorkflowDeployed(context.Background(), wf.ID, "/run/"+wf.ID, time.Now()); err != nil {
				t.Errorf("simulated deploy failed: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"update_node","args":{"id":"user-node","name":"Renamed by builder"}}}]}}]}`)
			return
		}
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"done"}]}}]}`)
	}))
	defer srv.Close()
	nodes.SetGeminiBaseURL(srv.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	rec := buildWorkflowReq(d, wf.ID, user.ID, "rename my trigger")
	if rec.Code != http.StatusOK {
		t.Fatalf("a deploy or a drag mid-build is not a conflict, got %d: %s", rec.Code, rec.Body.String())
	}
	saved, err := d.Store.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Nodes) != 1 || saved.Nodes[0].Name != "Renamed by builder" {
		t.Fatalf("the build must be saved, got %+v", saved.Nodes)
	}
	if n := saved.Nodes[0]; n.X != 500 || n.Y != 300 {
		t.Fatalf("the drag made during the build must be kept, got x=%v y=%v", n.X, n.Y)
	}
}

// Review finding: the conflict check compared against the graph the build
// started from, but the builder shared that graph's edges and settings maps
// and edited them in place -- so removing a connection or changing a setting
// made an untouched workflow look changed, and the build got a 409 instead
// of being saved.
func TestBuildWorkflowSavesAnEdgeRemovalAndASettingsChange(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()

	user, err := d.Store.CreateUser(ctx, "wf-inplace-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "In Place", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })
	start := models.WorkflowGraph{
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
	if _, err := d.Store.UpdateWorkflow(ctx, wf.ID, "In Place", start); err != nil {
		t.Fatal(err)
	}

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
	nodes.SetGeminiBaseURL(srv.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	rec := buildWorkflowReq(d, wf.ID, user.ID, "unhook slack and use #new")
	if rec.Code != http.StatusOK {
		t.Fatalf("nobody else touched the workflow, want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	saved, err := d.Store.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Edges) != 1 || saved.Edges[0].ID != "e2" {
		t.Fatalf("want only e2 left, got %+v", saved.Edges)
	}
	if got := saved.Nodes[1].Config["slackChannel"]; got != "#new" {
		t.Fatalf("want the new channel saved, got %q", got)
	}
}

// The builder edits a redacted graph whose credentials are the "__enc__"
// sentinel. Its test run must still call the user's step with the real,
// decrypted credential -- a test with the placeholder would fail every
// authenticated step and teach the builder that a working workflow is broken.
func TestBuildWorkflowTestRunUsesTheRealCredential(t *testing.T) {
	d := testDeps(t)
	d.PlatformGeminiAPIKey = "test-key"
	ctx := context.Background()
	// Restore the permissive validator, not nil: nil switches every later
	// test in the package back to production dialing of 127.0.0.1 servers.
	nodes.SetURLValidatorForTest(func(string) error { return nil })
	defer nodes.SetURLValidatorForTest(func(string) error { return nil })

	var gotKey string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"price":1.5}`)
	}))
	defer target.Close()

	user, err := d.Store.CreateUser(ctx, "wf-dry-"+randSuffix(t)+"@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	wf, err := d.Store.CreateWorkflow(ctx, "Dry", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	// Saved through the normal handler, so the header is encrypted at rest.
	body, _ := json.Marshal(map[string]any{
		"name": "Dry",
		"nodes": []map[string]any{
			{"id": "t", "type": "trigger", "template": "manual"},
			{"id": "h", "type": "tool", "template": "http", "url": target.URL, "method": "GET",
				"secrets": map[string]string{"httpHeadersJSON": `{"X-Api-Key":"real-secret"}`}},
			{"id": "e", "type": "end", "template": "done"},
		},
		"edges": []map[string]any{
			{"id": "1", "from": "t", "to": "h", "kind": "flow", "toPort": "in"},
			{"id": "2", "from": "h", "to": "e", "kind": "flow", "toPort": "in"},
		},
	})
	up := httptest.NewRecorder()
	d.UpdateWorkflow(up, withUser(withURLParam(httptest.NewRequest(http.MethodPut, "/workflows/"+wf.ID, bytes.NewReader(body)), "id", wf.ID), user.ID))
	if up.Code != http.StatusOK {
		t.Fatalf("seeding the workflow got %d: %s", up.Code, up.Body.String())
	}

	turn := 0
	gem := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn++
		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"test_run","args":{}}}]}}]}`)
			return
		}
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"tested"}]}}]}`)
	}))
	defer gem.Close()
	nodes.SetGeminiBaseURL(gem.URL)
	defer nodes.SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	if rec := buildWorkflowReq(d, wf.ID, user.ID, "test it"); rec.Code != http.StatusOK {
		t.Fatalf("build got %d: %s", rec.Code, rec.Body.String())
	}
	if gotKey != "real-secret" {
		t.Fatalf("the test run must send the real decrypted credential, target received %q", gotKey)
	}
}
