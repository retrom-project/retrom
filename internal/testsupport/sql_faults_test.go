package testsupport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLFaultPoolRollsBackActualPriorWriteWithoutChangingOriginalPool(t *testing.T) {
	t.Parallel()
	db := sqlFaultTestDatabase(t)
	cause := errors.New("second value failed")
	first, hits := 0, 0
	fault := OpenSQLFaultDatabase(t, db, SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.HasPrefix(query, "INSERT INTO values_under_test") && len(args) == 1 && args[0].Value == "second" {
				hits++
				return cause
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(query, "INSERT INTO values_under_test") && len(args) == 1 && args[0].Value == "first" {
				first++
			}
			return result, nil
		},
	})
	err := writeTwoFaultValues(t, fault)
	if !errors.Is(err, cause) || first != 1 || hits != 1 {
		t.Fatalf("injection=%v first=%d hits=%d", err, first, hits)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM values_under_test`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("partial commit=%d", count)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO values_under_test VALUES(?)`, "second"); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatal("original pool was intercepted")
	}
}

func sqlFaultTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "faults.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE values_under_test(value TEXT)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func writeTwoFaultValues(t *testing.T, db *sql.DB) error {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO values_under_test VALUES(?)`, "first"); err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(t.Context(), `INSERT INTO values_under_test VALUES(?)`, "second")
	return err
}

func TestSQLFaultQueryHookPreservesCauseAndBoundArguments(t *testing.T) {
	t.Parallel()
	db := sqlFaultTestDatabase(t)
	cause := errors.New("injected query read failure")
	hits := 0
	fault := OpenSQLFaultDatabase(t, db, SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
		if query == "SELECT value FROM values_under_test WHERE value=?" && len(args) == 1 && args[0].Value == "blocked" {
			hits++
			return cause
		}
		return nil
	}})
	var value string
	err := fault.QueryRowContext(t.Context(), "SELECT value FROM values_under_test WHERE value=?", "blocked").Scan(&value)
	if !errors.Is(err, cause) || hits != 1 {
		t.Fatalf("query cause=%v hits=%d", err, hits)
	}
	if err := fault.QueryRowContext(t.Context(), "SELECT 'available'").Scan(&value); err != nil || value != "available" {
		t.Fatalf("unrelated query=%s %v", value, err)
	}
}
