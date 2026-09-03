package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const stripeAPIBaseURL = "https://api.stripe.com"

// stripeWebhookTolerance bounds how old a signed webhook may be. Stripe's own
// libraries default to 5 minutes. Without this check a signature stays valid
// forever, so anyone who captured one delivery could replay it indefinitely --
// the ledger's idempotency would absorb a replay of the *same* order, but the
// timestamp is what makes the signature itself expire.
const stripeWebhookTolerance = 5 * time.Minute

// StripeClient talks to Stripe's REST API directly, with no SDK dependency --
// consistent with CashfreeClient and NOWPaymentsClient. It creates hosted
// Checkout Sessions, so no card data ever touches this codebase.
//
// There is no separate sandbox base URL: Stripe distinguishes live from test
// purely by which secret key is in use (sk_test_ vs sk_live_), so pointing at
// the test environment is a matter of configuration, not a UseSandbox call.
type StripeClient struct {
	SecretKey     string
	WebhookSecret string
	baseURL       string
	client        *http.Client
}

func NewStripeClient(secretKey, webhookSecret string) *StripeClient {
	return &StripeClient{
		SecretKey:     secretKey,
		WebhookSecret: webhookSecret,
		baseURL:       stripeAPIBaseURL,
		client:        &http.Client{Timeout: 10 * time.Second},
	}
}

// SetBaseURLForTest points the client at a test server. Call with "" to reset.
func (c *StripeClient) SetBaseURLForTest(u string) {
	if u == "" {
		c.baseURL = stripeAPIBaseURL
	} else {
		c.baseURL = u
	}
}

// CheckoutSession is the subset of Stripe's Checkout Session the caller needs:
// where to send the browser, and the id to reconcile against later.
type CheckoutSession struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// CreateCheckoutSession opens a hosted Stripe Checkout page for amountUSDCents.
//
// orderID is passed as BOTH client_reference_id and metadata[order_id]. Stripe
// echoes client_reference_id back on the session object in every webhook event
// for this session, which is how the webhook finds the credit_ledger row.
// metadata is the belt-and-braces copy: it survives onto the PaymentIntent,
// so a manual reconciliation from the Stripe dashboard can still recover which
// ledger row a payment belonged to.
func (c *StripeClient) CreateCheckoutSession(ctx context.Context, amountUSDCents int64, orderID, customerEmail, successURL, cancelURL string) (CheckoutSession, error) {
	var session CheckoutSession
	if amountUSDCents < 50 {
		// Stripe rejects anything under $0.50 for USD outright.
		return session, fmt.Errorf("stripe: amount must be at least 50 cents, got %d", amountUSDCents)
	}

	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("client_reference_id", orderID)
	form.Set("metadata[order_id]", orderID)
	form.Set("success_url", successURL)
	form.Set("cancel_url", cancelURL)
	form.Set("line_items[0][quantity]", "1")
	form.Set("line_items[0][price_data][currency]", "usd")
	form.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(amountUSDCents, 10))
	form.Set("line_items[0][price_data][product_data][name]", "AgentMesh credits")
	if customerEmail != "" {
		form.Set("customer_email", customerEmail)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/checkout/sessions", strings.NewReader(form.Encode()))
	if err != nil {
		return session, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+c.SecretKey)
	// Stripe deduplicates retries of the same idempotency key for 24h. Our
	// orderID is a fresh UUID per ledger row, so a retried create can never
	// open a second payable session against one row.
	req.Header.Set("Idempotency-Key", orderID)

	resp, err := c.client.Do(req)
	if err != nil {
		return session, fmt.Errorf("stripe: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusUnauthorized {
		return session, fmt.Errorf("stripe: authentication failed")
	}
	if resp.StatusCode != http.StatusOK {
		return session, fmt.Errorf("stripe: checkout session create failed with status %d: %s", resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &session); err != nil {
		return session, fmt.Errorf("stripe: parse session response: %w", err)
	}
	if session.URL == "" {
		return session, fmt.Errorf("stripe: session response carried no redirect url")
	}
	return session, nil
}

// GetCheckoutSessionStatus fetches a session server-to-server and reports its
// payment_status. Used by the client-side return path, which cannot be trusted
// to report its own success -- exactly as VerifyCashfreePayment does.
func (c *StripeClient) GetCheckoutSessionStatus(ctx context.Context, sessionID string) (paymentStatus, paymentIntentID, clientReferenceID string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/checkout/sessions/"+url.PathEscape(sessionID), nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.SecretKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("stripe: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("stripe: session fetch failed with status %d: %s", resp.StatusCode, body)
	}
	// client_reference_id is the credit_ledger order id set at creation. It is
	// returned so the caller can prove this session belongs to the order it is
	// about to credit -- without that binding, a caller could pair any paid
	// session with any pending order. See VerifyStripePayment.
	var parsed struct {
		PaymentStatus     string `json:"payment_status"`
		PaymentIntent     any    `json:"payment_intent"`
		ClientReferenceID string `json:"client_reference_id"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", "", fmt.Errorf("stripe: parse session response: %w", err)
	}
	// payment_intent is a bare id string unless the caller expanded it, in
	// which case it arrives as an object. Accept either rather than failing
	// on a shape difference that does not change what we need from it.
	switch pi := parsed.PaymentIntent.(type) {
	case string:
		paymentIntentID = pi
	case map[string]any:
		if id, ok := pi["id"].(string); ok {
			paymentIntentID = id
		}
	}
	return parsed.PaymentStatus, paymentIntentID, parsed.ClientReferenceID, nil
}

// VerifyWebhookSignature checks the Stripe-Signature header, which is a
// comma-separated list of key=value pairs: a `t` timestamp and one or more
// `v1` HMAC-SHA256 signatures over "<t>.<rawBody>", hex-encoded, keyed with
// the endpoint's signing secret.
//
// More than one v1 can be present while a secret is being rotated, so every
// candidate is checked and any match accepts. The timestamp is compared
// against `now` within stripeWebhookTolerance in BOTH directions -- a
// far-future timestamp is as much a sign of a forged header as a stale one.
func (c *StripeClient) VerifyWebhookSignature(body []byte, header string, now time.Time) bool {
	if c.WebhookSecret == "" || header == "" {
		return false
	}

	var timestamp string
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch key {
		case "t":
			timestamp = value
		case "v1":
			signatures = append(signatures, value)
		}
	}
	if timestamp == "" || len(signatures) == 0 {
		return false
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if delta := now.Sub(time.Unix(ts, 0)); delta > stripeWebhookTolerance || delta < -stripeWebhookTolerance {
		return false
	}

	mac := hmac.New(sha256.New, []byte(c.WebhookSecret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	for _, sig := range signatures {
		if hmac.Equal([]byte(expected), []byte(sig)) {
			return true
		}
	}
	return false
}
