package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

const (
	nowPaymentsLiveBaseURL    = "https://api.nowpayments.io/v1"
	nowPaymentsSandboxBaseURL = "https://api-sandbox.nowpayments.io/v1"
)

// NOWPaymentsClient talks to the NOWPayments REST API directly (no SDK dependency,
// consistent with this codebase's hand-rolled HTTP clients for external APIs — see
// RazorpayClient). It issues hosted invoices covering 300+ coins/chains without this
// codebase taking on any chain-specific logic — unlike wallet.Service, which signs
// Algorand transactions directly for the unrelated x402 agent-tool-payment flow.
type NOWPaymentsClient struct {
	APIKey    string
	IPNSecret string
	baseURL   string
	client    *http.Client
}

func NewNOWPaymentsClient(apiKey, ipnSecret string) *NOWPaymentsClient {
	return &NOWPaymentsClient{
		APIKey:    apiKey,
		IPNSecret: ipnSecret,
		baseURL:   nowPaymentsLiveBaseURL,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// UseSandbox points the client at NOWPayments' sandbox API (api-sandbox.nowpayments.io),
// used with sandbox_-prefixed API keys and a separate sandbox IPN secret. Call once after
// construction when NOWPAYMENTS_SANDBOX is set — see cmd/server/main.go.
func (c *NOWPaymentsClient) UseSandbox() {
	c.baseURL = nowPaymentsSandboxBaseURL
}

// SetBaseURLForTest points the client at a test server. Call with "" to reset to the live API.
func (c *NOWPaymentsClient) SetBaseURLForTest(url string) {
	if url == "" {
		c.baseURL = nowPaymentsLiveBaseURL
	} else {
		c.baseURL = url
	}
}

type Invoice struct {
	ID         string `json:"id"`
	InvoiceURL string `json:"invoice_url"`
}

// CreateInvoice creates a NOWPayments hosted invoice for amountUSDCents (minimum 100 = $1).
// orderID must be unique per credit_ledger row — NOWPayments echoes it back on every IPN
// event for that invoice, which is how the webhook handler finds the row to complete.
func (c *NOWPaymentsClient) CreateInvoice(ctx context.Context, amountUSDCents int64, orderID, ipnCallbackURL, successURL, cancelURL string) (Invoice, error) {
	var invoice Invoice
	if amountUSDCents < 100 {
		return invoice, fmt.Errorf("nowpayments: amount must be at least 100 cents, got %d", amountUSDCents)
	}
	body, _ := json.Marshal(map[string]any{
		"price_amount":     float64(amountUSDCents) / 100.0,
		"price_currency":   "usd",
		"order_id":         orderID,
		"ipn_callback_url": ipnCallbackURL,
		"success_url":      successURL,
		"cancel_url":       cancelURL,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/invoice", bytes.NewReader(body))
	if err != nil {
		return invoice, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return invoice, fmt.Errorf("nowpayments: request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return invoice, fmt.Errorf("nowpayments: authentication failed")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return invoice, fmt.Errorf("nowpayments: invoice create failed with status %d: %s", resp.StatusCode, respBody)
	}
	if err := json.Unmarshal(respBody, &invoice); err != nil {
		return invoice, fmt.Errorf("nowpayments: parse invoice response: %w", err)
	}
	return invoice, nil
}

// VerifyIPNSignature checks the HMAC-SHA512 signature NOWPayments sends in the
// x-nowpayments-sig header: the hex-encoded HMAC (keyed with IPNSecret) of the JSON body
// with its keys sorted alphabetically at every nesting level and re-serialized with no
// extra whitespace — confirmed against NOWPayments' own Python (`json.dumps(msg,
// separators=(',', ':'), sort_keys=True)`) and Node reference implementations. Sorting
// matters because the raw body we receive may not preserve the key order NOWPayments used
// when it signed. HTML-escaping is disabled on the encoder to match Python's json.dumps,
// which never escapes '<', '>', '&', or U+2028/U+2029 — Go's default json.Marshal does,
// which would silently diverge from NOWPayments' own signed form for any payload field
// containing those characters. Full byte-for-byte parity with NOWPayments' real signer is
// still unproven against an actual signed payload from their servers; that requires the
// still-pending manual sandbox smoke test.
func (c *NOWPaymentsClient) VerifyIPNSignature(body []byte, signature string) bool {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(sortKeysDeep(parsed)); err != nil {
		return false
	}
	sorted := bytes.TrimRight(buf.Bytes(), "\n") // Encoder.Encode appends a trailing newline; strip it before hashing

	mac := hmac.New(sha512.New, []byte(c.IPNSecret))
	mac.Write(sorted)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// sortKeysDeep re-encodes a JSON-decoded value so map keys serialize in sorted order at
// every nesting level, matching NOWPayments' own canonicalization before signing. Go's
// map[string]any would otherwise serialize with json.Marshal's built-in alphabetical key
// sort at each level anyway — this function exists so that guarantee is explicit and
// verified (see TestVerifyIPNSignatureAcceptsOutOfOrderKeys) rather than an implicit
// assumption about encoding/json's undocumented-but-stable behavior.
func sortKeysDeep(v any) any {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ordered := make(orderedMap, 0, len(keys))
		for _, k := range keys {
			ordered = append(ordered, orderedEntry{k, sortKeysDeep(val[k])})
		}
		return ordered
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = sortKeysDeep(item)
		}
		return out
	default:
		return val
	}
}

type orderedEntry struct {
	Key   string
	Value any
}

// orderedMap marshals as a JSON object with keys emitted in the exact order appended,
// since Go's map[string]any would re-randomize key order and defeat the sort above.
type orderedMap []orderedEntry

func (m orderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, e := range m {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyJSON, err := marshalNoEscape(e.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(keyJSON)
		buf.WriteByte(':')
		valJSON, err := marshalNoEscape(e.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(valJSON)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalNoEscape is json.Marshal without HTML-escaping. It exists because
// json.Marshal's escaping (of '<', '>', '&', U+2028, U+2029) is hardcoded true at the
// package-level function and is NOT overridden by an enclosing Encoder's
// SetEscapeHTML(false): when an encoder encounters a value implementing json.Marshaler
// (orderedMap, here) it takes the bytes MarshalJSON returns as-is and only compacts them —
// it does not re-escape-or-unescape a nested MarshalJSON's own literal escape sequences.
// So orderedMap.MarshalJSON must opt out of escaping itself; relying on the caller's
// encoder setting to propagate down would silently reintroduce HTML-escaped output for
// '<', '>', '&' and defeat the fix in VerifyIPNSignature above.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
