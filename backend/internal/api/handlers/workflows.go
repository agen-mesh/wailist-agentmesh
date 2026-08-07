package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

func (d *Deps) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	wfs, err := d.Store.ListWorkflows(r.Context(), userID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wfs == nil {
		wfs = []models.Workflow{}
	}
	for i := range wfs {
		wfs[i].Nodes = maskNodes(wfs[i].Nodes)
	}
	respond.JSON(w, http.StatusOK, wfs)
}

func (d *Deps) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Name == "" {
		body.Name = "Untitled workflow"
	}
	wf, err := d.Store.CreateWorkflow(r.Context(), body.Name, userID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond.JSON(w, http.StatusCreated, wf)
}

func (d *Deps) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID, _ := r.Context().Value(CtxUserID).(string)
	wf, err := d.Store.GetWorkflow(r.Context(), id)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}
	wf.Nodes = maskNodes(wf.Nodes)
	respond.JSON(w, http.StatusOK, wf)
}

func (d *Deps) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID, _ := r.Context().Value(CtxUserID).(string)
	existing, err := d.Store.GetWorkflow(r.Context(), id)
	if err != nil || existing.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}
	var body struct {
		Name  string                `json:"name"`
		Nodes []models.WorkflowNode `json:"nodes"`
		Edges []models.WorkflowEdge `json:"edges"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	encryptedNodes := encryptNodes(body.Nodes, d.EncryptionKey, existing.Nodes)
	graph := models.WorkflowGraph{Nodes: encryptedNodes, Edges: body.Edges}
	wf, err := d.Store.UpdateWorkflow(r.Context(), id, body.Name, graph)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	wf.Nodes = maskNodes(wf.Nodes)
	respond.JSON(w, http.StatusOK, wf)
}

func (d *Deps) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID, _ := r.Context().Value(CtxUserID).(string)
	existing, err := d.Store.GetWorkflow(r.Context(), id)
	if err != nil || existing.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}
	if err := d.Store.DeleteWorkflow(r.Context(), id); err != nil {
		// tendril_leases.workflow_id is ON DELETE RESTRICT, not CASCADE
		// (migration 000018) -- a workflow with lease history (active or
		// released) can't be silently destroyed along with the only copy
		// of an active lease's encrypted credentials. Surface that as a
		// clear 409, not the raw Postgres constraint-violation message.
		// Both tendril_leases FKs are RESTRICT: workflow_id fires when the
		// workflow itself is deleted, run_id when the cascade reaches its runs.
		if strings.Contains(err.Error(), "tendril_leases_workflow_id_fkey") ||
			strings.Contains(err.Error(), "tendril_leases_run_id_fkey") {
			respond.Error(w, http.StatusConflict, "this workflow has Tendril machine lease history and can't be deleted")
			return
		}
		// Anything else is ours to fix, not the user's to read: a raw driver
		// message (constraint names, SQLSTATE) was being rendered straight into
		// the workflows list.
		log.Printf("delete workflow %s: %v", id, err)
		respond.Error(w, http.StatusInternalServerError, "could not delete this workflow")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
