package db

import (
	"context"
	"errors"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/agentmesh/backend/internal/models"
)

func TestWorkflowSharesMigrationPreservesPrerequisiteRows(t *testing.T) {
	base := accountTestStore(t)
	schema := "shares_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := base.pool.Exec(t.Context(), "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := base.pool.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	query.Set("default_query_exec_mode", "simple_protocol")
	u.RawQuery = query.Encode()
	conn, err := pgx.Connect(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	u.Scheme = "pgx5"
	m, err := migrate.NewWithSourceInstance("iofs", source, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Migrate(43); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO users (id,email,password_hash,created_at) VALUES
		 ('share-owner','owner@example.test','hash','2024-02-29T12:34:56.123456Z'),
		 ('other-owner','other@example.test','hash','2025-11-02T01:30:00.654321-04:00')`,
		`INSERT INTO workflows (id,user_id,name) VALUES ('kept-workflow','share-owner','Kept graph')`,
		`INSERT INTO user_settings (user_id,low_balance_usd_micros,max_call_spend_usd_micros,updated_at)
		 VALUES ('share-owner',1234567,250000,'2026-01-01T00:00:01.234567Z')`,
		`INSERT INTO waitlist_archived (id,email,created_at)
		 VALUES ('retained-waitlist','owner@example.test','2024-01-02T03:04:05.123456Z')`,
	} {
		if _, err := conn.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	tables := map[string]string{"users": "id", "workflows": "id", "user_settings": "user_id", "waitlist_archived": "id"}
	snapshot := func(table string) string {
		t.Helper()
		var value string
		statement := "SELECT jsonb_agg(to_jsonb(record) ORDER BY " + pgx.Identifier{tables[table]}.Sanitize() + ")::text FROM " + pgx.Identifier{table}.Sanitize() + " AS record"
		if err := conn.QueryRow(t.Context(), statement).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	want := make(map[string]string, len(tables))
	for table := range tables {
		want[table] = snapshot(table)
	}
	assertState := func(version uint, present bool) {
		t.Helper()
		got, dirty, err := m.Version()
		if err != nil || got != version || dirty {
			t.Fatalf("version=%d dirty=%v err=%v, want %d clean", got, dirty, err, version)
		}
		var exists bool
		if err := conn.QueryRow(t.Context(), `SELECT to_regclass('workflow_shares') IS NOT NULL`).Scan(&exists); err != nil || exists != present {
			t.Fatalf("share table exists=%v err=%v, want %v", exists, err, present)
		}
		for table, expected := range want {
			if got := snapshot(table); got != expected {
				t.Fatalf("%s IDs, timestamps or values changed: got %s, want %s", table, got, expected)
			}
		}
	}
	assertState(43, false)
	for range 2 {
		if err := m.Migrate(44); err != nil {
			t.Fatal(err)
		}
		assertState(44, true)
		if _, err := conn.Exec(t.Context(), `INSERT INTO workflow_shares
		 (token,workflow_id,user_id,name,graph) VALUES
		 ('migration-share-token','kept-workflow','share-owner','Frozen graph','{"nodes":[],"edges":[]}')`); err != nil {
			t.Fatal(err)
		}
		if err := m.Steps(-1); err != nil {
			t.Fatal(err)
		}
		assertState(43, false)
	}
	if err := m.Migrate(44); err != nil {
		t.Fatal(err)
	}
	assertState(44, true)
	var rows int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM workflow_shares`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("replayed share table rows=%d err=%v, want empty", rows, err)
	}
}

func shareForAccount(t *testing.T, s *Store, userID, workflowID string) models.WorkflowShare {
	t.Helper()
	share, _, err := s.CreateWorkflowShare(t.Context(), models.WorkflowShare{
		Token: uuid.NewString(), UserID: userID, WorkflowID: workflowID,
		Name: "Owned snapshot", NodeCount: 1,
		Graph: models.WorkflowGraph{Nodes: []models.WorkflowNode{{ID: "start", Type: models.NodeTypeTrigger}}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	return share
}

func TestWorkflowSharesFollowAccountDeletionTransaction(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "commit"
		if rollback {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			s := accountTestStore(t)
			owner, other := accountFixture(t, s), accountFixture(t, s)
			ownShare := shareForAccount(t, s, owner, owner)
			otherShare := shareForAccount(t, s, other, other)
			if rollback {
				if _, err := s.pool.Exec(t.Context(), `UPDATE tendril_leases SET workflow_id=$1, run_id=$1 WHERE user_id=$2`, owner, other); err != nil {
					t.Fatal(err)
				}
			}
			err := s.DeleteAccount(t.Context(), owner, "hash")
			if rollback != (err != nil) {
				t.Fatalf("rollback=%v deletion error=%v", rollback, err)
			}
			got, err := s.GetWorkflowShare(t.Context(), ownShare.Token)
			if rollback {
				assertAccountRows(t, s, owner, 1)
				if err != nil || !reflect.DeepEqual(got, ownShare) {
					t.Fatalf("rollback changed share: got=%+v err=%v want=%+v", got, err, ownShare)
				}
			} else {
				assertAccountRows(t, s, owner, 0)
				if !errors.Is(err, ErrShareNotFound) {
					t.Fatalf("deleted owner's share remains: %+v err=%v", got, err)
				}
			}
			assertAccountRows(t, s, other, 1)
			got, err = s.GetWorkflowShare(t.Context(), otherShare.Token)
			if err != nil || !reflect.DeepEqual(got, otherShare) {
				t.Fatalf("other owner's share changed: got=%+v err=%v want=%+v", got, err, otherShare)
			}
		})
	}
}

func TestDeletingWorkflowRetractsOnlyItsShares(t *testing.T) {
	s := accountTestStore(t)
	owner, other := accountFixture(t, s), accountFixture(t, s)
	workflow, err := s.CreateWorkflow(t.Context(), "Delete this graph", owner)
	if err != nil {
		t.Fatal(err)
	}
	share := shareForAccount(t, s, owner, workflow.ID)
	otherShare := shareForAccount(t, s, other, other)
	if err := s.DeleteWorkflow(t.Context(), workflow.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetWorkflowShare(t.Context(), share.Token); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("workflow share remains: %v", err)
	}
	got, err := s.GetWorkflowShare(t.Context(), otherShare.Token)
	if err != nil || !reflect.DeepEqual(got, otherShare) {
		t.Fatalf("unrelated share changed: got=%+v err=%v", got, err)
	}
}
