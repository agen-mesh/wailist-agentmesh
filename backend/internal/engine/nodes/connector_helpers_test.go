package nodes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/engine/nodes"
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

func TestReadBoundedForTest_ErrorsOverLimit(t *testing.T) {
	_, err := nodes.ReadBoundedForTest(strings.NewReader("hello world"), 5)
	if err == nil {
		t.Fatal("want error when reader exceeds limit")
	}
}
