package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// fetch_url lets the builder call an API before wiring an http node to it.
// Without it the builder guessed twice per endpoint -- whether it answers a
// server at all, and what its JSON looks like -- and a live build lost on
// both: Yahoo's quote API answers a server with 429, and the jsonPath it
// invented had never been checked against a real response.
//
// It sends the same request the http tool would at run time, through the
// same client (toolHTTPClient: private and internal addresses refused at
// dial time, redirects re-validated), so a refusal here is a refusal the
// workflow would hit too. GET only, never a credential, body truncated.

const (
	fetchURLReadLimit = 256 << 10 // read at most this much of a response
	fetchURLBodyShown = 3000      // and show the model at most this much of it
)

func fetchURL(ctx context.Context, rawURL string) string {
	result := func(v map[string]any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	rawURL = strings.TrimSpace(rawURL)
	if err := urlValidator(rawURL); err != nil {
		return result(map[string]any{"error": "fetch_url: " + err.Error()})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return result(map[string]any{"error": "fetch_url: " + err.Error()})
	}
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return result(map[string]any{
			"error": "fetch_url: request failed: " + err.Error(),
			"note":  "A workflow http step sends this same request and would fail the same way. Find another source.",
		})
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, fetchURLReadLimit))

	out := map[string]any{"status": resp.StatusCode, "contentType": resp.Header.Get("Content-Type")}
	var parsed any
	if json.Unmarshal(raw, &parsed) == nil {
		compact, _ := json.Marshal(parsed)
		out["json"] = true
		out["body"] = truncateBody(string(compact))
	} else {
		out["json"] = false
		out["body"] = truncateBody(string(raw))
	}
	switch {
	case resp.StatusCode != http.StatusOK:
		out["note"] = "This endpoint did not answer 200. A workflow http step sends the same request and will fail the same way -- do not wire it; find another source or tell the user."
	case out["json"] == true:
		out["note"] = "Derive any jsonPath from this body, not from memory: a dot path where numbers index arrays, e.g. data.0.lastPrice."
	default:
		out["note"] = "Not JSON: json_extract cannot read this. Use html_extract or xml, or find a JSON source."
	}
	return result(out)
}

func truncateBody(s string) string {
	if len(s) <= fetchURLBodyShown {
		return s
	}
	return strings.ToValidUTF8(s[:fetchURLBodyShown], "") + "…(truncated)"
}

// httpNodeURLChange reports the static GET url an add_node/update_node call
// would set on an http tool node, or "" if the call sets none. Only a static
// GET is probed: a POST may have side effects and needs a body, and a url
// containing {{ }} references is only known at run time.
func httpNodeURLChange(graph *models.WorkflowGraph, name string, args map[string]any) string {
	fields, _ := args["fields"].(map[string]any)
	url, _ := fields["url"].(string)
	url = strings.TrimSpace(url)
	if url == "" || strings.Contains(url, "{{") {
		return ""
	}
	method, _ := fields["method"].(string)
	switch name {
	case "add_node":
		if argString(args, "type") != "tool" || argString(args, "template") != "http" {
			return ""
		}
	case "update_node":
		n, ok := findGraphNode(graph, argString(args, "id"))
		if !ok || n.Type != models.NodeTypeTool {
			return ""
		}
		template := n.Template
		if t := argString(args, "template"); t != "" {
			template = t
		}
		if template != "http" {
			return ""
		}
		if method == "" {
			method = n.Method
		}
	default:
		return ""
	}
	if method != "" && !strings.EqualFold(method, http.MethodGet) {
		return ""
	}
	return url
}

// noWorkingURLAdvice is what the model is told when a url is refused. A live
// build that was only told "find a working source" spent its entire time
// budget searching for a free Nifty/Sensex API -- none works from a server
// (Yahoo answers 429, NSE blocks, community mirrors 404) -- and ended with
// no fetch step at all. For current information the reliable answer is the
// websearch tool, which looks it up live at run time with no API to break.
const noWorkingURLAdvice = "Do not keep hunting: free APIs for market prices, news and similar live data are usually blocked for servers. " +
	"For current information like that, attach a websearch tool to the agent's tools port instead of an http node -- " +
	"the agent looks it up live on every run. Otherwise try at most one other source you have real evidence for."

// judgeProbe turns a fetchURL result into what the builder does with the
// node: refuse it (a run would fail on this URL), or add it with a note --
// the verified body, or a warning that it needs a credential.
func judgeProbe(url, probe string) (refuse, note string) {
	var p struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
		Body   string `json:"body"`
		JSON   bool   `json:"json"`
	}
	_ = json.Unmarshal([]byte(probe), &p)
	switch {
	case p.Error != "":
		return fmt.Sprintf("refused: the http url %s could not be reached (%s). A workflow run would fail the same way. %s", url, p.Error, noWorkingURLAdvice), ""
	case p.Status >= 200 && p.Status < 300:
		note := fmt.Sprintf(" -- verified: the url answered HTTP %d.", p.Status)
		if p.JSON {
			note += " Read any jsonPath from this real response: " + p.Body
		} else {
			note += " It is not JSON, so json_extract cannot read it."
		}
		return "", note
	case p.Status == http.StatusUnauthorized || p.Status == http.StatusForbidden:
		return "", fmt.Sprintf(" -- note: the url requires authentication (HTTP %d). The user must add the credential (headers) to this node in the Inspector; say so in your reply. Its response shape could not be checked.", p.Status)
	default:
		return fmt.Sprintf("refused: the http url %s answered HTTP %d, so a workflow run would fail on it. %s", url, p.Status, noWorkingURLAdvice), ""
	}
}
