package nodes

import (
	"context"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// hubspotAPIBase is overridden in tests via SetHubSpotAPIBaseForTest.
var hubspotAPIBase = "https://api.hubapi.com"

// SetHubSpotAPIBaseForTest overrides the HubSpot API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetHubSpotAPIBaseForTest(base string) {
	if base == "" {
		hubspotAPIBase = "https://api.hubapi.com"
	} else {
		hubspotAPIBase = base
	}
}

func sendHubSpot(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "hubspotAPIKey")
	if apiKey == "" {
		return "hubspot_skipped_no_api_key", nil
	}
	payload := map[string]any{
		"properties": map[string]any{
			"hs_note_body": rc.Message(),
			"hs_timestamp": time.Now().UnixMilli(),
		},
	}
	headers := map[string]string{"Authorization": "Bearer " + apiKey}
	return postJSON(ctx, hubspotAPIBase+"/crm/v3/objects/notes", headers, payload, "hubspot_note_created", "HubSpot")
}
