package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	dbapi "retrom/internal/database"
)

func TestInterruptedQueryReplacementPreservesConnectionPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retrom.db")
	db, err := Open(t.Context(), path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	execConnectionTest(t, db.SQL, `CREATE TEMP TABLE connection_marker(value INTEGER)`)
	execConnectionTest(t, db.SQL, `CREATE TABLE connection_parent(id INTEGER PRIMARY KEY)`)
	execConnectionTest(t, db.SQL, `CREATE TABLE connection_child(parent_id INTEGER REFERENCES connection_parent(id) ON DELETE CASCADE)`)
	execConnectionTest(t, db.SQL, `CREATE TABLE connection_optional(parent_id INTEGER REFERENCES connection_parent(id) ON DELETE SET NULL)`)
	execConnectionTest(t, db.SQL, `INSERT INTO connection_parent VALUES(1)`)
	execConnectionTest(t, db.SQL, `INSERT INTO connection_child VALUES(1)`)
	execConnectionTest(t, db.SQL, `INSERT INTO connection_optional VALUES(1)`)

	// A timed-out query outside a transaction invalidates the modernc connection.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	var total int
	err = dbapi.QueryRowContext(ctx, db.SQL, `WITH RECURSIVE n(x) AS (
SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT sum(x) FROM n`).Scan(&total)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("interrupted query error = %v", err)
	}
	assertConnectionValue(t, db.SQL, "SELECT count(*) FROM sqlite_temp_schema WHERE name='connection_marker'", 0)
	assertConnectionValue(t, db.SQL, "PRAGMA foreign_keys", 1)
	assertConnectionValue(t, db.SQL, "PRAGMA busy_timeout", 5000)
	assertConnectionValue(t, db.SQL, "PRAGMA synchronous", 2)
	if _, err := db.SQL.ExecContext(t.Context(), "INSERT INTO connection_child VALUES(2)"); err == nil {
		t.Fatal("replacement connection accepted a missing parent")
	}
	execConnectionTest(t, db.SQL, "DELETE FROM connection_parent WHERE id=1")
	assertConnectionValue(t, db.SQL, "SELECT count(*) FROM connection_child", 0)
	assertConnectionValue(t, db.SQL, "SELECT count(*) FROM connection_optional WHERE parent_id IS NULL", 1)
	if err := db.IntegrityCheck(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path, time.Now)
	if err != nil {
		t.Fatalf("reopen after interruption: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.IntegrityCheck(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func execConnectionTest(t *testing.T, db dbapi.DB, query string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query); err != nil {
		t.Fatal(err)
	}
}

func TestReadOnlyPoolInitializesEveryPhysicalConnection(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for range 4 {
		tx, err := db.ReadOnly.BeginTx(t.Context(), &dbapi.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		for query, want := range map[string]int{
			"PRAGMA foreign_keys": 1, "PRAGMA busy_timeout": 5000, "PRAGMA synchronous": 2,
		} {
			var got int
			if err := dbapi.QueryRowContext(t.Context(), tx, query).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("%s = %d, want %d", query, got, want)
			}
		}
	}
}

func assertConnectionValue(t *testing.T, db dbapi.DB, query string, want int) {
	t.Helper()
	var got int
	if err := dbapi.QueryRowContext(t.Context(), db, query).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("%s = %d, want %d", query, got, want)
	}
}
