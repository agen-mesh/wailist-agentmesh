package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	payPalLiveBaseURL    = "https://api-m.paypal.com"
	payPalSandboxBaseURL = "https://api-m.sandbox.paypal.com"
)

// PayPalClient talks to PayPal's Orders v2 REST API directly, with no SDK
// dependency -- consistent with the other clients in this package. The payer
// is sent to a PayPal-hosted approval page, so no card data touches this
// codebase.
type PayPalClient struct {
	ClientID     string
	ClientSecret string
	// WebhookID identifies the webhook subscription configured in the PayPal
	// dashboard. PayPal's signature-verification API requires it, and a
	// signature only verifies against the webhook it was actually sent for --
	// so this is not optional decoration, it is half the check.
	WebhookID string

	baseURL string
	client  *http.Client

	// PayPal issues bearer tokens valid for ~9 hours. Caching one avoids an
	// OAuth round trip on every single call; the mutex is what keeps a burst
	// of concurrent checkouts from stampeding the token endpoint.
	tokenMu     sync.Mutex
	token       string
	tokenExpiry time.Time
}

func NewPayPalClient(clientID, clientSecret, webhookID string) *PayPalClient {
	return &PayPalClient{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		WebhookID:    webhookID,
		baseURL:      payPalLiveBaseURL,
		client:       &http.Client{Timeout: 15 * time.Second},
	}
}

// UseSandbox points the client at PayPal's sandbox, which issues its own
// distinct client id/secret and webhook id. Call once after construction when
// PAYPAL_SANDBOX is set -- see cmd/server/main.go.
func (c *PayPalClient) UseSandbox() {
	c.baseURL = payPalSandboxBaseURL
}

// SetBaseURLForTest points the client at a test server. Call with "" to reset.
func (c *PayPalClient) SetBaseURLForTest(u string) {
	if u == "" {
		c.baseURL = payPalLiveBaseURL
	} else {
		c.baseURL = u
	}
	// A test server issues its own tokens; drop any cached real one so the
	// next call re-authenticates against the new base URL.
	c.tokenMu.Lock()
	c.token, c.tokenExpiry = "", time.Time{}
	c.tokenMu.Unlock()
}

// accessToken returns a cached bearer token, fetching a fresh one when the
// cache is empty or close to expiry. The 60-second margin means a token is
// never handed out so close to its deadline that it expires mid-request.
func (c *PayPalClient) accessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return c.token, nil
	}

	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.ClientID, c.ClientSecret)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("paypal: token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("paypal: authentication failed")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("paypal: token request failed with status %d: %s", resp.StatusCode, body)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("paypal: parse token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", fmt.Errorf("paypal: token response carried no access_token")
	}
	c.token = parsed.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	return c.token, nil
}

// PayPalOrder is what the caller needs from a created order: PayPal's own id,
// and the approval page to send the payer to.
type PayPalOrder struct {
	ID         string
	ApproveURL string
}

// CreateOrder opens a PayPal-hosted approval flow for amountUSDCents.
//
// orderID goes into custom_id (and reference_id), which PayPal echoes back on
// the capture resource in every webhook for this order -- that is how the
// webhook finds the credit_ledger row. reference_id alone would not do: it is
// absent from some capture payloads, whereas custom_id survives to the capture.
func (c *PayPalClient) CreateOrder(ctx context.Context, amountUSDCents int64, orderID, returnURL, cancelURL string) (PayPalOrder, error) {
	var out PayPalOrder
	if amountUSDCents < 100 {
		return out, fmt.Errorf("paypal: amount must be at least 100 cents, got %d", amountUSDCents)
	}

	payload := map[string]any{
		"intent": "CAPTURE",
		"purchase_units": []map[string]any{{
			"reference_id": orderID,
			"custom_id":    orderID,
			"description":  "AgentMesh credits",
			"amount": map[string]any{
				"currency_code": "USD",
				// PayPal requires a decimal string, not a number.
				"value": fmt.Sprintf("%d.%02d", amountUSDCents/100, amountUSDCents%100),
			},
		}},
		"payment_source": map[string]any{
			"paypal": map[string]any{
				"experience_context": map[string]any{
					"return_url": returnURL,
					"cancel_url": cancelURL,
					// The payer sees a "Pay Now" button and a final amount,
					// rather than a "Continue" that implies a later review step
					// this flow does not have.
					"user_action": "PAY_NOW",
				},
			},
		},
	}
	body, _ := json.Marshal(payload)

	respBody, err := c.do(ctx, http.MethodPost, "/v2/checkout/orders", body, orderID)
	if err != nil {
		return out, err
	}

	var parsed struct {
		ID    string `json:"id"`
		Links []struct {
			Href string `json:"href"`
			Rel  string `json:"rel"`
		} `json:"links"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return out, fmt.Errorf("paypal: parse order response: %w", err)
	}
	out.ID = parsed.ID
	// "payer-action" is what the experience_context form of the request
	// returns; "approve" is the older application_context spelling. Accept
	// either so a PayPal-side default change doesn't strand the checkout.
	for _, l := range parsed.Links {
		if l.Rel == "payer-action" || l.Rel == "approve" {
			out.ApproveURL = l.Href
			break
		}
	}
	if out.ID == "" || out.ApproveURL == "" {
		return out, fmt.Errorf("paypal: order response carried no id or approval link")
	}
	return out, nil
}

// CaptureOrder captures an approved order, returning PayPal's order status and
// the capture id. Treats HTTP 422 ORDER_ALREADY_CAPTURED as success: the payer
// double-clicking the return link, or a retry racing the webhook, must not read
// as a failure when the money did in fact move.
func (c *PayPalClient) CaptureOrder(ctx context.Context, payPalOrderID string) (status, captureID, customID string, err error) {
	respBody, err := c.do(ctx, http.MethodPost, "/v2/checkout/orders/"+url.PathEscape(payPalOrderID)+"/capture", []byte("{}"), "capture-"+payPalOrderID)
	if err != nil {
		if strings.Contains(err.Error(), "ORDER_ALREADY_CAPTURED") {
			// Re-read the order to recover the capture id from the first,
			// successful capture.
			return c.getOrderCapture(ctx, payPalOrderID)
		}
		return "", "", "", err
	}

	parsed, err := parsePayPalOrder(respBody)
	if err != nil {
		return "", "", "", err
	}
	return parsed.status, parsed.captureID, parsed.customID, nil
}

// getOrderCapture re-reads an order and pulls the completed capture off it.
func (c *PayPalClient) getOrderCapture(ctx context.Context, payPalOrderID string) (status, captureID, customID string, err error) {
	respBody, err := c.do(ctx, http.MethodGet, "/v2/checkout/orders/"+url.PathEscape(payPalOrderID), nil, "")
	if err != nil {
		return "", "", "", err
	}
	parsed, err := parsePayPalOrder(respBody)
	if err != nil {
		return "", "", "", err
	}
	return parsed.status, parsed.captureID, parsed.customID, nil
}

// payPalOrder is the handful of fields both the capture response and the
// order re-read carry. custom_id is our own credit_ledger order id, echoed
// back by PayPal -- it is what binds a capture to the row it may credit.
type payPalOrder struct {
	status    string
	captureID string
	customID  string
}

// parsePayPalOrder decodes the shape shared by CaptureOrder and
// getOrderCapture, which previously hand-rolled the same struct twice.
func parsePayPalOrder(body []byte) (payPalOrder, error) {
	var raw struct {
		Status        string `json:"status"`
		PurchaseUnits []struct {
			CustomID string `json:"custom_id"`
			Payments struct {
				Captures []struct {
					ID       string `json:"id"`
					Status   string `json:"status"`
					CustomID string `json:"custom_id"`
				} `json:"captures"`
			} `json:"payments"`
		} `json:"purchase_units"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return payPalOrder{}, fmt.Errorf("paypal: parse order response: %w", err)
	}
	out := payPalOrder{status: raw.Status}
	if len(raw.PurchaseUnits) > 0 {
		unit := raw.PurchaseUnits[0]
		out.customID = unit.CustomID
		if len(unit.Payments.Captures) > 0 {
			out.captureID = unit.Payments.Captures[0].ID
			// The capture carries its own copy, which is the one that
			// survives on a webhook payload; prefer it when present.
			if unit.Payments.Captures[0].CustomID != "" {
				out.customID = unit.Payments.Captures[0].CustomID
			}
		}
	}
	return out, nil
}

// VerifyWebhookSignature asks PayPal itself whether a delivery is authentic.
//
// Unlike Cashfree, NOWPayments and Stripe -- all of which sign with a shared
// secret we hold -- PayPal signs with a rotating certificate and offers no
// local verification path, so this necessarily makes a network call. A failure
// to reach PayPal therefore has to be treated as "not verified", never as
// "assume good": the whole point is that an unverified body is attacker-
// controlled.
func (c *PayPalClient) VerifyWebhookSignature(ctx context.Context, body []byte, h http.Header) bool {
	if c.WebhookID == "" {
		return false
	}

	transmissionID := h.Get("paypal-transmission-id")
	transmissionTime := h.Get("paypal-transmission-time")
	transmissionSig := h.Get("paypal-transmission-sig")
	certURL := h.Get("paypal-cert-url")
	authAlgo := h.Get("paypal-auth-algo")
	if transmissionID == "" || transmissionTime == "" || transmissionSig == "" || certURL == "" || authAlgo == "" {
		return false
	}
	// cert_url is echoed straight back to PayPal's verifier from an
	// attacker-controlled header. PayPal validates it too, but pinning the
	// host here means a forged delivery naming an attacker's cert host is
	// rejected locally rather than depending on a remote check we don't own.
	if !isPayPalCertURL(certURL) {
		return false
	}

	// webhook_event must be the parsed body, not a re-serialization: PayPal
	// verifies against the exact bytes it sent, and json.RawMessage passes
	// them through untouched.
	payload, err := json.Marshal(map[string]any{
		"auth_algo":         authAlgo,
		"cert_url":          certURL,
		"transmission_id":   transmissionID,
		"transmission_sig":  transmissionSig,
		"transmission_time": transmissionTime,
		"webhook_id":        c.WebhookID,
		"webhook_event":     json.RawMessage(body),
	})
	if err != nil {
		return false
	}

	respBody, err := c.do(ctx, http.MethodPost, "/v1/notifications/verify-webhook-signature", payload, "")
	if err != nil {
		return false
	}
	var parsed struct {
		VerificationStatus string `json:"verification_status"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return false
	}
	return parsed.VerificationStatus == "SUCCESS"
}

// isPayPalCertURL reports whether u is an https URL on a paypal.com host.
// Checked on the parsed host, not by substring: "paypal.com.evil.test" and
// "https://evil.test/?x=paypal.com" both contain the string.
func isPayPalCertURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "paypal.com" || strings.HasSuffix(host, ".paypal.com")
}

// do issues an authenticated JSON request and returns the response body,
// turning any non-2xx into an error carrying PayPal's own message.
// requestID, when non-empty, is sent as PayPal-Request-Id, which makes the
// call idempotent on PayPal's side for 24 hours.
func (c *PayPalClient) do(ctx context.Context, method, path string, body []byte, requestID string) ([]byte, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if requestID != "" {
		req.Header.Set("PayPal-Request-Id", requestID)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("paypal: request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("paypal: %s %s failed with status %d: %s", method, path, resp.StatusCode, respBody)
	}
	return respBody, nil
}
