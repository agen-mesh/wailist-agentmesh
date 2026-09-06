package handlers

import (
	"log"
	"net/http"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/payments"
	"github.com/agentmesh/backend/internal/respond"
)

// FXRates serves the display exchange rates, USD-based, for the shortlist the
// settings page offers.
//
// Display only. Nothing here influences what a user is charged: top-ups fetch
// their own fresh rate at order time (payments.FetchINRToUSDRate) and lock it
// into the credit_ledger row, deliberately bypassing this cache.
//
// A 502 is the honest answer when the upstream is down and nothing has ever been
// cached — the client falls back to rendering USD rather than showing figures
// derived from a rate we cannot vouch for.
func (d *Deps) FXRates(w http.ResponseWriter, r *http.Request) {
	table, err := payments.FetchRateTable(r.Context(), models.SupportedCurrencies)
	if err != nil {
		log.Printf("fx rates: %v", err)
		respond.Error(w, http.StatusBadGateway, "could not fetch exchange rates")
		return
	}
	respond.JSON(w, http.StatusOK, table)
}
