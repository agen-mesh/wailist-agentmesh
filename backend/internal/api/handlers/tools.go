package handlers

import (
	"encoding/json"
	"log"
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
		log.Printf("x402 quote error: %v", err)
		respond.Error(w, http.StatusBadGateway, "upstream fetch failed")
		return
	}
	respond.JSON(w, http.StatusOK, quote)
}
