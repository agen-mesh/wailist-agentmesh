package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
	"github.com/jackc/pgx/v5"
)

// pgConnectTimeout bounds how long sendPostgres waits to establish a
// connection. Without it, a misconfigured or firewalled host that silently
// drops SYN packets (rather than refusing the connection) hangs this node —
// and the whole run, since node execution is sequential per topological
// level — until the caller cancels it by hand.
var pgConnectTimeout = 10 * time.Second

// SetPostgresConnectTimeoutForTest overrides pgConnectTimeout. Call only from
// tests. Pass 0 to reset to the real 10s timeout.
func SetPostgresConnectTimeoutForTest(d time.Duration) {
	if d <= 0 {
		pgConnectTimeout = 10 * time.Second
	} else {
		pgConnectTimeout = d
	}
}

// pgIdentifier matches a safe unquoted SQL identifier. Table and column names
// cannot be sent as bind parameters — they are part of the statement text — so
// anything not matching this is rejected outright rather than escaped. An
// optional schema qualifier is allowed: "public.events".
var pgIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// quotePGIdentifier wraps each dot-separated part in double quotes,
// lowercasing first. Only ever called on strings already validated by
// pgIdentifier.
//
// Postgres folds *unquoted* DDL identifiers to lowercase -- a table created
// as `CREATE TABLE Events (...)` is actually stored as `events`. Quoting a
// mixed-case value verbatim (without lowercasing) makes this connector
// case-sensitive against a database that isn't: a user who types
// pgTable="Events", matching what they see in their own schema/tooling,
// would get `INSERT INTO "Events"`, which Postgres rejects with `relation
// "Events" does not exist` -- a name that works fine unquoted fails here
// purely because this always quotes it. Lowercasing before quoting matches
// what typing that name unquoted in raw SQL would actually resolve to, the
// overwhelmingly common case for real schemas.
func quotePGIdentifier(ident string) string {
	parts := strings.Split(ident, ".")
	for i, p := range parts {
		parts[i] = `"` + strings.ToLower(p) + `"`
	}
	return strings.Join(parts, ".")
}

// sendPostgres inserts one row containing the run output. Values are always
// bound as parameters; only the table and column names are interpolated, and
// only after passing pgIdentifier.
func sendPostgres(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	connString := secretVal(node, "pgConnString")
	if connString == "" {
		return "db_skipped_no_conn_string", ErrActionSkipped
	}
	table := configVal(node, "pgTable", "")
	column := configVal(node, "pgColumn", "")
	if table == "" || column == "" {
		return "db_skipped_missing_config", ErrActionSkipped
	}
	if !pgIdentifier.MatchString(table) {
		return nil, fmt.Errorf("db: table name %q is not a valid SQL identifier", table)
	}
	if !pgIdentifier.MatchString(column) {
		return nil, fmt.Errorf("db: column name %q is not a valid SQL identifier", column)
	}

	cols := []string{quotePGIdentifier(column)}
	vals := []any{rc.Message()}

	if extra := configVal(node, "pgExtraColumns", ""); extra != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(extra), &m); err != nil {
			return nil, fmt.Errorf("db: `pgExtraColumns` is not a valid JSON object: %w", err)
		}
		for k, v := range m {
			if !pgIdentifier.MatchString(k) {
				return nil, fmt.Errorf("db: extra column name %q is not a valid SQL identifier", k)
			}
			cols = append(cols, quotePGIdentifier(k))
			if s, ok := v.(string); ok {
				vals = append(vals, resolveTemplate(s, rc))
				continue
			}
			vals = append(vals, v)
		}
	}

	placeholders := make([]string, len(vals))
	for i := range vals {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quotePGIdentifier(table), strings.Join(cols, ", "), strings.Join(placeholders, ", "))

	connCtx, cancel := context.WithTimeout(ctx, pgConnectTimeout)
	defer cancel()
	conn, err := pgx.Connect(connCtx, connString)
	if err != nil {
		return nil, fmt.Errorf("db: could not connect: %w", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, stmt, vals...); err != nil {
		return nil, fmt.Errorf("db: insert failed: %w", err)
	}
	return "db_row_inserted", nil
}
