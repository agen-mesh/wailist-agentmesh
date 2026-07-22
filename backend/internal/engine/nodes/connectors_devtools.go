package nodes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

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
		return "github_skipped_no_token", ErrActionSkipped
	}
	repo := configVal(node, "githubRepo", "")
	if repo == "" {
		return "github_skipped_no_repo", ErrActionSkipped
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return "github_skipped_invalid_repo", ErrActionSkipped
	}
	target := githubAPIBase + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/issues"
	msg := rc.Message()
	payload := map[string]any{"title": issueTitle(msg), "body": msg}
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
		return "jira_skipped_no_api_token", ErrActionSkipped
	}
	email := configVal(node, "jiraEmail", "")
	domain := configVal(node, "jiraDomain", "")
	projectKey := configVal(node, "jiraProjectKey", "")
	if email == "" || domain == "" || projectKey == "" {
		return "jira_skipped_missing_config", ErrActionSkipped
	}
	if !jiraDomainPattern.MatchString(domain) {
		return "jira_skipped_invalid_domain", ErrActionSkipped
	}
	issueType := configVal(node, "jiraIssueType", "Task")
	base := jiraAPIBase
	if base == "" {
		base = "https://" + domain + ".atlassian.net"
	}
	target := base + "/rest/api/3/issue"
	msg := rc.Message()
	payload := map[string]any{
		"fields": map[string]any{
			"project":   map[string]any{"key": projectKey},
			"summary":   issueTitle(msg),
			"issuetype": map[string]any{"name": issueType},
			"description": map[string]any{
				"type":    "doc",
				"version": 1,
				"content": []map[string]any{{
					"type":    "paragraph",
					"content": []map[string]any{{"type": "text", "text": msg}},
				}},
			},
		},
	}
	headers := basicAuthHeader(email, apiToken)
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
		return "linear_skipped_no_api_key", ErrActionSkipped
	}
	teamID := configVal(node, "linearTeamID", "")
	if teamID == "" {
		return "linear_skipped_no_team_id", ErrActionSkipped
	}
	msg := rc.Message()
	payload := map[string]any{
		"query": `mutation IssueCreate($input: IssueCreateInput!) { issueCreate(input: $input) { success } }`,
		"variables": map[string]any{
			"input": map[string]any{
				"teamId":      teamID,
				"title":       issueTitle(msg),
				"description": msg,
			},
		},
	}
	headers := map[string]string{"Authorization": apiKey}
	// Linear's GraphQL API returns HTTP 200 for application-level failures
	// (bad auth, bad team ID, validation errors), putting them in a top-level
	// "errors" array or issueCreate.success:false instead of the status code —
	// postJSON/doAndCheck only inspects the status code, so this can't route
	// through them the way the REST connectors do.
	req, err := newJSONRequest(ctx, http.MethodPost, linearAPIBase+"/graphql", headers, payload)
	if err != nil {
		return nil, fmt.Errorf("Linear: %w", err)
	}
	resp, err := doValidatedRequest(req, "Linear")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Linear API %d: %s", resp.StatusCode, readErrorBody(resp))
	}
	body, err := readBounded(resp.Body, httpResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("Linear: read response: %w", err)
	}
	var result struct {
		Data struct {
			IssueCreate struct {
				Success bool `json:"success"`
			} `json:"issueCreate"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("Linear: decode response: %w", err)
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("Linear: %s", result.Errors[0].Message)
	}
	if !result.Data.IssueCreate.Success {
		return nil, fmt.Errorf("Linear: issueCreate returned success:false")
	}
	return "linear_issue_created", nil
}

func sendGitLab(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "gitlabAPIToken")
	if token == "" {
		return "gitlab_skipped_no_token", ErrActionSkipped
	}
	projectID := configVal(node, "gitlabProjectID", "")
	if projectID == "" {
		return "gitlab_skipped_no_project_id", ErrActionSkipped
	}
	base := strings.TrimRight(configVal(node, "gitlabBaseURL", "https://gitlab.com"), "/")
	target := base + "/api/v4/projects/" + url.PathEscape(projectID) + "/issues"
	msg := rc.Message()
	payload := map[string]any{"title": issueTitle(msg), "description": msg}
	headers := map[string]string{"PRIVATE-TOKEN": token}
	return postJSON(ctx, target, headers, payload, "gitlab_issue_created", "GitLab")
}

func sendSentry(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	dsn := secretVal(node, "sentryDSN")
	if dsn == "" {
		return "sentry_skipped_no_dsn", ErrActionSkipped
	}
	publicKey, host, projectID, err := parseSentryDSN(dsn)
	if err != nil {
		return nil, err
	}
	eventID, err := randomHex32()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	header := fmt.Sprintf(`{"event_id":%q,"sent_at":%q}`, eventID, now.Format(time.RFC3339))
	itemHeader := `{"type":"event"}`
	itemBody, err := json.Marshal(map[string]any{
		"event_id":  eventID,
		"timestamp": now.Unix(),
		"platform":  "other",
		"level":     "info",
		"message":   map[string]any{"formatted": rc.Message()},
	})
	if err != nil {
		return nil, fmt.Errorf("Sentry: encode event: %w", err)
	}
	envelope := header + "\n" + itemHeader + "\n" + string(itemBody) + "\n"
	scheme := "https"
	if strings.HasPrefix(dsn, "http://") {
		scheme = "http" // only ever true for the local httptest server in tests
	}
	target := scheme + "://" + host + "/api/" + url.PathEscape(projectID) + "/envelope/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(envelope))
	if err != nil {
		return nil, fmt.Errorf("Sentry: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("X-Sentry-Auth", fmt.Sprintf("Sentry sentry_version=7, sentry_key=%s, sentry_client=agentmesh/1.0", publicKey))
	return doAndCheck(req, "sentry_event_sent", "Sentry")
}

// parseSentryDSN extracts the public key, ingest host, and numeric project ID
// from a DSN of the form scheme://<publicKey>@<host>/<projectID>.
func parseSentryDSN(dsn string) (publicKey, host, projectID string, err error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", "", "", fmt.Errorf("Sentry: invalid DSN: %w", err)
	}
	if u.User == nil {
		return "", "", "", fmt.Errorf("Sentry: DSN missing public key")
	}
	publicKey = u.User.Username()
	host = u.Host
	projectID = strings.Trim(u.Path, "/")
	if publicKey == "" || host == "" || projectID == "" {
		return "", "", "", fmt.Errorf("Sentry: DSN missing host or project id")
	}
	return publicKey, host, projectID, nil
}

func randomHex32() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("Sentry: generate event id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ParseSentryDSNForTest is a test-only exported wrapper for parseSentryDSN.
func ParseSentryDSNForTest(dsn string) (publicKey, host, projectID string, err error) {
	return parseSentryDSN(dsn)
}
