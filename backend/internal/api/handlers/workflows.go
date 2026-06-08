package handlers

import (
	"encoding/json"
	"net/http"

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
	graph := models.WorkflowGraph{Nodes: body.Nodes, Edges: body.Edges}
	wf, err := d.Store.UpdateWorkflow(r.Context(), id, body.Name, graph)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
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
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
