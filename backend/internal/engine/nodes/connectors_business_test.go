package nodes_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestTwilioAction_SendsSMSWithBasicAuth(t *testing.T) {
	var gotAuth string
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		r.ParseForm()
		gotForm = r.Form
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	nodes.SetTwilioAPIBaseForTest(srv.URL)
	defer nodes.SetTwilioAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "tw1", Type: models.NodeTypeAction, Template: "twilio",
		Secrets: map[string]string{"twilioAccountSID": "AC123", "twilioAuthToken": "tok"},
		Config:  map[string]string{"twilioTo": "+15551234567", "twilioFrom": "+15559876543"},
	}
	rc := engine.NewRunContext("r1", []byte(`"build finished"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "twilio_sms_sent" {
		t.Errorf("want sentinel, got %v", result)
	}
	if gotAuth == "" {
		t.Error("want basic auth header set")
	}
	if gotForm.Get("To") != "+15551234567" || gotForm.Get("Body") != "build finished" {
		t.Errorf("want To/Body form fields, got %v", gotForm)
	}
}

func TestTwilioAction_SkipsWhenNoCredentials(t *testing.T) {
	node := models.WorkflowNode{ID: "tw2", Type: models.NodeTypeAction, Template: "twilio"}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "twilio_skipped_no_credentials" {
		t.Errorf("want skip sentinel, got %v / %v", result, err)
	}
}

func TestStripeAction_CreatesCustomerFromMessageEmail(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotForm = r.Form
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	nodes.SetStripeAPIBaseForTest(srv.URL)
	defer nodes.SetStripeAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "st1", Type: models.NodeTypeAction, Template: "stripe",
		Secrets: map[string]string{"stripeSecretKey": "sk_test_123"},
	}
	rc := engine.NewRunContext("r1", []byte(`"customer@example.com"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "stripe_customer_created" {
		t.Errorf("want sentinel, got %v", result)
	}
	if gotForm.Get("email") != "customer@example.com" {
		t.Errorf("want email from message, got %v", gotForm.Get("email"))
	}
}

func TestStripeAction_SkipsWhenNoAPIKey(t *testing.T) {
	node := models.WorkflowNode{ID: "st2", Type: models.NodeTypeAction, Template: "stripe"}
	rc := engine.NewRunContext("r1", []byte(`"x@example.com"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "stripe_skipped_no_api_key" {
		t.Errorf("want skip sentinel, got %v / %v", result, err)
	}
}

func TestPagerDutyAction_TriggersIncidentWithRoutingKeyInBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	nodes.SetPagerDutyAPIBaseForTest(srv.URL)
	defer nodes.SetPagerDutyAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "pd1", Type: models.NodeTypeAction, Template: "pagerduty",
		Secrets: map[string]string{"pagerdutyRoutingKey": "rk_123"},
	}
	rc := engine.NewRunContext("r1", []byte(`"database is down"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "pagerduty_incident_triggered" {
		t.Errorf("want sentinel, got %v", result)
	}
	if gotBody["routing_key"] != "rk_123" || gotBody["event_action"] != "trigger" {
		t.Errorf("want routing_key/event_action in body, got %v", gotBody)
	}
	payload, ok := gotBody["payload"].(map[string]any)
	if !ok || payload["summary"] != "database is down" {
		t.Errorf("want summary in nested payload, got %v", gotBody["payload"])
	}
}

func TestPagerDutyAction_SkipsWhenNoRoutingKey(t *testing.T) {
	node := models.WorkflowNode{ID: "pd2", Type: models.NodeTypeAction, Template: "pagerduty"}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "pagerduty_skipped_no_routing_key" {
		t.Errorf("want skip sentinel, got %v / %v", result, err)
	}
}

func TestZendeskAction_CreatesTicketWithTokenAuth(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	nodes.SetZendeskAPIBaseForTest(srv.URL)
	defer nodes.SetZendeskAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "zd1", Type: models.NodeTypeAction, Template: "zendesk",
		Secrets: map[string]string{"zendeskAPIToken": "tok"},
		Config:  map[string]string{"zendeskEmail": "agent@example.com", "zendeskSubdomain": "mycompany"},
	}
	rc := engine.NewRunContext("r1", []byte(`"help me"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "zendesk_ticket_created" {
		t.Errorf("want sentinel, got %v", result)
	}
	if gotAuth == "" {
		t.Error("want basic auth header set")
	}
	ticket, ok := gotBody["ticket"].(map[string]any)
	if !ok {
		t.Fatalf("want a ticket object, got %v", gotBody)
	}
	comment, ok := ticket["comment"].(map[string]any)
	if !ok || comment["body"] != "help me" {
		t.Errorf("want comment body from message, got %v", ticket["comment"])
	}
}

func TestZendeskAction_SkipsOnInvalidSubdomain(t *testing.T) {
	node := models.WorkflowNode{
		ID: "zd3", Type: models.NodeTypeAction, Template: "zendesk",
		Secrets: map[string]string{"zendeskAPIToken": "tok"},
		Config:  map[string]string{"zendeskEmail": "agent@example.com", "zendeskSubdomain": "bad/subdomain!"},
	}
	rc := engine.NewRunContext("r1", []byte(`"help me"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "zendesk_skipped_invalid_subdomain" {
		t.Errorf("want invalid-subdomain skip, got %v / %v", result, err)
	}
}

func TestZendeskAction_SkipsWhenMissingConfig(t *testing.T) {
	node := models.WorkflowNode{ID: "zd2", Type: models.NodeTypeAction, Template: "zendesk"}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "zendesk_skipped_missing_config" {
		t.Errorf("want skip sentinel, got %v / %v", result, err)
	}
}

func TestIntercomAction_CreatesLeadFromMessageEmail(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	nodes.SetIntercomAPIBaseForTest(srv.URL)
	defer nodes.SetIntercomAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "ic1", Type: models.NodeTypeAction, Template: "intercom",
		Secrets: map[string]string{"intercomAccessToken": "tok"},
	}
	rc := engine.NewRunContext("r1", []byte(`"lead@example.com"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "intercom_lead_created" {
		t.Errorf("want sentinel, got %v", result)
	}
	if gotBody["email"] != "lead@example.com" || gotBody["role"] != "lead" {
		t.Errorf("want email/role in body, got %v", gotBody)
	}
}

func TestIntercomAction_SkipsWhenNoAccessToken(t *testing.T) {
	node := models.WorkflowNode{ID: "ic2", Type: models.NodeTypeAction, Template: "intercom"}
	rc := engine.NewRunContext("r1", []byte(`"x@example.com"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "intercom_skipped_no_access_token" {
		t.Errorf("want skip sentinel, got %v / %v", result, err)
	}
}

func TestOpenWeatherMapAction_ReturnsDecodedWeather(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"weather":[{"main":"Clear"}],"main":{"temp":22.5}}`))
	}))
	defer srv.Close()
	nodes.SetOpenWeatherAPIBaseForTest(srv.URL)
	defer nodes.SetOpenWeatherAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "ow1", Type: models.NodeTypeAction, Template: "openweathermap",
		Secrets: map[string]string{"openWeatherAPIKey": "key123"},
		Config:  map[string]string{"weatherCity": "London"},
	}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("q") != "London" || gotQuery.Get("appid") != "key123" {
		t.Errorf("want city/key query params, got %v", gotQuery)
	}
	m, ok := result.(map[string]any)
	if !ok || m["weather"] == nil {
		t.Errorf("want decoded weather response, got %v", result)
	}
}

func TestOpenWeatherMapAction_FallsBackToMessageAsCity(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	nodes.SetOpenWeatherAPIBaseForTest(srv.URL)
	defer nodes.SetOpenWeatherAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "ow2", Type: models.NodeTypeAction, Template: "openweathermap",
		Secrets: map[string]string{"openWeatherAPIKey": "key123"},
	}
	rc := engine.NewRunContext("r1", []byte(`"Tokyo"`))
	if _, err := nodes.ExecuteAction(context.Background(), node, rc); err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("q") != "Tokyo" {
		t.Errorf("want message used as city fallback, got %v", gotQuery.Get("q"))
	}
}

func TestOpenWeatherMapAction_SkipsWhenNoAPIKey(t *testing.T) {
	node := models.WorkflowNode{ID: "ow3", Type: models.NodeTypeAction, Template: "openweathermap"}
	rc := engine.NewRunContext("r1", []byte(`"Paris"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "weather_skipped_no_api_key" {
		t.Errorf("want skip sentinel, got %v / %v", result, err)
	}
}

func TestCalendlyAction_ReturnsDecodedEvents(t *testing.T) {
	var gotAuth string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.Query()
		w.Write([]byte(`{"collection":[{"name":"1:1 call"}]}`))
	}))
	defer srv.Close()
	nodes.SetCalendlyAPIBaseForTest(srv.URL)
	defer nodes.SetCalendlyAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "cl1", Type: models.NodeTypeAction, Template: "calendly",
		Secrets: map[string]string{"calendlyAccessToken": "tok"},
		Config:  map[string]string{"calendlyUserURI": "https://api.calendly.com/users/abc"},
	}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("want bearer auth, got %q", gotAuth)
	}
	if gotQuery.Get("user") != "https://api.calendly.com/users/abc" {
		t.Errorf("want user uri query param, got %v", gotQuery.Get("user"))
	}
	m, ok := result.(map[string]any)
	if !ok || m["collection"] == nil {
		t.Errorf("want decoded events collection, got %v", result)
	}
}

func TestCalendlyAction_SkipsWhenNoUserURI(t *testing.T) {
	node := models.WorkflowNode{
		ID: "cl2", Type: models.NodeTypeAction, Template: "calendly",
		Secrets: map[string]string{"calendlyAccessToken": "tok"},
	}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "calendly_skipped_no_user_uri" {
		t.Errorf("want skip sentinel, got %v / %v", result, err)
	}
}

func TestShopifyAction_AddsOrderNoteWithAccessTokenHeader(t *testing.T) {
	var gotToken string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Shopify-Access-Token")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	nodes.SetShopifyAPIBaseForTest(srv.URL)
	defer nodes.SetShopifyAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "sh3", Type: models.NodeTypeAction, Template: "shopify",
		Secrets: map[string]string{"shopifyAccessToken": "shpat_123"},
		Config:  map[string]string{"shopifyShopDomain": "mystore.myshopify.com", "shopifyOrderID": "9001"},
	}
	rc := engine.NewRunContext("r1", []byte(`"refunded per customer request"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "shopify_order_note_added" {
		t.Errorf("want sentinel, got %v", result)
	}
	if gotToken != "shpat_123" {
		t.Errorf("want access token header, got %q", gotToken)
	}
	order, ok := gotBody["order"].(map[string]any)
	if !ok || order["note"] != "refunded per customer request" {
		t.Errorf("want note from message, got %v", gotBody["order"])
	}
}

func TestShopifyAction_SkipsWhenMissingConfig(t *testing.T) {
	node := models.WorkflowNode{
		ID: "sh1", Type: models.NodeTypeAction, Template: "shopify",
		Secrets: map[string]string{"shopifyAccessToken": "tok"},
	}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "shopify_skipped_missing_config" {
		t.Errorf("want skip sentinel, got %v / %v", result, err)
	}
}

func TestShopifyAction_SkipsWhenNoAccessToken(t *testing.T) {
	node := models.WorkflowNode{ID: "sh2", Type: models.NodeTypeAction, Template: "shopify"}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "shopify_skipped_no_access_token" {
		t.Errorf("want skip sentinel, got %v / %v", result, err)
	}
}

func TestBaserowAction_CreatesRowWithTokenAuth(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	nodes.SetBaserowAPIBaseForTest(srv.URL)
	defer nodes.SetBaserowAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "br1", Type: models.NodeTypeAction, Template: "baserow",
		Secrets: map[string]string{"baserowAPIToken": "tok"},
		Config:  map[string]string{"baserowTableID": "42", "baserowFieldName": "Summary"},
	}
	rc := engine.NewRunContext("r1", []byte(`"row content"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "baserow_row_created" {
		t.Errorf("want sentinel, got %v", result)
	}
	if gotAuth != "Token tok" {
		t.Errorf("want Baserow's own 'Token' auth scheme, got %q", gotAuth)
	}
	if gotBody["Summary"] != "row content" {
		t.Errorf("want configured field name used, got %v", gotBody)
	}
}

func TestBaserowAction_SkipsWhenNoTableID(t *testing.T) {
	node := models.WorkflowNode{
		ID: "br2", Type: models.NodeTypeAction, Template: "baserow",
		Secrets: map[string]string{"baserowAPIToken": "tok"},
	}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if !errors.Is(err, nodes.ErrActionSkipped) || result != "baserow_skipped_no_table_id" {
		t.Errorf("want skip sentinel, got %v / %v", result, err)
	}
}
