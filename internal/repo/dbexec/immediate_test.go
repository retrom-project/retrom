package dbexec_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/repo/dbexec"
	"retrom/internal/testkit/testsupport"

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

	errCommitInjected := errors.New("injected commit failure")
	var faultHit atomic.Int32
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.EqualFold(strings.TrimSpace(query), "COMMIT") {
				faultHit.Add(1)
				return errCommitInjected
			}
			return nil
		},
	})

	err := dbexec.Immediate(ctx, faultDB, func(exec dbexec.Executor) error {
		_, err := exec.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('commit-test','should-rollback')`)
		return err
	})
	if !errors.Is(err, errCommitInjected) {
		t.Fatalf("expected commit error, got: %v", err)
	}
	if faultHit.Load() != 1 {
		t.Fatalf("faultHit = %d, want 1", faultHit.Load())
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM kv WHERE key='commit-test'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("after commit failure rows = %d, want 0 (rollback expected)", count)
	}
}

func TestLayeringRowsAffectedErrorIsPreserved(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('ra','1')`); err != nil {
		t.Fatal(err)
	}

	errRowsAffectedInjected := errors.New("injected RowsAffected error")
	var faultHit atomic.Int32
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(query)), "UPDATE") {
				faultHit.Add(1)
				return faultResult{result: result, raErr: errRowsAffectedInjected}, nil
			}
			return result, nil
		},
	})

	err := dbexec.Immediate(ctx, faultDB, func(exec dbexec.Executor) error {
		result, err := exec.ExecContext(ctx, `UPDATE kv SET value='2' WHERE key='ra'`)
		if err != nil {
			return err
		}
		_, raErr := result.RowsAffected()
		if raErr != nil {
			return raErr
		}
		return nil
	})
	if !errors.Is(err, errRowsAffectedInjected) {
		t.Fatalf("expected RowsAffected error, got: %v", err)
	}
	if faultHit.Load() != 1 {
		t.Fatalf("faultHit = %d, want 1", faultHit.Load())
	}
}

type faultResult struct {
	result driver.Result
	raErr  error
}

func (f faultResult) LastInsertId() (int64, error) { return f.result.LastInsertId() }
func (f faultResult) RowsAffected() (int64, error) { return 0, f.raErr }

func TestLayeringImmediateSuccessDoesNotRollback(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ctx := context.Background()

	var commitCount, rollbackCount atomic.Int32
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			trimmed := strings.TrimSpace(strings.ToUpper(query))
			if trimmed == "COMMIT" {
				commitCount.Add(1)
			}
			if trimmed == "ROLLBACK" {
				rollbackCount.Add(1)
			}
			return nil
		},
	})

	err := dbexec.Immediate(ctx, faultDB, func(exec dbexec.Executor) error {
		_, err := exec.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('success','1')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if commitCount.Load() != 1 {
		t.Fatalf("commitCount = %d, want 1", commitCount.Load())
	}
	if rollbackCount.Load() != 0 {
		t.Fatalf("rollbackCount = %d, want 0", rollbackCount.Load())
	}
}

func TestLayeringImmediateRollbackFailureDiscardsConnection(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ctx := context.Background()

	sentinel := errors.New("business error after write")
	errRollbackInjected := errors.New("injected rollback failure")
	var rollbackHit atomic.Int32
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.EqualFold(strings.TrimSpace(query), "ROLLBACK") {
				rollbackHit.Add(1)
				return errRollbackInjected
			}
			return nil
		},
	})

	err := dbexec.Immediate(ctx, faultDB, func(exec dbexec.Executor) error {
		if _, err := exec.ExecContext(ctx, `INSERT INTO kv(key,value) VALUES('rb-fail','1')`); err != nil {
			return err
		}
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("original business error lost: %v", err)
	}
	if !errors.Is(err, errRollbackInjected) {
		t.Fatalf("rollback error lost: %v", err)
	}
	if rollbackHit.Load() != 1 {
		t.Fatalf("rollbackHit = %d, want 1", rollbackHit.Load())
	}

	// Verify pool is still usable with a new connection
	if err := dbexec.Immediate(context.Background(), faultDB, func(exec dbexec.Executor) error {
		_, err := exec.ExecContext(context.Background(), `INSERT INTO kv(key,value) VALUES('after-rb-fail','ok')`)
		return err
	}); err != nil {
		t.Fatalf("pool after rollback failure: %v", err)
	}
}
