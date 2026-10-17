package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
)

func TestAgentFeeHoldLifecycle(t *testing.T) {
	for _, mode := range []string{
		"charge", "charge_after_cancel", "release", "release_after_cancel", "release_before_charge",
		"byok", "zero", "insufficient", "cancelled_reservation", "settings_error", "negative",
	} {
		t.Run(mode, func(t *testing.T) {
			fee := models.PlatformKeyStandardFeeUSDMicros
			balance := fee
			if mode == "byok" || mode == "zero" {
				balance = 0
			}
			if mode == "insufficient" {
				balance--
			}
			runner, store, wf, run := flatFeeFixture(t, balance)
			owner := wf.UserID
			var total int64
			runner.runBilling.Store(run.ID, &total)
			defer runner.runBilling.Delete(run.ID)
			defer runner.trackSpendLimit(wf, run)()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancelled_reservation" {
				if err := runner.checkCallSpend(ctx, wf, run, fee); err != nil {
					t.Fatal(err)
				}
				cancel()
			}
			if mode == "settings_error" {
				wf.UserID = "00000000-0000-0000-0000-000000000001"
			}
			if mode == "zero" {
				fee = 0
			}
			if mode == "negative" {
				fee = -1
			}

			hold, err := runner.holdAgentFee(ctx, wf, run, "agent", fee, mode != "byok", "gpt-4.1")
			var wantErr error
			switch mode {
			case "insufficient":
				wantErr = db.ErrInsufficientCredits
			case "cancelled_reservation", "settings_error", "negative":
				wantErr = errBillingCheckFailed
			}
			if wantErr != nil {
				if !errors.Is(err, wantErr) || !isBillingRefusal(err) || hold != nil {
					t.Fatalf("hold=%v error=%v, want billing refusal %v", hold, err, wantErr)
				}
				if mode == "cancelled_reservation" && err.Error() != "reserving agent fee: the billing check could not be completed" {
					t.Fatalf("reservation error exposed internal detail: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer hold.release(context.Background())
				remaining, readErr := store.GetCreditBalance(t.Context(), owner)
				if readErr != nil || remaining != 0 {
					t.Fatalf("balance while held=%d error=%v, want 0", remaining, readErr)
				}
				if mode == "charge_after_cancel" || mode == "release_after_cancel" {
					cancel()
				}
				switch mode {
				case "charge", "charge_after_cancel", "byok", "zero":
					hold.charge(ctx, 12, 8)
					hold.charge(ctx, 99, 99)
					hold.release(ctx)
				default:
					hold.release(ctx)
					hold.release(ctx)
					if mode == "release_before_charge" {
						hold.charge(ctx, 12, 8)
					}
				}
			}

			wantEntries := 0
			wantTotal := int64(0)
			if mode == "charge" || mode == "charge_after_cancel" {
				balance = 0
				wantEntries = 1
				wantTotal = fee
			}
			remaining, readErr := store.GetCreditBalance(t.Context(), owner)
			if readErr != nil || remaining != balance {
				t.Fatalf("final balance=%d error=%v, want %d", remaining, readErr, balance)
			}
			entries, readErr := store.ListDebitLedger(t.Context(), run.ID)
			if readErr != nil || len(entries) != wantEntries {
				t.Fatalf("ledger=%+v error=%v, want %d entries", entries, readErr, wantEntries)
			}
			if wantEntries == 1 {
				entry := entries[0]
				if entry.NodeID != "agent" || entry.AmountUSDMicros != fee || entry.Kind != models.DebitKindPlatformKeyLLMFee ||
					entry.Model == nil || *entry.Model != "gpt-4.1" || entry.TokensIn == nil || *entry.TokensIn != 12 || entry.TokensOut == nil || *entry.TokensOut != 8 {
					t.Fatalf("unexpected committed usage: %+v", entry)
				}
			}
			if got := atomic.LoadInt64(&total); got != wantTotal {
				t.Fatalf("run billing=%d, want %d", got, wantTotal)
			}
		})
	}
}

func TestAgentFeeHoldConcurrentReservations(t *testing.T) {
	fee := models.PlatformKeyStandardFeeUSDMicros
	runner, store, wf, run := flatFeeFixture(t, fee)
	defer runner.trackSpendLimit(wf, run)()
	type result struct {
		hold *agentFeeHold
		err  error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			select {
			case <-start:
			case <-t.Context().Done():
				results <- result{err: t.Context().Err()}
				return
			}
			hold, err := runner.holdAgentFee(t.Context(), wf, run, "agent", fee, true, "gpt-4.1")
			results <- result{hold: hold, err: err}
		}()
	}
	close(start)
	var holds []*agentFeeHold
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err == nil {
			holds = append(holds, got.hold)
			defer got.hold.release(context.Background())
		} else if !errors.Is(got.err, db.ErrInsufficientCredits) {
			t.Errorf("unexpected reservation error: %v", got.err)
		}
	}
	if len(holds) != 1 {
		t.Fatalf("granted %d holds from one fee, want 1", len(holds))
	}
	balance, err := store.GetCreditBalance(t.Context(), wf.UserID)
	if err != nil || balance != 0 {
		t.Fatalf("held balance=%d error=%v, want 0", balance, err)
	}
	holds[0].charge(t.Context(), 3, 4)
	entries, err := store.ListDebitLedger(t.Context(), run.ID)
	if err != nil || len(entries) != 1 || entries[0].AmountUSDMicros != fee {
		t.Fatalf("ledger=%+v error=%v, want one fee", entries, err)
	}
}
