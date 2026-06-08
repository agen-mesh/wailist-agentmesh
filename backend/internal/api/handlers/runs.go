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
	}

	var inputBody any
	json.NewDecoder(r.Body).Decode(&inputBody)
	inputJSON, _ := json.Marshal(inputBody)

	run, err := d.Store.CreateRun(ctx, workflowID, triggeredBy, inputJSON)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

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
	w.Header().Set("Access-Control-Allow-Origin", "*")

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
