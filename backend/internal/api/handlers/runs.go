package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

// runTriggerCooldown is the minimum gap between two run starts for the same
// workflow, across BOTH TriggerRun (authenticated) and PublicTrigger
// (unauthenticated webhook) -- the latter is the real target, since it's
// reachable by anyone who has the URL with no rate limiting of its own
// otherwise, but it's enforced on the shared startRun path so a bot hitting
// either endpoint (or both, alternating) is caught the same way. A flat
// constant rather than a per-workflow setting: this is a blunt deterrent
// against naive burst-triggering, not a real per-customer rate limit --
// legitimate rapid re-runs are rare enough that 5s costs nothing.
const runTriggerCooldown = 5 * time.Second

func (d *Deps) TriggerRun(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "id")
	d.startRun(w, r, workflowID, "manual", true)
}

func (d *Deps) PublicTrigger(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "workflowId")
	d.startRun(w, r, workflowID, "webhook", false)
}

func (d *Deps) startRun(w http.ResponseWriter, r *http.Request, workflowID, triggeredBy string, checkOwner bool) {
	ctx := r.Context()

	wf, err := d.Store.GetWorkflow(ctx, workflowID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}
	if checkOwner {
		userID, _ := ctx.Value(CtxUserID).(string)
		if wf.UserID != userID {
			respond.Error(w, http.StatusNotFound, "workflow not found")
			return
		}
	} else {
		// Public webhook path: only deployed workflows with an explicit trigger node
		// can be invoked without authentication. Return 404 on all failures to avoid
		// leaking whether a workflow ID exists.
		if wf.Status != models.WorkflowStatusDeployed {
			respond.Error(w, http.StatusNotFound, "workflow not found")
			return
		}
		hasTrigger := false
		for _, n := range wf.Nodes {
			if n.Type == models.NodeTypeTrigger {
				hasTrigger = true
				break
			}
		}
		if !hasTrigger {
			respond.Error(w, http.StatusNotFound, "workflow not found")
			return
		}
	}

	// Checked only after the existence/ownership/deploy checks above, never
	// before: doing it earlier would let an unauthenticated caller on the
	// PublicTrigger path distinguish "workflow doesn't exist" (404) from
	// "workflow exists but is on cooldown" (429) for an ID they have no
	// business confirming -- the exact leak the deploy/trigger checks above
	// already go out of their way to avoid.
	if retryAfter, blocked := d.checkRunCooldown(workflowID, time.Now()); blocked {
		w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
		respond.Error(w, http.StatusTooManyRequests,
			fmt.Sprintf("this workflow was triggered too recently — wait %.0fs and try again", retryAfter.Seconds()))
		return
	}

	var inputBody any
	json.NewDecoder(r.Body).Decode(&inputBody)
	inputJSON, _ := json.Marshal(inputBody)

	run, err := d.Store.CreateRun(ctx, workflowID, triggeredBy, inputJSON)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	wf.Nodes = decryptNodes(wf.Nodes, d.EncryptionKey)
	d.Broker.Create(run.ID)
	d.Engine.Start(wf, run)

	respond.JSON(w, http.StatusAccepted, map[string]string{"runId": run.ID})
}

// checkRunCooldown atomically checks and records this workflow's
// last-triggered time, all under one lock, so a burst of concurrent
// requests can't all read "no recent run" before any of them writes their
// own attempt -- a check-then-write split across two calls would let
// exactly that race slip a whole burst through on the very first window.
func (d *Deps) checkRunCooldown(workflowID string, now time.Time) (retryAfter time.Duration, blocked bool) {
	d.runCooldownMu.Lock()
	defer d.runCooldownMu.Unlock()

	if last, ok := d.lastRunAt[workflowID]; ok {
		if elapsed := now.Sub(last); elapsed < runTriggerCooldown {
			return runTriggerCooldown - elapsed, true
		}
	}
	if d.lastRunAt == nil {
		d.lastRunAt = make(map[string]time.Time)
	}
	d.lastRunAt[workflowID] = now
	return 0, false
}

func (d *Deps) StopWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	wf, err := d.Store.GetWorkflow(ctx, id)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}

	d.Engine.Stop(id)
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) GetRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	run, err := d.Store.GetRun(ctx, runID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "run not found")
		return
	}
	wf, err := d.Store.GetWorkflow(ctx, run.WorkflowID)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "run not found")
		return
	}
	logs, err := d.Store.GetRunLogs(ctx, runID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []models.RunLog{}
	}
	respond.JSON(w, http.StatusOK, map[string]any{"run": run, "logs": logs})
}

func (d *Deps) StreamRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	run, err := d.Store.GetRun(ctx, runID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "run not found")
		return
	}
	wf, err := d.Store.GetWorkflow(ctx, run.WorkflowID)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "run not found")
		return
	}
	_ = wf

	flusher, ok := w.(http.Flusher)
	if !ok {
		respond.Error(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsub := d.Broker.Subscribe(runID)
	defer unsub()

	done := d.Broker.Done(runID)

	for {
		select {
		case ev, open := <-ch:
			if !open {
				return
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", string(b))
			flusher.Flush()
		case <-done:
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			return
		case <-r.Context().Done():
			return
		// Keepalive well under the ~35s at which these connections were
		// observed dying with no data flowing, so an idle stream keeps
		// proving itself alive instead of looking hung to the browser or any
		// intermediary. A run can legitimately be silent far longer than
		// this: a single agent step takes ~70s.
		case <-time.After(10 * time.Second):
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
