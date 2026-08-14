package nodes

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// twilioAPIBase is overridden in tests via SetTwilioAPIBaseForTest.
var twilioAPIBase = "https://api.twilio.com/2010-04-01"

// SetTwilioAPIBaseForTest overrides the Twilio API base URL. Call only from
// tests. Pass "" to reset to the real API.
func SetTwilioAPIBaseForTest(base string) {
	if base == "" {
		twilioAPIBase = "https://api.twilio.com/2010-04-01"
	} else {
		twilioAPIBase = base
	}
}

// sendTwilio sends the run output as an SMS. Twilio uses HTTP Basic auth
// (Account SID / Auth Token) and form encoding.
func sendTwilio(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "twilioAuthToken")
	if token == "" {
		return "twilio_skipped_no_auth_token", ErrActionSkipped
	}
	sid := configVal(node, "twilioAccountSID", "")
	if sid == "" {
		return "twilio_skipped_no_account_sid", ErrActionSkipped
	}
	to := resolveTemplate(configVal(node, "twilioTo", ""), rc)
	if to == "" {
		return "twilio_skipped_no_recipient", ErrActionSkipped
	}
	from := configVal(node, "twilioFrom", "")
	if from == "" {
		return "twilio_skipped_no_sender", ErrActionSkipped
	}
	form := url.Values{}
	form.Set("To", to)
	form.Set("From", from)
	form.Set("Body", rc.Message())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		twilioAPIBase+"/Accounts/"+url.PathEscape(sid)+"/Messages.json",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(sid, token)
	return doAndCheck(req, "twilio_sms_sent", "Twilio")
}

// sendMattermost posts the run output to a Mattermost incoming webhook. The
// webhook URL is the credential — there is no separate token.
func sendMattermost(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	hookURL := secretVal(node, "mattermostWebhookURL")
	if hookURL == "" {
		return "mattermost_skipped_no_webhook_url", ErrActionSkipped
	}
	payload := map[string]any{"text": rc.Message()}
	if ch := configVal(node, "mattermostChannel", ""); ch != "" {
		payload["channel"] = ch
	}
	if user := configVal(node, "mattermostUsername", ""); user != "" {
		payload["username"] = user
	}
	return postJSON(ctx, hookURL, nil, payload, "mattermost_sent", "Mattermost")
}

// pagerdutyAPIBase is overridden in tests via SetPagerDutyAPIBaseForTest.
var pagerdutyAPIBase = "https://events.pagerduty.com"

// SetPagerDutyAPIBaseForTest overrides the PagerDuty Events API base URL.
// Call only from tests. Pass "" to reset to the real API.
func SetPagerDutyAPIBaseForTest(base string) {
	if base == "" {
		pagerdutyAPIBase = "https://events.pagerduty.com"
	} else {
		pagerdutyAPIBase = base
	}
}

// sendPagerDuty triggers a PagerDuty Events API v2 alert with the run output
// as the incident summary.
func sendPagerDuty(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	routingKey := secretVal(node, "pagerdutyRoutingKey")
	if routingKey == "" {
		return "pagerduty_skipped_no_routing_key", ErrActionSkipped
	}
	payload := map[string]any{
		"routing_key":  routingKey,
		"event_action": "trigger",
		"payload": map[string]any{
			"summary":  rc.Message(),
			"severity": configVal(node, "pagerdutySeverity", "error"),
			"source":   configVal(node, "pagerdutySource", "agentmesh"),
		},
	}
	return postJSON(ctx, pagerdutyAPIBase+"/v2/enqueue", nil, payload,
		"pagerduty_event_triggered", "PagerDuty")
}
