package db_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/agentmesh/backend/internal/db"
)

func TestAllowShareCapsTheUserAndTheDay(t *testing.T) {
	store := testStore(t)
	now := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)
	err := store.WithShareLock(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM arkiv_share_meter`); err != nil {
			return err
		}
		for i := 0; i < 10; i++ {
			if err := db.RecordShare(ctx, tx, "dev", now.Add(-time.Duration(i)*time.Minute)); err != nil {
				return err
			}
		}
		if err := db.AllowShare(ctx, tx, "dev", now); !errors.Is(err, db.ErrShareRate) {
			t.Fatalf("11th in an hour = %v, want ErrShareRate", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestShareLockDoesNotQueueWaiters(t *testing.T) {
	src, err := os.ReadFile("arkiv_share_meter.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(src, []byte("pg_try_advisory_xact_lock($1, $2)")) {
		t.Fatal("lock SQL must be the two-key try-lock")
	}
	if bytes.Contains(src, []byte("pg_advisory_lock(")) || bytes.Contains(src, []byte("pg_advisory_xact_lock(")) {
		t.Fatal("a blocking or session advisory lock pins a transaction-pooler connection per waiter")
	}
	store := testStore(t)
	started := make(chan struct{})
	release := make(chan struct{})
	holdDone := make(chan error, 1)
	go func() {
		holdDone <- store.WithShareLock(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	entered := false
	err = store.WithShareLock(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		entered = true
		return nil
	})
	if entered || !errors.Is(err, db.ErrShareBusy) {
		t.Fatalf("waiter entered=%v err=%v, want ErrShareBusy", entered, err)
	}
	close(release)
	if err := <-holdDone; err != nil {
		t.Fatal(err)
	}
	if err := store.WithShareLock(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return nil
	}); err != nil {
		t.Fatalf("lock after release: %v", err)
	}
}

func TestShareLockDoesNotWaitOnAnOAuthCredentialLock(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	release, err := store.LockOAuthCredentialForRefresh(ctx, "7794095")
	if err != nil {
		t.Fatal(err)
	}
	defer release(ctx)
	done := make(chan error, 1)
	go func() {
		done <- store.WithShareLock(ctx, func(ctx context.Context, tx pgx.Tx) error { return nil })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("share lock waited on an unrelated OAuth credential lock")
	}
}

func TestAllowShareCapsTheDay(t *testing.T) {
	store := testStore(t)
	now := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)
	err := store.WithShareLock(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM arkiv_share_meter`); err != nil {
			return err
		}
		for i := 0; i < 200; i++ {
			if err := db.RecordShare(ctx, tx, "dev", now.Add(-2*time.Hour)); err != nil {
				return err
			}
		}
		if err := db.AllowShare(ctx, tx, "dev", now); !errors.Is(err, db.ErrShareBudget) {
			t.Fatalf("200 today = %v, want ErrShareBudget", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAllowShareKeepsYesterdayOutsideTheDay(t *testing.T) {
	store := testStore(t)
	now := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)
	err := store.WithShareLock(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM arkiv_share_meter`); err != nil {
			return err
		}
		for i := 0; i < 199; i++ {
			if err := db.RecordShare(ctx, tx, "dev", now.Add(-2*time.Hour)); err != nil {
				return err
			}
		}
		if err := db.RecordShare(ctx, tx, "dev", now.Add(-25*time.Hour)); err != nil {
			return err
		}
		if err := db.AllowShare(ctx, tx, "dev", now); err != nil {
			t.Fatalf("199 today plus one yesterday = %v, want nil", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestShareMeterHasNoPayloadColumn(t *testing.T) {
	store := testStore(t)
	err := store.WithShareLock(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT column_name FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'arkiv_share_meter'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		cols := map[string]bool{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			cols[name] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, want := range []string{"id", "user_id", "created_at"} {
			if !cols[want] {
				t.Fatalf("missing column %s in %v", want, cols)
			}
		}
		if cols["payload"] || cols["entity_key"] {
			t.Fatalf("meter stores a share body: %v", cols)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
