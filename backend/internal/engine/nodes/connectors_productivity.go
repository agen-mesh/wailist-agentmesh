package nodes

import (
	"context"
	"fmt"
	"net/http"

	"github.com/agentmesh/backend/internal/models"
)

// notionAPIBase is overridden in tests via SetNotionAPIBaseForTest.
var notionAPIBase = "https://api.notion.com"

// SetNotionAPIBaseForTest overrides the Notion API base URL. Call only from
// tests. Pass "" to reset to the real API.
func SetNotionAPIBaseForTest(base string) {
	if base == "" {
		notionAPIBase = "https://api.notion.com"
	} else {
		notionAPIBase = base
	}
}

func sendNotion(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "notionAPIKey")
	if apiKey == "" {
		return "notion_skipped_no_api_key", nil
	}
	pageID := configVal(node, "notionPageID", "")
	if pageID == "" {
		return "notion_skipped_no_page_id", nil
	}
	target := notionAPIBase + "/v1/blocks/" + pageID + "/children"
	payload := map[string]any{
		"children": []map[string]any{{
			"object": "block",
			"type":   "paragraph",
			"paragraph": map[string]any{
				"rich_text": []map[string]any{{
					"type": "text",
					"text": map[string]any{"content": rc.Message()},
				}},
			},
		}},
	}
	headers := map[string]string{
		"Authorization":  "Bearer " + apiKey,
		"Notion-Version": "2022-06-28",
	}
	req, err := newJSONRequest(ctx, http.MethodPatch, target, headers, payload)
	if err != nil {
		return nil, fmt.Errorf("Notion: %w", err)
	}
	return doAndCheck(req, "notion_block_appended", "Notion")
}
