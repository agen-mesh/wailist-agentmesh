package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/testutil"
	"github.com/jackc/pgx/v5"
)

func arcRunnerFixture(t *testing.T, template string) (*Runner, *db.Store, models.Workflow, models.Run) {
	t.Helper()
	t.Setenv("ARC_NETWORK", "testnet")
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	runner, store := newConfigStalenessTestRunner(t)
	userID := fmt.Sprintf("arc-owner-%d", time.Now().UnixNano())
	testutil.EnsureUsers(t, userID)
	wf, err := store.CreateWorkflow(t.Context(), "Arc policy", userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runner.Stop(wf.ID)
		if err := store.DeleteWorkflow(context.Background(), wf.ID); err != nil {
			t.Error(err)
		}
	})
	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "start", Type: models.NodeTypeTrigger},
			{ID: "pay", Type: models.NodeTypeArc, Template: template, Price: "0.01", Endpoint: "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC", MaxRetries: 3,
				Config: map[string]string{"broadcast": "true", "chain": "arc-testnet", "destChain": "base-sepolia", "operator": "true", "userID": "owner", "worldVerified": "true"}},
			{ID: "end", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{{ID: "a", From: "start", To: "pay", Kind: models.EdgeKindFlow}, {ID: "b", From: "pay", To: "end", Kind: models.EdgeKindFlow}},
	}
	wf, err = store.UpdateWorkflow(t.Context(), wf.ID, wf.Name, graph)
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(t.Context(), wf.ID, "test", []byte(`{"operator":true,"userID":"owner","role":"operator","worldVerified":true}`))
	if err != nil {
		t.Fatal(err)
	}
	runner.broker.Create(run.ID)
	return runner, store, wf, run
}

func waitArcRun(t *testing.T, runner *Runner, store *db.Store, wf models.Workflow, run models.Run, want models.RunStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runner.IsRunning(wf.ID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runner.IsRunning(wf.ID) {
		t.Fatal("Arc run did not finish")
	}
	got, err := store.GetRun(t.Context(), run.ID)
	if err != nil || got.Status != want {
		t.Fatalf("run=%+v err=%v want=%s", got, err, want)
	}
}

func TestArcRunnerStartAndResumeAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, mode, principal, policy string
		allowed                       bool
	}{
		{"default deny", "start", "owner", "", false},
		{"operator start", "start", "owner", "owner", true},
		{"member start", "start", "member", "owner", false},
		{"owner field is not authority", "legacy", "", "owner", false},
		{"scheduled start", "scheduled", "", "owner", false},
		{"operator resume", "resume", "owner", "owner", true},
		{"forced member resume", "resume", "member", "owner", false},
		{"forged context resume", "resume", "", "owner", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ARC_OPERATOR_USER_IDS", tc.policy)
			rpc := &testutil.ArcRPC{}
			testutil.UseArcRPC(t, rpc)
			runner, store, wf, run := arcRunnerFixture(t, "transfer")
			ctx := context.WithValue(context.Background(), "userID", "owner")
			if tc.principal != "" {
				ctx = arc.WithPrincipal(ctx, tc.principal)
			}
			// Policy comes from runner construction, not a later environment read.
			t.Setenv("ARC_OPERATOR_USER_IDS", "different")
			switch tc.mode {
			case "start":
				runner.StartWithContext(ctx, wf, run)
			case "legacy":
				wf.UserID = "owner"
				runner.Start(wf, run)
			case "scheduled":
				wf.UserID = "owner"
				if !runner.StartIfNotRunning(wf, run) {
					t.Fatal("schedule not admitted")
				}
			case "resume":
				if err := store.FinishRun(t.Context(), run.ID, models.RunStatusFailed); err != nil {
					t.Fatal(err)
				}
				run.Status = models.RunStatusFailed
				if claimed, err := runner.StartResume(ctx, wf, run, true); !claimed || err != nil {
					t.Fatalf("resume admission: %v %v", claimed, err)
				}
			}
			want := models.RunStatusFailed
			if tc.allowed {
				want = models.RunStatusSuccess
			}
			waitArcRun(t, runner, store, wf, run, want)
			if tc.allowed && rpc.Sends.Load() != 1 || !tc.allowed && rpc.Calls.Load() != 0 {
				t.Fatalf("calls=%d sends=%d allowed=%v", rpc.Calls.Load(), rpc.Sends.Load(), tc.allowed)
			}
			logs, err := store.GetRunLogs(t.Context(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range logs {
				if !tc.allowed && entry.NodeID == "end" {
					t.Fatal("denied Arc node advanced downstream")
				}
			}
		})
	}
}

func TestArcRunnerUncertainWriteCannotReplayEvenWithForce(t *testing.T) {
	for _, template := range []string{"transfer", "nanopay", "bridge", "gateway"} {
		t.Run(template, func(t *testing.T) {
			t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
			rpc := &testutil.ArcRPC{LoseResponse: true}
			testutil.UseArcRPC(t, rpc)
			runner, store, wf, run := arcRunnerFixture(t, template)
			ctx := arc.WithPrincipal(context.Background(), "owner")
			runner.StartWithContext(ctx, wf, run)
			waitArcRun(t, runner, store, wf, run, models.RunStatusFailed)
			if rpc.Sends.Load() != 1 {
				t.Fatalf("initial sends=%d", rpc.Sends.Load())
			}
			dead, err := store.GetDeadLetterRuns(t.Context(), run.ID)
			if err != nil || len(dead) != 1 || !dead[0].PaymentRisk || !strings.Contains(dead[0].Error, rpc.Hash.Load().(string)) {
				t.Fatalf("reconciliation evidence: %+v %v", dead, err)
			}
			run.Status = models.RunStatusFailed
			for _, force := range []bool{false, true} {
				if claimed, err := runner.StartResume(ctx, wf, run, force); !claimed || err != nil {
					t.Fatalf("resume admission: %v %v", claimed, err)
				}
				waitArcRun(t, runner, store, wf, run, models.RunStatusFailed)
				if rpc.Sends.Load() != 1 {
					t.Fatalf("force=%v replayed payment: %d sends", force, rpc.Sends.Load())
				}
			}
			logs, err := store.GetRunLogs(t.Context(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range logs {
				if entry.NodeID == "end" {
					t.Fatal("uncertain Arc node advanced downstream")
				}
			}
		})
	}
}

func TestArcRunnerLegacyUncertainStatesRefuseForcedResume(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status models.LogStatus
		output any
	}{
		{"running", models.LogStatusRunning, nil},
		{"failed without dead letter", models.LogStatusFailed, "connection lost"},
		{"legacy pending success", models.LogStatusSuccess, map[string]any{"status": "pending", "txHash": "0x123"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
			rpc := &testutil.ArcRPC{}
			testutil.UseArcRPC(t, rpc)
			runner, store, wf, run := arcRunnerFixture(t, "transfer")
			entry, err := store.InsertRunLog(t.Context(), models.RunLog{RunID: run.ID, NodeID: "pay", NodeType: models.NodeTypeArc, Status: models.LogStatusRunning})
			if err != nil {
				t.Fatal(err)
			}
			if tc.status != models.LogStatusRunning {
				raw, err := json.Marshal(tc.output)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.UpdateRunLog(t.Context(), entry.ID, tc.status, raw, 1, ""); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.FinishRun(t.Context(), run.ID, models.RunStatusFailed); err != nil {
				t.Fatal(err)
			}
			run.Status = models.RunStatusFailed
			if claimed, err := runner.StartResume(arc.WithPrincipal(context.Background(), "owner"), wf, run, true); !claimed || err != nil {
				t.Fatalf("resume: %v %v", claimed, err)
			}
			waitArcRun(t, runner, store, wf, run, models.RunStatusFailed)
			if rpc.Calls.Load() != 0 {
				t.Fatalf("uncertain prior attempt dispatched %d calls", rpc.Calls.Load())
			}
		})
	}
}

func TestArcRunnerSuccessfulWriteIsNotRepeatedAfterConfigChange(t *testing.T) {
	t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
	rpc := &testutil.ArcRPC{}
	testutil.UseArcRPC(t, rpc)
	runner, store, wf, run := arcRunnerFixture(t, "transfer")
	ctx := arc.WithPrincipal(context.Background(), "owner")
	runner.StartWithContext(ctx, wf, run)
	waitArcRun(t, runner, store, wf, run, models.RunStatusSuccess)
	wf.Nodes[1].Price = "2"
	if err := store.FinishRun(t.Context(), run.ID, models.RunStatusFailed); err != nil {
		t.Fatal(err)
	}
	run.Status = models.RunStatusFailed
	if claimed, err := runner.StartResume(ctx, wf, run, true); !claimed || err != nil {
		t.Fatalf("resume: %v %v", claimed, err)
	}
	waitArcRun(t, runner, store, wf, run, models.RunStatusSuccess)
	if rpc.Sends.Load() != 1 {
		t.Fatalf("successful Arc payment repeated: %d sends", rpc.Sends.Load())
	}
}

func TestArcRunnerRefusesBroadcastWithoutInitialRunLog(t *testing.T) {
	for _, broadcast := range []string{"true", "{{state.send}}", "false"} {
		t.Run(broadcast, func(t *testing.T) {
			t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
			rpc := &testutil.ArcRPC{}
			testutil.UseArcRPC(t, rpc)
			runner, store, wf, run := arcRunnerFixture(t, "transfer")
			wf.Nodes[1].Config["broadcast"] = broadcast
			if err := store.SetWorkflowVariable(t.Context(), wf.ID, "send", []byte(`true`)); err != nil {
				t.Fatal(err)
			}
			conn, err := pgx.Connect(t.Context(), os.Getenv("TEST_DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { conn.Close(context.Background()) })
			name := pgx.Identifier{"arc_log_fault_" + strings.ReplaceAll(run.ID, "-", "")}.Sanitize()
			if _, err := conn.Exec(t.Context(), `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN
					IF NEW.run_id::text = TG_ARGV[0] AND NEW.node_type = 'arc' AND NEW.status = 'running' THEN
						RAISE EXCEPTION 'fixture initial Arc log failure';
					END IF;
					RETURN NEW;
				END $$`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := conn.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+name+` ON run_logs`); err != nil {
					t.Error(err)
				}
				if _, err := conn.Exec(context.Background(), `DROP FUNCTION `+name+`() `); err != nil {
					t.Error(err)
				}
			})
			if _, err := conn.Exec(t.Context(), `CREATE TRIGGER `+name+` BEFORE INSERT ON run_logs
				FOR EACH ROW EXECUTE FUNCTION `+name+`('`+strings.ReplaceAll(run.ID, "'", "''")+`')`); err != nil {
				t.Fatal(err)
			}
			if _, err := store.InsertRunLog(t.Context(), models.RunLog{RunID: run.ID, NodeID: "probe", NodeType: models.NodeTypeArc, Status: models.LogStatusRunning}); err == nil || !strings.Contains(err.Error(), "fixture initial Arc log failure") {
				t.Fatalf("fault did not reject initial Arc log: %v", err)
			}
			runner.StartWithContext(arc.WithPrincipal(context.Background(), "owner"), wf, run)
			want := models.RunStatusFailed
			if broadcast == "false" {
				want = models.RunStatusSuccess
			}
			waitArcRun(t, runner, store, wf, run, want)
			if rpc.Calls.Load() != 0 || rpc.Sends.Load() != 0 {
				t.Fatalf("Arc executed without a durable attempt: calls=%d sends=%d", rpc.Calls.Load(), rpc.Sends.Load())
			}
			letters, err := store.GetDeadLetterRuns(t.Context(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if broadcast != "false" && (len(letters) != 1 || !strings.Contains(letters[0].Error, "record initial run log") || letters[0].AttemptCount != 0 || letters[0].PaymentRisk) {
				t.Fatalf("refusal was not recorded as an unattempted write: %+v", letters)
			}
			logs, err := store.GetRunLogs(t.Context(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range logs {
				if broadcast != "false" && entry.NodeID == "end" {
					t.Fatal("run advanced past an unrecorded Arc write")
				}
			}
		})
	}
}
