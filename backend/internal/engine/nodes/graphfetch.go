package nodes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
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
