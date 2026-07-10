package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

func ExecuteAction(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	switch node.Template {
	case "webhook", "post_webhook":
		return callWebhook(ctx, node, rc)
	case "email":
		return sendEmail(ctx, node, rc)
	case "slack":
		return sendSlack(ctx, node, rc)
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
	apiKey := node.EmailAPIKey
	if apiKey == "" {
		return "email_skipped_no_api_key", nil
	}
	to := node.EmailTo
	if to == "" {
		return "email_skipped_no_recipient", nil
	}
	from := node.EmailFrom
	if from == "" {
		from = "AgentMesh <onboarding@resend.dev>"
	}
	subject := node.EmailSubject
	if subject == "" {
		subject = "AgentMesh workflow result"
	}
	// Build body: replace {{ result }} with agent output
	agentOutput := rc.Message()
	bodyText := node.EmailBody
	if bodyText == "" {
		bodyText = "Hi,\n\nHere is your result:\n\n" + agentOutput + "\n\n— AgentMesh"
	} else {
		bodyText = replaceVar(bodyText, "result", agentOutput)
	}

	provider := node.EmailProvider
	if provider == "" {
		provider = "resend"
	}

	switch provider {
	case "resend":
		return sendViaResend(ctx, apiKey, from, to, subject, bodyText)
	default:
		return sendViaResend(ctx, apiKey, from, to, subject, bodyText)
	}
}

func replaceVar(s, key, val string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "{{ "+key+" }}", val), "{{"+key+"}}", val)
}

func sendViaResend(ctx context.Context, apiKey, from, to, subject, body string) (any, error) {
	payload := map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
		"text":    body,
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		rb, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Resend API %d: %s", resp.StatusCode, string(rb))
	}
	return "email_sent", nil
}
