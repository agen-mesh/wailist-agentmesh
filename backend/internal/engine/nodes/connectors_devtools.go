package nodes

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// jiraDomainPattern matches Atlassian's site-naming rules: letters, digits,
// and hyphens only, starting with a letter or digit. jiraDomain is
// user-supplied config that gets interpolated directly into the request
// host (unlike other connectors, where user config only ever becomes a path
// segment), so it must be validated before being used to build a URL —
// otherwise a crafted value could redirect the request (and the Basic-auth
// header carrying the real API token) to an attacker-controlled host.
var jiraDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

// githubAPIBase is overridden in tests via SetGitHubAPIBaseForTest.
var githubAPIBase = "https://api.github.com"

// SetGitHubAPIBaseForTest overrides the GitHub API base URL. Call only from
// tests. Pass "" to reset to the real API.
func SetGitHubAPIBaseForTest(base string) {
	if base == "" {
		githubAPIBase = "https://api.github.com"
	} else {
		githubAPIBase = base
	}
}

func sendGitHub(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "githubToken")
	if token == "" {
		return "github_skipped_no_token", nil
	}
	repo := configVal(node, "githubRepo", "")
	if repo == "" {
		return "github_skipped_no_repo", nil
	}
	target := githubAPIBase + "/repos/" + repo + "/issues"
	payload := map[string]any{"title": issueTitle(rc.Message()), "body": rc.Message()}
	headers := map[string]string{
		"Authorization": "Bearer " + token,
		"Accept":        "application/vnd.github+json",
	}
	return postJSON(ctx, target, headers, payload, "github_issue_created", "GitHub")
}

// jiraAPIBase is overridden in tests via SetJiraAPIBaseForTest — normally
// "https://{domain}.atlassian.net" is built per-node, so the test override
// replaces the whole scheme+host, and sendJira skips the ".atlassian.net"
// suffix when a test base is set.
var jiraAPIBase = ""

// SetJiraAPIBaseForTest overrides the Jira API base URL entirely (including
// scheme+host). Call only from tests. Pass "" to reset to the real
// https://{domain}.atlassian.net construction.
func SetJiraAPIBaseForTest(base string) {
	jiraAPIBase = base
}

func sendJira(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiToken := secretVal(node, "jiraAPIToken")
	if apiToken == "" {
		return "jira_skipped_no_api_token", nil
	}
	email := configVal(node, "jiraEmail", "")
	domain := configVal(node, "jiraDomain", "")
	projectKey := configVal(node, "jiraProjectKey", "")
	if email == "" || domain == "" || projectKey == "" {
		return "jira_skipped_missing_config", nil
	}
	if !jiraDomainPattern.MatchString(domain) {
		return "jira_skipped_invalid_domain", nil
	}
	issueType := configVal(node, "jiraIssueType", "Task")
	base := jiraAPIBase
	if base == "" {
		base = "https://" + domain + ".atlassian.net"
	}
	target := base + "/rest/api/3/issue"
	payload := map[string]any{
		"fields": map[string]any{
			"project":   map[string]any{"key": projectKey},
			"summary":   issueTitle(rc.Message()),
			"issuetype": map[string]any{"name": issueType},
			"description": map[string]any{
				"type":    "doc",
				"version": 1,
				"content": []map[string]any{{
					"type":    "paragraph",
					"content": []map[string]any{{"type": "text", "text": rc.Message()}},
				}},
			},
		},
	}
	auth := base64.StdEncoding.EncodeToString([]byte(email + ":" + apiToken))
	headers := map[string]string{"Authorization": "Basic " + auth}
	return postJSON(ctx, target, headers, payload, "jira_issue_created", "Jira")
}

// linearAPIBase is overridden in tests via SetLinearAPIBaseForTest.
var linearAPIBase = "https://api.linear.app"

// SetLinearAPIBaseForTest overrides the Linear API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetLinearAPIBaseForTest(base string) {
	if base == "" {
		linearAPIBase = "https://api.linear.app"
	} else {
		linearAPIBase = base
	}
}

func sendLinear(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "linearAPIKey")
	if apiKey == "" {
		return "linear_skipped_no_api_key", nil
	}
	teamID := configVal(node, "linearTeamID", "")
	if teamID == "" {
		return "linear_skipped_no_team_id", nil
	}
	payload := map[string]any{
		"query": `mutation IssueCreate($input: IssueCreateInput!) { issueCreate(input: $input) { success } }`,
		"variables": map[string]any{
			"input": map[string]any{
				"teamId":      teamID,
				"title":       issueTitle(rc.Message()),
				"description": rc.Message(),
			},
		},
	}
	headers := map[string]string{"Authorization": apiKey}
	return postJSON(ctx, linearAPIBase+"/graphql", headers, payload, "linear_issue_created", "Linear")
}

func sendGitLab(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "gitlabAPIToken")
	if token == "" {
		return "gitlab_skipped_no_token", nil
	}
	projectID := configVal(node, "gitlabProjectID", "")
	if projectID == "" {
		return "gitlab_skipped_no_project_id", nil
	}
	base := strings.TrimRight(configVal(node, "gitlabBaseURL", "https://gitlab.com"), "/")
	target := base + "/api/v4/projects/" + url.PathEscape(projectID) + "/issues"
	if err := urlValidator(target); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("title", issueTitle(rc.Message()))
	q.Set("description", rc.Message())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("GitLab: build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	return doAndCheck(req, "gitlab_issue_created", "GitLab")
}
