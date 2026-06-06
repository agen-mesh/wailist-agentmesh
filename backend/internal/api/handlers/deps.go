package handlers

import (
	"net/http"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/wallet"
)

type contextKey string

const CtxUserID contextKey = "userID"

type Deps struct {
	Store   *db.Store
	Broker  *sse.Broker
	Wallet  *wallet.Service
	Engine  *engine.Runner
	BaseURL string
}

// Stub handlers — replaced one by one as tasks complete
func (d *Deps) TriggerRun(w http.ResponseWriter, r *http.Request)    { w.WriteHeader(501) }
func (d *Deps) StopWorkflow(w http.ResponseWriter, r *http.Request)  { w.WriteHeader(501) }
func (d *Deps) GetRun(w http.ResponseWriter, r *http.Request)        { w.WriteHeader(501) }
func (d *Deps) StreamRun(w http.ResponseWriter, r *http.Request)     { w.WriteHeader(501) }
func (d *Deps) PublicTrigger(w http.ResponseWriter, r *http.Request) { w.WriteHeader(501) }
