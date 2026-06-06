package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/respond"
)

func (d *Deps) X402Quote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		respond.Error(w, http.StatusBadRequest, "url required")
		return
	}
	quote, err := nodes.QuoteX402(r.Context(), body.URL)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, quote)
}
