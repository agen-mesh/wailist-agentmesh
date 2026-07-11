package nodes

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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

func sendNtfy(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	topic := configVal(node, "ntfyTopic", "")
	if topic == "" {
		return "ntfy_skipped_no_topic", nil
	}
	server := configVal(node, "ntfyServerURL", "https://ntfy.sh")
	target := strings.TrimRight(server, "/") + "/" + topic
	if err := urlValidator(target); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(rc.Message()))
	if err != nil {
		return nil, fmt.Errorf("ntfy: build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if token := secretVal(node, "ntfyAuthToken"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return doAndCheck(req, "ntfy_sent", "ntfy")
}

// telegramAPIBase is overridden in tests via SetTelegramAPIBaseForTest so requests
// hit an httptest server instead of the real Telegram Bot API.
var telegramAPIBase = "https://api.telegram.org"

// SetTelegramAPIBaseForTest overrides the Telegram API base URL. Call only from
// tests. Pass "" to reset to the real API.
func SetTelegramAPIBaseForTest(base string) {
	if base == "" {
		telegramAPIBase = "https://api.telegram.org"
	} else {
		telegramAPIBase = base
	}
}

func sendTelegram(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "telegramBotToken")
	if token == "" {
		return "telegram_skipped_no_bot_token", nil
	}
	chatID := configVal(node, "telegramChatID", "")
	if chatID == "" {
		return "telegram_skipped_no_chat_id", nil
	}
	target := telegramAPIBase + "/bot" + token + "/sendMessage"
	payload := map[string]any{"chat_id": chatID, "text": rc.Message()}
	return postJSON(ctx, target, nil, payload, "telegram_sent", "Telegram")
}
