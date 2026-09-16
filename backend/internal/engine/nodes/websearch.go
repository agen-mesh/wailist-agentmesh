package nodes

import (
	"context"
	"fmt"
	"strings"
)

// defaultWebSearchModel is what grounded search runs on when nothing is
// configured. Grounding is billed per grounded prompt, not per token, and the
// 3.x tier is the cheaper one once past its free allowance: 5,000 free a
// month then $14 per 1,000, against 2.5's 1,500 free a day then $35 per 1,000.
// The default stays on 2.5 until a 3.x model has been confirmed to ground on
// this account, since a model that rejects grounding breaks every web search
// -- and a failed read step now degrades quietly instead of stopping the run.
const defaultWebSearchModel = "gemini-2.5-flash"

// webSearchModelOverride is set once at startup from WEB_SEARCH_MODEL,
// mirroring platformKeysForTools rather than widening webSearch's signature
// for a value that is genuinely process-wide.
var webSearchModelOverride string

// SetWebSearchModel installs the model grounded search runs on. Blank
// restores the default.
func SetWebSearchModel(model string) { webSearchModelOverride = strings.TrimSpace(model) }

func webSearchModelID() string {
	if webSearchModelOverride != "" {
		return webSearchModelOverride
	}
	return defaultWebSearchModel
}

// webSearch answers a query by asking Gemini to ground its response in a
// live Google Search -- the model itself issues and reads the search; this
// just relays the query and returns the grounded text plus its sources.
// Runs regardless of which provider is driving the calling agent (an
// OpenAI- or Anthropic-backed agent can attach this tool exactly like a
// Gemini-backed one), since it's a standalone call against the platform's
// own Gemini key, not a capability of whatever model requested it.
func webSearch(ctx context.Context, query, geminiAPIKey string) (any, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("websearch: query is required")
	}
	if geminiAPIKey == "" {
		return nil, fmt.Errorf("websearch: platform Gemini key is not configured")
	}
	apiURL := fmt.Sprintf("%s/v1beta/models/%s:generateContent", geminiBaseURL, webSearchModelID())
	headers := map[string]string{"x-goog-api-key": geminiAPIKey}
	payload := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": query}}},
		},
		"tools": []map[string]any{{"google_search": map[string]any{}}},
	}
	resp, err := postLLMJSON(ctx, apiURL, headers, payload)
	if err != nil {
		return nil, fmt.Errorf("websearch: %w", err)
	}
	text, err := extractGeminiText(resp)
	if err != nil {
		return nil, fmt.Errorf("websearch: %w", err)
	}
	result := map[string]any{"answer": text}
	if sources := extractGroundingSources(resp); len(sources) > 0 {
		result["sources"] = sources
	}
	return result, nil
}

// extractGroundingSources pulls the pages Gemini actually grounded its
// answer in out of groundingMetadata, so the caller sees not just an answer
// but where it came from.
func extractGroundingSources(resp map[string]any) []map[string]string {
	candidates, _ := resp["candidates"].([]any)
	if len(candidates) == 0 {
		return nil
	}
	cand, _ := candidates[0].(map[string]any)
	meta, _ := cand["groundingMetadata"].(map[string]any)
	chunks, _ := meta["groundingChunks"].([]any)
	var sources []map[string]string
	for _, c := range chunks {
		chunk, _ := c.(map[string]any)
		web, _ := chunk["web"].(map[string]any)
		uri, _ := web["uri"].(string)
		if uri == "" {
			continue
		}
		title, _ := web["title"].(string)
		sources = append(sources, map[string]string{"uri": uri, "title": title})
	}
	return sources
}
