package handlers

import (
	"sort"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// /auth/me and PATCH /auth/me previously built their response maps by hand, and
// the PATCH one was missing createdAt and displayCurrency. Both are optional on
// the frontend's AuthUser type -- so a stale cached response still type-checks --
// which meant nothing failed when a field went missing: saving a profile just
// reset the app's display currency to USD and blanked "member since".
//
// Pinning the key set here is what stops that recurring. It runs without a
// database, so it fails fast rather than waiting on the DB-gated handler tests.
func TestUserResponseCarriesEveryFieldTheClientReadsBack(t *testing.T) {
	got := userResponse(models.User{
		ID:        "u1",
		Email:     "dev@local",
		Name:      "Ada",
		OrgName:   "Acme",
		CreatedAt: time.Unix(0, 0).UTC(),
	}, "EUR")

	want := []string{
		"createdAt",
		"displayCurrency",
		"email",
		"id",
		"name",
		"needsOnboarding",
		"orgName",
	}

	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if len(keys) != len(want) {
		t.Fatalf("key set changed: want %v, got %v", want, keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("key set changed: want %v, got %v", want, keys)
		}
	}
}

// needsOnboarding is derived, not passed in: an OAuth account arrives with no
// name and must be prompted, while a saved profile never should be.
func TestUserResponseDerivesNeedsOnboardingFromTheName(t *testing.T) {
	unnamed := userResponse(models.User{ID: "u1"}, models.DefaultCurrency)
	if unnamed["needsOnboarding"] != true {
		t.Error("want an account with no name to still need onboarding")
	}
	named := userResponse(models.User{ID: "u1", Name: "Ada"}, models.DefaultCurrency)
	if named["needsOnboarding"] != false {
		t.Error("want a named account not to be prompted again")
	}
}
