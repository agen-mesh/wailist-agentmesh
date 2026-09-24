package db

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// Internal to the package so a test can pin runs.started_at directly: ordering
// and cursor behaviour cannot be tested on timestamps that depend on how fast
// the inserts happen to run.

func runsListStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func runsListUser(t *testing.T, s *Store) string {
	t.Helper()
	email := fmt.Sprintf("runs-list-%d@example.com", time.Now().UnixNano())
	u, err := s.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func runsListWorkflow(t *testing.T, s *Store, userID, name string) string {
	t.Helper()
	wf, err := s.CreateWorkflow(context.Background(), name, userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DeleteWorkflow(context.Background(), wf.ID) })
	return wf.ID
}

// runAt creates a run and pins its started_at.
func runAt(t *testing.T, s *Store, workflowID string, startedAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	r, err := s.CreateRun(ctx, workflowID, "manual", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE runs SET started_at = $1 WHERE id = $2`, startedAt, r.ID); err != nil {
		t.Fatal(err)
	}
	return r.ID
}

// A timestamp with microseconds, so the cursor's precision is exercised.
var runsListBase = time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)

func summaryIDs(runs []models.RunSummary) []string {
	ids := make([]string, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	return ids
}

func assertRunIDs(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("run ids\n got  %v\n want %v", got, want)
	}
}

func TestListWorkflowRunsNewestFirst(t *testing.T) {
	s := runsListStore(t)
	ctx := context.Background()
	user := runsListUser(t, s)
	wf := runsListWorkflow(t, s, user, "Runs List Newest First")

	r1 := runAt(t, s, wf, runsListBase)
	r2 := runAt(t, s, wf, runsListBase.Add(time.Minute))
	r3 := runAt(t, s, wf, runsListBase.Add(2*time.Minute))

	runs, err := s.ListWorkflowRuns(ctx, user, wf, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	assertRunIDs(t, summaryIDs(runs), []string{r3, r2, r1})

	first := runs[0]
	if first.WorkflowID != wf || first.WorkflowName != "Runs List Newest First" ||
		first.TriggeredBy != "manual" || first.Status != models.RunStatusRunning ||
		first.FinishedAt != nil || !first.StartedAt.Equal(runsListBase.Add(2*time.Minute)) {
		t.Fatalf("unexpected summary: %+v", first)
	}
}

func TestListWorkflowRunsTiesBrokenByID(t *testing.T) {
	s := runsListStore(t)
	ctx := context.Background()
	user := runsListUser(t, s)
	wf := runsListWorkflow(t, s, user, "Runs List Ties")

	// Same started_at for all three: the order falls to id, descending.
	want := []string{runAt(t, s, wf, runsListBase), runAt(t, s, wf, runsListBase), runAt(t, s, wf, runsListBase)}
	sort.Sort(sort.Reverse(sort.StringSlice(want)))

	runs, err := s.ListWorkflowRuns(ctx, user, wf, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	assertRunIDs(t, summaryIDs(runs), want)
}

// pageAll walks a list two rows at a time, following the cursor.
func pageAll(t *testing.T, list func(*RunCursor, int) ([]models.RunSummary, error)) []string {
	t.Helper()
	var ids []string
	var cursor *RunCursor
	for page := 0; page < 20; page++ {
		rows, err := list(cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			return ids
		}
		ids = append(ids, summaryIDs(rows)...)
		last := rows[len(rows)-1]
		cursor = &RunCursor{StartedAt: last.StartedAt, ID: last.ID}
	}
	t.Fatal("paging did not terminate")
	return nil
}

func TestListWorkflowRunsCursorPagesWithoutGapsOrDuplicates(t *testing.T) {
	s := runsListStore(t)
	ctx := context.Background()
	user := runsListUser(t, s)
	wf := runsListWorkflow(t, s, user, "Runs List Paging")

	// Two runs share a timestamp and straddle a page boundary, which is where
	// a cursor on started_at alone would drop or repeat a row.
	runAt(t, s, wf, runsListBase)
	runAt(t, s, wf, runsListBase.Add(time.Minute))
	runAt(t, s, wf, runsListBase.Add(time.Minute))
	runAt(t, s, wf, runsListBase.Add(2*time.Minute))
	runAt(t, s, wf, runsListBase.Add(3*time.Minute))

	all, err := s.ListWorkflowRuns(ctx, user, wf, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("want 5 runs, got %d", len(all))
	}
	paged := pageAll(t, func(c *RunCursor, n int) ([]models.RunSummary, error) {
		return s.ListWorkflowRuns(ctx, user, wf, c, n)
	})
	assertRunIDs(t, paged, summaryIDs(all))
}

func TestListWorkflowRunsOtherUsersWorkflowEmpty(t *testing.T) {
	s := runsListStore(t)
	ctx := context.Background()
	owner := runsListUser(t, s)
	other := runsListUser(t, s)
	wf := runsListWorkflow(t, s, owner, "Runs List Owner Only")
	runAt(t, s, wf, runsListBase)

	runs, err := s.ListWorkflowRuns(ctx, other, wf, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if runs == nil || len(runs) != 0 {
		t.Fatalf("another user's workflow must list as an empty, non-nil slice, got %#v", runs)
	}
}

func TestListRecentRunsSpansWorkflowsAndExcludesSystem(t *testing.T) {
	s := runsListStore(t)
	ctx := context.Background()
	user := runsListUser(t, s)
	a := runsListWorkflow(t, s, user, "Runs List A")
	b := runsListWorkflow(t, s, user, "Runs List B")
	sys, err := s.GetOrCreateSystemWorkflow(ctx, user, "Runs List Console")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DeleteWorkflow(context.Background(), sys.ID) })

	a1 := runAt(t, s, a, runsListBase)
	b1 := runAt(t, s, b, runsListBase.Add(time.Minute))
	runAt(t, s, sys.ID, runsListBase.Add(2*time.Minute))
	a2 := runAt(t, s, a, runsListBase.Add(3*time.Minute))

	runs, err := s.ListRecentRuns(ctx, user, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	assertRunIDs(t, summaryIDs(runs), []string{a2, b1, a1})
	if runs[1].WorkflowName != "Runs List B" {
		t.Fatalf("workflowName = %q, want Runs List B", runs[1].WorkflowName)
	}
}

func TestListRecentRunsExcludesOtherUsers(t *testing.T) {
	s := runsListStore(t)
	ctx := context.Background()
	user := runsListUser(t, s)
	other := runsListUser(t, s)
	mine := runsListWorkflow(t, s, user, "Runs List Mine")
	theirs := runsListWorkflow(t, s, other, "Runs List Theirs")

	m := runAt(t, s, mine, runsListBase)
	runAt(t, s, theirs, runsListBase.Add(time.Minute))

	runs, err := s.ListRecentRuns(ctx, user, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	assertRunIDs(t, summaryIDs(runs), []string{m})
}

func TestListRecentRunsCursorPagesAcrossWorkflows(t *testing.T) {
	s := runsListStore(t)
	ctx := context.Background()
	user := runsListUser(t, s)
	a := runsListWorkflow(t, s, user, "Runs List Page A")
	b := runsListWorkflow(t, s, user, "Runs List Page B")

	// Interleaved, so every page mixes both workflows and a per-workflow
	// LIMIT that ignored the cursor would repeat rows.
	for i := 0; i < 6; i++ {
		wf := a
		if i%2 == 1 {
			wf = b
		}
		runAt(t, s, wf, runsListBase.Add(time.Duration(i)*time.Minute))
	}

	all, err := s.ListRecentRuns(ctx, user, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 6 {
		t.Fatalf("want 6 runs, got %d", len(all))
	}
	paged := pageAll(t, func(c *RunCursor, n int) ([]models.RunSummary, error) {
		return s.ListRecentRuns(ctx, user, c, n)
	})
	assertRunIDs(t, paged, summaryIDs(all))
}

func runsListFund(t *testing.T, s *Store, userID string, micros int64) {
	t.Helper()
	ctx := context.Background()
	orderID := fmt.Sprintf("runs_list_%s_%d", userID, time.Now().UnixNano())
	if _, err := s.CreateCreditTransaction(ctx, userID, orderID, 100, float64(micros)/1e6); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CompleteCreditTransaction(ctx, "cashfree", orderID, "pay_"+orderID); err != nil {
		t.Fatal(err)
	}
}

func TestRunSpendSumsLedgerRowsPerRun(t *testing.T) {
	s := runsListStore(t)
	ctx := context.Background()
	user := runsListUser(t, s)
	other := runsListUser(t, s)
	runsListFund(t, s, user, 1_000_000)
	runsListFund(t, s, other, 1_000_000)
	wf := runsListWorkflow(t, s, user, "Runs List Spend")

	charged := runAt(t, s, wf, runsListBase.Add(time.Minute))
	free := runAt(t, s, wf, runsListBase)

	if err := s.DebitCredits(ctx, user, 10_000, "byok_flat_fee", wf, charged, "n1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DebitCredits(ctx, user, 25_000, "x402_platform_fee", wf, charged, "n2"); err != nil {
		t.Fatal(err)
	}
	// Charged to a different user against the same run. It is not this
	// user's spend and must not be counted.
	if err := s.DebitCredits(ctx, other, 7_000, "byok_flat_fee", wf, charged, "n3"); err != nil {
		t.Fatal(err)
	}

	lists := map[string]func() ([]models.RunSummary, error){
		"workflow": func() ([]models.RunSummary, error) { return s.ListWorkflowRuns(ctx, user, wf, nil, 50) },
		"recent":   func() ([]models.RunSummary, error) { return s.ListRecentRuns(ctx, user, nil, 50) },
	}
	for name, list := range lists {
		t.Run(name, func(t *testing.T) {
			rows, err := list()
			if err != nil {
				t.Fatal(err)
			}
			assertRunIDs(t, summaryIDs(rows), []string{charged, free})
			if rows[0].SpendUSDMicros != 35_000 {
				t.Fatalf("charged run spend = %d, want 35000", rows[0].SpendUSDMicros)
			}
			if rows[1].SpendUSDMicros != 0 {
				t.Fatalf("uncharged run spend = %d, want 0", rows[1].SpendUSDMicros)
			}
		})
	}
}

func TestRunCursorRoundTrip(t *testing.T) {
	c := RunCursor{StartedAt: runsListBase, ID: "0b7c9a8e-2f4d-4c1a-9e0b-5d6f7a8b9c0d"}
	got, err := ParseRunCursor(c.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if !got.StartedAt.Equal(c.StartedAt) || got.ID != c.ID {
		t.Fatalf("round trip: got %+v want %+v", got, c)
	}
}

func TestParseRunCursorRejectsWhatEncodeDidNotProduce(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, raw := range map[string]string{
		"empty":        "",
		"not base64":   "not base64!!",
		"no separator": enc("2026-03-04T05:06:07Z"),
		"empty id":     enc("2026-03-04T05:06:07Z|"),
		"bad time":     enc("yesterday|run-1"),
		"too long":     strings.Repeat("a", maxRunCursorLen+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRunCursor(raw); err == nil {
				t.Fatalf("ParseRunCursor(%q) accepted a malformed cursor", raw)
			}
		})
	}
}
