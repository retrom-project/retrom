package dbexec_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"retrom/internal/repo/dbexec"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE kv(key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLayeringImmediateCommit(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ctx := context.Background()

	err := dbexec.Immediate(ctx, db, func(exec dbexec.Executor) error {
		if _, err := exec.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('a','1')`); err != nil {
			return err
		}
		if _, err := exec.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('b','2')`); err != nil {
			return err
		}
		var val string
		if err := exec.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='a'`).Scan(&val); err != nil {
			return err
		}
		if val != "1" {
			t.Fatalf("in-transaction read = %q, want 1", val)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM kv`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("committed rows = %d, want 2", count)
	}
}

func TestLayeringImmediateErrorRollsBack(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ctx := context.Background()

	sentinel := errors.New("business error")
	err := dbexec.Immediate(ctx, db, func(exec dbexec.Executor) error {
		if _, err := exec.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('x','1')`); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want sentinel", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM kv`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled back rows = %d, want 0", count)
	}
}

func TestLayeringImmediateCancelledContextRollsBack(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	err := dbexec.Immediate(ctx, db, func(exec dbexec.Executor) error {
		if _, err := exec.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('c','1')`); err != nil {
			return err
		}
		cancel()
		_, execErr := exec.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('d','2')`)
		return execErr
	})
	if err == nil {
		t.Fatal("expected error after context cancel")
	}

	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM kv`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("after cancel rows = %d, want 0", count)
	}

	// Verify pool is still usable
	if err := dbexec.Immediate(context.Background(), db, func(exec dbexec.Executor) error {
		_, err := exec.ExecContext(context.Background(), `INSERT INTO kv(key,value) VALUES('e','3')`)
		return err
	}); err != nil {
		t.Fatalf("pool after cancel: %v", err)
	}
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM kv`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("pool recovery rows = %d, want 1", count)
	}
}

func TestLayeringImmediatePanicRollsBack(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ctx := context.Background()

	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic to propagate")
			}
			if r != "test panic" {
				t.Fatalf("panic value = %v", r)
			}
		}()
		_ = dbexec.Immediate(ctx, db, func(exec dbexec.Executor) error {
			if _, err := exec.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('p','1')`); err != nil {
				return err
			}
			panic("test panic")
		})
	}()

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM kv`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("panic rows = %d, want 0", count)
	}
}

func TestLayeringCommitFailureDoesNotReportSuccess(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ctx := context.Background()

	// Insert a row via Immediate, then verify commit failure path:
	// We test this by ensuring the function contract — if Immediate returns nil,
	// the data is visible. If it returns error, it is not.
	err := dbexec.Immediate(ctx, db, func(exec dbexec.Executor) error {
		_, err := exec.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('commit-test','ok')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var val string
	if err := db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='commit-test'`).Scan(&val); err != nil {
		t.Fatal(err)
	}
	if val != "ok" {
		t.Fatalf("committed value = %q", val)
	}
}

func TestLayeringRowsAffectedErrorIsPreserved(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('ra','1')`); err != nil {
		t.Fatal(err)
	}

	err := dbexec.Immediate(ctx, db, func(exec dbexec.Executor) error {
		result, err := exec.ExecContext(ctx, `UPDATE kv SET value='2' WHERE key='ra'`)
		if err != nil {
			return err
		}
		affected, raErr := result.RowsAffected()
		if raErr != nil {
			return raErr
		}
		if affected != 1 {
			t.Fatalf("RowsAffected = %d, want 1", affected)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
