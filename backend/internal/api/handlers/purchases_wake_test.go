package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/google/uuid"
)

type slowPurchaseSibling struct {
	purchase.Store
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *slowPurchaseSibling) ByRequest(ctx context.Context, account, request string) (purchase.Order, error) {
	if strings.HasSuffix(request, "/slow/0") {
		s.once.Do(func() { close(s.entered) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return purchase.Order{}, ctx.Err()
		}
	}
	return s.Store.ByRequest(ctx, account, request)
}

func TestCompletedPurchaseWakesAfterSlowSiblingAndRestart(t *testing.T) {
	d, account := purchaseReconcilerFixture(t)
	wf, err := d.Store.CreateWorkflow(t.Context(), "Purchase wake regression", account)
	if err != nil {
		t.Fatal(err)
	}
	request := json.RawMessage(`{"text":"stored workflow result"}`)
	purchaseNode := func(id string) models.WorkflowNode {
		return models.WorkflowNode{ID: id, Type: models.NodeTypePurchase, Template: "service", Config: map[string]string{"offerKey": "offer-key", "maxMicros": "100", "request": string(request)}}
	}
	graph := models.WorkflowGraph{Nodes: []models.WorkflowNode{{ID: "start", Type: models.NodeTypeTrigger}, purchaseNode("purchase"), purchaseNode("slow"), {ID: "end", Type: models.NodeTypeEnd}}, Edges: []models.WorkflowEdge{{ID: "a", From: "start", To: "purchase", Kind: models.EdgeKindFlow}, {ID: "b", From: "start", To: "slow", Kind: models.EdgeKindFlow}, {ID: "c", From: "purchase", To: "end", Kind: models.EdgeKindFlow}, {ID: "d", From: "slow", To: "end", Kind: models.EdgeKindFlow}}}
	wf, err = d.Store.UpdateWorkflow(t.Context(), wf.ID, wf.Name, graph)
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.Store.CreateRun(t.Context(), wf.ID, "test", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	prefix := uuid.NewString()[:24]
	orders := make([]purchase.Order, 2)
	for i, node := range []string{"purchase", "slow"} {
		id := prefix + "000000000001"
		state := purchase.AwaitingIdentity
		if i == 1 {
			id, state = prefix+"000000000002", purchase.Completed
		}
		o := purchase.Order{ID: id, AccountID: account, ClientRequestID: "run/" + run.ID + "/" + node + "/0", MerchantAccepted: true, State: state, CreatedAt: time.Now().UTC(), Offer: purchase.Offer{Key: "offer-key"}, Request: request}
		o.Intent = purchase.Intent{Version: purchase.Protocol, OrderID: id, AccountID: account, WorkflowID: wf.ID, RunID: run.ID, NodeID: node, Invocation: "0", Payer: "0x1111111111111111111111111111111111111111", ChainID: purchase.ChainID, Asset: purchase.Asset, AmountMicros: "100", MaxMicros: "100", RequestDigest: purchase.Digest(request)}
		if state == purchase.Completed {
			o.Result = json.RawMessage(`{"result":"slow"}`)
		}
		raw, _ := json.Marshal(o.Intent)
		o.IntentHash = purchase.Digest(raw)
		if _, _, err = d.Store.Purchases().Create(t.Context(), o); err != nil {
			t.Fatal(err)
		}
		orders[i] = o
	}
	blocked := &slowPurchaseSibling{Store: d.Purchases.Service.Store, entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(blocked.release) })
	d.Purchases.Service.Store = blocked
	broker := sse.NewBroker()
	d.Engine = engine.NewRunner(d.Store, broker, nil, "", "", "", engine.X402Config{}, arc.Config{})
	d.Engine.SetPurchases(d.Purchases.Service)
	broker.Create(run.ID)
	done := make(chan struct{})
	go func() { defer close(done); d.Engine.Run(t.Context(), wf, run, 0) }()
	select {
	case <-blocked.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("slow sibling did not start")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		logs, err := d.Store.GetRunLogs(t.Context(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		waiting := false
		for _, entry := range logs {
			waiting = waiting || entry.NodeID == "purchase" && entry.Status == models.LogStatusWaiting
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("purchase did not yield while sibling was running")
		}
		time.Sleep(time.Millisecond)
	}
	completed, err := d.Store.Purchases().Update(t.Context(), account, orders[0].ID, func(o *purchase.Order, _ purchase.Ledger) error {
		o.State = purchase.Completed
		o.Result = json.RawMessage(`{"result":"paid"}`)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	d.wakePurchase(t.Context(), completed)
	current, err := d.Store.GetRun(t.Context(), run.ID)
	if err != nil || current.Status != models.RunStatusRunning {
		t.Fatal("early wake changed the active execution")
	}
	release.Do(func() { close(blocked.release) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("sibling did not finish")
	}
	current, err = d.Store.GetRun(t.Context(), run.ID)
	if err != nil || current.Status != models.RunStatusWaiting {
		t.Fatal("engine did not persist waiting after the early completion")
	}
	// Recreate the process-local runner and reconciler after the lost wake.
	restarted := &Deps{Store: d.Store, Purchases: d.Purchases, Engine: engine.NewRunner(d.Store, sse.NewBroker(), nil, "", "", "", engine.X402Config{}, arc.Config{})}
	restarted.Engine.SetPurchases(d.Purchases.Service)
	restarted.reconcilePurchaseBatch(t.Context(), prefix+"000000000000")
	deadline = time.Now().Add(3 * time.Second)
	for {
		current, err = d.Store.GetRun(t.Context(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == models.RunStatusSuccess {
			break
		}
		if current.Status != models.RunStatusRunning && current.Status != models.RunStatusWaiting || time.Now().After(deadline) {
			t.Fatalf("completed order did not wake its run: %s", current.Status)
		}
		time.Sleep(time.Millisecond)
	}
	restarted.reconcilePurchaseBatch(t.Context(), prefix+"000000000000")
	logs, err := d.Store.GetRunLogs(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	ends := 0
	for _, entry := range logs {
		if entry.NodeID == "end" && entry.Status == models.LogStatusSuccess {
			ends++
		}
	}
	if ends != 1 {
		t.Fatalf("downstream executions=%d", ends)
	}
}
