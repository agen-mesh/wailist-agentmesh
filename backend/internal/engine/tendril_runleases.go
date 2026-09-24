package engine

import (
	"context"
	"log"
	"time"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

type runLeaseLister interface {
	ListActiveTendrilLeasesForRun(ctx context.Context, runID string) ([]models.TendrilLease, error)
}

// tendrilNodesDoing returns the graph's tendril nodes performing action.
// ExecuteTendril treats an empty action as "rent", so this does too.
func tendrilNodesDoing(wf models.Workflow, action string) []models.WorkflowNode {
	var out []models.WorkflowNode
	for _, n := range wf.Nodes {
		if n.Type != models.NodeTypeTendril {
			continue
		}
		a := n.TendrilAction
		if a == "" {
			a = "rent"
		}
		if a == action {
			out = append(out, n)
		}
	}
	return out
}

// releaseRunLeasesWith is the testable core of end-of-run lease cleanup. A
// workflow with a Release step means its author wanted every machine it
// rents handed back by the end of the run. When a failure, a cancel or an
// untaken branch keeps that step from running, the machine would otherwise
// meter until the reaper closes it at the end of its reserved hours. A
// workflow with no Release step deliberately leaves its lease open (to SSH
// into it, say), so nothing is released for it.
func releaseRunLeasesWith(ctx context.Context, wf models.Workflow, runID string, lister runLeaseLister, release func(context.Context, models.TendrilLease) error) int {
	if len(tendrilNodesDoing(wf, "release")) == 0 {
		return 0
	}
	leases, err := lister.ListActiveTendrilLeasesForRun(ctx, runID)
	if err != nil {
		log.Printf("tendril: listing run %s's open leases for cleanup failed, the reaper will close them: %v", runID, err)
		return 0
	}
	released := 0
	for _, lease := range leases {
		if err := release(ctx, lease); err != nil {
			log.Printf("tendril: end-of-run release of lease %s (run %s) failed, the reaper will close it: %v", lease.LeaseID, runID, err)
			continue
		}
		released++
	}
	return released
}

func (r *Runner) releaseRunLeases(ctx context.Context, wf models.Workflow, run models.Run) {
	if r.tendrilClient == nil {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	n := releaseRunLeasesWith(rctx, wf, run.ID, r.store, func(ctx context.Context, lease models.TendrilLease) error {
		_, _, err := nodes.ReleaseLease(ctx, nodes.TendrilConfig{
			Client: r.tendrilClient, Store: r.store, EncryptKey: r.encryptionKey,
		}, lease)
		return err
	})
	if n > 0 {
		log.Printf("tendril: released %d lease(s) run %s left open", n, run.ID)
	}
}

// reopenReleasedRentsWith lets Resume rent again. releaseRunLeases hands a
// failed run's machine back, but Resume skips the Rent step that already
// succeeded, so the resumed jobs would find no lease for this run. Dropping
// that Rent step's success from seed makes it execute again. Only done while
// a Release step is still to run: if every Release already succeeded, the
// workflow was finished with its machine and renting again would be waste.
func reopenReleasedRentsWith(ctx context.Context, wf models.Workflow, runID string, seed map[string]models.RunLog, lister runLeaseLister) error {
	rents := tendrilNodesDoing(wf, "rent")
	releases := tendrilNodesDoing(wf, "release")
	if len(rents) == 0 || len(releases) == 0 {
		return nil
	}
	pending := false
	for _, n := range releases {
		if seed[n.ID].Status != models.LogStatusSuccess {
			pending = true
		}
	}
	if !pending {
		return nil
	}
	leases, err := lister.ListActiveTendrilLeasesForRun(ctx, runID)
	if err != nil {
		return err
	}
	open := make(map[string]bool, len(leases))
	for _, l := range leases {
		open[l.NodeID] = true
	}
	for _, n := range rents {
		if seed[n.ID].Status == models.LogStatusSuccess && !open[n.ID] {
			delete(seed, n.ID)
		}
	}
	return nil
}
