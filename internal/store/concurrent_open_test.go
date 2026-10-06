package store

import (
	"context"
	"testing"
	"time"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	dbpostgres "retrom/internal/database/postgres"
	"retrom/internal/testsupport/testpostgres"
)

func TestConcurrentOpenSeesSchemaCommittedByLockPredecessor(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	dsn := testpostgres.DSN(t)
	observer, err := dbpostgres.Open(dsn, dbpostgres.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close migration observer", observer.Close()) }()
	blocker, err := observer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback() }()
	if _, err := blocker.ExecContext(ctx, "SELECT pg_advisory_xact_lock(824392571)"); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			database, openErr := Open(ctx, dsn, time.Now)
			if openErr == nil {
				openErr = database.Close()
			}
			results <- openErr
		}()
	}
	waitForInitializers(ctx, t, observer)
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	var migrations int
	if err := dbapi.QueryRowContext(ctx, observer, "SELECT count(*) FROM schema_migrations").Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 2 {
		t.Fatalf("migration records = %d, want 2", migrations)
	}
}

func waitForInitializers(ctx context.Context, t *testing.T, observer dbapi.DB) {
	t.Helper()
	// Both initializers must take their first snapshot while waiting for the
	// same lock, so a transaction-wide snapshot would miss its predecessor.
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		err := dbapi.QueryRowContext(ctx, observer, `
SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted
AND objid=824392571 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting == 2 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("concurrent initializers did not wait for the migration lock")
		case <-ticker.C:
		}
	}
}
