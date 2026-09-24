package engine

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

type fakeRunLeases struct {
	leases []models.TendrilLease
	err    error
	asked  string
}

func (f *fakeRunLeases) ListActiveTendrilLeasesForRun(_ context.Context, runID string) ([]models.TendrilLease, error) {
	f.asked = runID
	return f.leases, f.err
}

func lifecycleWorkflow(withRelease bool) models.Workflow {
	wf := models.Workflow{Nodes: []models.WorkflowNode{
		{ID: "t", Type: models.NodeTypeTrigger},
		{ID: "rent", Type: models.NodeTypeTendril, TendrilAction: "rent"},
		{ID: "job", Type: models.NodeTypeTendril, TendrilAction: "run"},
	}}
	if withRelease {
		wf.Nodes = append(wf.Nodes, models.WorkflowNode{ID: "rel", Type: models.NodeTypeTendril, TendrilAction: "release"})
	}
	return wf
}

func TestReleaseRunLeases(t *testing.T) {
	open := []models.TendrilLease{{LeaseID: "a", NodeID: "rent"}, {LeaseID: "b", NodeID: "rent"}}

	t.Run("releases every open lease when the workflow has a Release step", func(t *testing.T) {
		lister := &fakeRunLeases{leases: open}
		var released []string
		n := releaseRunLeasesWith(context.Background(), lifecycleWorkflow(true), "run1", lister,
			func(_ context.Context, l models.TendrilLease) error {
				released = append(released, l.LeaseID)
				if l.LeaseID == "a" {
					return errors.New("tendril down")
				}
				return nil
			})
		if lister.asked != "run1" {
			t.Errorf("listed leases for run %q, want run1", lister.asked)
		}
		// A failure on one lease must not stop the rest.
		if !slices.Equal(released, []string{"a", "b"}) || n != 1 {
			t.Errorf("attempted %v, released %d; want both attempted, 1 released", released, n)
		}
	})

	t.Run("leaves leases open when the workflow never releases", func(t *testing.T) {
		lister := &fakeRunLeases{leases: open}
		n := releaseRunLeasesWith(context.Background(), lifecycleWorkflow(false), "run1", lister,
			func(context.Context, models.TendrilLease) error {
				t.Error("released a lease the workflow meant to keep")
				return nil
			})
		if n != 0 || lister.asked != "" {
			t.Errorf("released %d, listed %q; want nothing touched", n, lister.asked)
		}
	})

	t.Run("a listing failure releases nothing", func(t *testing.T) {
		n := releaseRunLeasesWith(context.Background(), lifecycleWorkflow(true), "run1",
			&fakeRunLeases{err: errors.New("db down")},
			func(context.Context, models.TendrilLease) error { return nil })
		if n != 0 {
			t.Errorf("released %d, want 0", n)
		}
	})
}

func TestReopenReleasedRents(t *testing.T) {
	ok := models.RunLog{Status: models.LogStatusSuccess}
	failed := models.RunLog{Status: models.LogStatusFailed}

	cases := []struct {
		name       string
		wf         models.Workflow
		seed       map[string]models.RunLog
		open       []models.TendrilLease
		wantRented bool
	}{
		{"released lease with Release still to run re-rents",
			lifecycleWorkflow(true), map[string]models.RunLog{"rent": ok, "job": failed}, nil, true},
		{"lease still open keeps the rent",
			lifecycleWorkflow(true), map[string]models.RunLog{"rent": ok, "job": failed},
			[]models.TendrilLease{{NodeID: "rent"}}, false},
		{"Release already done does not rent again",
			lifecycleWorkflow(true), map[string]models.RunLog{"rent": ok, "job": ok, "rel": ok}, nil, false},
		{"no Release step leaves Resume alone",
			lifecycleWorkflow(false), map[string]models.RunLog{"rent": ok, "job": failed}, nil, false},
		{"an empty action is a rent",
			models.Workflow{Nodes: []models.WorkflowNode{
				{ID: "rent", Type: models.NodeTypeTendril},
				{ID: "rel", Type: models.NodeTypeTendril, TendrilAction: "release"},
			}}, map[string]models.RunLog{"rent": ok}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := reopenReleasedRentsWith(context.Background(), tc.wf, "run1", tc.seed, &fakeRunLeases{leases: tc.open}); err != nil {
				t.Fatal(err)
			}
			_, kept := tc.seed["rent"]
			if kept == tc.wantRented {
				t.Errorf("rent kept in seed = %v, want re-rent = %v", kept, tc.wantRented)
			}
		})
	}

	t.Run("a listing failure is returned", func(t *testing.T) {
		seed := map[string]models.RunLog{"rent": ok}
		err := reopenReleasedRentsWith(context.Background(), lifecycleWorkflow(true), "run1", seed, &fakeRunLeases{err: errors.New("db down")})
		if err == nil {
			t.Error("want the listing error")
		}
	})
}
