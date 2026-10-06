package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
)

// setupRunFixture creates a bare user/workflow/run triple for tests that need
// a valid foreign-key target but don't care about balances. It skips when
// TEST_DATABASE_URL is unset, mirroring every other DB test in this package.
func setupRunFixture(t *testing.T) (store *db.Store, userID, workflowID, runID string) {
	t.Helper()
	store = testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("tendril-lease-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	wf, err := store.CreateWorkflow(ctx, "Tendril Lease Test WF", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	return store, user.ID, wf.ID, run.ID
}

// tendril_leases.lease_id carries a UNIQUE constraint, and nothing in these
// tests can remove the rows they insert -- DeleteWorkflow is RESTRICTed by the
// very lease being tested, which is the point of the first test below. Fixed
// literals therefore passed once and then failed forever against the same
// database on tendril_leases_lease_id_key. Unique per insert, the way the
// credit tests already mint order ids.
func leaseID(prefix string) string {
	return fmt.Sprintf("lease_%s_%d", prefix, time.Now().UnixNano())
}

// tendril_leases.workflow_id must be ON DELETE RESTRICT, not CASCADE
// (migration 000018): a lease row is the ONLY copy of the encrypted SSH
// credentials and lease token needed to release a machine that may still be
// actively metering against the shared pool. Deleting the workflow that
// opened it -- e.g. from the workflows list UI -- must never silently
// destroy that row. (run_id carries the identical constraint, but this repo
// has no DeleteRun to exercise that half through the Store API yet.)
func TestDeleteWorkflowRestrictedByTendrilLease(t *testing.T) {
	store, userID, wfID, runID := setupRunFixture(t)
	ctx := context.Background()

	if _, err := store.InsertTendrilLease(ctx, models.TendrilLease{
		UserID: userID, WorkflowID: wfID, RunID: runID, NodeID: "n2",
		LeaseID: leaseID("restrict"), LeaseTokenEnc: "enc-token",
		TendrilNodeID: "x", RateUSDMicrosPerHour: 1, HoursPurchased: 1,
		ReservedUSDMicros: 1, FundedUntil: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("InsertTendrilLease: %v", err)
	}

	if err := store.DeleteWorkflow(ctx, wfID); err == nil {
		t.Fatal("want DeleteWorkflow to fail while an active lease references this workflow")
	}
}

func TestTendrilLeaseRoundTripAndRelease(t *testing.T) {
	store, userID, wfID, runID := setupRunFixture(t)
	ctx := context.Background()

	wantLeaseID := leaseID("roundtrip")
	in := models.TendrilLease{
		UserID: userID, WorkflowID: wfID, RunID: runID, NodeID: "n2",
		LeaseID: wantLeaseID, LeaseTokenEnc: "enc-token",
		TendrilNodeID: "I8zY887UpE", TendrilNodeLabel: "my-laptop",
		SSHHost: "bore.pub", SSHPort: 41823, SSHUsername: "root",
		SSHCommand:   "ssh root@bore.pub -p 41823",
		SSHPublicKey: "ssh-ed25519 AAAA agentmesh", SSHPrivateKeyEnc: "enc-key",
		RateUSDMicrosPerHour: 6_000_000, HoursPurchased: 2,
		ReservedUSDMicros: 12_010_000,
		FundedUntil:       time.Now().Add(2 * time.Hour),
	}
	saved, err := store.InsertTendrilLease(ctx, in)
	if err != nil {
		t.Fatalf("InsertTendrilLease: %v", err)
	}
	if saved.ID == "" || saved.Status != "active" {
		t.Fatalf("saved = %+v, want an id and status active", saved)
	}

	active, err := store.ListActiveTendrilLeases(ctx, userID)
	if err != nil {
		t.Fatalf("ListActiveTendrilLeases: %v", err)
	}
	if len(active) != 1 || active[0].LeaseID != wantLeaseID {
		t.Fatalf("active = %+v", active)
	}
	if active[0].LeaseTokenEnc != "enc-token" {
		t.Error("lease token did not round-trip")
	}

	if transitioned, err := store.MarkTendrilLeaseReleased(ctx, saved.ID, 3600, 6_000_000); err != nil {
		t.Fatalf("MarkTendrilLeaseReleased: %v", err)
	} else if !transitioned {
		t.Error("want transitioned = true releasing an active lease")
	}
	after, err := store.ListActiveTendrilLeases(ctx, userID)
	if err != nil {
		t.Fatalf("ListActiveTendrilLeases after release: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("released lease still listed as active: %+v", after)
	}

	got, err := store.GetTendrilLease(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetTendrilLease: %v", err)
	}
	if got.Status != "released" || got.ChargedUSDMicros == nil || *got.ChargedUSDMicros != 6_000_000 {
		t.Errorf("released lease = %+v", got)
	}

	// A second release attempt against the same, already-released row (a
	// concurrent double-click, or the reaper racing the user) must be a
	// genuine no-op -- transitioned = false, no error, and the row's data
	// from the FIRST release must not be overwritten by whatever the second
	// caller happened to pass.
	transitioned, err := store.MarkTendrilLeaseReleased(ctx, saved.ID, 9999, 1)
	if err != nil {
		t.Fatalf("MarkTendrilLeaseReleased (second release): %v", err)
	}
	if transitioned {
		t.Error("want transitioned = false releasing an already-released lease")
	}
	after2, err := store.GetTendrilLease(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetTendrilLease: %v", err)
	}
	if after2.ChargedUSDMicros == nil || *after2.ChargedUSDMicros != 6_000_000 {
		t.Errorf("second release overwrote the first release's charged amount: %+v", after2)
	}
}

// The reaper must find leases whose window has closed, and must not find
// ones already released. Expiry is started_at + hours_purchased -- what the
// renter actually paid for -- not funded_until (the shared pool's own
// runway, set far in the future here on both rows to prove it's ignored).
// started_at is always server-defaulted to NOW() at insert (not settable via
// the Go struct), so "already expired" is expressed as HoursPurchased: 0 --
// a window that closes the instant it opens -- rather than backdating the
// timestamp directly.
func TestListExpiredTendrilLeases(t *testing.T) {
	store, userID, wfID, runID := setupRunFixture(t)
	ctx := context.Background()

	past, err := store.InsertTendrilLease(ctx, models.TendrilLease{
		UserID: userID, WorkflowID: wfID, RunID: runID, NodeID: "n2",
		LeaseID: leaseID("old"), LeaseTokenEnc: "e", TendrilNodeID: "x",
		RateUSDMicrosPerHour: 1, HoursPurchased: 0, ReservedUSDMicros: 1,
		FundedUntil: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("insert past: %v", err)
	}
	if _, err := store.InsertTendrilLease(ctx, models.TendrilLease{
		UserID: userID, WorkflowID: wfID, RunID: runID, NodeID: "n3",
		LeaseID: leaseID("future"), LeaseTokenEnc: "e", TendrilNodeID: "x",
		RateUSDMicrosPerHour: 1, HoursPurchased: 1, ReservedUSDMicros: 1,
		FundedUntil: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("insert future: %v", err)
	}

	// ListExpiredTendrilLeases is deliberately unscoped -- the reaper sweeps
	// every user -- so the rows this test inserted are not the only ones it
	// can return. Asserting on the whole result made the test own the entire
	// table: one leftover expired lease from any earlier run, and it failed.
	// Narrowing to this fixture's own user keeps exactly what the test is for
	// (the window that has closed is found, the one still open is not) and
	// drops the assumption it was never entitled to make.
	all, err := store.ListExpiredTendrilLeases(ctx, time.Now())
	if err != nil {
		t.Fatalf("ListExpiredTendrilLeases: %v", err)
	}
	var mine []models.TendrilLease
	for _, l := range all {
		if l.UserID == userID {
			mine = append(mine, l)
		}
	}
	if len(mine) != 1 || mine[0].ID != past.ID {
		t.Fatalf("expired for this user = %+v, want only the closed window", mine)
	}
}
