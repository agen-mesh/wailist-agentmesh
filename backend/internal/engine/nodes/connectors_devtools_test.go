package nodes_test

import (
	"context"
	"encoding/base64"
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

func TestJiraAction_CreatesIssue(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	nodes.SetJiraAPIBaseForTest(srv.URL)
	defer nodes.SetJiraAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "jr1", Type: models.NodeTypeAction, Template: "jira",
		Secrets: map[string]string{"jiraAPIToken": "tok_xxx"},
		Config: map[string]string{
			"jiraEmail": "bot@acme.com", "jiraDomain": "acme", "jiraProjectKey": "ENG",
		},
	}
	rc := engine.NewRunContext("r1", []byte(`"deploy failed"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "jira_issue_created" {
		t.Errorf("want 'jira_issue_created', got %v", result)
	}
	if gotPath != "/rest/api/3/issue" {
		t.Errorf("want issue path, got %q", gotPath)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("bot@acme.com:tok_xxx"))
	if gotAuth != wantAuth {
		t.Errorf("want basic auth %q, got %q", wantAuth, gotAuth)
	}
	fields, _ := gotBody["fields"].(map[string]any)
	if fields["summary"] != "deploy failed" {
		t.Errorf("want summary from message, got %v", fields)
	}
	project, _ := fields["project"].(map[string]any)
	if project["key"] != "ENG" {
		t.Errorf("want project key ENG, got %v", project)
	}
}

func TestJiraAction_SkipsWhenNoAPIToken(t *testing.T) {
	node := models.WorkflowNode{
		ID: "jr2", Type: models.NodeTypeAction, Template: "jira",
		Config: map[string]string{
			"jiraEmail": "bot@acme.com", "jiraDomain": "acme", "jiraProjectKey": "ENG",
		},
	}
	rc := engine.NewRunContext("r1", []byte(`"deploy failed"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "jira_skipped_no_api_token" {
		t.Errorf("want 'jira_skipped_no_api_token', got %v", result)
	}
}

func TestJiraAction_SkipsWhenMissingConfig(t *testing.T) {
	node := models.WorkflowNode{
		ID: "jr3", Type: models.NodeTypeAction, Template: "jira",
		Secrets: map[string]string{"jiraAPIToken": "tok_xxx"},
		Config:  map[string]string{"jiraEmail": "bot@acme.com", "jiraDomain": "acme"},
	}
	rc := engine.NewRunContext("r1", []byte(`"deploy failed"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "jira_skipped_missing_config" {
		t.Errorf("want 'jira_skipped_missing_config', got %v", result)
	}
}

func TestJiraAction_SkipsWhenDomainInvalid(t *testing.T) {
	requestReceived := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	nodes.SetJiraAPIBaseForTest(srv.URL)
	defer nodes.SetJiraAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "jr4", Type: models.NodeTypeAction, Template: "jira",
		Secrets: map[string]string{"jiraAPIToken": "tok_xxx"},
		Config: map[string]string{
			"jiraEmail": "bot@acme.com", "jiraDomain": "evil.com#", "jiraProjectKey": "ENG",
		},
	}
	rc := engine.NewRunContext("r1", []byte(`"deploy failed"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "jira_skipped_invalid_domain" {
		t.Errorf("want 'jira_skipped_invalid_domain', got %v", result)
	}
	if requestReceived {
		t.Error("expected no HTTP request to be dispatched for an invalid domain")
	}
}
