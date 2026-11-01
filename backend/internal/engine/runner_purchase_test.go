package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/agentmesh/backend/internal/testutil"
	"github.com/google/uuid"
)

type unavailablePurchaseCatalog struct{ purchase.Catalog }
type unavailablePurchaseMerchant struct{ purchase.Merchant }
type unavailablePurchaseRail struct{ purchase.Rail }
type unavailablePurchaseIdentity struct{ purchase.Identity }

func TestPurchaseWaitingRestartAndForcedResumeReuseJournal(t *testing.T) {
	runner, store := newConfigStalenessTestRunner(t)
	ctx := t.Context()
	account := uuid.NewString()
	testutil.EnsureUsers(t, account)
	wf, err := store.CreateWorkflow(ctx, "Purchase waiting regression", account)
	if err != nil {
		t.Fatal(err)
	}
	request := json.RawMessage(`{"text":"durable result"}`)
	graph := models.WorkflowGraph{Nodes: []models.WorkflowNode{{ID: "start", Type: models.NodeTypeTrigger}, {ID: "purchase", Type: models.NodeTypePurchase, Template: "service", Config: map[string]string{"offerKey": "offer-key", "maxMicros": "100", "request": string(request)}}, {ID: "end", Type: models.NodeTypeEnd}}, Edges: []models.WorkflowEdge{{ID: "first", From: "start", To: "purchase", Kind: models.EdgeKindFlow}, {ID: "second", From: "purchase", To: "end", Kind: models.EdgeKindFlow}}}
	wf, err = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(ctx, wf.ID, "test", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	o := purchase.Order{ID: uuid.NewString(), AccountID: account, ClientRequestID: "run/" + run.ID + "/purchase/0", MerchantAccepted: true, State: purchase.AwaitingIdentity, CreatedAt: time.Now().UTC(), Offer: purchase.Offer{Key: "offer-key"}, Request: request}
	o.Intent = purchase.Intent{Version: purchase.Protocol, OrderID: o.ID, AccountID: account, WorkflowID: wf.ID, RunID: run.ID, NodeID: "purchase", Invocation: "0", Payer: "0x1111111111111111111111111111111111111111", Payee: "0x2222222222222222222222222222222222222222", ChainID: purchase.ChainID, Asset: purchase.Asset, AmountMicros: "100", MaxMicros: "100", RequestDigest: purchase.Digest(request)}
	raw, _ := json.Marshal(o.Intent)
	o.IntentHash = purchase.Digest(raw)
	if _, _, err = store.Purchases().Create(ctx, o); err != nil {
		t.Fatal(err)
	}
	service := &purchase.Service{Enabled: true, Store: store.Purchases(), Catalog: unavailablePurchaseCatalog{}, Merchant: unavailablePurchaseMerchant{}, Rail: unavailablePurchaseRail{}, Identity: unavailablePurchaseIdentity{}, Domain: "https://scratch.invalid", Seal: func(s string) (string, error) { return s, nil }, Open: func(s string) (string, error) { return s, nil }}
	runner.SetPurchases(service)
	runner.broker.Create(run.ID)
	runner.Run(ctx, wf, run, 0)
	assertWaiting := func() {
		t.Helper()
		actual, err := store.GetRun(ctx, run.ID)
		if err != nil || actual.Status != models.RunStatusWaiting || actual.FinishedAt != nil {
			t.Fatalf("run is not durably waiting: %+v %v", actual, err)
		}
		logs, err := store.GetRunLogs(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range logs {
			if l.NodeID == "end" {
				t.Fatal("waiting purchase advanced downstream")
			}
		}
	}
	assertWaiting()
	if _, err = store.CreateRun(ctx, wf.ID, "scheduler", []byte(`{}`)); !errors.Is(err, db.ErrPurchaseWaiting) {
		t.Fatalf("waiting admission: %v", err)
	}
	if ok, err := store.MarkRunRunning(ctx, run.ID); err != nil || !ok {
		t.Fatal("resume claim denied")
	}
	runner.broker.Create(run.ID)
	runner.Resume(ctx, wf, run, 0, true)
	assertWaiting()
	restarted, _ := newConfigStalenessTestRunner(t)
	restarted.SetPurchases(service)
	if _, err = store.Purchases().Update(ctx, account, o.ID, func(current *purchase.Order, _ purchase.Ledger) error {
		current.State = purchase.Completed
		current.Result = json.RawMessage(`{"digest":"persisted-result"}`)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.MarkRunRunning(ctx, run.ID); err != nil || !ok {
		t.Fatal("restart resume claim denied")
	}
	restarted.broker.Create(run.ID)
	restarted.Resume(ctx, wf, run, 0, false)
	actual, err := store.GetRun(ctx, run.ID)
	if err != nil || actual.Status != models.RunStatusSuccess {
		t.Fatalf("restart did not complete: %s %v", actual.Status, err)
	}
	stored, err := store.Purchases().ByRequest(ctx, account, o.ClientRequestID)
	if err != nil || stored.ID != o.ID || stored.TxHash != "" {
		t.Fatal("resume changed the order or dispatched a payment")
	}
	logs, err := store.GetRunLogs(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	endCount := 0
	for _, l := range logs {
		if l.NodeID == "end" && l.Status == models.LogStatusSuccess {
			endCount++
		}
	}
	if endCount != 1 {
		t.Fatalf("downstream executions=%d", endCount)
	}
}

func TestPurchaseStopIsDurableAndDryRunCannotCreateOrder(t *testing.T) {
	runner, store := newConfigStalenessTestRunner(t)
	ctx := context.Background()
	account := fmt.Sprintf("purchase-stop-%s", uuid.NewString())
	testutil.EnsureUsers(t, account)
	wf, err := store.CreateWorkflow(ctx, "Stop waiting purchase", account)
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(ctx, wf.ID, "test", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.WaitForPurchase(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if !runner.Stop(wf.ID) {
		t.Fatal("waiting stop was not observed")
	}
	actual, err := store.GetRun(ctx, run.ID)
	if err != nil || actual.Status != models.RunStatusStopped {
		t.Fatal("waiting stop was not persisted")
	}
	if _, err = store.CreateRun(ctx, wf.ID, "test", []byte(`{}`)); err != nil {
		t.Fatal("stopped purchase still blocked admission")
	}
	if !nodeMayHaveRealSideEffect(models.NodeTypePurchase, "service", "") {
		t.Fatal("purchase classified as pure compute")
	}
	if executes, _ := nodes.DryRunExecutes(models.WorkflowNode{Type: models.NodeTypePurchase, Template: "service"}); executes {
		t.Fatal("dry run executes a purchase")
	}
}
