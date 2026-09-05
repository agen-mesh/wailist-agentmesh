package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/agentmesh/backend/internal/respond"
)

// Device registration for push notifications.
//
// Both endpoints live inside the authenticated group: a device belongs to the
// person signed in on it, and nothing in the request carries a secret that
// could authorise it otherwise -- the same reasoning as the geofence routes.

// An FCM registration token runs to about 160 characters today, but Google has
// never promised a length and has changed the format before. This ceiling
// exists only to stop an unbounded body reaching the database, so it sits well
// clear of anything real rather than tight to the current shape.
const maxDeviceTokenLen = 4096

type deviceTokenBody struct {
	Token string `json:"token"`
	// Optional. Only "android" exists today and the column defaults to it, so
	// a client that omits this stays correct.
	Platform string `json:"platform"`
}

// RegisterDevice records this device as a notification target for the signed-in
// user, or moves it to them if it was registered to somebody else.
//
// Idempotent: the app calls it on every sign-in, so re-registering the same
// token is the normal case rather than an error. See Store.RegisterDeviceToken
// for why the upsert is keyed on the token alone.
func (d *Deps) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	var body deviceTokenBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" {
		respond.Error(w, http.StatusBadRequest, "token is required")
		return
	}
	if len(token) > maxDeviceTokenLen {
		respond.Error(w, http.StatusBadRequest, "token is too long")
		return
	}

	rec, err := d.Store.RegisterDeviceToken(ctx, userID, token, strings.TrimSpace(body.Platform))
	if err != nil {
		// Logged rather than returned. A store error can carry a constraint
		// name or a fragment of the statement, and this endpoint is reachable
		// by anything holding a session -- the caller can do nothing with the
		// detail, and the operator is the one who needs it.
		log.Printf("devices: registering a token for user %s failed: %v", userID, err)
		respond.Error(w, http.StatusInternalServerError, "could not register this device")
		return
	}

	// The token is deliberately NOT echoed back. The client already has it,
	// and a response body carrying a device's push address is one more place
	// for a proxy or an access log to keep it.
	respond.JSON(w, http.StatusOK, map[string]string{
		"id":       rec.ID,
		"platform": rec.Platform,
	})
}

// UnregisterDevice removes this device as a notification target.
//
// Called on sign-out. Answers 204 whether or not a row existed, on purpose: a
// device that never managed to register must still sign out cleanly, and
// distinguishing the two cases would answer a question about which tokens
// exist that the caller has no business asking.
func (d *Deps) UnregisterDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	var body deviceTokenBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" {
		respond.Error(w, http.StatusBadRequest, "token is required")
		return
	}

	if err := d.Store.DeleteDeviceToken(ctx, userID, token); err != nil {
		log.Printf("devices: removing a token for user %s failed: %v", userID, err)
		respond.Error(w, http.StatusInternalServerError, "could not unregister this device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
