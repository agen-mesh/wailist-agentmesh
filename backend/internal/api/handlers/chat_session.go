package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/respond"
	"github.com/go-chi/chi/v5"
)

// The console chat transcript, stored server-side so it follows the user
// across browsers and devices instead of living in one browser's
// localStorage. The client owns the transcript's shape and sends it whole.

func (d *Deps) GetChatSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID, _ := r.Context().Value(CtxUserID).(string)
	wf, err := d.Store.GetWorkflow(r.Context(), id)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}
	session, err := d.Store.GetChatSession(r.Context(), id)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "could not load the conversation")
		return
	}
	respond.JSON(w, http.StatusOK, session)
}

func (d *Deps) SaveChatSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID, _ := r.Context().Value(CtxUserID).(string)
	wf, err := d.Store.GetWorkflow(r.Context(), id)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}
	var body struct {
		SessionID string          `json:"sessionId"`
		Messages  json.RawMessage `json:"messages"`
	}
	// Bounded before decoding: an unbounded body would be read into memory
	// whole only to be rejected by the store's own cap.
	if err := json.NewDecoder(io.LimitReader(r.Body, db.MaxChatTranscriptBytes+1024)).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid conversation")
		return
	}
	if len(body.Messages) == 0 {
		body.Messages = json.RawMessage("[]")
	}
	if err := d.Store.SaveChatSession(r.Context(), id, body.SessionID, body.Messages); err != nil {
		if errors.Is(err, db.ErrInvalidTranscript) {
			respond.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		// A database failure is not the client's to correct, and its text
		// carries internals the chat should never show.
		log.Printf("save chat transcript for workflow %s: %v", id, err)
		respond.Error(w, http.StatusInternalServerError, "could not save the conversation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
