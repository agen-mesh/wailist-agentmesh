package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"

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

	type agentResult struct {
		NodeID         string `json:"nodeId"`
		Address        string `json:"address"`
		Network        string `json:"network"`
		PlatformFunded bool   `json:"platformFunded,omitempty"`
	}
	type webhookResult struct {
		NodeID string `json:"nodeId"`
		URL    string `json:"url"`
		Method string `json:"method"`
	}
	var agents []agentResult
	var webhooks []webhookResult

	platformMnemonic := os.Getenv("PLATFORM_ALGO_MNEMONIC")

	for i, node := range wf.Nodes {
		switch node.Type {
		case models.NodeTypeAgent:
			platformFunded := !node.SelfFundWallet
			var address, encMnemonic string
			var err error
			if platformFunded && platformMnemonic != "" {
				address, encMnemonic, err = d.Wallet.WrapMnemonic(platformMnemonic)
			} else {
				address, encMnemonic, err = d.Wallet.GenerateWallet()
				platformFunded = false
			}
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
			agents = append(agents, agentResult{NodeID: node.ID, Address: address, Network: d.Wallet.Network(), PlatformFunded: platformFunded})

		case models.NodeTypeTrigger:
			if node.Template == "webhook" && node.WebhookLiveURL == "" {
				method := node.WebhookMethod
				if method == "" {
					method = "POST"
				}
				liveURL := fmt.Sprintf("%s/hooks/%s/%s", d.BaseURL, id, node.ID)
				wf.Nodes[i].WebhookLiveURL = liveURL
				wf.Nodes[i].WebhookMethod = method
				webhooks = append(webhooks, webhookResult{NodeID: node.ID, URL: liveURL, Method: method})
			}
		}
	}

	// If any webhook URLs were generated, persist the updated nodes.
	if len(webhooks) > 0 {
		graph := models.WorkflowGraph{Nodes: wf.Nodes, Edges: wf.Edges}
		if _, err := d.Store.UpdateWorkflow(ctx, id, wf.Name, graph); err != nil {
			respond.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
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
	if webhooks == nil {
		webhooks = []webhookResult{}
	}
	respond.JSON(w, http.StatusOK, map[string]any{
		"workflowId":  id,
		"status":      "deployed",
		"runEndpoint": runEndpoint,
		"agents":      agents,
		"webhooks":    webhooks,
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
