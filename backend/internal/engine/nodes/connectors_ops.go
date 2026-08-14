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
