package db_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
)

func TestWorkflowShareQuotaConcurrentBoundary(t *testing.T) {
	for _, tc := range []struct {
		name      string
		different bool
		reuse     bool
	}{
		{name: "same workflow without reuse"},
		{name: "different workflows without reuse", different: true},
		{name: "different workflows with reuse", different: true, reuse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			databaseURL := os.Getenv("TEST_DATABASE_URL")
			if databaseURL == "" {
				t.Skip("TEST_DATABASE_URL not set")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			appName := "share-quota-" + uuid.NewString()
			u, err := url.Parse(databaseURL)
			if err != nil {
				t.Fatal(err)
			}
			query := u.Query()
			query.Set("application_name", appName)
			u.RawQuery = query.Encode()
			store, err := db.New(ctx, u.String())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			pool, err := pgxpool.New(ctx, databaseURL)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			user, err := store.CreateUser(ctx, appName+"@example.invalid", "hash")
			if err != nil {
				t.Fatal(err)
			}
			var workflows []models.Workflow
			for range 3 {
				wf, err := store.CreateWorkflow(ctx, "Quota test", user.ID)
				if err != nil {
					t.Fatal(err)
				}
				workflows = append(workflows, wf)
				defer store.DeleteWorkflow(context.Background(), wf.ID)
			}
			shareFor := func(wf models.Workflow) models.WorkflowShare {
				return models.WorkflowShare{
					Token: uuid.NewString(), WorkflowID: wf.ID, UserID: user.ID, Name: wf.Name,
					Graph:     models.WorkflowGraph{Nodes: []models.WorkflowNode{{ID: "start", Type: models.NodeTypeTrigger}}},
					NodeCount: 1,
				}
			}
			for range db.MaxActiveWorkflowShares - 1 {
				if _, _, err := store.CreateWorkflowShare(ctx, shareFor(workflows[0]), false); err != nil {
					t.Fatal(err)
				}
			}
			first, second := shareFor(workflows[1]), shareFor(workflows[1])
			if tc.different {
				second = shareFor(workflows[2])
			}

			// Hold the FK targets so inserts pause after their quota check.
			// Both calls must reach a database lock before either can commit.
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			if _, err := blocker.Exec(ctx, "SELECT id FROM workflows WHERE id = ANY($1) FOR UPDATE", []string{first.WorkflowID, second.WorkflowID}); err != nil {
				t.Fatal(err)
			}
			type result struct {
				share models.WorkflowShare
				err   error
			}
			results := make(chan result, 2)
			for _, share := range []models.WorkflowShare{first, second} {
				go func() {
					created, _, err := store.CreateWorkflowShare(ctx, share, tc.reuse)
					results <- result{created, err}
				}()
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				var waiting int
				err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND application_name = $1 AND wait_event_type = 'Lock'", appName).Scan(&waiting)
				if err != nil {
					t.Fatal(err)
				}
				if waiting == 2 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("only %d share requests reached the lock barrier", waiting)
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := blocker.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			created, refused := 0, 0
			var winner models.WorkflowShare
			for range 2 {
				res := <-results
				switch {
				case res.err == nil:
					created++
					winner = res.share
				case errors.Is(res.err, db.ErrShareQuotaExceeded):
					refused++
				default:
					t.Fatalf("unexpected share error: %v", res.err)
				}
			}
			shares, err := store.ListUserShares(ctx, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if created != 1 || refused != 1 || len(shares) != db.MaxActiveWorkflowShares {
				t.Fatalf("created=%d refused=%d stored=%d; want 1, 1, %d", created, refused, len(shares), db.MaxActiveWorkflowShares)
			}
			winner.Token = uuid.NewString()
			reused, ok, err := store.CreateWorkflowShare(ctx, winner, true)
			if err != nil || !ok || reused.Token == winner.Token {
				t.Fatalf("reuse at quota: reused=%v err=%v", ok, err)
			}
		})
	}
}
