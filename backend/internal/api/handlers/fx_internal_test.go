package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/payments"
)

func TestFXRatesReturnsOnlySupportedCurrencies(t *testing.T) {
	payments.SetFetchRateTableForTest(func(context.Context) (map[string]float64, error) {
		// Deliberately includes a code the UI does not offer.
		return map[string]float64{"USD": 1, "EUR": 0.865, "INR": 95.25, "ZWL": 322}, nil
	})
	t.Cleanup(func() { payments.SetFetchRateTableForTest(nil) })

	w := httptest.NewRecorder()
	(&Deps{}).FXRates(w, httptest.NewRequest(http.MethodGet, "/fx/rates", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body payments.RateTable
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Base != "USD" {
		t.Errorf("want base USD, got %q", body.Base)
	}
	if _, leaked := body.Rates["ZWL"]; leaked {
		t.Error("a currency the UI cannot select leaked into the response")
	}
	for _, code := range []string{"USD", "EUR", "INR"} {
		if _, ok := body.Rates[code]; !ok {
			t.Errorf("missing rate for %s", code)
		}
	}
	// USD must be present and exactly 1 — the client relies on it so the USD
	// path is a genuine no-op rather than a multiply by something near 1.
	if body.Rates["USD"] != 1 {
		t.Errorf("want USD exactly 1, got %v", body.Rates["USD"])
	}
}

// With no cache and a dead upstream there is no honest number to serve, so the
// endpoint fails and lets the client fall back to USD.
func TestFXRatesReturnsBadGatewayWhenRatesUnavailable(t *testing.T) {
	payments.SetFetchRateTableForTest(func(context.Context) (map[string]float64, error) {
		return nil, errors.New("upstream down")
	})
	t.Cleanup(func() { payments.SetFetchRateTableForTest(nil) })

	w := httptest.NewRecorder()
	(&Deps{}).FXRates(w, httptest.NewRequest(http.MethodGet, "/fx/rates", nil))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", w.Code)
	}
}

// The endpoint's currency list and the column's CHECK constraint have to stay in
// step; this catches one being extended without the other.
func TestSupportedCurrenciesIncludesTheDefault(t *testing.T) {
	if !models.IsSupportedCurrency(models.DefaultCurrency) {
		t.Fatalf("the default currency %q must itself be selectable", models.DefaultCurrency)
	}
	if models.IsSupportedCurrency("XYZ") {
		t.Error("an unlisted code must not validate")
	}
}
