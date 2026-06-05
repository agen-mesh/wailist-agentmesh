package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

func (d *Deps) Deploy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	wf, err := d.Store.GetWorkflow(ctx, id)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}

	type agentResult struct {
		NodeID  string `json:"nodeId"`
		Address string `json:"address"`
		Network string `json:"network"`
	}
	var agents []agentResult

	for _, node := range wf.Nodes {
		if node.Type != models.NodeTypeAgent {
			continue
		}
		address, encMnemonic, err := d.Wallet.GenerateWallet()
		if err != nil {
			respond.Error(w, http.StatusInternalServerError, fmt.Sprintf("wallet creation failed: %v", err))
			return
		}
		if err := d.Store.InsertAgentWallet(ctx, models.AgentWallet{
			WorkflowID:        id,
			AgentNodeID:       node.ID,
			Address:           address,
			EncryptedMnemonic: encMnemonic,
			Network:           d.Wallet.Network(),
		}); err != nil {
			respond.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		agents = append(agents, agentResult{NodeID: node.ID, Address: address, Network: d.Wallet.Network()})
	}

	runEndpoint := fmt.Sprintf("%s/run/%s", d.BaseURL, id)
	now := time.Now()
	if err := d.Store.SetWorkflowDeployed(ctx, id, runEndpoint, now); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if agents == nil {
		agents = []agentResult{}
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

	var body struct {
		Amount uint64 `json:"amount"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Amount == 0 {
		body.Amount = 1_000_000
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
