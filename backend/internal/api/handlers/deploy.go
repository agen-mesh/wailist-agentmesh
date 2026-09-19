package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/robfig/cron/v3"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

const maxFundAmount uint64 = 10_000_000 // 10 ALGO per call

func (d *Deps) Deploy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	wf, err := d.Store.GetWorkflow(ctx, id)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}

	// Agents no longer get their own on-chain wallet. Every paid x402 call is
	// funded by the platform's own wallets — Wallet 1 (spend) pays the target
	// via the relay, Wallet 2 settles — and is metered against the user's
	// credit balance. Minting a per-agent Algorand account here produced
	// accounts that had to be separately funded, could hold stranded dust,
	// and were only ever reachable by the legacy pre-relay direct-pay dialect.
	type agentResult struct {
		NodeID string `json:"nodeId"`
	}
	agents := []agentResult{}
	for _, node := range wf.Nodes {
		if node.Type == models.NodeTypeAgent {
			agents = append(agents, agentResult{NodeID: node.ID})
		}
	}

	runEndpoint := fmt.Sprintf("%s/run/%s", d.BaseURL, id)
	now := time.Now()
	// A schedule can be saved before deployment (the chat builder does), and
	// its next run was computed then. Left alone, a next run that has since
	// passed is due the moment the workflow goes live, so deploying would
	// fire an unasked-for run. Count from now instead.
	//
	// Before marking it deployed, not after: the scheduler only claims
	// deployed workflows, so while this runs the stale time cannot fire. A
	// failure here stops the deploy for the same reason. Conditional on the
	// schedule still being the one read above, so a schedule removed in the
	// meantime is not written back.
	if wf.ScheduleCron != nil && *wf.ScheduleCron != "" {
		sched, err := cron.ParseStandard(*wf.ScheduleCron)
		if err != nil {
			log.Printf("deploy %s: stored schedule %q does not parse: %v", id, *wf.ScheduleCron, err)
			respond.Error(w, http.StatusConflict, "this workflow's schedule is not valid -- remove it or set it again, then deploy")
			return
		}
		if _, err := d.Store.RescheduleWorkflowNextRun(ctx, id, *wf.ScheduleCron, sched.Next(now.UTC())); err != nil {
			log.Printf("deploy %s: recompute next scheduled run: %v", id, err)
			respond.Error(w, http.StatusInternalServerError, "could not update the workflow's schedule")
			return
		}
	}
	if err := d.Store.SetWorkflowDeployed(ctx, id, runEndpoint, now); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	respond.JSON(w, http.StatusOK, map[string]any{
		"workflowId":  id,
		"status":      "deployed",
		"runEndpoint": runEndpoint,
		"agents":      agents,
		"deployedAt":  now,
	})
}

func (d *Deps) AgentBalance(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "id")
	agentID := chi.URLParam(r, "agentId")
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	wf, err := d.Store.GetWorkflow(ctx, workflowID)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "wallet not found")
		return
	}
	aw, err := d.Store.GetAgentWallet(ctx, workflowID, agentID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "wallet not found")
		return
	}
	microAlgo, err := d.Wallet.Balance(ctx, aw.Address)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{
		"address": aw.Address,
		"balance": fmt.Sprintf("%.6f", float64(microAlgo)/1e6),
		"network": aw.Network,
	})
}

func (d *Deps) FundAgent(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "id")
	agentID := chi.URLParam(r, "agentId")
	ctx := r.Context()
	userID, _ := ctx.Value(CtxUserID).(string)

	wf, err := d.Store.GetWorkflow(ctx, workflowID)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "wallet not found")
		return
	}

	var body struct {
		Amount uint64 `json:"amount"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Amount == 0 {
		body.Amount = 1_000_000
	}
	if body.Amount > maxFundAmount {
		respond.Error(w, http.StatusBadRequest, fmt.Sprintf("amount exceeds maximum of %d microAlgo", maxFundAmount))
		return
	}

	aw, err := d.Store.GetAgentWallet(ctx, workflowID, agentID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "wallet not found")
		return
	}
	txHash, err := d.Wallet.FundFromDispenser(ctx, aw.Address, body.Amount)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{
		"txHash":  txHash,
		"balance": fmt.Sprintf("%.6f", float64(body.Amount)/1e6),
	})
}
