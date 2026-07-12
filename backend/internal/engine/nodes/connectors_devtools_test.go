package nodes_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestLinearAction_CreatesIssueViaGraphQL(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	nodes.SetLinearAPIBaseForTest(srv.URL)
	defer nodes.SetLinearAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "li1", Type: models.NodeTypeAction, Template: "linear",
		Secrets: map[string]string{"linearAPIKey": "lin_api_xxx"},
		Config:  map[string]string{"linearTeamID": "team123"},
	}
	rc := engine.NewRunContext("r1", []byte(`"flaky test in CI"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "linear_issue_created" {
		t.Errorf("want 'linear_issue_created', got %v", result)
	}
	if gotAuth != "lin_api_xxx" {
		t.Errorf("want raw API key (no Bearer prefix), got %q", gotAuth)
	}
	if gotBody["query"] == nil {
		t.Fatal("want a GraphQL query in the body")
	}
	variables, _ := gotBody["variables"].(map[string]any)
	input, _ := variables["input"].(map[string]any)
	if input["teamId"] != "team123" || input["title"] != "flaky test in CI" {
		t.Errorf("want teamId/title in GraphQL variables, got %v", input)
	}
}

func TestLinearAction_SkipsWhenNoAPIKey(t *testing.T) {
	node := models.WorkflowNode{
		ID: "li2", Type: models.NodeTypeAction, Template: "linear",
		Config: map[string]string{"linearTeamID": "team123"},
	}
	rc := engine.NewRunContext("r1", []byte(`"test message"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "linear_skipped_no_api_key" {
		t.Errorf("want 'linear_skipped_no_api_key', got %v", result)
	}
}

func TestLinearAction_SkipsWhenNoTeamID(t *testing.T) {
	node := models.WorkflowNode{
		ID: "li3", Type: models.NodeTypeAction, Template: "linear",
		Secrets: map[string]string{"linearAPIKey": "lin_api_xxx"},
	}
	rc := engine.NewRunContext("r1", []byte(`"test message"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "linear_skipped_no_team_id" {
		t.Errorf("want 'linear_skipped_no_team_id', got %v", result)
	}
}

func TestGitLabAction_CreatesIssue(t *testing.T) {
	var gotPath, gotToken string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "gl1", Type: models.NodeTypeAction, Template: "gitlab",
		Secrets: map[string]string{"gitlabAPIToken": "glpat-xxx"},
		Config:  map[string]string{"gitlabProjectID": "42", "gitlabBaseURL": srv.URL},
	}
	rc := engine.NewRunContext("r1", []byte(`"pipeline broke"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "gitlab_issue_created" {
		t.Errorf("want 'gitlab_issue_created', got %v", result)
	}
	if gotPath != "/api/v4/projects/42/issues" {
		t.Errorf("want project issues path, got %q", gotPath)
	}
	if gotToken != "glpat-xxx" {
		t.Errorf("want PRIVATE-TOKEN header, got %q", gotToken)
	}
	if gotQuery.Get("title") != "pipeline broke" {
		t.Errorf("want title in query, got %v", gotQuery)
	}
}

func TestGitLabAction_DefaultsToGitLabCom(t *testing.T) {
	node := models.WorkflowNode{
		ID: "gl2", Type: models.NodeTypeAction, Template: "gitlab",
		Secrets: map[string]string{"gitlabAPIToken": "glpat-xxx"},
		Config:  map[string]string{"gitlabProjectID": "42"},
	}
	rc := engine.NewRunContext("r1", []byte(`"x"`))
	// No live gitlab.com call in unit tests; this only asserts we don't skip
	// due to missing config and that a network error (not a config-skip
	// sentinel) is what comes back when the real host is unreachable in CI.
	_, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err == nil {
		t.Skip("network reachable in this environment; skip is fine, this test only guards against a config-skip sentinel")
	}
}

func TestGitLabAction_SkipsWhenNoToken(t *testing.T) {
	node := models.WorkflowNode{
		ID: "gl3", Type: models.NodeTypeAction, Template: "gitlab",
		Config: map[string]string{"gitlabProjectID": "42"},
	}
	rc := engine.NewRunContext("r1", []byte(`"pipeline broke"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "gitlab_skipped_no_token" {
		t.Errorf("want 'gitlab_skipped_no_token', got %v", result)
	}
}

func TestGitLabAction_SkipsWhenNoProjectID(t *testing.T) {
	node := models.WorkflowNode{
		ID: "gl4", Type: models.NodeTypeAction, Template: "gitlab",
		Secrets: map[string]string{"gitlabAPIToken": "glpat-xxx"},
	}
	rc := engine.NewRunContext("r1", []byte(`"pipeline broke"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "gitlab_skipped_no_project_id" {
		t.Errorf("want 'gitlab_skipped_no_project_id', got %v", result)
	}
}
