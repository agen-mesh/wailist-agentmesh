package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/agentmesh/backend/internal/respond"
)

func (d *Deps) SignUp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Org      string `json:"org"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	respond.JSON(w, http.StatusOK, map[string]string{"token": "dev-token"})
}

func (d *Deps) SignIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	respond.JSON(w, http.StatusOK, map[string]string{"token": "dev-token"})
}

func (d *Deps) SignOut(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) Me(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"id": "dev", "email": "dev@agentmesh.local"})
}
