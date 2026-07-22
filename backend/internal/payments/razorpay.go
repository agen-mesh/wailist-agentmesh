package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const razorpayLiveBaseURL = "https://api.razorpay.com/v1"

// RazorpayClient talks to the Razorpay REST API directly (no SDK dependency,
// consistent with this codebase's hand-rolled HTTP clients for external APIs).
type RazorpayClient struct {
	KeyID         string
	KeySecret     string
	WebhookSecret string
	baseURL       string
	client        *http.Client
}

func NewRazorpayClient(keyID, keySecret, webhookSecret string) *RazorpayClient {
	return &RazorpayClient{
		KeyID:         keyID,
		KeySecret:     keySecret,
		WebhookSecret: webhookSecret,
		baseURL:       razorpayLiveBaseURL,
		client:        &http.Client{Timeout: 10 * time.Second},
	}
}

// SetBaseURLForTest points the client at a test server. Call with "" to reset to the live API.
func (c *RazorpayClient) SetBaseURLForTest(url string) {
	if url == "" {
		c.baseURL = razorpayLiveBaseURL
	} else {
		c.baseURL = url
	}
}

type RazorpayOrder struct {
	ID       string `json:"id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// CreateOrder creates a Razorpay order for amountPaise (INR minor units, minimum 100 = ₹1).
// receipt must be 40 characters or fewer per Razorpay's API limit.
func (c *RazorpayClient) CreateOrder(ctx context.Context, amountPaise int64, receipt string) (RazorpayOrder, error) {
	var order RazorpayOrder
	if amountPaise < 100 {
		return order, fmt.Errorf("razorpay: amount must be at least 100 paise, got %d", amountPaise)
	}
	body, _ := json.Marshal(map[string]any{
		"amount":   amountPaise,
		"currency": "INR",
		"receipt":  receipt,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/orders", bytes.NewReader(body))
	if err != nil {
		return order, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.KeyID, c.KeySecret)

	resp, err := c.client.Do(req)
	if err != nil {
		return order, fmt.Errorf("razorpay: request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusUnauthorized {
		return order, fmt.Errorf("razorpay: authentication failed")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return order, fmt.Errorf("razorpay: order create failed with status %d: %s", resp.StatusCode, respBody)
	}
	if err := json.Unmarshal(respBody, &order); err != nil {
		return order, fmt.Errorf("razorpay: parse order response: %w", err)
	}
	return order, nil
}

// VerifySignature checks the HMAC-SHA256 signature Razorpay's checkout.js handler returns,
// per https://razorpay.com/docs/payments/payment-gateway/web-integration/standard/integration-steps/.
func (c *RazorpayClient) VerifySignature(orderID, paymentID, signature string) bool {
	mac := hmac.New(sha256.New, []byte(c.KeySecret))
	mac.Write([]byte(orderID + "|" + paymentID))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// VerifyWebhookSignature checks the HMAC-SHA256 signature Razorpay sends in the
// X-Razorpay-Signature header, computed over the raw webhook request body using the
// webhook secret configured in the Razorpay dashboard (distinct from KeySecret).
// See https://razorpay.com/docs/webhooks/validate-test/.
func (c *RazorpayClient) VerifyWebhookSignature(body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(c.WebhookSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
