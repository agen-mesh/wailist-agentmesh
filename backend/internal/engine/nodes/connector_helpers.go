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
	"regexp"
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

// messageTemplateKey is the Config key a message-sending connector (Slack,
// Discord, Telegram, GitHub, Notion, ...) reads an optional template from.
// Empty (the default) preserves every such connector's original behavior
// exactly: send rc.Message() verbatim.
const messageTemplateKey = "messageTemplate"

// resolveMessage is what a connector should call instead of rc.Message()
// directly, so every one of them picks up template support for free. See
// expandTemplate for the placeholder syntax.
func resolveMessage(node models.WorkflowNode, rc RunContexter) string {
	tmpl := configVal(node, messageTemplateKey, "")
	if tmpl == "" {
		return rc.Message()
	}
	return expandTemplate(tmpl, rc)
}

// templatePlaceholder matches {{ result }} and {{ result.field.path }}.
// Capture group 1 is "" for the bare form, or ".field.path" for a dotted one.
var templatePlaceholder = regexp.MustCompile(`\{\{\s*result((?:\.[A-Za-z0-9_]+)*)\s*\}\}`)

// expandTemplate replaces every {{ result }} / {{ result.field.path }}
// placeholder in tmpl against the run's most recent output. This is the
// direct answer to "the whole raw response gets forwarded, not just the
// part I want" -- {{ result }} keeps today's behavior (the whole thing,
// stringified), while {{ result.extract }} against e.g. a Wikipedia API
// response picks just that one field out of it instead.
//
// A path that doesn't resolve (wrong field name, or the output isn't a JSON
// object at that point) expands to "" rather than leaving the literal
// placeholder text in whatever gets sent -- there's no good way to surface
// an error mid-template, and a blank beats a broken-looking "{{ result.x }}"
// showing up in a real Slack message or email.
func expandTemplate(tmpl string, rc RunContexter) string {
	return templatePlaceholder.ReplaceAllStringFunc(tmpl, func(match string) string {
		groups := templatePlaceholder.FindStringSubmatch(match)
		path := groups[1]
		if path == "" {
			return rc.Message()
		}
		var cur any = rc.LastOutput()
		for _, key := range strings.Split(strings.TrimPrefix(path, "."), ".") {
			m, ok := cur.(map[string]any)
			if !ok {
				return ""
			}
			cur, ok = m[key]
			if !ok {
				return ""
			}
		}
		return templateValueToString(cur)
	})
}

// templateValueToString renders one resolved template value. Mirrors
// engine.anyToString's string/JSON-fallback behavior, duplicated here (not
// imported) because nodes is a lower-level package engine itself imports --
// importing back would be circular.
func templateValueToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// ExpandTemplateForTest and ResolveMessageForTest are test-only exported
// wrappers, used by connector_helpers_test.go (package nodes_test) to test
// the unexported helpers above without exporting them from the package's
// real API.
func ExpandTemplateForTest(tmpl string, rc RunContexter) string {
	return expandTemplate(tmpl, rc)
}

func ResolveMessageForTest(node models.WorkflowNode, rc RunContexter) string {
	return resolveMessage(node, rc)
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

// readErrorBody reads a bounded excerpt of a non-2xx response body for the
// error message, then drains any remainder so the underlying connection can
// be returned to the transport's idle pool instead of being torn down on
// Close() — the same reason doAndCheck's success path drains in full.
func readErrorBody(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
	io.Copy(io.Discard, resp.Body)
	return string(b)
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
		return nil, fmt.Errorf("%s API %d: %s", serviceName, resp.StatusCode, readErrorBody(resp))
	}
	io.Copy(io.Discard, resp.Body)
	return sentinel, nil
}

// getJSON executes req on the shared toolHTTPClient and decodes a successful
// JSON response body into `any`, returning it to the caller. Unlike
// doAndCheck -- which discards the body and hands back a fixed sentinel --
// this is for connector operations that read data (list/get) rather than
// just report success, so the result can flow to the next node.
func getJSON(req *http.Request, serviceName string) (any, error) {
	resp, err := doValidatedRequest(req, serviceName)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s API %d: %s", serviceName, resp.StatusCode, readErrorBody(resp))
	}
	b, err := readBounded(resp.Body, httpResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", serviceName, err)
	}
	var result any
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", serviceName, err)
	}
	return result, nil
}

// GetJSONForTest is a test-only exported wrapper for getJSON, used by
// connector_helpers_test.go (package nodes_test) to test the unexported
// helper without exporting it from the package's real API.
func GetJSONForTest(req *http.Request, serviceName string) (any, error) {
	return getJSON(req, serviceName)
}

// mediaResponseLimit bounds binary media payloads (e.g. generated audio),
// distinct from httpResponseLimit which only bounds error-body excerpts.
//
// Kept well below what the underlying API allows: a node's result — audio
// included, base64-encoded — is JSON-marshaled into a run-log DB row and
// broadcast over the run's SSE stream in full, and the frontend log viewer
// renders it inline with no truncation. 5 MiB of audio (~6.7 MiB base64,
// ~5 min at 128kbps) keeps a single action node's worst case from bloating
// the DB or freezing that viewer; it is not meant as a generation-quality
// limit. Longer-form audio needs the node to return a storage reference
// instead of inline bytes, which is a larger change to the run-log/SSE
// pipeline than this connector alone should make.
const mediaResponseLimit = 5 << 20 // 5 MiB (~5 min of 128kbps audio)

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
