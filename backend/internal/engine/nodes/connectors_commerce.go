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
