package sqlite_test

import (
	"context"
	"testing"
	"time"

	dbapi "retrom/internal/database"
)

func TestObservationsSeparatePoolWaitAndTransactionSQL(t *testing.T) {
	db := openTransactionDatabase(t, ":memory:")
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	if _, err := tx.ExecContext(t.Context(), "CREATE TABLE observations(value INTEGER)"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.ExecContext(ctx, "SELECT 1"); err == nil {
		t.Fatal("canceled queued SQL succeeded")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	stats := db.Stats()
	if stats.SQLCalls != 2 || stats.Transactions != 1 || stats.SQLCallDuration <= 0 || stats.TransactionDuration <= 0 {
		t.Fatalf("SQL and transaction observations missing: %+v", stats)
	}
	if err := tx.Rollback(); err == nil {
		t.Fatal("committed transaction rolled back")
	}
	if db.Stats().Transactions != 1 {
		t.Fatal("deferred rollback counted transaction twice")
	}
}

func TestObservationsExposeWriterPoolQueue(t *testing.T) {
	db := openTransactionDatabase(t, ":memory:")
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := db.ExecContext(ctx, "SELECT 1"); err == nil {
		t.Fatal("occupied writer accepted queued SQL")
	}
	if stats := db.Stats(); stats.WaitCount != 1 || stats.WaitDuration <= 0 {
		t.Fatalf("pool wait observations missing: %+v", stats)
	}
}

func TestObservationsIncludePreparedImportStatements(t *testing.T) {
	db := openTransactionDatabase(t, ":memory:")
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	if _, err := tx.ExecContext(t.Context(), "CREATE TABLE observations(value INTEGER)"); err != nil {
		t.Fatal(err)
	}
	statement, err := tx.PrepareContext(t.Context(), "INSERT INTO observations VALUES(?)")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []int{1, 2} {
		if _, err := statement.ExecContext(t.Context(), value); err != nil {
			t.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := dbapi.QueryRowContext(t.Context(), db, "SELECT COUNT(*) FROM observations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if stats := db.Stats(); count != 2 || stats.SQLCalls != 5 || stats.Transactions != 1 {
		t.Fatalf("prepared SQL observations: count=%d stats=%+v", count, stats)
	}
}
