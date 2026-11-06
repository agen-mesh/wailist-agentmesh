package handlers

import (
	"context"
	"errors"
	"io"
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
	shareChainID    = 7794095
	shareWriteLimit = 30 * time.Second
)

var entityKeyPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

type shareReceipt struct {
	EntityKey string `json:"entityKey"`
	ChainID   int    `json:"chainId"`
}

func (d *Deps) CreateShare(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/octet-stream" {
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
	var entityKey string
	err = d.Store.WithShareLock(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := db.AllowShare(ctx, tx, userID, now); err != nil {
			return err
		}
		writeCtx, cancel := context.WithTimeout(ctx, shareWriteLimit)
		defer cancel()
		key, err := d.ShareWriter.CreateShare(writeCtx, body)
		if err != nil {
			return err
		}
		if !entityKeyPattern.MatchString(key) {
			return arkivshare.ErrUnconfirmed
		}
		entityKey = key
		return db.RecordShare(ctx, tx, userID, now)
	})
	if err != nil {
		switch {
		case errors.Is(err, db.ErrShareRate):
			respond.Error(w, http.StatusTooManyRequests, "share_rate")
		case errors.Is(err, db.ErrShareBudget):
			respond.Error(w, http.StatusTooManyRequests, "share_budget")
		default:
			respond.Error(w, http.StatusBadGateway, "share_unconfirmed")
		}
		return
	}
	respond.JSON(w, http.StatusCreated, shareReceipt{EntityKey: entityKey, ChainID: shareChainID})
}
