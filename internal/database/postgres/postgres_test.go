package postgres_test

import (
	"context"
	"errors"
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/database/postgres"
	"retrom/internal/testsupport/testpostgres"
)

func TestTransactionCommitRollbackAndRelease(t *testing.T) {
	ctx := context.Background()
	db, err := postgres.Open(testpostgres.DSN(t), postgres.Options{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE TABLE items(value BIGINT)"); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("abort")
	err = dbapi.InTransaction(ctx, db, nil, func(tx dbapi.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO items(value) VALUES (3)")
		if err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("rollback error=%v", err)
	}
	err = dbapi.InTransaction(ctx, db, nil, func(tx dbapi.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO items(value) VALUES (5)")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var count, value int
	if err := dbapi.QueryRowContext(ctx, db, "SELECT count(*), min(value) FROM items").Scan(&count, &value); err != nil {
		t.Fatal(err)
	}
	if count != 1 || value != 5 || db.Stats().InUse != 0 {
		t.Fatalf("count=%d value=%d inUse=%d", count, value, db.Stats().InUse)
	}
}
