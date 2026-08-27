package handlers

import (
	"fmt"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// The validation branches are the whole point of parseSettingsPatch: they are
// what stops an out-of-range ceiling or an unsupported currency from reaching a
// table whose CHECK constraints would reject it as a 500 instead of a 400.
func TestParseSettingsPatchRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"malformed JSON", `{`, "invalid JSON body"},
		{"negative threshold", `{"lowBalanceUsdMicros":-1}`, "cannot be negative"},
		{"non-numeric threshold", `{"lowBalanceUsdMicros":"5"}`, "whole number of USD micros"},
		{"zero ceiling", `{"maxCallSpendUsdMicros":0}`, "greater than zero"},
		{"negative ceiling", `{"maxCallSpendUsdMicros":-500}`, "greater than zero"},
		{"ceiling below the probe floor", `{"maxCallSpendUsdMicros":10000}`, "below the $0.05 minimum"},
		{"ceiling above the platform cap", `{"maxCallSpendUsdMicros":1000000001}`, "cannot exceed the platform ceiling"},
		{"unsupported currency", `{"displayCurrency":"XYZ"}`, "displayCurrency must be one of"},
		{"lowercase currency", `{"displayCurrency":"eur"}`, "displayCurrency must be one of"},
		{"non-string currency", `{"displayCurrency":5}`, "displayCurrency must be a string"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, msg := parseSettingsPatch(strings.NewReader(tc.body))
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("want message containing %q, got %q", tc.want, msg)
			}
		})
	}
}

// The global cap itself must remain settable — the boundary is inclusive, so a
// user pinning their ceiling to exactly the platform maximum is legal.
func TestParseSettingsPatchAcceptsTheGlobalCapExactly(t *testing.T) {
	body := `{"maxCallSpendUsdMicros":1000000000}`
	patch, msg := parseSettingsPatch(strings.NewReader(body))
	if msg != "" {
		t.Fatalf("want no error, got %q", msg)
	}
	if patch.MaxCallSpendUSDMicros == nil || *patch.MaxCallSpendUSDMicros != models.MaxSingleX402QuoteUSDMicros {
		t.Fatalf("want ceiling %d, got %v", models.MaxSingleX402QuoteUSDMicros, patch.MaxCallSpendUSDMicros)
	}
}

// The probe floor is the lower bound, inclusive. Runner.preflightCheck reuses
// X402ProbeFloorUSDMicros as a pre-price guard on tool402 nodes, so a ceiling
// exactly at it still lets those nodes run; anything under it would block them
// outright and report the wrong reason.
func TestParseSettingsPatchAcceptsTheProbeFloorExactly(t *testing.T) {
	body := `{"maxCallSpendUsdMicros":50000}`
	patch, msg := parseSettingsPatch(strings.NewReader(body))
	if msg != "" {
		t.Fatalf("want no error, got %q", msg)
	}
	if patch.MaxCallSpendUSDMicros == nil || *patch.MaxCallSpendUSDMicros != models.X402ProbeFloorUSDMicros {
		t.Fatalf("want ceiling %d, got %v", models.X402ProbeFloorUSDMicros, patch.MaxCallSpendUSDMicros)
	}
}

// An omitted key must leave the stored value alone. This is the regression that
// matters most: a settings page that PATCHes one field must not blank the rest.
func TestParseSettingsPatchLeavesOmittedFieldsUntouched(t *testing.T) {
	existing := int64(250_000)
	settings := models.UserSettings{
		LowBalanceUSDMicros:   9_000_000,
		MaxCallSpendUSDMicros: &existing,
	}

	patch, msg := parseSettingsPatch(strings.NewReader(`{"lowBalanceUsdMicros":1000000}`))
	if msg != "" {
		t.Fatalf("want no error, got %q", msg)
	}
	patch.ApplyTo(&settings)

	if settings.LowBalanceUSDMicros != 1_000_000 {
		t.Errorf("threshold: want 1000000, got %d", settings.LowBalanceUSDMicros)
	}
	if settings.MaxCallSpendUSDMicros == nil || *settings.MaxCallSpendUSDMicros != existing {
		t.Errorf("ceiling should survive an unrelated patch, got %v", settings.MaxCallSpendUSDMicros)
	}
}

// An explicit null is the only way to remove a ceiling, and it has to be
// distinguishable from omitting the field — otherwise every partial save would
// silently drop the user's spend limit.
func TestParseSettingsPatchClearsCeilingOnExplicitNull(t *testing.T) {
	existing := int64(250_000)
	settings := models.UserSettings{MaxCallSpendUSDMicros: &existing}

	patch, msg := parseSettingsPatch(strings.NewReader(`{"maxCallSpendUsdMicros":null}`))
	if msg != "" {
		t.Fatalf("want no error, got %q", msg)
	}
	patch.ApplyTo(&settings)

	if settings.MaxCallSpendUSDMicros != nil {
		t.Fatalf("want ceiling cleared, got %d", *settings.MaxCallSpendUSDMicros)
	}
}

// Defaults are what every account has before it opens the settings page, so
// they have to match the column DEFAULTs in migration 000020.
func TestDefaultUserSettingsMatchTheMigration(t *testing.T) {
	d := models.DefaultUserSettings()
	if d.LowBalanceUSDMicros != 5_000_000 {
		t.Errorf("low balance default: want 5000000, got %d", d.LowBalanceUSDMicros)
	}
	if d.MaxCallSpendUSDMicros != nil {
		t.Errorf("ceiling should default to unset, got %d", *d.MaxCallSpendUSDMicros)
	}
	// The opt-in invariant starts here: an account that has never chosen a
	// currency must report USD, so the frontend renders exactly as it did
	// before this feature existed.
	if d.DisplayCurrency != models.DefaultCurrency {
		t.Errorf("currency default: want %q, got %q", models.DefaultCurrency, d.DisplayCurrency)
	}
}

// Selecting a currency must not disturb the other settings, and USD must remain
// selectable so a user can switch back to the default rendering.
func TestParseSettingsPatchAcceptsEverySupportedCurrency(t *testing.T) {
	for _, code := range models.SupportedCurrencies {
		t.Run(code, func(t *testing.T) {
			ceiling := int64(250_000)
			settings := models.UserSettings{
				LowBalanceUSDMicros:   9_000_000,
				MaxCallSpendUSDMicros: &ceiling,
				DisplayCurrency:       models.DefaultCurrency,
			}

			patch, msg := parseSettingsPatch(strings.NewReader(`{"displayCurrency":"` + code + `"}`))
			if msg != "" {
				t.Fatalf("want %s accepted, got %q", code, msg)
			}
			patch.ApplyTo(&settings)

			if settings.DisplayCurrency != code {
				t.Errorf("currency: want %q, got %q", code, settings.DisplayCurrency)
			}
			if settings.LowBalanceUSDMicros != 9_000_000 ||
				settings.MaxCallSpendUSDMicros == nil || *settings.MaxCallSpendUSDMicros != ceiling {
				t.Error("changing currency must not disturb the other settings")
			}
		})
	}
}

// preflightCheck skips its settings lookup at or below the probe floor, which
// is only sound while this validation refuses anything beneath it. Pinned here
// so lowering one without the other fails loudly instead of quietly making a
// sub-floor ceiling storable and unenforced.
func TestParseSettingsPatchCeilingFloorTracksTheProbeFloor(t *testing.T) {
	below := fmt.Sprintf(`{"maxCallSpendUsdMicros":%d}`, models.X402ProbeFloorUSDMicros-1)
	if _, msg := parseSettingsPatch(strings.NewReader(below)); msg == "" {
		t.Error("want a ceiling one micro below the probe floor rejected, got accepted")
	}
	at := fmt.Sprintf(`{"maxCallSpendUsdMicros":%d}`, models.X402ProbeFloorUSDMicros)
	if _, msg := parseSettingsPatch(strings.NewReader(at)); msg != "" {
		t.Errorf("want a ceiling exactly at the probe floor accepted, got %q", msg)
	}
}
