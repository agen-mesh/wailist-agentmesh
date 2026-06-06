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

const (
	httpResponseLimit = 5 << 20 // 5 MiB
	httpTimeout       = 10 * time.Second
	calcMaxExprLen    = 256
)

// urlValidator can be swapped in tests to allow localhost servers.
var urlValidator = validateURL

// SetURLValidatorForTest replaces the SSRF validator. Call only from tests. Pass nil to reset.
func SetURLValidatorForTest(fn func(string) error) {
	if fn == nil {
		urlValidator = validateURL
	} else {
		urlValidator = fn
	}
}

var toolHTTPClient = &http.Client{
	Timeout: httpTimeout,
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

// validateURL rejects non-http(s) schemes, userinfo, and private/loopback addresses.
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
	host := u.Hostname()
	ips, err := net.LookupHost(host)
	if err != nil {
		// If DNS fails, let the request fail naturally
		return nil
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if isPrivateIP(ip) {
			return fmt.Errorf("requests to private/internal addresses are not allowed")
		}
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
