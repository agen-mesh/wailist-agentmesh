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

func (d *Deps) TriggerRun(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "id")
	d.startRun(w, r, workflowID, "manual", true)
}

func (d *Deps) PublicTrigger(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "workflowId")
	d.startRunSync(w, r, workflowID)
}

// startRunSync runs a workflow and waits for completion, returning the agent output.
// Used by public webhook triggers so the caller gets the result in the HTTP response.
func (d *Deps) startRunSync(w http.ResponseWriter, r *http.Request, workflowID string) {
	ctx := r.Context()

	wf, err := d.Store.GetWorkflow(ctx, workflowID)
	if err != nil || wf.Status != models.WorkflowStatusDeployed {
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

	var inputBody any
	json.NewDecoder(r.Body).Decode(&inputBody)
	inputJSON, _ := json.Marshal(inputBody)

	run, err := d.Store.CreateRun(ctx, workflowID, "webhook", inputJSON)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	wf.Nodes = decryptNodes(wf.Nodes, d.EncryptionKey)
	d.Broker.Create(run.ID)
	d.Engine.Start(wf, run)

	// Wait for the engine to finish (up to 60s).
	select {
	case <-d.Broker.Done(run.ID):
	case <-time.After(60 * time.Second):
		respond.JSON(w, http.StatusAccepted, map[string]string{"runId": run.ID, "status": "timeout"})
		return
	case <-ctx.Done():
		return
	}

	output, _ := d.Store.GetLastAgentOutput(ctx, run.ID)
	finalRun, _ := d.Store.GetRun(ctx, run.ID)
	respond.JSON(w, http.StatusOK, map[string]any{
		"runId":  run.ID,
		"status": string(finalRun.Status),
		"output": output,
	})
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
		case <-time.After(30 * time.Second):
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
