package nodes_test

import (
	"context"
	"os"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestPostgresAction_SkipsWithoutConnString(t *testing.T) {
	node := models.WorkflowNode{
		ID: "db1", Type: models.NodeTypeAction, Template: "db",
		Config: map[string]string{"pgTable": "events", "pgColumn": "payload"},
	}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "db_skipped_no_conn_string" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestPostgresAction_SkipsWithoutTableOrColumn(t *testing.T) {
	node := models.WorkflowNode{
		ID: "db1", Type: models.NodeTypeAction, Template: "db",
		Secrets: map[string]string{"pgConnString": "postgres://localhost/x"},
	}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "db_skipped_missing_config" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

// Identifiers reach SQL as text and cannot be bound as parameters, so they
// must be rejected rather than escaped-and-hoped.
func TestPostgresAction_RejectsUnsafeIdentifiers(t *testing.T) {
	for _, bad := range []string{
		`events; DROP TABLE users--`,
		`events"`,
		`ev ents`,
		`"events"`,
		``,
	} {
		node := models.WorkflowNode{
			ID: "db1", Type: models.NodeTypeAction, Template: "db",
			Secrets: map[string]string{"pgConnString": "postgres://localhost/x"},
			Config:  map[string]string{"pgTable": bad, "pgColumn": "payload"},
		}
		rc := engine.NewRunContext("r1", nil)
		got, err := nodes.ExecuteAction(context.Background(), node, rc)
		if err == nil && got != "db_skipped_missing_config" {
			t.Errorf("table %q should be rejected, got %v (err %v)", bad, got, err)
		}
	}
}

// Real insert against a live Postgres. Skipped unless TEST_POSTGRES_URL is set,
// so the default `go test ./...` needs no database.
func TestPostgresAction_InsertsRow(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_URL to run the live Postgres insert test")
	}
	node := models.WorkflowNode{
		ID: "db1", Type: models.NodeTypeAction, Template: "db",
		Secrets: map[string]string{"pgConnString": dsn},
		Config: map[string]string{
			"pgTable":  "agentmesh_test_events",
			"pgColumn": "payload",
		},
	}
	rc := engine.NewRunContext("r1", []byte(`"row from the workflow"`))
	got, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatalf("insert failed — create the table first:\n"+
			"  CREATE TABLE agentmesh_test_events (payload text);\n%v", err)
	}
	if got != "db_row_inserted" {
		t.Errorf("want 'db_row_inserted', got %v", got)
	}
}
