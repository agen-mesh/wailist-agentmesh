package x402

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	// Resource/Extensions carry the Bazaar-catalogable identity of what this
	// payment is for. The facilitator's discovery extraction
	// (extractDiscoveryInfo, @x402/extensions) reads these fields off the
	// payment PAYLOAD, not off PaymentRequirements below -- a real v2 client
	// is expected to copy them verbatim from the challenge it received onto
	// its own outgoing payload. Before this field existed, any caller that
	// did send them here (correctly) got them silently dropped on decode
	// (Go's json.Unmarshal ignores unknown fields), so they could never
	// reach the facilitator at all regardless of what the caller sent.
	// Optional on the wire (omitempty): a minimal/legacy caller that omits
	// them decodes safely into a nil map either way -- see
	// x402relay.go's relaySettleAndForward and relaySelfSettle, which now
	// set these fields SERVER-SIDE, authoritatively, before forwarding to
	// Verify/Settle regardless of what the caller's own payload carried:
	// the resource server, not the caller, is the one that actually knows
	// what a payment against its own route is for, so correctness here
	// should never depend on every caller (including this codebase's own
	// internal engine self-call) remembering to echo it back.
	Resource   map[string]any `json:"resource,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
	// Accepted echoes back the single PaymentRequirements entry this payment
	// was created against. It is a REQUIRED field of the real v2 payload
	// schema -- @x402-avm/core's PaymentPayloadV2Schema is
	// z.object({x402Version, resource: ResourceInfoSchema.optional(),
	// accepted: PaymentRequirementsV2Schema, payload, extensions}), with
	// `accepted` carrying no .optional(). The real client's own
	// createPaymentPayload sets it verbatim (`accepted: requirements`).
	//
	// The AVM scheme mechanism never reads it -- it works off
	// paymentRequirements and payload.paymentGroup -- which is exactly why
	// every real settlement this codebase has performed succeeded and moved
	// real money while omitting it. Anything that runs the payload through
	// the published schema first (parsePaymentPayload/isPaymentPayloadV2),
	// however, sees a hard validation failure on a field the settle path
	// never needed, and the Bazaar catalog is the one consumer known to sit
	// behind such a gate.
	Accepted *AcceptedV2 `json:"accepted,omitempty"`
}

// AcceptedV2 is exactly PaymentRequirementsV2Schema's field set -- scheme,
// network, amount, asset, payTo, maxTimeoutSeconds, extra -- and nothing
// else. Deliberately a separate, narrower type rather than reusing
// PaymentRequirements: that struct still carries the v1-dialect resource/
// description/mimeType/outputSchema fields, which the v2 requirements schema
// does not declare and which a plain zod object silently strips. Sending the
// clean subset means what the facilitator parses is what we meant to send.
type AcceptedV2 struct {
	Scheme  string `json:"scheme"`
	Network string `json:"network"`
	Amount  string `json:"amount"`
	Asset   string `json:"asset"`
	PayTo   string `json:"payTo"`
	// Must be strictly positive: the schema types this as
	// z.number().positive(), so a zero here is a validation failure, not a
	// "use the default" signal.
	MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
	Extra             map[string]any `json:"extra,omitempty"`
}

// AcceptedV2 projects these requirements onto the v2 accepted-requirements
// shape, for assignment to PaymentPayload.Accepted at every verify/settle
// call site.
func (r PaymentRequirements) AcceptedV2() *AcceptedV2 {
	return &AcceptedV2{
		Scheme:            r.Scheme,
		Network:           r.Network,
		Amount:            r.MaxAmountRequired,
		Asset:             r.Asset,
		PayTo:             r.PayTo,
		MaxTimeoutSeconds: r.MaxTimeoutSeconds,
		Extra:             r.Extra,
	}
}

type PaymentRequirements struct {
	Scheme  string `json:"scheme"`
	Network string `json:"network"`
	// GoPlausible's facilitator reads this key as "amount", not the more
	// common x402 "maxAmountRequired" — sending the latter left their
	// server reading undefined and crashing on BigInt(undefined)
	// (confirmed live 2026-07-31 against /verify). Go field name kept as
	// MaxAmountRequired since every caller already treats it as such;
	// only the wire tag changes.
	MaxAmountRequired string         `json:"amount"`
	Resource          string         `json:"resource"`
	Description       string         `json:"description"`
	MimeType          string         `json:"mimeType"`
	PayTo             string         `json:"payTo"`
	MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
	Asset             string         `json:"asset"`
	Extra             map[string]any `json:"extra"`
	// Extensions carries the Bazaar discovery declaration (equivalent to the
	// TS SDK's declareDiscoveryExtension). Without it here, on the struct
	// that's actually POSTed to /verify and /settle, the facilitator never
	// learns this route exists to catalog — extra.tag alone only attributes
	// an already-discovered route's activity to the challenge, it doesn't
	// register the route. Confirmed missing 2026-07-31: our own 402 body to
	// the paying caller carried an "extensions" key, but neither Verify nor
	// Settle ever sent it, so no catalog entry was ever created regardless
	// of how many payments settled.
	Extensions map[string]any `json:"extensions,omitempty"`
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
	// Three top-level keys, not two. The reference client sends
	// {x402Version, paymentPayload, paymentRequirements} on both /verify and
	// /settle (@x402-avm/core, dist/cjs/server/index.js -- the verify body at
	// :167 and the settle body at :200 are identical in shape); this client
	// has only ever sent the latter two.
	//
	// Load-bearing beyond mere shape-matching: in that same SDK both
	// PaymentRequiredSchema and PaymentPayloadSchema are
	// z.discriminatedUnion("x402Version", [V1, V2]), and V1 vs V2
	// requirements differ in precisely the field this package hand-rolls --
	// V1 reads maxAmountRequired, V2 reads amount, which is what the
	// MaxAmountRequired `json:"amount"` tag above emits. A server that keys
	// its parse off a top-level version it never received can fall through
	// to the V1 branch, where `amount` is unrecognized and `extra` (carrying
	// tag: x402-global-challenge) comes along only as far as a half-parsed
	// object -- which is a plausible route to settlements being classified
	// `direct` instead of `x402-global-challenge`, exactly what
	// /data/leaderboards?src=... reports for this merchant today.
	//
	// Marked plausible, not proven: inferred from the reference client's
	// wire shape, not confirmed against GoPlausible's server, which we
	// cannot read. Applied regardless because it is strictly closer to the
	// reference implementation and costs nothing.
	body, err := json.Marshal(map[string]any{
		"x402Version":         payload.X402Version,
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
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("facilitator %s: server error %d: %s", path, resp.StatusCode, errBody)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
