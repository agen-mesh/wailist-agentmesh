package x402

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type FacilitatorClient struct {
	baseURL string
	client  *http.Client
}

func NewFacilitatorClient(baseURL string) *FacilitatorClient {
	return &FacilitatorClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

type PaymentGroup struct {
	PaymentGroup []string `json:"paymentGroup"`
	PaymentIndex int      `json:"paymentIndex"`
}

type PaymentPayload struct {
	X402Version int          `json:"x402Version"`
	Scheme      string       `json:"scheme"`
	Network     string       `json:"network"`
	Payload     PaymentGroup `json:"payload"`
}

type PaymentRequirements struct {
	Scheme            string         `json:"scheme"`
	Network           string         `json:"network"`
	MaxAmountRequired string         `json:"maxAmountRequired"`
	Resource          string         `json:"resource"`
	Description       string         `json:"description"`
	MimeType          string         `json:"mimeType"`
	PayTo             string         `json:"payTo"`
	MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
	Asset             string         `json:"asset"`
	Extra             map[string]any `json:"extra"`
}

type VerifyResult struct {
	IsValid bool   `json:"isValid"`
	Invalid string `json:"invalidReason,omitempty"`
}

type SettleResult struct {
	Success bool   `json:"success"`
	TxID    string `json:"transaction,omitempty"`
	Error   string `json:"errorReason,omitempty"`
}

func (c *FacilitatorClient) Verify(ctx context.Context, payload PaymentPayload, reqs PaymentRequirements) (VerifyResult, error) {
	var result VerifyResult
	err := c.post(ctx, "/verify", payload, reqs, &result)
	return result, err
}

func (c *FacilitatorClient) Settle(ctx context.Context, payload PaymentPayload, reqs PaymentRequirements) (SettleResult, error) {
	var result SettleResult
	err := c.post(ctx, "/settle", payload, reqs, &result)
	return result, err
}

func (c *FacilitatorClient) post(ctx context.Context, path string, payload PaymentPayload, reqs PaymentRequirements, out any) error {
	body, err := json.Marshal(map[string]any{
		"paymentPayload":      payload,
		"paymentRequirements": reqs,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("facilitator %s: server error %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
