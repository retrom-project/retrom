package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/store"
)

func TestWriterConfigurationSurvivesCancelledTransaction(t *testing.T) {
	ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	txContext, cancel := context.WithCancel(ctx)
	defer cancel()
	tx, err := database.SQL.BeginTx(txContext, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dbapi.Rollback(tx) })
	cancel()
	var foreignKeys, busyTimeout, synchronous int
	if err := dbapi.QueryRowContext(ctx, database.SQL, `
SELECT (SELECT foreign_keys FROM pragma_foreign_keys),
       (SELECT timeout FROM pragma_busy_timeout),
       (SELECT synchronous FROM pragma_synchronous)
`).Scan(&foreignKeys, &busyTimeout, &synchronous); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || busyTimeout != 5000 || synchronous != 2 {
		t.Fatalf("after cancellation: foreign_keys=%d busy_timeout=%d synchronous=%d",
			foreignKeys, busyTimeout, synchronous)
	}
}
