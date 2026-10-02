package sqlite

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type observations struct {
	sqlCalls, sqlDuration, transactions, transactionDuration atomic.Int64
}

func (db *handle) observeAcquisition(ctx context.Context, elapsed time.Duration) {
	if elapsed >= 500*time.Millisecond {
		stats := db.raw.Stats()
		slog.WarnContext(ctx, "slow SQLite transaction acquisition", "duration_ms", elapsed.Milliseconds(),
			"pool_wait_count", stats.WaitCount, "pool_wait_ms", stats.WaitDuration.Milliseconds())
	}
}

func (db *handle) observeSQL(ctx context.Context, started time.Time) {
	elapsed := time.Since(started)
	db.observations.sqlCalls.Add(1)
	db.observations.sqlDuration.Add(int64(elapsed))
	if elapsed >= 500*time.Millisecond {
		stats := db.raw.Stats()
		slog.WarnContext(ctx, "slow SQLite call", "duration_ms", elapsed.Milliseconds(),
			"pool_wait_count", stats.WaitCount, "pool_wait_ms", stats.WaitDuration.Milliseconds())
	}
}

func (tx *transaction) observeSQL(started time.Time) {
	elapsed := time.Since(started)
	tx.sqlDuration.Add(int64(elapsed))
	tx.database.observations.sqlCalls.Add(1)
	tx.database.observations.sqlDuration.Add(int64(elapsed))
}

func (tx *transaction) observeEnd(outcome string) {
	if !tx.observed.CompareAndSwap(false, true) {
		return
	}
	elapsed := time.Since(tx.started)
	tx.database.observations.transactions.Add(1)
	tx.database.observations.transactionDuration.Add(int64(elapsed))
	if !tx.readOnly && elapsed >= 100*time.Millisecond {
		slog.WarnContext(tx.context, "slow SQLite write transaction", "outcome", outcome,
			"hold_ms", elapsed.Milliseconds(), "sql_call_ms", time.Duration(tx.sqlDuration.Load()).Milliseconds(),
			"begin_ms", tx.beginDuration.Milliseconds())
	}
}
