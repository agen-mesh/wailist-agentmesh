package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

// ListPublishedWorkflows — GET /marketplace/workflows?q=&limit=24&offset=0 (public)
func (d *Deps) ListPublishedWorkflows(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 || limit > 100 {
		limit = 24
	}
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}
	workflows, err := d.Store.ListPublishedWorkflows(r.Context(), q, limit, offset)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if workflows == nil {
		workflows = []models.PublishedWorkflow{}
	}
	respond.JSON(w, http.StatusOK, map[string]any{"workflows": workflows})
}

// PublishWorkflow — POST /marketplace/workflows (protected)
func (d *Deps) PublishWorkflow(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	var body struct {
		WorkflowID  string   `json:"workflowId"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
		FeePerRun   float64  `json:"feePerRun"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.WorkflowID == "" || body.Title == "" {
		respond.Error(w, http.StatusBadRequest, "workflowId and title required")
		return
	}

	wf, err := d.Store.GetWorkflow(r.Context(), body.WorkflowID)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}

	graph := models.WorkflowGraph{Nodes: wf.Nodes, Edges: wf.Edges}
	if body.Tags == nil {
		body.Tags = []string{}
	}
	pw, err := d.Store.PublishWorkflow(r.Context(), userID, body.Title, body.Description, body.Tags, graph, body.FeePerRun)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond.JSON(w, http.StatusCreated, pw)
}

// ImportWorkflow — POST /marketplace/workflows/:id/import (protected)
func (d *Deps) ImportWorkflow(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	publishedID := chi.URLParam(r, "id")
	wf, err := d.Store.ImportPublishedWorkflow(r.Context(), userID, publishedID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "published workflow not found")
		return
	}
	respond.JSON(w, http.StatusCreated, wf)
}

// UpvoteWorkflow — POST /marketplace/workflows/:id/upvote (protected)
func (d *Deps) UpvoteWorkflow(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	publishedID := chi.URLParam(r, "id")
	count, upvoted, err := d.Store.ToggleUpvote(r.Context(), userID, publishedID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"upvoted": upvoted, "count": count})
}
