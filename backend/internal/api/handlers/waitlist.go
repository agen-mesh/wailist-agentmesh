package handlers

import "net/http"

func (d *Deps) JoinWaitlist(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
