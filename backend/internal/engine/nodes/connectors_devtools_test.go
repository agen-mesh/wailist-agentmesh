package nodes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestGitHubAction_CreatesIssue(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	nodes.SetGitHubAPIBaseForTest(srv.URL)
	defer nodes.SetGitHubAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "gh1", Type: models.NodeTypeAction, Template: "github",
		Secrets: map[string]string{"githubToken": "ghp_xxx"},
		Config:  map[string]string{"githubRepo": "acme/widgets"},
	}
	rc := engine.NewRunContext("r1", []byte(`"build failed on main"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "github_issue_created" {
		t.Errorf("want 'github_issue_created', got %v", result)
	}
	if gotPath != "/repos/acme/widgets/issues" {
		t.Errorf("want issues path, got %q", gotPath)
	}
	if gotAuth != "Bearer ghp_xxx" {
		t.Errorf("want bearer auth, got %q", gotAuth)
	}
	if gotBody["title"] != "build failed on main" {
		t.Errorf("want title from message, got %v", gotBody)
	}
}

func TestGitHubAction_SkipsWhenNoToken(t *testing.T) {
	node := models.WorkflowNode{
		ID: "gh2", Type: models.NodeTypeAction, Template: "github",
		Config: map[string]string{"githubRepo": "acme/widgets"},
	}
	rc := engine.NewRunContext("r1", []byte(`"build failed on main"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "github_skipped_no_token" {
		t.Errorf("want 'github_skipped_no_token', got %v", result)
	}
}

func TestGitHubAction_SkipsWhenNoRepo(t *testing.T) {
	node := models.WorkflowNode{
		ID: "gh3", Type: models.NodeTypeAction, Template: "github",
		Secrets: map[string]string{"githubToken": "ghp_xxx"},
	}
	rc := engine.NewRunContext("r1", []byte(`"build failed on main"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "github_skipped_no_repo" {
		t.Errorf("want 'github_skipped_no_repo', got %v", result)
	}
}
