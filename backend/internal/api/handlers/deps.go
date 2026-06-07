package handlers

import (
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/wallet"
)

type contextKey string

const CtxUserID contextKey = "userID"

type Deps struct {
	Store     *db.Store
	Broker    *sse.Broker
	Wallet    *wallet.Service
	Engine    *engine.Runner
	BaseURL   string
	JWTSecret string

	FrontendURL        string
	GithubClientID     string
	GithubClientSecret string
	GoogleClientID     string
	GoogleClientSecret string
}
