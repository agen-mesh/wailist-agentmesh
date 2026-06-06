package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/agentmesh/backend/internal/models"
)

func ExecuteAction(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	switch node.Template {
	case "webhook", "post_webhook":
		return callWebhook(ctx, node, rc)
	case "email":
		return sendEmail(ctx, node, rc)
	default:
		return "logged", nil
	}
}

func callWebhook(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	if err := urlValidator(node.URL); err != nil {
		return nil, err
	}
	payload := map[string]any{"output": rc.Message()}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, node.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
		return nil, fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(b))
	}
	return map[string]any{"status": resp.StatusCode}, nil
}

func sendEmail(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	if node.URL == "" {
		return "email_skipped_no_api_key", nil
	}
	payload := map[string]any{
		"from":    "AgentMesh <noreply@agentmesh.io>",
		"to":      []string{node.Source},
		"subject": "AgentMesh workflow result",
		"text":    rc.Message(),
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+node.URL)
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Resend API %d: %s", resp.StatusCode, string(b))
	}
	return "email_sent", nil
}
