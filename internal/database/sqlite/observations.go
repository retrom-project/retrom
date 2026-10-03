package sqlite

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"retrom/internal/telemetry"
)

type observations struct {
	sqlCalls, sqlDuration, transactions, transactionDuration atomic.Int64
}

func (db *handle) observeAcquisition(ctx context.Context, elapsed time.Duration) {
	if elapsed >= 500*time.Millisecond {
		stats := db.raw.Stats()
		slog.WarnContext(ctx, "slow SQLite transaction acquisition", "request_id", telemetry.TraceID(ctx),
			"read_only_pool", db.readOnly,
			"duration_ms", elapsed.Milliseconds(),
			"pool_wait_count", stats.WaitCount, "pool_wait_ms", stats.WaitDuration.Milliseconds())
	}
}

func (db *handle) observeSQL(ctx context.Context, started time.Time) {
	elapsed := time.Since(started)
	db.observations.sqlCalls.Add(1)
	db.observations.sqlDuration.Add(int64(elapsed))
	telemetry.RecordTiming(ctx, telemetry.SQL, elapsed)
	if elapsed >= 500*time.Millisecond {
		stats := db.raw.Stats()
		slog.WarnContext(ctx, "slow SQLite call", "request_id", telemetry.TraceID(ctx),
			"read_only_pool", db.readOnly,
			"duration_ms", elapsed.Milliseconds(),
			"pool_wait_count", stats.WaitCount, "pool_wait_ms", stats.WaitDuration.Milliseconds())
	}
}

func (tx *transaction) observeSQL(started time.Time) {
	elapsed := time.Since(started)
	tx.sqlDuration.Add(int64(elapsed))
	telemetry.RecordTiming(tx.context, telemetry.SQL, elapsed)
	tx.database.observations.sqlCalls.Add(1)
	tx.database.observations.sqlDuration.Add(int64(elapsed))
}

func (tx *transaction) observeEnd(outcome string) {
	if !tx.observed.CompareAndSwap(false, true) {
		return
	}
	tx.stopCancellation()
	elapsed := time.Since(tx.started)
	if tx.connection != nil {
		_ = tx.connection.Close()
	}
	nonSQL := max(0, elapsed-time.Duration(tx.sqlDuration.Load())-time.Duration(tx.rowsDuration.Load())-tx.commitDuration)
	if !tx.readOnly {
		telemetry.RecordTiming(tx.context, telemetry.WriteHold, elapsed)
		telemetry.RecordTiming(tx.context, telemetry.NonSQL, nonSQL)
	}
	tx.database.observations.transactions.Add(1)
	tx.database.observations.transactionDuration.Add(int64(elapsed))
	if !tx.readOnly && elapsed >= 100*time.Millisecond {
		slog.WarnContext(tx.context, "slow SQLite write transaction", "outcome", outcome,
			"owner", tx.owner, "request_id", telemetry.TraceID(tx.context),
			"rows_ms", time.Duration(tx.rowsDuration.Load()).Milliseconds(), "commit_ms", tx.commitDuration.Milliseconds(),
			"non_sql_ms", nonSQL.Milliseconds(),
			"hold_ms", elapsed.Milliseconds(), "sql_call_ms", time.Duration(tx.sqlDuration.Load()).Milliseconds(),
			"begin_ms", tx.beginDuration.Milliseconds())
	}
}

func transactionOwner() string {
	var pcs [12]uintptr
	count := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:count])
	for {
		frame, more := frames.Next()
		if strings.HasPrefix(frame.Function, "retrom/internal/") &&
			!strings.HasPrefix(frame.Function, "retrom/internal/database") {
			return frame.Function
		}
		if !more {
			return "unknown"
		}
	}
}
