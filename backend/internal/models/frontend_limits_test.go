package models_test

import (
	"os"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// The spend-limit form mirrors two backend constants as its own literals so it
// can reject an out-of-range value before a round trip. Nothing linked the two,
// so changing the Go value would leave the form silently stale -- rejecting
// amounts the server now accepts, or offering ones it refuses.
//
// This reads the actual component and fails if they drift. It is a blunt
// instrument (a string match against a .tsx file), but the alternative is
// either no guard at all or reshaping the settings API for a pre-flight hint.
func TestFrontendSpendLimitsMirrorTheBackendConstants(t *testing.T) {
	const path = "../../../frontend/src/components/settings/sections/Execution.tsx"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("frontend not present in this checkout: %v", err)
	}
	body := string(src)

	// $1000/call, written as a plain dollar figure in the form.
	if models.MaxSingleX402QuoteUSDMicros != 1_000_000_000 {
		t.Fatalf("backend cap changed to %d — update Execution.tsx's PLATFORM_CEILING_USD and this test",
			models.MaxSingleX402QuoteUSDMicros)
	}
	if !strings.Contains(body, "const PLATFORM_CEILING_USD = 1000;") {
		t.Error("Execution.tsx no longer mirrors MaxSingleX402QuoteUSDMicros ($1000/call)")
	}

	// $0.05, the floor every paid call is checked against.
	if models.X402ProbeFloorUSDMicros != 50_000 {
		t.Fatalf("probe floor changed to %d — update Execution.tsx's MIN_CEILING_USD and this test",
			models.X402ProbeFloorUSDMicros)
	}
	if !strings.Contains(body, "const MIN_CEILING_USD = 0.05;") {
		t.Error("Execution.tsx no longer mirrors X402ProbeFloorUSDMicros ($0.05)")
	}
}
