package sqlite_test

import (
	"context"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/telemetry"
)

func TestTraceSeparatesStatementRowsCommitAndApplicationWork(t *testing.T) {
	db := openTransactionDatabase(t, ":memory:")
	ctx, trace := telemetry.StartTrace(t.Context(), "request", "test operation")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	if _, err := tx.ExecContext(ctx, "CREATE TABLE metrics(value INTEGER)"); err != nil {
		t.Fatal(err)
	}
	var value int
	if err := dbapi.QueryRowContext(ctx, tx, "SELECT 1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []telemetry.TimingPhase{telemetry.PoolWait, telemetry.Begin, telemetry.SQL, telemetry.RowsRead, telemetry.Commit, telemetry.WriteHold, telemetry.NonSQL} {
		if trace.Duration(phase) <= 0 {
			t.Fatalf("missing phase %d", phase)
		}
	}
	if trace.Duration(telemetry.NonSQL) < 5*time.Millisecond || trace.Duration(telemetry.WriteHold) < trace.Duration(telemetry.NonSQL) {
		t.Fatal("application work was incorrectly attributed to SQL")
	}
	if trace.Duration(telemetry.WriterPoolWait) != trace.Duration(telemetry.PoolWait) ||
		trace.Duration(telemetry.ReaderPoolWait) != 0 {
		t.Fatal("writer acquisition attributed to the reader pool")
	}
	_, separate := telemetry.StartTrace(t.Context(), "other", "test operation")
	if separate.Duration(telemetry.SQL) != 0 || telemetry.TraceID(ctx) != "request" {
		t.Fatal("request observations leaked")
	}
}

func TestCancelledRowsReleaseConnectionBeforeCallerCleanup(t *testing.T) {
	db := openTransactionDatabase(t, ":memory:")
	ctx, cancel := context.WithCancel(t.Context())
	rows, err := db.QueryContext(ctx, "SELECT 1 UNION ALL SELECT 2")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline, finish := context.WithTimeout(t.Context(), time.Second)
	defer finish()
	if _, err := db.ExecContext(deadline, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
}
