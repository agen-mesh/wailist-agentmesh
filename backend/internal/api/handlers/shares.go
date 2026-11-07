package handlers

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/agentmesh/backend/internal/arkivshare"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/respond"
)

const (
	shareMaxBytes   = 131072
	shareWriteLimit = 30 * time.Second
)

var entityKeyPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

type shareReceipt struct {
	EntityKey string `json:"entityKey"`
	ChainID   int    `json:"chainId"`
}

func (d *Deps) CreateShare(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/octet-stream" {
		respond.Error(w, http.StatusUnsupportedMediaType, "share_type")
		return
	}
	if d.ShareWriter == nil || d.Store == nil {
		respond.Error(w, http.StatusServiceUnavailable, "share_disabled")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, shareMaxBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respond.Error(w, http.StatusRequestEntityTooLarge, "share_too_large")
			return
		}
		respond.Error(w, http.StatusBadRequest, "share_empty")
		return
	}
	if len(body) == 0 {
		respond.Error(w, http.StatusBadRequest, "share_empty")
		return
	}

	userID, _ := r.Context().Value(CtxUserID).(string)
	now := time.Now().UTC()
	// The row commits before the chain write. A timeout can still land the
	// entity, and that attempt has to count or the daily cap does not cap spend.
	err = d.Store.WithShareLock(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := db.AllowShare(ctx, tx, userID, now); err != nil {
			return err
		}
		return db.RecordShare(ctx, tx, userID, now)
	})
	if err != nil {
		writeShareError(w, err)
		return
	}
	writeCtx, cancel := context.WithTimeout(r.Context(), shareWriteLimit)
	defer cancel()
	entityKey, err := d.ShareWriter.CreateShare(writeCtx, body)
	if err != nil {
		writeShareError(w, err)
		return
	}
	if !entityKeyPattern.MatchString(entityKey) {
		writeShareError(w, arkivshare.ErrUnconfirmed)
		return
	}
	respond.JSON(w, http.StatusCreated, shareReceipt{EntityKey: entityKey, ChainID: arkivshare.ChainID})
}

func writeShareError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrShareRate):
		respond.Error(w, http.StatusTooManyRequests, "share_rate")
	case errors.Is(err, db.ErrShareBudget):
		respond.Error(w, http.StatusTooManyRequests, "share_budget")
	case errors.Is(err, db.ErrShareBusy):
		respond.Error(w, http.StatusServiceUnavailable, "share_busy")
	case errors.Is(err, arkivshare.ErrDisabled):
		respond.Error(w, http.StatusServiceUnavailable, "share_disabled")
	default:
		respond.Error(w, http.StatusBadGateway, "share_unconfirmed")
	}
}
