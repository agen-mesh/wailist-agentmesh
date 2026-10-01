package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/netutil"
)

// dialAndValidate resolves host, blocks private IPs, then dials the validated address.
// This runs at actual connect time, preventing DNS rebinding attacks.
func dialAndValidate(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses resolved for %s", host)
	}
	for _, ia := range ips {
		if netutil.IsPrivateIP(ia.IP) {
			return nil, fmt.Errorf("requests to private/internal addresses are not allowed")
		}
	}
	target := net.JoinHostPort(ips[0].IP.String(), port)
	return (&net.Dialer{Timeout: httpTimeout}).DialContext(ctx, network, target)
}

const (
	httpResponseLimit = 5 << 20 // 5 MiB
	httpTimeout       = 10 * time.Second
	calcMaxExprLen    = 256
)

// urlValidator can be swapped in tests to allow localhost servers.
var urlValidator = validateURL

// dialFn is the DialContext used by toolHTTPClient. Swappable in tests.
var dialFn = dialAndValidate

// SetURLValidatorForTest replaces both the URL validator and dialer. Call only from tests. Pass nil to reset.
func SetURLValidatorForTest(fn func(string) error) {
	if fn == nil {
		urlValidator = validateURL
		dialFn = dialAndValidate
	} else {
		urlValidator = fn
		dialFn = (&net.Dialer{Timeout: httpTimeout}).DialContext
	}
}

var toolHTTPClient = &http.Client{
	Timeout: httpTimeout,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialFn(ctx, network, addr)
		},
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if err := validateURL(req.URL.String()); err != nil {
			return err
		}
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// outboundPayHTTPTimeout is deliberately longer than httpTimeout's 10s --
// the outbound paid leg to a real x402 target isn't a simple fetch: a
// standards-compliant target does its own facilitator verify+settle round
// trip before it can answer at all (confirmed live 2026-08-01: a real
// target, canix402-api.compx.io, genuinely took >10s end-to-end and was
// timing out here, producing "context deadline exceeded" on a payment that
// had, in fact, already been signed and was headed to a real merchant --
// the outbound leg specifically needs more patience than a generic tool
// HTTP call or an unauthenticated 402 probe, neither of which involves a
// third party's own settlement machinery).
const outboundPayHTTPTimeout = 30 * time.Second

var outboundPayHTTPClient = &http.Client{
	Timeout: outboundPayHTTPTimeout,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialFn(ctx, network, addr)
		},
	},
	CheckRedirect: toolHTTPClient.CheckRedirect,
}

// SafeOutboundPayHTTPClient is SafeHTTPClient's counterpart for the one
// call site that pays a real target directly (PayTargetFromWallet2) --
// same SSRF-safe dial/redirect behavior, longer timeout. See
// outboundPayHTTPTimeout's doc comment for why a longer timeout is needed
// specifically here and not for SafeHTTPClient's other callers.
func SafeOutboundPayHTTPClient() *http.Client {
	return outboundPayHTTPClient
}

func ExecuteTool(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	return executeTool(ctx, node, rc, nil)
}

// ExecuteToolWithArgs is ExecuteTool plus the LLM's chosen function-call
// arguments. Only "websearch" reads them today -- a per-call query the
// static node config can't supply, unlike "http"'s fixed URL/calc's fixed
// expression. A separate entry point rather than widening ExecuteTool's own
// signature so the many call sites that never have LLM args (every
// standalone, non-agent-attached tool node) don't need to pass nil through.
func ExecuteToolWithArgs(ctx context.Context, node models.WorkflowNode, rc RunContexter, args map[string]any) (any, error) {
	return executeTool(ctx, node, rc, args)
}

func executeTool(ctx context.Context, node models.WorkflowNode, rc RunContexter, args map[string]any) (any, error) {
	switch node.Template {
	case "calc":
		return evalMath(node.URL)
	case "datetime":
		return executeDateTime(node)
	case "http":
		return callHTTP(ctx, node, rc)
	case "set":
		return executeSet(node, rc)
	case "json_extract":
		return executeJSONExtract(node, rc)
	case "crypto":
		return executeCrypto(node, rc)
	case "xml":
		return executeXMLToJSON(rc)
	case "template":
		return executeTemplate(node, rc)
	case "html_extract":
		return executeHTMLExtract(node, rc)
	case "markdown":
		return executeMarkdown(node, rc)
	case "quickchart":
		return executeQuickChart(node, rc)
	case "websearch":
		return webSearch(ctx, websearchQuery(node, args, rc), platformGeminiKey())
	default:
		return rc.Message(), nil
	}
}

// websearchQuery decides what this node searches for, in priority order.
//
// An agent's function-call argument wins: that is the question actually being
// asked. Then the node's own searchQuery, which is what makes a standalone
// flow node possible at all -- the catalog used to give this template no
// fields, so a node wired into the flow had nothing to search for and failed
// with "query is required" whenever the step before it produced no text, as a
// manual trigger always does. The upstream message is the last resort and the
// original behaviour.
func websearchQuery(node models.WorkflowNode, args map[string]any, rc RunContexter) string {
	if q, ok := args["query"].(string); ok && strings.TrimSpace(q) != "" {
		return q
	}
	if q := configVal(node, "searchQuery", ""); strings.TrimSpace(q) != "" {
		return resolveTemplate(q, rc)
	}
	return rc.Message()
}

// platformKeysForTools holds AgentMesh's own provider API keys for tool
// execution -- set once at startup, mirroring geminiBaseURL/urlValidator's
// swappable-package-var pattern above, rather than widening ExecuteTool's
// signature (and every one of its many existing call sites) just to carry
// one map that's genuinely process-wide, not per-call.
var platformKeysForTools map[string]string

// SetPlatformKeys installs the keys "websearch" (and any future built-in
// tool needing a platform-held credential) reads from. Called once from
// engine.Runner.SetPlatformKeys.
func SetPlatformKeys(keys map[string]string) { platformKeysForTools = keys }

func platformGeminiKey() string { return platformKeysForTools["gemini"] }

// PlatformKeys returns the provider key map the runner was configured with
// (SetPlatformKeys), for code outside a run -- the builder's dry run -- that
// has to call a platform-key agent the same way a run would.
func PlatformKeys() map[string]string { return platformKeysForTools }

// httpMethodsWithBody are the methods callHTTP attaches rc.Message() to as a
// request body -- GET/HEAD/OPTIONS never carry one, matching real HTTP
// semantics rather than the old POST-only special case.
var httpMethodsWithBody = map[string]bool{
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

// idempotentHTTPMethods are the methods where repeating an identical request
// cannot compound whatever the first attempt already did server-side --
// standard HTTP idempotency (RFC 7231 §4.2.2), used here to decide whether a
// failed call is safe to retry. POST and PATCH are deliberately excluded:
// neither is idempotent, so a retry after an ambiguous failure (e.g. the
// response was lost after the server already processed the write) could
// double the effect.
var idempotentHTTPMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPut:     true,
	http.MethodDelete:  true,
	http.MethodOptions: true,
}

func isIdempotentHTTPMethod(method string) bool {
	return idempotentHTTPMethods[method]
}

// IsIdempotentHTTPMethod exports isIdempotentHTTPMethod for callers outside
// this package -- currently engine.nodeMayHaveRealSideEffect, which needs
// the identical GET/HEAD/PUT/DELETE/OPTIONS classification to decide
// whether an "http" Tool node's config-staleness check is safe to apply
// (see its own doc comment).
func IsIdempotentHTTPMethod(method string) bool {
	return isIdempotentHTTPMethod(method)
}

// httpRequestBody is the body callHTTP sends for this node, whether it sends
// one at all, and whether it came from httpBodyTemplate. The one place this
// decision lives: callHTTP sends it, and the builder's dry run reports it as
// what a simulated request would have carried, so the two cannot disagree.
//
// Only POST, PUT, PATCH and DELETE carry a body. httpBodyTemplate is this
// node's own template key, distinct from messageTemplate -- a different node
// type/Inspector, and "body" is the accurate term for what a request carries,
// vs. a connector's "message". Same {{ result }} / {{ result.field }} /
// {{ node.<id> }} syntax either way (resolveTemplate).
//
// Only POST defaults to rc.Message() verbatim with no template set -- that's
// the pre-existing behavior and changing it would silently alter every
// already-saved POST node. That comparison is against the trimmed but NOT
// case-normalized method, so an already-saved "post" keeps sending no body,
// exactly as it did under the old literal method == "POST" check.
// PUT/PATCH/DELETE only attach a body when the node explicitly opts in via
// httpBodyTemplate -- never defaulted, to avoid silently changing behavior
// for nodes saved before this method-aware body logic existed.
func httpRequestBody(node models.WorkflowNode, rc RunContexter) (body string, sends, templated bool) {
	rawMethod := strings.TrimSpace(node.Method)
	method := strings.ToUpper(rawMethod)
	if method == "" {
		method = http.MethodGet
	}
	if !httpMethodsWithBody[method] {
		return "", false, false
	}
	if tmpl := configVal(node, "httpBodyTemplate", ""); tmpl != "" {
		return resolveTemplate(tmpl, rc), true, true
	}
	if rawMethod == http.MethodPost {
		return rc.Message(), true, false
	}
	return "", false, false
}

// HTTPRequestBodyForDryRun exports httpRequestBody for the builder's dry run.
func HTTPRequestBodyForDryRun(node models.WorkflowNode, rc RunContexter) (body string, sends, templated bool) {
	return httpRequestBody(node, rc)
}

func callHTTP(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	if err := urlValidator(node.URL); err != nil {
		return nil, err
	}
	// method (normalized) is what's sent on the wire; whether a body goes
	// with it is httpRequestBody's decision.
	method := strings.ToUpper(strings.TrimSpace(node.Method))
	if method == "" {
		method = http.MethodGet
	}
	var bodyReader io.Reader
	if body, sends, _ := httpRequestBody(node, rc); sends {
		bodyReader = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, node.URL, bodyReader)
	if err != nil {
		return nil, err
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Custom headers: a JSON object of header name -> value, e.g.
	// {"X-Api-Key": "...", "Authorization": "Bearer ..."}. Kept in Secrets
	// rather than Config -- headers commonly carry credentials and there's
	// no way to tell which ones from the shape alone, so the whole blob
	// gets the encrypted-at-rest treatment. Applied AFTER the Content-Type
	// default above so an explicit Content-Type here (e.g. for an
	// XML/form-urlencoded httpBodyTemplate) overrides the application/json
	// default instead of being clobbered by it.
	if raw := secretVal(node, "httpHeadersJSON"); raw != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(raw), &headers); err != nil {
			return nil, fmt.Errorf("http: invalid headers JSON: %w", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}
	if user := secretVal(node, "httpBasicUser"); user != "" {
		req.SetBasicAuth(user, secretVal(node, "httpBasicPass"))
	}
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		// A transport failure on an idempotent method (nothing non-repeatable
		// can have happened server-side) is safe to retry. POST/PATCH stay
		// unwrapped and therefore non-retryable by default: the request may
		// have reached the server and been acted on before the connection
		// dropped, so a retry could replay a write we can't confirm failed.
		if isIdempotentHTTPMethod(method) {
			return nil, Retryable(err)
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		httpErr := fmt.Errorf("http: %s %d: %s", method, resp.StatusCode, readErrorBody(resp))
		if isIdempotentHTTPMethod(method) {
			return nil, Retryable(httpErr)
		}
		return nil, httpErr
	}
	if resp.StatusCode >= 400 {
		// 4xx is a client error, not a transient one -- retrying sends the
		// exact same broken request again. Never retryable regardless of
		// method.
		return nil, fmt.Errorf("http: %s %d: %s", method, resp.StatusCode, readErrorBody(resp))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
	if err != nil {
		return nil, err
	}
	var result any
	if json.Unmarshal(b, &result) == nil {
		return result, nil
	}
	return string(b), nil
}

// ValidateURL rejects non-http(s) schemes and userinfo — the same guard
// used before every tool node HTTP call. Exported so other packages (e.g.
// the x402 relay handler) that fetch a caller-supplied URL can apply the
// identical scheme/userinfo check before making an outbound request.
func ValidateURL(raw string) error {
	return validateURL(raw)
}

// SafeHTTPClient returns the shared http.Client whose Transport re-resolves
// and blocks private/internal IPs at dial time (defeating DNS rebinding) and
// re-validates every redirect hop. Exported so other packages that fetch a
// caller-supplied URL (e.g. the x402 relay handler) reuse the same SSRF
// protection as tool node HTTP execution, rather than making an unguarded
// request with http.DefaultClient.
func SafeHTTPClient() *http.Client {
	return toolHTTPClient
}

// validateURL rejects non-http(s) schemes and userinfo.
// IP-level SSRF blocking happens at dial time via dialAndValidate.
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme %q not allowed", u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("URL must not contain userinfo")
	}
	return nil
}

// evalMath evaluates a simple arithmetic expression using the go/constant package.
// Expression length is capped and evaluation runs with panic recovery.
func evalMath(expr string) (result string, err error) {
	expr = strings.TrimSpace(expr)
	if len(expr) > calcMaxExprLen {
		return "", fmt.Errorf("calc: expression exceeds %d character limit", calcMaxExprLen)
	}
	// Reject shift operators — they can produce arbitrary-precision integers
	if strings.ContainsAny(expr, "<>") {
		return "", fmt.Errorf("calc: shift operators not allowed")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("calc: evaluation panicked: %v", r)
			}
		}()
		fset := token.NewFileSet()
		tv, evalErr := types.Eval(fset, nil, token.NoPos, expr)
		if evalErr != nil {
			err = fmt.Errorf("calc: %w", evalErr)
			return
		}
		if tv.Value == nil {
			err = fmt.Errorf("calc: nil result")
			return
		}
		if tv.Value.Kind() == constant.Int {
			result = tv.Value.String()
			return
		}
		f, _ := strconv.ParseFloat(tv.Value.String(), 64)
		result = strconv.FormatFloat(f, 'f', -1, 64)
	}()

	select {
	case <-done:
		return result, err
	case <-time.After(2 * time.Second):
		return "", fmt.Errorf("calc: evaluation timed out")
	}
}
