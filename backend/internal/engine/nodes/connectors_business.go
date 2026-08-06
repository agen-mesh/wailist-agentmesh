package nodes

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// postForm POSTs application/x-www-form-urlencoded, unlike postJSON --
// Twilio and Stripe's REST APIs both use form-encoded bodies, not JSON.
func postForm(ctx context.Context, target string, headers map[string]string, form url.Values, sentinel, serviceName string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", serviceName, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doAndCheck(req, sentinel, serviceName)
}

// twilioAPIBase is overridden in tests via SetTwilioAPIBaseForTest.
var twilioAPIBase = "https://api.twilio.com"

// SetTwilioAPIBaseForTest overrides the Twilio API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetTwilioAPIBaseForTest(base string) {
	if base == "" {
		twilioAPIBase = "https://api.twilio.com"
	} else {
		twilioAPIBase = base
	}
}

func sendTwilio(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	accountSID := secretVal(node, "twilioAccountSID")
	authToken := secretVal(node, "twilioAuthToken")
	if accountSID == "" || authToken == "" {
		return "twilio_skipped_no_credentials", ErrActionSkipped
	}
	to := configVal(node, "twilioTo", "")
	from := configVal(node, "twilioFrom", "")
	if to == "" || from == "" {
		return "twilio_skipped_missing_config", ErrActionSkipped
	}
	target := twilioAPIBase + "/2010-04-01/Accounts/" + url.PathEscape(accountSID) + "/Messages.json"
	form := url.Values{}
	form.Set("To", to)
	form.Set("From", from)
	form.Set("Body", resolveMessage(node, rc))
	return postForm(ctx, target, basicAuthHeader(accountSID, authToken), form, "twilio_sms_sent", "Twilio")
}

// stripeAPIBase is overridden in tests via SetStripeAPIBaseForTest.
var stripeAPIBase = "https://api.stripe.com"

// SetStripeAPIBaseForTest overrides the Stripe API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetStripeAPIBaseForTest(base string) {
	if base == "" {
		stripeAPIBase = "https://api.stripe.com"
	} else {
		stripeAPIBase = base
	}
}

// sendStripe creates a Customer -- Stripe's simplest, most broadly useful
// single-call operation, and (like sendMailchimp) treats the upstream
// message as an email address when no explicit one is configured.
func sendStripe(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "stripeSecretKey")
	if apiKey == "" {
		return "stripe_skipped_no_api_key", ErrActionSkipped
	}
	email := configVal(node, "stripeEmail", "")
	if email == "" {
		email = resolveMessage(node, rc)
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return "stripe_skipped_no_email", ErrActionSkipped
	}
	form := url.Values{}
	form.Set("email", email)
	if name := configVal(node, "stripeName", ""); name != "" {
		form.Set("name", name)
	}
	headers := map[string]string{"Authorization": "Bearer " + apiKey}
	return postForm(ctx, stripeAPIBase+"/v1/customers", headers, form, "stripe_customer_created", "Stripe")
}

// pagerDutyAPIBase is overridden in tests via SetPagerDutyAPIBaseForTest.
var pagerDutyAPIBase = "https://events.pagerduty.com"

// SetPagerDutyAPIBaseForTest overrides the PagerDuty Events API base URL.
// Call only from tests. Pass "" to reset to the real API.
func SetPagerDutyAPIBaseForTest(base string) {
	if base == "" {
		pagerDutyAPIBase = "https://events.pagerduty.com"
	} else {
		pagerDutyAPIBase = base
	}
}

// sendPagerDuty triggers an incident via the Events API v2 -- auth here is
// the integration's routing_key carried IN the JSON body, not a header,
// which is PagerDuty's real (if unusual) design for this endpoint.
func sendPagerDuty(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	routingKey := secretVal(node, "pagerdutyRoutingKey")
	if routingKey == "" {
		return "pagerduty_skipped_no_routing_key", ErrActionSkipped
	}
	msg := resolveMessage(node, rc)
	payload := map[string]any{
		"routing_key":  routingKey,
		"event_action": "trigger",
		"payload": map[string]any{
			"summary":  issueTitle(msg),
			"source":   "AgentMesh",
			"severity": configVal(node, "pagerdutySeverity", "info"),
		},
	}
	return postJSON(ctx, pagerDutyAPIBase+"/v2/enqueue", nil, payload, "pagerduty_incident_triggered", "PagerDuty")
}

// zendeskDomainPattern mirrors jiraDomainPattern (connectors_devtools.go) --
// the subdomain is user-supplied config interpolated directly into the
// request host, so it must be validated before use for the same reason.
var zendeskDomainPattern = jiraDomainPattern

// zendeskAPIBase is overridden in tests via SetZendeskAPIBaseForTest --
// normally "https://{subdomain}.zendesk.com" is built per-node, so the
// test override replaces the whole scheme+host and sendZendesk skips that
// construction when set. Mirrors sendJira's jiraAPIBase exactly.
var zendeskAPIBase = ""

// SetZendeskAPIBaseForTest overrides the Zendesk API base URL entirely
// (including scheme+host). Call only from tests. Pass "" to reset to the
// real https://{subdomain}.zendesk.com construction.
func SetZendeskAPIBaseForTest(base string) {
	zendeskAPIBase = base
}

func sendZendesk(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	email := configVal(node, "zendeskEmail", "")
	apiToken := secretVal(node, "zendeskAPIToken")
	subdomain := configVal(node, "zendeskSubdomain", "")
	if email == "" || apiToken == "" || subdomain == "" {
		return "zendesk_skipped_missing_config", ErrActionSkipped
	}
	if !zendeskDomainPattern.MatchString(subdomain) {
		return "zendesk_skipped_invalid_subdomain", ErrActionSkipped
	}
	msg := resolveMessage(node, rc)
	payload := map[string]any{
		"ticket": map[string]any{
			"subject": issueTitle(msg),
			"comment": map[string]any{"body": msg},
		},
	}
	base := zendeskAPIBase
	if base == "" {
		base = "https://" + subdomain + ".zendesk.com"
	}
	target := base + "/api/v2/tickets.json"
	// Zendesk's token-auth convention: username is "email/token", password
	// is the API token -- not the email/password pair the Basic-auth name
	// might suggest.
	headers := basicAuthHeader(email+"/token", apiToken)
	return postJSON(ctx, target, headers, payload, "zendesk_ticket_created", "Zendesk")
}

// intercomAPIBase is overridden in tests via SetIntercomAPIBaseForTest.
var intercomAPIBase = "https://api.intercom.io"

// SetIntercomAPIBaseForTest overrides the Intercom API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetIntercomAPIBaseForTest(base string) {
	if base == "" {
		intercomAPIBase = "https://api.intercom.io"
	} else {
		intercomAPIBase = base
	}
}

// sendIntercom creates a lead Contact -- same "message as email" fallback
// as sendStripe/sendMailchimp, and the simplest Intercom operation that
// doesn't need a pre-existing contact/admin id to already be known.
func sendIntercom(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "intercomAccessToken")
	if apiKey == "" {
		return "intercom_skipped_no_access_token", ErrActionSkipped
	}
	email := configVal(node, "intercomEmail", "")
	if email == "" {
		email = resolveMessage(node, rc)
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return "intercom_skipped_no_email", ErrActionSkipped
	}
	payload := map[string]any{"role": "lead", "email": email}
	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Accept":        "application/json",
	}
	return postJSON(ctx, intercomAPIBase+"/contacts", headers, payload, "intercom_lead_created", "Intercom")
}

// openWeatherAPIBase is overridden in tests via SetOpenWeatherAPIBaseForTest.
var openWeatherAPIBase = "https://api.openweathermap.org"

// SetOpenWeatherAPIBaseForTest overrides the OpenWeatherMap API base URL.
// Call only from tests. Pass "" to reset to the real API.
func SetOpenWeatherAPIBaseForTest(base string) {
	if base == "" {
		openWeatherAPIBase = "https://api.openweathermap.org"
	} else {
		openWeatherAPIBase = base
	}
}

// getOpenWeather is a read op (like getTelegramUpdates) -- the first of
// this batch that returns data rather than just confirming a side effect.
func getOpenWeather(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "openWeatherAPIKey")
	if apiKey == "" {
		return "weather_skipped_no_api_key", ErrActionSkipped
	}
	city := configVal(node, "weatherCity", "")
	if city == "" {
		city = resolveMessage(node, rc)
	}
	city = strings.TrimSpace(city)
	if city == "" {
		return "weather_skipped_no_city", ErrActionSkipped
	}
	q := url.Values{}
	q.Set("q", city)
	q.Set("appid", apiKey)
	q.Set("units", configVal(node, "weatherUnits", "metric"))
	target := openWeatherAPIBase + "/data/2.5/weather?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("OpenWeatherMap: build request: %w", err)
	}
	return getJSON(req, "OpenWeatherMap")
}

// calendlyAPIBase is overridden in tests via SetCalendlyAPIBaseForTest.
var calendlyAPIBase = "https://api.calendly.com"

// SetCalendlyAPIBaseForTest overrides the Calendly API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetCalendlyAPIBaseForTest(base string) {
	if base == "" {
		calendlyAPIBase = "https://api.calendly.com"
	} else {
		calendlyAPIBase = base
	}
}

// getCalendlyEvents is a read op: lists this account's scheduled events.
func getCalendlyEvents(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "calendlyAccessToken")
	if token == "" {
		return "calendly_skipped_no_access_token", ErrActionSkipped
	}
	userURI := configVal(node, "calendlyUserURI", "")
	if userURI == "" {
		return "calendly_skipped_no_user_uri", ErrActionSkipped
	}
	q := url.Values{}
	q.Set("user", userURI)
	q.Set("count", configVal(node, "calendlyCount", "10"))
	target := calendlyAPIBase + "/scheduled_events?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("Calendly: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return getJSON(req, "Calendly")
}

// shopifyAPIBase is overridden in tests via SetShopifyAPIBaseForTest --
// normally "https://{shop}" is built per-node (shopifyShopDomain is already
// a full host like "mystore.myshopify.com"), so the test override replaces
// the "https://" + shop prefix entirely, letting a test point at a plain
// http:// httptest server. Mirrors sendJira/sendZendesk's same-shaped fix.
var shopifyAPIBase = ""

// SetShopifyAPIBaseForTest overrides the Shopify API base URL entirely
// (including scheme+host). Call only from tests. Pass "" to reset to the
// real "https://" + shopifyShopDomain construction.
func SetShopifyAPIBaseForTest(base string) {
	shopifyAPIBase = base
}

func sendShopify(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	accessToken := secretVal(node, "shopifyAccessToken")
	if accessToken == "" {
		return "shopify_skipped_no_access_token", ErrActionSkipped
	}
	shop := configVal(node, "shopifyShopDomain", "")
	orderID := configVal(node, "shopifyOrderID", "")
	if shop == "" || orderID == "" {
		return "shopify_skipped_missing_config", ErrActionSkipped
	}
	base := shopifyAPIBase
	if base == "" {
		base = "https://" + shop
	}
	target := base + "/admin/api/2024-01/orders/" + url.PathEscape(orderID) + ".json"
	payload := map[string]any{
		"order": map[string]any{"id": orderID, "note": resolveMessage(node, rc)},
	}
	req, err := newJSONRequest(ctx, http.MethodPut, target, map[string]string{"X-Shopify-Access-Token": accessToken}, payload)
	if err != nil {
		return nil, fmt.Errorf("Shopify: %w", err)
	}
	return doAndCheck(req, "shopify_order_note_added", "Shopify")
}

// baserowAPIBase is overridden in tests via SetBaserowAPIBaseForTest.
var baserowAPIBase = "https://api.baserow.io"

// SetBaserowAPIBaseForTest overrides the Baserow API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetBaserowAPIBaseForTest(base string) {
	if base == "" {
		baserowAPIBase = "https://api.baserow.io"
	} else {
		baserowAPIBase = base
	}
}

func sendBaserow(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "baserowAPIToken")
	if token == "" {
		return "baserow_skipped_no_api_token", ErrActionSkipped
	}
	tableID := configVal(node, "baserowTableID", "")
	if tableID == "" {
		return "baserow_skipped_no_table_id", ErrActionSkipped
	}
	fieldName := configVal(node, "baserowFieldName", "Notes")
	target := baserowAPIBase + "/api/database/rows/table/" + url.PathEscape(tableID) + "/?user_field_names=true"
	payload := map[string]any{fieldName: resolveMessage(node, rc)}
	// Baserow's own auth scheme: "Authorization: Token <api_token>", not
	// the Bearer prefix most of the rest of these connectors use.
	headers := map[string]string{"Authorization": "Token " + token}
	return postJSON(ctx, target, headers, payload, "baserow_row_created", "Baserow")
}
