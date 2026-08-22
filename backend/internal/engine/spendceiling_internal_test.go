package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sse"
)

// CreateCreditTransaction takes INR paise and derives the credit itself:
//
//	credit_usd_micros = round(amountINRPaise / 100 * fxRate * 1e6)
//
// A rate of 1e-4 makes that conversion the identity, so the fixture funds
// exactly fundUSDMicros and every assertion below can be written in the same
// units preflightCheck compares against. Passing 1.0 instead scales the balance
// by 10,000x, quietly funding $1000 where $0.10 was meant — which makes a
// balance assertion impossible to fail rather than making it wrong.
const paiseToMicrosIdentityRate = 0.0001

// ceilingFixture seeds a user funded with exactly fundUSDMicros, plus a
// workflow. Fund generously to isolate the ceiling; fund below the charge to
// exercise the balance branch underneath it.
func ceilingFixture(t *testing.T, fundUSDMicros int64) (*Runner, models.Workflow) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	ctx := context.Background()
	email := fmt.Sprintf("ceiling-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	orderID := fmt.Sprintf("fund_%s_%d", user.ID, time.Now().UnixNano())
	if _, err := store.CreateCreditTransaction(ctx, user.ID, orderID, fundUSDMicros, paiseToMicrosIdentityRate); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CompleteCreditTransaction(ctx, "cashfree", orderID, "pay_"+orderID); err != nil {
		t.Fatal(err)
	}

	wf, err := store.CreateWorkflow(ctx, "Spend Ceiling Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })
	wf.UserID = user.ID

	return NewRunner(store, sse.NewBroker(), nil, "", "", "", X402Config{USDCAssetID: 10458941}), wf
}

func setCeiling(t *testing.T, r *Runner, userID string, ceilingUSDMicros int64) {
	t.Helper()
	patch := models.UserSettingsPatch{SetMaxCallSpend: true, MaxCallSpendUSDMicros: &ceilingUSDMicros}
	if _, err := r.store.UpsertUserSettings(context.Background(), userID, patch); err != nil {
		t.Fatal(err)
	}
}

// The setting has to actually stop a spend, or the settings page is claiming a
// safety control the engine ignores.
func TestPreflightCheckRejectsChargeAboveTheUserCeiling(t *testing.T) {
	r, wf := ceilingFixture(t, 1_000_000)
	setCeiling(t, r, wf.UserID, 50_000)

	err := r.preflightCheck(context.Background(), wf, 60_000)
	if err == nil {
		t.Fatal("want a charge above the ceiling to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "per-call limit") {
		t.Fatalf("want a per-call-limit error, got %v", err)
	}
}

// The boundary is inclusive: spending exactly the ceiling is allowed, so a user
// who sets it to their expected call price isn't blocked from every run.
func TestPreflightCheckAllowsChargeAtTheUserCeiling(t *testing.T) {
	r, wf := ceilingFixture(t, 1_000_000)
	setCeiling(t, r, wf.UserID, 50_000)

	if err := r.preflightCheck(context.Background(), wf, 50_000); err != nil {
		t.Fatalf("want a charge exactly at the ceiling to pass, got %v", err)
	}
}

// No ceiling must behave exactly as before this change — the global
// MaxSingleX402QuoteUSDMicros is still the only bound.
func TestPreflightCheckIgnoresAnUnsetCeiling(t *testing.T) {
	r, wf := ceilingFixture(t, 1_000_000)

	if err := r.preflightCheck(context.Background(), wf, 900_000); err != nil {
		t.Fatalf("want no ceiling to allow any affordable charge, got %v", err)
	}
}

// A ceiling must never turn into a spending permit: an unaffordable charge is
// still refused for lack of credits even when it sits under the limit.
func TestPreflightCheckStillEnforcesBalanceUnderACeiling(t *testing.T) {
	r, wf := ceilingFixture(t, 100_000)
	setCeiling(t, r, wf.UserID, 900_000)

	err := r.preflightCheck(context.Background(), wf, 500_000)
	if err == nil || !strings.Contains(err.Error(), "insufficient credits") {
		t.Fatalf("want an insufficient-credits error, got %v", err)
	}
}

// preflightCheck skips the ceiling lookup at or below the probe floor. The
// balance check underneath it must still run, or a user with no credits would
// sail through every tool402 pre-check in an agent's function-calling loop.
func TestPreflightCheckEnforcesBalanceAtTheProbeFloor(t *testing.T) {
	r, wf := ceilingFixture(t, 10_000) // $0.01, under the $0.05 probe floor
	setCeiling(t, r, wf.UserID, 900_000)

	err := r.preflightCheck(context.Background(), wf, models.X402ProbeFloorUSDMicros)
	if err == nil || !strings.Contains(err.Error(), "insufficient credits") {
		t.Fatalf("want an insufficient-credits error at the probe floor, got %v", err)
	}
}
