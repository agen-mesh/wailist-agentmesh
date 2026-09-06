package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

// Settings returns the signed-in user's account settings. An account with no
// user_settings row gets the defaults rather than a 404 — see
// db.Store.GetUserSettings for why a missing row is the normal case.
func (d *Deps) Settings(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)

	settings, err := d.Store.GetUserSettings(r.Context(), userID)
	if err != nil {
		log.Printf("get user settings: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	respond.JSON(w, http.StatusOK, settings)
}

// UpdateSettings applies a partial update and returns the full merged result.
//
// Presence is decoded by hand rather than with pointer struct fields, because
// maxCallSpendUsdMicros is nullable and the two cases have to stay distinct:
// an absent key means "leave the ceiling alone", while an explicit null means
// "remove my ceiling". Unmarshalling straight into a *int64 collapses both to
// nil, which would silently clear a user's spend limit on any request that
// merely didn't mention it.
func (d *Deps) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)

	// Parse and validate before touching the database: a malformed request is
	// rejected on its own terms, without a round-trip that can only be thrown away.
	patch, msg := parseSettingsPatch(r.Body)
	if msg != "" {
		respond.Error(w, http.StatusBadRequest, msg)
		return
	}

	// Applied in one statement: the store merges per column, so a concurrent
	// PATCH to a different section cannot be clobbered by a stale read here.
	saved, err := d.Store.UpsertUserSettings(r.Context(), userID, patch)
	if err != nil {
		log.Printf("update user settings: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	respond.JSON(w, http.StatusOK, saved)
}

// parseSettingsPatch decodes a PATCH body, returning a human-readable message
// for the 400 when it is invalid and "" when it is fine. Kept separate from the
// handler so every validation branch is reachable without a database.
func parseSettingsPatch(body io.Reader) (models.UserSettingsPatch, string) {
	var patch models.UserSettingsPatch

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return patch, "invalid JSON body"
	}

	if v, ok := raw["lowBalanceUsdMicros"]; ok {
		var threshold int64
		if err := json.Unmarshal(v, &threshold); err != nil {
			return patch, "lowBalanceUsdMicros must be a whole number of USD micros"
		}
		if threshold < 0 {
			return patch, "lowBalanceUsdMicros cannot be negative"
		}
		patch.LowBalanceUSDMicros = &threshold
	}

	if v, ok := raw["maxCallSpendUsdMicros"]; ok {
		var ceiling *int64
		if err := json.Unmarshal(v, &ceiling); err != nil {
			return patch, "maxCallSpendUsdMicros must be a whole number of USD micros, or null"
		}
		if ceiling != nil {
			if *ceiling <= 0 {
				return patch, "maxCallSpendUsdMicros must be greater than zero — send null to remove the ceiling"
			}
			// Below the probe floor the ceiling stops being a spend limit and
			// becomes an outage. Runner.preflightCheck is also the guard for
			// X402ProbeFloorUSDMicros — a conservative reservation taken before
			// a tool402 node's price is even known (runner.go's NodeTypeTool402
			// branch), not a charge. A ceiling under it therefore rejects every
			// tool402 node up front, including ones that would have cost a
			// fraction of a cent, and the failure reads as "above your per-call
			// limit", which is not what actually went wrong.
			if *ceiling < models.X402ProbeFloorUSDMicros {
				return patch, "maxCallSpendUsdMicros cannot be below the $0.05 minimum every paid call is checked against"
			}
			// A user ceiling only ever tightens the global one. Accepting a
			// higher number would read as a raise the engine never honours.
			if *ceiling > models.MaxSingleX402QuoteUSDMicros {
				return patch, "maxCallSpendUsdMicros cannot exceed the platform ceiling"
			}
		}
		patch.SetMaxCallSpend = true
		patch.MaxCallSpendUSDMicros = ceiling
	}

	if v, ok := raw["displayCurrency"]; ok {
		var code string
		if err := json.Unmarshal(v, &code); err != nil {
			return patch, "displayCurrency must be a string"
		}
		// Rejected here as well as by the column's CHECK so an unsupported code
		// comes back as a 400 the settings form can show, not a 500 from a
		// constraint violation.
		if !models.IsSupportedCurrency(code) {
			return patch, "displayCurrency must be one of " + strings.Join(models.SupportedCurrencies, ", ")
		}
		patch.DisplayCurrency = &code
	}

	return patch, ""
}
