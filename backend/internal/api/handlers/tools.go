package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

func (d *Deps) X402Quote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		respond.Error(w, http.StatusBadRequest, "url required")
		return
	}
	quote, err := nodes.QuoteX402(r.Context(), body.URL)
	if err != nil {
		log.Printf("x402 quote error: %v", err)
		// "This isn't an x402 endpoint" is a fact about the URL the user
		// typed, not an outage — surfacing it verbatim is what lets them fix
		// the URL instead of retrying a generic "upstream fetch failed".
		var noChallenge *nodes.ErrNoPaymentChallenge
		if errors.As(err, &noChallenge) {
			respond.Error(w, http.StatusUnprocessableEntity, noChallenge.Error())
			return
		}
		respond.Error(w, http.StatusBadGateway, "upstream fetch failed")
		return
	}
	if params, ok := quote["params"].([]models.ParamDef); ok {
		quote["params"] = d.prefillKnownParams(params)
	}
	respond.JSON(w, http.StatusOK, quote)
}

// prefillKnownParams supplies a default for the parameters AgentMesh can
// answer on the caller's behalf, so a user pasting an endpoint URL isn't
// asked to hand-copy a value the platform already knows. Only the wallet
// address qualifies today: an endpoint asking for one is asking "which
// account is this call about", and for a call the platform itself pays and
// signs, that is the platform's own wallet.
//
// PlatformWalletAddress is safe to hand a browser — it is already published
// as payTo in every x402 challenge this backend emits, and visible on-chain.
// The value is only ever a default: it lands in an editable field, so a user
// wanting a different account just types it.
func (d *Deps) prefillKnownParams(params []models.ParamDef) []models.ParamDef {
	if d.PlatformWalletAddress == "" {
		return params
	}
	for i := range params {
		if params[i].Default != "" || !isWalletAddressParam(params[i].Name) {
			continue
		}
		params[i].Default = d.PlatformWalletAddress
	}
	return params
}

// isWalletAddressParam matches the names real x402 endpoints use for "the
// account this request is about". Deliberately conservative: a false
// positive silently sends an Algorand address where something else belongs,
// which is worse than leaving a field for the user to fill in.
func isWalletAddressParam(name string) bool {
	switch strings.ToLower(name) {
	case "address", "addr", "wallet", "account",
		"walletaddress", "wallet_address", "accountaddress", "account_address":
		return true
	}
	return false
}
