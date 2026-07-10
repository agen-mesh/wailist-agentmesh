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

func sendDiscord(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	webhookURL := secretVal(node, "discordWebhookURL")
	if webhookURL == "" {
		return "discord_skipped_no_webhook_url", nil
	}
	if err := urlValidator(webhookURL); err != nil {
		return nil, err
	}
	payload := map[string]any{"content": rc.Message()}
	return postJSON(ctx, webhookURL, nil, payload, "discord_sent", "Discord")
}

func sendTeams(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	webhookURL := secretVal(node, "teamsWebhookURL")
	if webhookURL == "" {
		return "teams_skipped_no_webhook_url", nil
	}
	if err := urlValidator(webhookURL); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"@type":    "MessageCard",
		"@context": "http://schema.org/extensions",
		"text":     rc.Message(),
	}
	return postJSON(ctx, webhookURL, nil, payload, "teams_sent", "Teams")
}

func sendGoogleChat(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	webhookURL := secretVal(node, "googleChatWebhookURL")
	if webhookURL == "" {
		return "google_chat_skipped_no_webhook_url", nil
	}
	if err := urlValidator(webhookURL); err != nil {
		return nil, err
	}
	payload := map[string]any{"text": rc.Message()}
	return postJSON(ctx, webhookURL, nil, payload, "google_chat_sent", "Google Chat")
}
