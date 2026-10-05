package store_test

import (
	"context"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/store"
	"retrom/internal/testsupport/testpostgres"
)

func TestWriterConfigurationSurvivesCancelledTransaction(t *testing.T) {
	ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	database, err := store.Open(ctx, testpostgres.DSN(t), time.Now)
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
SELECT CASE WHEN current_setting('session_replication_role')='origin' THEN 1 ELSE 0 END,
 (extract(epoch FROM current_setting('lock_timeout')::interval)*1000)::bigint,
 CASE WHEN current_setting('synchronous_commit')='on' THEN 2 ELSE 0 END
`).Scan(&foreignKeys, &busyTimeout, &synchronous); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || busyTimeout != 5000 || synchronous != 2 {
		t.Fatalf("after cancellation: foreign_keys=%d busy_timeout=%d synchronous=%d",
			foreignKeys, busyTimeout, synchronous)
	}
}
