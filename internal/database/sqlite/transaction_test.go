package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	moderncsqlite "modernc.org/sqlite"
	sqlitecodes "modernc.org/sqlite/lib"

	dbapi "retrom/internal/database"
	"retrom/internal/database/sqlite"
)

func openTransactionDatabase(t *testing.T, dsn string) dbapi.DB {
	t.Helper()
	db, err := sqlite.Open(dsn, sqlite.Options{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func transactionDatabases(t *testing.T) (dbapi.DB, dbapi.DB) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "transactions.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(0)&_pragma=foreign_keys(1)"
	first := openTransactionDatabase(t, dsn)
	if _, err := first.ExecContext(t.Context(), "CREATE TABLE items(value INTEGER); INSERT INTO items VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	second := openTransactionDatabase(t, dsn)
	if err := second.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return first, second
}

func requireSQLiteCode(t *testing.T, err error, code int) {
	t.Helper()
	var sqliteError *moderncsqlite.Error
	if !errors.As(err, &sqliteError) || sqliteError.Code() != code {
		t.Fatalf("sqlite error=%v, want code %d", err, code)
	}
}

func requireItemSum(ctx context.Context, t *testing.T, db dbapi.Queryer, want int) {
	t.Helper()
	var value int
	if err := dbapi.QueryRowContext(ctx, db, "SELECT sum(value) FROM items").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != want {
		t.Fatalf("item sum=%d, want %d", value, want)
	}
}

func TestWriteTransactionReservesWriterBeforeQueries(t *testing.T) {
	first, second := transactionDatabases(t)
	tx, err := first.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dbapi.Rollback(tx) })
	blocked, err := second.BeginTx(t.Context(), nil)
	if blocked != nil {
		dbapi.Rollback(blocked)
		t.Fatal("second writer started before first writer released its reservation")
	}
	requireSQLiteCode(t, err, sqlitecodes.SQLITE_BUSY)
	if second.Stats().InUse != 0 {
		t.Fatal("failed begin leaked a connection")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.InTransaction(t.Context(), second, nil, func(tx dbapi.Tx) error {
		_, err := tx.ExecContext(t.Context(), "INSERT INTO items VALUES(2)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	requireItemSum(t.Context(), t, first, 3)
}

func TestReadOnlyTransactionKeepsSnapshotWhileWriterCommits(t *testing.T) {
	writer, reader := transactionDatabases(t)
	write, err := writer.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dbapi.Rollback(write) })
	read, err := reader.BeginTx(t.Context(), &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dbapi.Rollback(read) })
	requireItemSum(t.Context(), t, read, 1)
	if _, err := write.ExecContext(t.Context(), "INSERT INTO items VALUES(2)"); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(); err != nil {
		t.Fatal(err)
	}
	requireItemSum(t.Context(), t, read, 1)
	if err := read.Commit(); err != nil {
		t.Fatal(err)
	}
	requireItemSum(t.Context(), t, reader, 3)
}

func TestCancelledTransactionRollsBackAndReleasesWriter(t *testing.T) {
	first, second := transactionDatabases(t)
	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	txContext, cancel := context.WithCancel(ctx)
	defer cancel()
	tx, err := first.BeginTx(txContext, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dbapi.Rollback(tx) })
	if _, err := tx.ExecContext(txContext, "INSERT INTO items VALUES(2)"); err != nil {
		t.Fatal(err)
	}
	cancel()
	// Reacquiring the sole connection waits for database/sql's automatic rollback.
	requireItemSum(ctx, t, first, 1)
	if first.Stats().InUse != 0 {
		t.Fatal("cancelled transaction leaked a connection")
	}
	if err := dbapi.InTransaction(ctx, second, nil, func(tx dbapi.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO items VALUES(3)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	requireItemSum(ctx, t, first, 4)
}

func TestFailedCommitRollsBackAndReleasesConnection(t *testing.T) {
	db, _ := transactionDatabases(t)
	if _, err := db.ExecContext(t.Context(), `
CREATE TABLE parents(id INTEGER PRIMARY KEY);
CREATE TABLE children(parent_id INTEGER REFERENCES parents(id) DEFERRABLE INITIALLY DEFERRED);
`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dbapi.Rollback(tx) })
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO children VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	requireSQLiteCode(t, tx.Commit(), sqlitecodes.SQLITE_CONSTRAINT_FOREIGNKEY)
	var count int
	if err := dbapi.QueryRowContext(t.Context(), db, "SELECT count(*) FROM children").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 || db.Stats().InUse != 0 {
		t.Fatalf("failed commit left rows=%d connections=%d", count, db.Stats().InUse)
	}
	if err := dbapi.InTransaction(t.Context(), db, nil, func(tx dbapi.Tx) error {
		_, err := tx.ExecContext(t.Context(), "INSERT INTO parents VALUES(1); INSERT INTO children VALUES(1)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
