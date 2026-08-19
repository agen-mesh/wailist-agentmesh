package nodes_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestPostJSON_SendsAuthHeaderAndBody(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result, err := nodes.PostJSONForTest(context.Background(), srv.URL, map[string]string{"Authorization": "Bearer tok"}, map[string]any{"text": "hi"}, "sent", "TestSvc")
	if err != nil {
		t.Fatal(err)
	}
	if result != "sent" {
		t.Errorf("want sentinel 'sent', got %v", result)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("want Authorization header, got %q", gotAuth)
	}
	if gotBody["text"] != "hi" {
		t.Errorf("want body text=hi, got %v", gotBody)
	}
}

func TestPostJSON_ErrorStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	_, err := nodes.PostJSONForTest(context.Background(), srv.URL, nil, map[string]any{}, "sent", "TestSvc")
	if err == nil {
		t.Fatal("want error for 400 response")
	}
}

func TestIssueTitleForTest_FirstLineCapped(t *testing.T) {
	got := nodes.IssueTitleForTest("first line\nsecond line")
	if got != "first line" {
		t.Errorf("want 'first line', got %q", got)
	}
	empty := nodes.IssueTitleForTest("   \n rest")
	if empty != "AgentMesh workflow result" {
		t.Errorf("want fallback title for blank first line, got %q", empty)
	}
}

func TestReadBoundedForTest_PassesUnderLimit(t *testing.T) {
	got, err := nodes.ReadBoundedForTest(strings.NewReader("hi"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi" {
		t.Errorf("want 'hi', got %q", got)
	}
}

func TestPostJSON_RejectsBlockedURL(t *testing.T) {
	blockErr := errors.New("requests to private/internal addresses are not allowed")
	nodes.SetURLValidatorForTest(func(string) error { return blockErr })
	// Restore the package-wide permissive validator (set once in TestMain)
	// rather than passing nil, which would flip global state to the real
	// strict validator for every test that runs after this one in the binary.
	defer nodes.SetURLValidatorForTest(func(string) error { return nil })

	_, err := nodes.PostJSONForTest(context.Background(), "http://127.0.0.1:1/x", nil, map[string]any{}, "sent", "TestSvc")
	if err == nil {
		t.Fatal("want error when urlValidator rejects the target, got nil")
	}
	if !strings.Contains(err.Error(), "private/internal addresses") {
		t.Errorf("want validator error to propagate, got %v", err)
	}
}

func TestReadBoundedForTest_ErrorsOverLimit(t *testing.T) {
	_, err := nodes.ReadBoundedForTest(strings.NewReader("hello world"), 5)
	if err == nil {
		t.Fatal("want error when reader exceeds limit")
	}
}

func TestGetAndDecodeReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("want auth header forwarded, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"id":1}]}`))
	}))
	defer srv.Close()

	got, err := nodes.GetAndDecodeForTest(context.Background(), srv.URL,
		map[string]string{"Authorization": "Bearer tok"}, "Test")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("want a decoded map, got %T", got)
	}
	if _, ok := m["items"]; !ok {
		t.Errorf("want the decoded body, got %#v", m)
	}
}

func TestGetAndDecodeSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"nope"}`))
	}))
	defer srv.Close()

	if _, err := nodes.GetAndDecodeForTest(context.Background(), srv.URL, nil, "Test"); err == nil {
		t.Error("want an error for a 403, got nil")
	}
}

func TestGetAndDecodeRejectsSSRFTarget(t *testing.T) {
	if _, err := nodes.GetAndDecodeForTest(context.Background(), "http://169.254.169.254/latest/meta-data/", nil, "Test"); err == nil {
		t.Error("want the SSRF guard to reject link-local metadata, got nil")
	}
}

func TestGetJSON_DecodesSuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"items":[1,2,3]}`))
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	result, err := nodes.GetJSONForTest(req, "TestSvc")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]any)
	if !ok || m["ok"] != true {
		t.Fatalf("want decoded map with ok=true, got %v", result)
	}
	items, ok := m["items"].([]any)
	if !ok || len(items) != 3 {
		t.Errorf("want 3 decoded items, got %v", m["items"])
	}
}

func TestGetJSON_ErrorStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := nodes.GetJSONForTest(req, "TestSvc")
	if err == nil {
		t.Fatal("want error for 403 response")
	}
}

func TestGetJSON_InvalidJSONReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := nodes.GetJSONForTest(req, "TestSvc")
	if err == nil {
		t.Fatal("want error for non-JSON body")
	}
}

// ── message templating ("{{ result }}" / "{{ result.field }}") ────────────

func TestResolveTemplate_BareResultReturnsWholeMessage(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"hello world"`))
	got := nodes.ResolveTemplateForTest("{{ result }}", rc)
	if got != "hello world" {
		t.Errorf("want whole message, got %q", got)
	}
}

func TestResolveTemplate_DottedPathExtractsField(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", map[string]any{
		"extract": "Algorand is a proof-of-stake blockchain.",
		"title":   "Algorand",
	})
	got := nodes.ResolveTemplateForTest("{{ result.extract }}", rc)
	if got != "Algorand is a proof-of-stake blockchain." {
		t.Errorf("want extracted field, got %q", got)
	}
}

func TestResolveTemplate_NestedDottedPath(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", map[string]any{
		"content_urls": map[string]any{
			"desktop": map[string]any{"page": "https://en.wikipedia.org/wiki/Algorand"},
		},
	})
	got := nodes.ResolveTemplateForTest("{{ result.content_urls.desktop.page }}", rc)
	if got != "https://en.wikipedia.org/wiki/Algorand" {
		t.Errorf("want nested field, got %q", got)
	}
}

func TestResolveTemplate_MissingFieldExpandsToEmpty(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", map[string]any{"extract": "hi"})
	got := nodes.ResolveTemplateForTest("[{{ result.nonexistent }}]", rc)
	if got != "[]" {
		t.Errorf("want empty expansion for missing field, got %q", got)
	}
}

func TestResolveTemplate_NonObjectOutputWithPathExpandsToEmpty(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"just a string"`))
	got := nodes.ResolveTemplateForTest("[{{ result.field }}]", rc)
	if got != "[]" {
		t.Errorf("want empty expansion when output isn't an object, got %q", got)
	}
}

func TestResolveTemplate_LiteralTextAndMultiplePlaceholdersPreserved(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", map[string]any{"title": "Algorand", "extract": "A blockchain."})
	got := nodes.ResolveTemplateForTest("New article: {{ result.title }}\n\n{{ result.extract }}", rc)
	want := "New article: Algorand\n\nA blockchain."
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestResolveMessage_EmptyTemplateFallsBackToRawMessage(t *testing.T) {
	node := models.WorkflowNode{ID: "n1", Type: models.NodeTypeAction, Template: "slack"}
	rc := engine.NewRunContext("r1", []byte(`"unchanged"`))
	got := nodes.ResolveMessageForTest(node, rc)
	if got != "unchanged" {
		t.Errorf("want rc.Message() verbatim when no template is configured, got %q", got)
	}
}

func TestResolveMessage_UsesConfiguredTemplate(t *testing.T) {
	node := models.WorkflowNode{
		ID: "n1", Type: models.NodeTypeAction, Template: "slack",
		Config: map[string]string{"messageTemplate": "Summary: {{ result.extract }}"},
	}
	rc := engine.NewRunContext("r1", nil)
	rc.Set("h1", map[string]any{"extract": "Algorand is fast."})
	got := nodes.ResolveMessageForTest(node, rc)
	if got != "Summary: Algorand is fast." {
		t.Errorf("want templated message, got %q", got)
	}
}
