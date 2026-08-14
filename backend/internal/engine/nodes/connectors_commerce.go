package nodes

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// stripeAPIBase is overridden in tests via SetStripeAPIBaseForTest.
var stripeAPIBase = "https://api.stripe.com"

// SetStripeAPIBaseForTest overrides the Stripe API base URL. Call only from
// tests. Pass "" to reset to the real API.
func SetStripeAPIBaseForTest(base string) {
	if base == "" {
		stripeAPIBase = "https://api.stripe.com"
	} else {
		stripeAPIBase = base
	}
}

// sendStripe creates a Stripe customer, using the run output as the
// description. Stripe's API takes form encoding, not JSON, so this builds the
// request directly rather than going through postJSON.
func sendStripe(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "stripeAPIKey")
	if apiKey == "" {
		return "stripe_skipped_no_api_key", ErrActionSkipped
	}
	email := resolveTemplate(configVal(node, "stripeEmail", ""), rc)
	if email == "" {
		return "stripe_skipped_no_email", ErrActionSkipped
	}
	form := url.Values{}
	form.Set("email", email)
	form.Set("description", rc.Message())
	if name := resolveTemplate(configVal(node, "stripeName", ""), rc); name != "" {
		form.Set("name", name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		stripeAPIBase+"/v1/customers", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return doAndCheck(req, "stripe_customer_created", "Stripe")
}

// shopifyAPIBase is overridden in tests via SetShopifyAPIBaseForTest.
// Normally "https://{store}.myshopify.com" is built per-node, so the test
// override replaces the whole scheme+host.
var shopifyAPIBase = ""

// SetShopifyAPIBaseForTest overrides the Shopify API base URL entirely.
// Call only from tests. Pass "" to reset to the real per-store host.
func SetShopifyAPIBaseForTest(base string) { shopifyAPIBase = base }

// shopifyAPIVersion pins the Admin API version. Shopify dates its API and
// removes versions after ~12 months — bumping this is a deliberate, tested
// change, not something to leave floating.
const shopifyAPIVersion = "2024-10"

// sendShopify creates a Shopify customer with the run output as the note.
func sendShopify(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "shopifyAccessToken")
	if token == "" {
		return "shopify_skipped_no_access_token", ErrActionSkipped
	}
	store := resolveTemplate(configVal(node, "shopifyStore", ""), rc)
	email := resolveTemplate(configVal(node, "shopifyEmail", ""), rc)
	if store == "" || email == "" {
		return "shopify_skipped_missing_config", ErrActionSkipped
	}
	base := shopifyAPIBase
	if base == "" {
		base = "https://" + url.PathEscape(store) + ".myshopify.com"
	}
	payload := map[string]any{"customer": map[string]any{
		"email": email,
		"note":  rc.Message(),
	}}
	headers := map[string]string{"X-Shopify-Access-Token": token}
	return postJSON(ctx, base+"/admin/api/"+shopifyAPIVersion+"/customers.json",
		headers, payload, "shopify_customer_created", "Shopify")
}

// pipedriveAPIBase is overridden in tests via SetPipedriveAPIBaseForTest.
var pipedriveAPIBase = ""

// SetPipedriveAPIBaseForTest overrides the Pipedrive API base URL entirely.
// Call only from tests. Pass "" to reset to the real per-company host.
func SetPipedriveAPIBaseForTest(base string) { pipedriveAPIBase = base }

// sendPipedrive logs a CRM note with the run output. Pipedrive takes its API
// token as a query parameter rather than a header.
func sendPipedrive(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := secretVal(node, "pipedriveAPIToken")
	if token == "" {
		return "pipedrive_skipped_no_api_token", ErrActionSkipped
	}
	base := pipedriveAPIBase
	if base == "" {
		domain := configVal(node, "pipedriveCompanyDomain", "")
		if domain == "" {
			return "pipedrive_skipped_missing_config", ErrActionSkipped
		}
		base = "https://" + url.PathEscape(domain) + ".pipedrive.com"
	}
	payload := map[string]any{"content": rc.Message()}
	if dealID := resolveTemplate(configVal(node, "pipedriveDealID", ""), rc); dealID != "" {
		payload["deal_id"] = dealID
	}
	if personID := resolveTemplate(configVal(node, "pipedrivePersonID", ""), rc); personID != "" {
		payload["person_id"] = personID
	}
	target := base + "/api/v1/notes?api_token=" + url.QueryEscape(token)
	return postJSON(ctx, target, nil, payload, "pipedrive_note_created", "Pipedrive")
}
