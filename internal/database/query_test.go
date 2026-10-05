package database_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"retrom/internal/database/postgres"
	"retrom/internal/testsupport/testpostgres"

	dbapi "retrom/internal/database"
)

func TestQueryRowContextScansFirstRowAndReleasesRows(t *testing.T) {
	ctx := context.Background()
	db, err := postgres.Open(testpostgres.DSN(t), postgres.Options{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE TABLE items(value BIGINT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO items(value) VALUES (3),(5)"); err != nil {
		t.Fatal(err)
	}
	var value int
	if err := dbapi.QueryRowContext(ctx, db, "SELECT value FROM items ORDER BY value").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 3 || db.Stats().InUse != 0 {
		t.Fatalf("value=%d inUse=%d", value, db.Stats().InUse)
	}
	if err := dbapi.QueryRowContext(ctx, db, "SELECT value FROM items WHERE value=99").Scan(&value); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty row error=%v", err)
	}
	if err := dbapi.QueryRowContext(ctx, db, "SELECT missing FROM items").Scan(&value); err == nil {
		t.Fatal("invalid query succeeded")
	}
	if db.Stats().InUse != 0 {
		t.Fatalf("query leaked a connection: %+v", db.Stats())
	}
}

func TestColumnMapUsesReturnedColumnOrder(t *testing.T) {
	ctx := context.Background()
	db, err := postgres.Open(testpostgres.DSN(t), postgres.Options{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rows, err := db.QueryContext(ctx, "SELECT 1 AS second, 2 AS first")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := dbapi.ColumnMap(rows)
	if err != nil {
		t.Fatal(err)
	}
	if columns["second"] != 0 || columns["first"] != 1 {
		t.Fatalf("column order=%v", columns)
	}
	if !rows.Next() {
		t.Fatal("query returned no row")
	}
	var second, first int
	if err := rows.Scan(&second, &first); err != nil {
		t.Fatal(err)
	}
	if second != 1 || first != 2 || rows.Next() {
		t.Fatalf("unexpected row values: %d, %d", second, first)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
