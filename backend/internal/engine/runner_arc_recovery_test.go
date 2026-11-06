package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/testutil"
	"github.com/jackc/pgx/v5"
)

type arcResumeTransport struct {
	rpc      testutil.ArcRPC
	mode     string
	active   atomic.Bool
	attempts atomic.Int64
}

func (f *arcResumeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var req struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Method == "eth_sendRawTransaction" {
		f.attempts.Add(1)
	}
	if f.active.Load() {
		if f.mode == "pre-send" && req.Method == "eth_estimateGas" {
			return nil, fmt.Errorf("fixture gas estimate unavailable")
		}
		if f.mode == "rejected" && req.Method == "eth_sendRawTransaction" {
			return arcFixtureResponse(r, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"insufficient funds for gas * price + value"}}`), nil
		}
		if f.mode == "reverted" && req.Method == "eth_getTransactionReceipt" {
			return arcFixtureResponse(r, `{"jsonrpc":"2.0","id":1,"result":{"status":"0x0"}}`), nil
		}
	}
	return f.rpc.RoundTrip(r)
}

func arcFixtureResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(body)), Request: r}
}

func TestArcResumeKnownFailures(t *testing.T) {
	for _, mode := range []string{"quote", "validation", "pre-send", "rejected", "reverted"} {
		for _, force := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/force=%v", mode, force), func(t *testing.T) {
				t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
				fixture := &arcResumeTransport{mode: mode}
				fixture.active.Store(true)
				old := http.DefaultTransport
				http.DefaultTransport = fixture
				t.Cleanup(func() { http.DefaultTransport = old })
				runner, store, wf, run := arcRunnerFixture(t, "transfer")
				if mode == "quote" {
					wf.Nodes[1].Config["broadcast"] = "false"
				}
				if mode == "quote" || mode == "validation" {
					wf.Nodes[1].Price = "invalid"
				}
				ctx := arc.WithPrincipal(context.Background(), "owner")
				runner.StartWithContext(ctx, wf, run)
				waitArcRun(t, runner, store, wf, run, models.RunStatusFailed)
				if got := fixture.attempts.Load(); got > 1 {
					t.Fatalf("failure retried automatically: attempts=%d", got)
				}
				fixture.active.Store(false)
				wf.Nodes[1].Price = "0.01"
				run.Status = models.RunStatusFailed
				// A new runner must decide from the durable records alone.
				restarted, _ := newConfigStalenessTestRunner(t)
				if claimed, err := restarted.StartResume(ctx, wf, run, force); !claimed || err != nil {
					t.Fatalf("resume admission: %v %v", claimed, err)
				}
				waitArcRun(t, restarted, store, wf, run, models.RunStatusSuccess)
				wantSends := int64(1)
				if mode == "quote" {
					wantSends = 0
				} else if mode == "reverted" {
					wantSends = 2
				}
				if fixture.rpc.Sends.Load() != wantSends {
					t.Fatalf("accepted sends=%d want=%d", fixture.rpc.Sends.Load(), wantSends)
				}
			})
		}
	}
}

func TestArcResumeUnknownHistory(t *testing.T) {
	for _, mode := range []string{"running", "legacy failure", "pending", "older unknown then known failure"} {
		for _, force := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/force=%v", mode, force), func(t *testing.T) {
				t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
				rpc := &testutil.ArcRPC{}
				testutil.UseArcRPC(t, rpc)
				runner, store, wf, run := arcRunnerFixture(t, "transfer")
				status, output := models.LogStatusRunning, any(nil)
				switch mode {
				case "legacy failure":
					status, output = models.LogStatusFailed, "connection lost"
				case "pending":
					status, output = models.LogStatusSuccess, map[string]any{"status": "pending", "txHash": "0x123"}
				case "older unknown then known failure":
					status, output = models.LogStatusFailed, map[string]any{"arcVersion": 1, "arcOutcome": "uncertain", "error": "response lost", "txHash": "0x123"}
				}
				add := func(status models.LogStatus, output any) {
					t.Helper()
					entry, err := store.InsertRunLog(t.Context(), models.RunLog{RunID: run.ID, NodeID: "pay", NodeType: models.NodeTypeArc, Status: models.LogStatusRunning})
					if err != nil {
						t.Fatal(err)
					}
					raw, err := json.Marshal(output)
					if err != nil {
						t.Fatal(err)
					}
					if err := store.UpdateRunLog(t.Context(), entry.ID, status, raw, 1, ""); err != nil {
						t.Fatal(err)
					}
				}
				add(status, output)
				if mode == "older unknown then known failure" {
					add(models.LogStatusFailed, map[string]any{"arcVersion": 1, "arcOutcome": "not_submitted", "error": "invalid amount"})
				}
				if err := store.FinishRun(t.Context(), run.ID, models.RunStatusFailed); err != nil {
					t.Fatal(err)
				}
				run.Status = models.RunStatusFailed
				if claimed, err := runner.StartResume(arc.WithPrincipal(context.Background(), "owner"), wf, run, force); !claimed || err != nil {
					t.Fatalf("resume admission: %v %v", claimed, err)
				}
				waitArcRun(t, runner, store, wf, run, models.RunStatusFailed)
				if rpc.Calls.Load() != 0 {
					t.Fatalf("unresolved prior attempt dispatched %d RPC calls", rpc.Calls.Load())
				}
			})
		}
	}
}

func TestArcResumeRecordedReadOnlyAttempt(t *testing.T) {
	t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
	rpc := &testutil.ArcRPC{}
	testutil.UseArcRPC(t, rpc)
	runner, store, wf, run := arcRunnerFixture(t, "transfer")
	wf.Nodes[1].Config["broadcast"] = "false"
	entry, err := store.InsertRunLog(t.Context(), models.RunLog{RunID: run.ID, NodeID: "pay", NodeType: models.NodeTypeArc, Status: models.LogStatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRunLog(t.Context(), entry.ID, models.LogStatusRunning, []byte(`{"arcVersion":1,"arcOutcome":"read_only"}`), 0, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishRun(t.Context(), run.ID, models.RunStatusFailed); err != nil {
		t.Fatal(err)
	}
	run.Status = models.RunStatusFailed
	if claimed, err := runner.StartResume(arc.WithPrincipal(context.Background(), "owner"), wf, run, false); !claimed || err != nil {
		t.Fatalf("resume admission: %v %v", claimed, err)
	}
	waitArcRun(t, runner, store, wf, run, models.RunStatusSuccess)
	if rpc.Calls.Load() != 0 {
		t.Fatalf("read-only resume dispatched %d RPC calls", rpc.Calls.Load())
	}
}

func TestArcResumeAfterAcceptedSendAndLostTerminalLog(t *testing.T) {
	t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
	rpc := &testutil.ArcRPC{}
	testutil.UseArcRPC(t, rpc)
	runner, store, wf, run := arcRunnerFixture(t, "transfer")
	wf.Nodes[2].Type = models.NodeTypeArc
	wf.Nodes[2].Template = "fixture-unsupported-arc-action"
	wf.Nodes[2].Config = map[string]string{"broadcast": "false"}
	conn, err := pgx.Connect(t.Context(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	name := pgx.Identifier{"arc_terminal_fault_" + strings.ReplaceAll(run.ID, "-", "")}.Sanitize()
	if _, err := conn.Exec(t.Context(), `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.run_id::text = TG_ARGV[0] AND NEW.node_id = 'pay' AND NEW.status <> 'running' THEN
				RAISE EXCEPTION 'fixture terminal Arc log failure';
			END IF;
			RETURN NEW;
		END $$`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+name+` ON run_logs`); err != nil {
			t.Error(err)
		}
		if _, err := conn.Exec(context.Background(), `DROP FUNCTION `+name+`()`); err != nil {
			t.Error(err)
		}
	})
	if _, err := conn.Exec(t.Context(), `CREATE TRIGGER `+name+` BEFORE UPDATE ON run_logs
		FOR EACH ROW EXECUTE FUNCTION `+name+`('`+run.ID+`')`); err != nil {
		t.Fatal(err)
	}
	ctx := arc.WithPrincipal(context.Background(), "owner")
	runner.StartWithContext(ctx, wf, run)
	waitArcRun(t, runner, store, wf, run, models.RunStatusFailed)
	if rpc.Sends.Load() != 1 {
		t.Fatalf("initial accepted submissions=%d", rpc.Sends.Load())
	}
	logs, err := store.GetRunLogs(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	running := false
	for _, entry := range logs {
		running = running || entry.NodeID == "pay" && entry.Status == models.LogStatusRunning
	}
	if !running {
		t.Fatal("terminal-log fault did not preserve the unfinished attempt")
	}
	before := rpc.Calls.Load()
	restarted, _ := newConfigStalenessTestRunner(t)
	run.Status = models.RunStatusFailed
	if claimed, err := restarted.StartResume(ctx, wf, run, true); !claimed || err != nil {
		t.Fatalf("resume admission: %v %v", claimed, err)
	}
	waitArcRun(t, restarted, store, wf, run, models.RunStatusFailed)
	if rpc.Sends.Load() != 1 || rpc.Calls.Load() != before {
		t.Fatalf("unknown accepted transfer dispatched again: sends=%d calls=%d", rpc.Sends.Load(), rpc.Calls.Load())
	}
}
