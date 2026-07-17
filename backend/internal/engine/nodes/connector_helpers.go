package nodes

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// secretVal reads a per-connector credential stored on the node's Secrets map.
// Returns "" if the node has no Secrets map or the key is unset.
func secretVal(node models.WorkflowNode, key string) string {
	if node.Secrets == nil {
		return ""
	}
	return node.Secrets[key]
}

// configVal reads a per-connector non-secret setting from the node's Config map,
// falling back to def when unset.
func configVal(node models.WorkflowNode, key, def string) string {
	if node.Config == nil || node.Config[key] == "" {
		return def
	}
	return node.Config[key]
}

// newJSONRequest builds a JSON request with the given method, target, and headers.
// Content-Type is always application/json; extraHeaders may add Authorization etc.
func newJSONRequest(ctx context.Context, method, target string, extraHeaders map[string]string, payload any) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	return req, nil
}

// postJSON POSTs payload as JSON to target and returns sentinel on success.
func postJSON(ctx context.Context, target string, extraHeaders map[string]string, payload any, sentinel, serviceName string) (any, error) {
	req, err := newJSONRequest(ctx, http.MethodPost, target, extraHeaders, payload)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", serviceName, err)
	}
	return doAndCheck(req, sentinel, serviceName)
}

// doValidatedRequest runs req through the SSRF guard and executes it on
// the shared toolHTTPClient, wrapping any transport failure with a
// redacted URL. Callers own reading and closing resp.Body — use this for
// callers that need the raw response (doAndCheck wraps it for the common
// sentinel-on-success case).
func doValidatedRequest(req *http.Request, serviceName string) (*http.Response, error) {
	if err := urlValidator(req.URL.String()); err != nil {
		return nil, err
	}
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: request to %s failed: %w", serviceName, redactedURL(req.URL), unwrapURLError(err))
	}
	return resp, nil
}

// doAndCheck executes req on the shared toolHTTPClient, treats any status >= 400
// as an error carrying a bounded body excerpt, and returns sentinel otherwise.
func doAndCheck(req *http.Request, sentinel, serviceName string) (any, error) {
	resp, err := doValidatedRequest(req, serviceName)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
		return nil, fmt.Errorf("%s API %d: %s", serviceName, resp.StatusCode, string(b))
	}
	io.Copy(io.Discard, resp.Body)
	return sentinel, nil
}

// mediaResponseLimit bounds binary media payloads (e.g. generated audio),
// distinct from httpResponseLimit which only bounds error-body excerpts.
const mediaResponseLimit = 25 << 20 // 25 MiB (~25-30 min of 128kbps audio)

// readBounded reads r fully but errors if it exceeds limit bytes, instead
// of silently truncating like io.ReadAll(io.LimitReader(r, limit)) would.
func readBounded(r io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("response exceeds %d byte limit", limit)
	}
	return data, nil
}

// redactedURL renders just a URL's scheme and host, so request-failure errors
// never echo credentials embedded in the query string (e.g. Trello's
// key/token params) or the path (e.g. Telegram's bot token) into logs or SSE
// run output. The path/query never help diagnose a transport failure anyway —
// serviceName already identifies which connector failed.
func redactedURL(u *url.URL) string {
	c := *u
	c.User = nil
	c.Path = ""
	c.RawPath = ""
	c.RawQuery = ""
	c.Fragment = ""
	c.RawFragment = ""
	return c.String()
}

// unwrapURLError returns the underlying transport error for a *url.Error,
// whose own Error() string embeds the full request URL (including any query
// string). Returning the wrapped reason instead of err itself keeps error
// messages informative without leaking query-string credentials.
func unwrapURLError(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Err
	}
	return err
}

// basicAuthHeader builds an Authorization: Basic header map from a
// username:password pair (RFC 7617).
func basicAuthHeader(user, pass string) map[string]string {
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	return map[string]string{"Authorization": "Basic " + auth}
}

// issueTitle derives a short title from a longer message: its first non-blank
// line, capped at 120 runes, falling back to a generic title when blank.
func issueTitle(message string) string {
	line := message
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		line = message[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "AgentMesh workflow result"
	}
	r := []rune(line)
	if len(r) > 120 {
		return string(r[:120])
	}
	return line
}

// PostJSONForTest and IssueTitleForTest are test-only exported wrappers, used by
// connector_helpers_test.go (package nodes_test) to test the unexported helpers
// above without exporting them from the package's real API.
func PostJSONForTest(ctx context.Context, target string, extraHeaders map[string]string, payload any, sentinel, serviceName string) (any, error) {
	return postJSON(ctx, target, extraHeaders, payload, sentinel, serviceName)
}

func IssueTitleForTest(message string) string {
	return issueTitle(message)
}

func ReadBoundedForTest(r io.Reader, limit int) ([]byte, error) {
	return readBounded(r, limit)
}
