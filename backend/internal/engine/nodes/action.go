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
	case "discord":
		return sendDiscord(ctx, node, rc)
	case "teams":
		return sendTeams(ctx, node, rc)
	case "google_chat":
		return sendGoogleChat(ctx, node, rc)
	case "ntfy":
		return sendNtfy(ctx, node, rc)
	case "telegram":
		return sendTelegram(ctx, node, rc)
	case "github":
		return sendGitHub(ctx, node, rc)
	case "notion":
		return sendNotion(ctx, node, rc)
	case "airtable":
		return sendAirtable(ctx, node, rc)
	case "hubspot":
		return sendHubSpot(ctx, node, rc)
	case "trello":
		return sendTrello(ctx, node, rc)
	case "asana":
		return sendAsana(ctx, node, rc)
	case "clickup":
		return sendClickUp(ctx, node, rc)
	case "jira":
		return sendJira(ctx, node, rc)
	case "mailchimp":
		return sendMailchimp(ctx, node, rc)
	case "linear":
		return sendLinear(ctx, node, rc)
	case "todoist":
		return sendTodoist(ctx, node, rc)
	case "gitlab":
		return sendGitLab(ctx, node, rc)
	case "sentry":
		return sendSentry(ctx, node, rc)
	case "supabase":
		return sendSupabase(ctx, node, rc)
	case "woocommerce":
		return sendWooCommerce(ctx, node, rc)
	case "elevenlabs":
		return sendElevenLabs(ctx, node, rc)
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
	provider := node.EmailProvider
	if provider == "" {
		provider = "resend"
	}

	from := node.EmailFrom
	if from == "" {
		if provider == "resend" {
			// Resend's shared sandbox sender — only usable on Resend itself,
			// so it must not leak into the other providers' requests below.
			from = "AgentMesh <onboarding@resend.dev>"
		} else {
			return "email_skipped_no_from_address", nil
		}
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

	switch provider {
	case "resend":
		return sendViaResend(ctx, apiKey, from, to, subject, bodyText)
	case "sendgrid":
		return sendViaSendGrid(ctx, apiKey, from, to, subject, bodyText)
	case "brevo":
		return sendViaBrevo(ctx, apiKey, from, to, subject, bodyText)
	case "postmark":
		return sendViaPostmark(ctx, apiKey, from, to, subject, bodyText)
	default:
		return sendViaResend(ctx, apiKey, from, to, subject, bodyText)
	}
}

func replaceVar(s, key, val string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "{{ "+key+" }}", val), "{{"+key+"}}", val)
}

// resendAPIBase is overridden in tests via SetResendAPIBaseForTest.
var resendAPIBase = "https://api.resend.com"

// SetResendAPIBaseForTest overrides the Resend API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetResendAPIBaseForTest(base string) {
	if base == "" {
		resendAPIBase = "https://api.resend.com"
	} else {
		resendAPIBase = base
	}
}

func sendViaResend(ctx context.Context, apiKey, from, to, subject, body string) (any, error) {
	payload := map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
		"text":    body,
	}
	headers := map[string]string{"Authorization": "Bearer " + apiKey}
	return postJSON(ctx, resendAPIBase+"/emails", headers, payload, "email_sent", "Resend")
}

// sendGridAPIBase is overridden in tests via SetSendGridAPIBaseForTest.
var sendGridAPIBase = "https://api.sendgrid.com"

// SetSendGridAPIBaseForTest overrides the SendGrid API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetSendGridAPIBaseForTest(base string) {
	if base == "" {
		sendGridAPIBase = "https://api.sendgrid.com"
	} else {
		sendGridAPIBase = base
	}
}

func sendViaSendGrid(ctx context.Context, apiKey, from, to, subject, body string) (any, error) {
	fromName, fromEmail := parseEmailAddress(from)
	fromObj := map[string]any{"email": fromEmail}
	if fromName != "" {
		fromObj["name"] = fromName
	}
	payload := map[string]any{
		"personalizations": []map[string]any{{"to": []map[string]any{{"email": to}}}},
		"from":             fromObj,
		"subject":          subject,
		"content":          []map[string]any{{"type": "text/plain", "value": body}},
	}
	headers := map[string]string{"Authorization": "Bearer " + apiKey}
	return postJSON(ctx, sendGridAPIBase+"/v3/mail/send", headers, payload, "email_sent", "SendGrid")
}

// brevoAPIBase is overridden in tests via SetBrevoAPIBaseForTest.
var brevoAPIBase = "https://api.brevo.com"

// SetBrevoAPIBaseForTest overrides the Brevo API base URL. Call only from
// tests. Pass "" to reset to the real API.
func SetBrevoAPIBaseForTest(base string) {
	if base == "" {
		brevoAPIBase = "https://api.brevo.com"
	} else {
		brevoAPIBase = base
	}
}

func sendViaBrevo(ctx context.Context, apiKey, from, to, subject, body string) (any, error) {
	fromName, fromEmail := parseEmailAddress(from)
	senderObj := map[string]any{"email": fromEmail}
	if fromName != "" {
		senderObj["name"] = fromName
	}
	payload := map[string]any{
		"sender":      senderObj,
		"to":          []map[string]any{{"email": to}},
		"subject":     subject,
		"textContent": body,
	}
	headers := map[string]string{"api-key": apiKey}
	return postJSON(ctx, brevoAPIBase+"/v3/smtp/email", headers, payload, "email_sent", "Brevo")
}

// postmarkAPIBase is overridden in tests via SetPostmarkAPIBaseForTest.
var postmarkAPIBase = "https://api.postmarkapp.com"

// SetPostmarkAPIBaseForTest overrides the Postmark API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetPostmarkAPIBaseForTest(base string) {
	if base == "" {
		postmarkAPIBase = "https://api.postmarkapp.com"
	} else {
		postmarkAPIBase = base
	}
}

func sendViaPostmark(ctx context.Context, apiKey, from, to, subject, body string) (any, error) {
	payload := map[string]any{
		"From":     from,
		"To":       to,
		"Subject":  subject,
		"TextBody": body,
	}
	headers := map[string]string{"X-Postmark-Server-Token": apiKey}
	return postJSON(ctx, postmarkAPIBase+"/email", headers, payload, "email_sent", "Postmark")
}

// parseEmailAddress splits an RFC5322-style "Name <email>" string into name and
// email. Falls back to treating the whole string as the email when there's no
// angle-bracket form.
func parseEmailAddress(raw string) (name, email string) {
	raw = strings.TrimSpace(raw)
	if i := strings.LastIndexByte(raw, '<'); i >= 0 && strings.HasSuffix(raw, ">") {
		return strings.TrimSpace(raw[:i]), raw[i+1 : len(raw)-1]
	}
	return "", raw
}

// ParseEmailAddressForTest is a test-only exported wrapper for parseEmailAddress.
func ParseEmailAddressForTest(raw string) (name, email string) {
	return parseEmailAddress(raw)
}
