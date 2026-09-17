package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/agentmesh/backend/internal/bazaar"
)

// An http tool node the builder adds is probed before it reaches the canvas
// (judgeProbe): a url that 404s is refused rather than wired in, because a
// run would fail on it. A paid x402 node had no such check, and it is the
// one that needs it most. The Bazaar is a mirror of a permissionless
// registry: an entry stays listed after the service behind it dies, its
// price can change under us, and the worst case is not a failed run -- it is
// a real USDC payment for a dead endpoint, plus the AgentMesh fee on top.
//
// So: ask the endpoint, before adding it, the one question that can be asked
// for free -- send the unpaid request and read the 402 it should answer with.

// x402Prober is the probe the builder runs. A package var so tests can run
// the add path without a network, mirroring urlValidator/geminiBaseURL.
var x402Prober = probeX402Resource

// x402Probe is what one unpaid request to a catalog endpoint found.
type x402Probe struct {
	// Status is the HTTP status, or 0 when the request never completed.
	Status int
	// Err is a transport failure. Never wrapped into the message the model
	// sees verbatim -- it names hosts and dialer internals.
	Err error
	// AmountMicros is the price the live challenge demands, or -1 when the
	// response carried no readable x402 v2 challenge.
	AmountMicros int64
	// Asset is the live challenge's asset id, empty when it carried none.
	Asset string
}

// probeX402Resource sends one unpaid request to a catalog entry and reports
// what came back.
//
// Unpaid is the whole point: a correctly gated endpoint answers 402 and does
// no work, so this costs nobody anything and has no side effect. An endpoint
// that is NOT gated will actually run, which is why a non-GET probe is sent
// with no body (see judgeX402Probe for how its result is read).
func probeX402Resource(ctx context.Context, r bazaar.Resource) x402Probe {
	out := x402Probe{AmountMicros: -1}
	if err := urlValidator(r.URL); err != nil {
		out.Err = err
		return out
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, r.URL, nil)
	if err != nil {
		out.Err = err
		return out
	}
	req.Header.Set("Accept", "application/json")
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		out.Err = err
		return out
	}
	defer resp.Body.Close()
	out.Status = resp.StatusCode
	if resp.StatusCode != http.StatusPaymentRequired {
		return out
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
	var challenge struct {
		Accepts []map[string]any `json:"accepts"`
	}
	json.Unmarshal(body, &challenge)
	if len(challenge.Accepts) == 0 {
		// Some real targets (Prism's live endpoint, confirmed 2026-07-31)
		// put the challenge in the Payment-Required header instead of the
		// body, exactly as probeTool402Endpoint handles.
		challenge.Accepts = ChallengeAcceptsFromHeader(resp.Header)
	}
	if len(challenge.Accepts) == 0 {
		return out
	}
	a := challenge.Accepts[0]
	if s, ok := a["amount"].(string); ok {
		// Parsed from the string the challenge sent, never via float: these
		// are atomic units and a price is not the place to round.
		if v, err := strconv.ParseInt(s, 10, 64); err == nil && v >= 0 {
			out.AmountMicros = v
		}
	}
	if s, ok := a["asset"].(string); ok {
		out.Asset = s
	}
	return out
}

// judgeX402Probe turns a probe into what the builder does with the entry:
// refuse it, or add it with a note and the price to put on the node.
//
// price is the endpoint's own per-call price in atomic units, taken from the
// live challenge when it gave one and from the catalog otherwise. The live
// value wins because it is what the user will actually be charged; the
// catalog is a mirror that can be hours out of date.
//
// A non-GET entry is judged loosely on purpose. The probe sends no body, so
// a 400 from a POST endpoint says "you sent me nothing", not "I am broken",
// and refusing on it would reject working endpoints. Only a refusal that
// cannot be explained by the missing body -- unreachable, or gone -- stands.
func judgeX402Probe(r bazaar.Resource, p x402Probe) (refuse, note string, price int64) {
	price = r.AmountMicros
	isGet := r.Method == "" || strings.EqualFold(r.Method, http.MethodGet)

	switch {
	case p.Err != nil:
		return fmt.Sprintf("refused: the x402 endpoint %s could not be reached, so paying it would fail the same way. "+
			"Call search_x402 again and pick a different endpoint, or build this another way.", r.URL), "", price

	case p.Status == http.StatusPaymentRequired:
		if p.AmountMicros < 0 {
			return "", " -- checked: the endpoint still answers and still asks for payment, but its challenge could not be read," +
				" so the catalog price below is the best available and may be out of date.", price
		}
		note = " -- verified: the endpoint answered with a live payment challenge."
		if p.AmountMicros != r.AmountMicros {
			note += fmt.Sprintf(" Its price has changed since the catalog was mirrored: it now charges %s %s per call, not %s. The node uses the live price.",
				formatMicros(p.AmountMicros), assetSymbol(p.Asset), formatMicros(r.AmountMicros))
		}
		return "", note, p.AmountMicros

	case p.Status == http.StatusNotFound || p.Status == http.StatusGone:
		return fmt.Sprintf("refused: the x402 endpoint %s answered HTTP %d, so it is gone. "+
			"Call search_x402 again and pick a different endpoint, or build this another way.", r.URL, p.Status), "", price

	case !isGet:
		// Everything below this point is read as a statement about the
		// endpoint's health, which a body-less non-GET probe cannot make.
		return "", fmt.Sprintf(" -- note: this is a %s endpoint, so it could not be checked without sending a real request."+
			" Its price and its response shape are the catalog's, unverified.", strings.ToUpper(r.Method)), price

	case p.Status == http.StatusUnauthorized || p.Status == http.StatusForbidden:
		return "", fmt.Sprintf(" -- note: the endpoint answered HTTP %d rather than a payment challenge, so it wants a credential as well as payment."+
			" Say so in your reply; the user adds it in the Inspector.", p.Status), price

	case p.Status >= 200 && p.Status < 300:
		return "", fmt.Sprintf(" -- warning: the endpoint answered HTTP %d without asking for payment."+
			" It is either free now or returning an error page, and either way it is not behaving as a paid endpoint. Tell the user before relying on it.", p.Status), price

	default:
		return fmt.Sprintf("refused: the x402 endpoint %s answered HTTP %d instead of a payment challenge, so a paid call would fail on it. "+
			"Call search_x402 again and pick a different endpoint, or build this another way.", r.URL, p.Status), "", price
	}
}
