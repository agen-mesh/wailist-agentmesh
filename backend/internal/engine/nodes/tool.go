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
		if isPrivateIP(ia.IP) {
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

func ExecuteTool(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	switch node.Template {
	case "calc":
		return evalMath(node.URL)
	case "datetime":
		return time.Now().UTC().Format(time.RFC3339), nil
	case "http":
		return callHTTP(ctx, node, rc)
	default:
		return rc.Message(), nil
	}
}

func callHTTP(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	if err := urlValidator(node.URL); err != nil {
		return nil, err
	}
	method := node.Method
	if method == "" {
		method = http.MethodGet
	}
	var bodyReader io.Reader
	if method == http.MethodPost {
		bodyReader = bytes.NewReader([]byte(rc.Message()))
	}
	req, err := http.NewRequestWithContext(ctx, method, node.URL, bodyReader)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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

func isPrivateIP(ip net.IP) bool {
	private := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",   // link-local
		"100.64.0.0/10",    // CGNAT
		"::1/128",          // loopback IPv6
		"fc00::/7",         // unique local IPv6
		"fe80::/10",        // link-local IPv6
		"224.0.0.0/4",      // multicast
		"240.0.0.0/4",      // reserved
		"0.0.0.0/8",        // this network
	}
	for _, cidr := range private {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
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
