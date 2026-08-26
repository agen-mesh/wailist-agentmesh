package nodes

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
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

// apiBaseDefaults holds each connector's real API base URL, keyed by
// service name -- "" for Zendesk/Shopify, whose base is built per-node from
// user-supplied subdomain/shop config rather than a fixed host. apiBases
// holds the current (possibly test-overridden) value, seeded from the
// defaults. Together with apiBase/setAPIBaseForTest below, this replaces
// what would otherwise be nine hand-written var + SetXAPIBaseForTest pairs,
// one per connector.
var apiBaseDefaults = map[string]string{
	"twilio":      "https://api.twilio.com",
	"stripe":      "https://api.stripe.com",
	"pagerduty":   "https://events.pagerduty.com",
	"zendesk":     "",
	"intercom":    "https://api.intercom.io",
	"openweather": "https://api.openweathermap.org",
	"calendly":    "https://api.calendly.com",
	"shopify":     "",
	"baserow":     "https://api.baserow.io",
}

var apiBases = cloneAPIBaseDefaults()

func cloneAPIBaseDefaults() map[string]string {
	m := make(map[string]string, len(apiBaseDefaults))
	for k, v := range apiBaseDefaults {
		m[k] = v
	}
	return m
}

// apiBase returns service's current API base URL (real, or test-overridden).
func apiBase(service string) string {
	return apiBases[service]
}

// setAPIBaseForTest overrides service's API base URL, resetting to its real
// default when base is "". Backs every SetXAPIBaseForTest export below.
func setAPIBaseForTest(service, base string) {
	if base == "" {
		base = apiBaseDefaults[service]
	}
	apiBases[service] = base
}

// SetTwilioAPIBaseForTest overrides the Twilio API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetTwilioAPIBaseForTest(base string) { setAPIBaseForTest("twilio", base) }

// SetStripeAPIBaseForTest overrides the Stripe API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetStripeAPIBaseForTest(base string) { setAPIBaseForTest("stripe", base) }

// SetPagerDutyAPIBaseForTest overrides the PagerDuty Events API base URL.
// Call only from tests. Pass "" to reset to the real API.
func SetPagerDutyAPIBaseForTest(base string) { setAPIBaseForTest("pagerduty", base) }

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
	target := apiBase("twilio") + "/2010-04-01/Accounts/" + url.PathEscape(accountSID) + "/Messages.json"
	form := url.Values{}
	form.Set("To", to)
	form.Set("From", from)
	form.Set("Body", resolveMessage(node, rc))
	return postForm(ctx, target, basicAuthHeader(accountSID, authToken), form, "twilio_sms_sent", "Twilio")
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
	return postForm(ctx, apiBase("stripe")+"/v1/customers", headers, form, "stripe_customer_created", "Stripe")
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
	return postJSON(ctx, apiBase("pagerduty")+"/v2/enqueue", nil, payload, "pagerduty_incident_triggered", "PagerDuty")
}

// zendeskDomainPattern mirrors jiraDomainPattern (connectors_devtools.go) --
// the subdomain is user-supplied config interpolated directly into the
// request host, so it must be validated before use for the same reason.
var zendeskDomainPattern = jiraDomainPattern

// SetZendeskAPIBaseForTest overrides the Zendesk API base URL entirely
// (including scheme+host) -- normally "https://{subdomain}.zendesk.com" is
// built per-node, so the override replaces that whole construction and
// sendZendesk skips it when set. Call only from tests. Pass "" to reset to
// the real per-node construction.
func SetZendeskAPIBaseForTest(base string) { setAPIBaseForTest("zendesk", base) }

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
	base := apiBase("zendesk")
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

// SetIntercomAPIBaseForTest overrides the Intercom API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetIntercomAPIBaseForTest(base string) { setAPIBaseForTest("intercom", base) }

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
	return postJSON(ctx, apiBase("intercom")+"/contacts", headers, payload, "intercom_lead_created", "Intercom")
}

// SetOpenWeatherAPIBaseForTest overrides the OpenWeatherMap API base URL.
// Call only from tests. Pass "" to reset to the real API.
func SetOpenWeatherAPIBaseForTest(base string) { setAPIBaseForTest("openweather", base) }

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
	target := apiBase("openweather") + "/data/2.5/weather?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("OpenWeatherMap: build request: %w", err)
	}
	return getJSON(req, "OpenWeatherMap")
}

// SetCalendlyAPIBaseForTest overrides the Calendly API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetCalendlyAPIBaseForTest(base string) { setAPIBaseForTest("calendly", base) }

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
	target := apiBase("calendly") + "/scheduled_events?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("Calendly: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return getJSON(req, "Calendly")
}

// shopifyDomainPattern mirrors jiraDomainPattern's reasoning (connectors_
// devtools.go): shopifyShopDomain is user-supplied config interpolated
// directly into the request host (along with the real Shopify access
// token, via X-Shopify-Access-Token), so it must be validated before use --
// otherwise a crafted value on a copied/imported workflow could redirect
// the request, and the token with it, to an attacker-controlled host.
// Unlike Jira/Zendesk's bare subdomain, shopifyShopDomain is already a full
// host, so this anchors the whole string to the real "*.myshopify.com"
// shape rather than just checking character set.
var shopifyDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*\.myshopify\.com$`)

// SetShopifyAPIBaseForTest overrides the Shopify API base URL entirely
// (including scheme+host) -- normally "https://{shop}" is built per-node
// (shopifyShopDomain is already a full host like "mystore.myshopify.com"),
// so the override replaces that "https://" + shop prefix entirely, letting
// a test point at a plain http:// httptest server. Call only from tests.
// Pass "" to reset to the real per-node construction.
func SetShopifyAPIBaseForTest(base string) { setAPIBaseForTest("shopify", base) }

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
	if !shopifyDomainPattern.MatchString(shop) {
		return "shopify_skipped_invalid_domain", ErrActionSkipped
	}
	base := apiBase("shopify")
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

// SetBaserowAPIBaseForTest overrides the Baserow API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetBaserowAPIBaseForTest(base string) { setAPIBaseForTest("baserow", base) }

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
	target := apiBase("baserow") + "/api/database/rows/table/" + url.PathEscape(tableID) + "/?user_field_names=true"
	payload := map[string]any{fieldName: resolveMessage(node, rc)}
	// Baserow's own auth scheme: "Authorization: Token <api_token>", not
	// the Bearer prefix most of the rest of these connectors use.
	headers := map[string]string{"Authorization": "Token " + token}
	return postJSON(ctx, target, headers, payload, "baserow_row_created", "Baserow")
}
