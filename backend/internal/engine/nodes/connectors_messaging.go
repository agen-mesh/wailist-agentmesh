package nodes

import (
	"context"

	"github.com/agentmesh/backend/internal/models"
)

func sendSlack(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	webhookURL := secretVal(node, "slackWebhookURL")
	if webhookURL == "" {
		return "slack_skipped_no_webhook_url", nil
	}
	if err := urlValidator(webhookURL); err != nil {
		return nil, err
	}
	payload := map[string]any{"text": rc.Message()}
	return postJSON(ctx, webhookURL, nil, payload, "slack_sent", "Slack")
}
