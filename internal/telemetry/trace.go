// Package telemetry carries non-secret operation timing across application boundaries.
package telemetry

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type TimingPhase uint8

const (
	PoolWait TimingPhase = iota
	ReaderPoolWait
	WriterPoolWait
	Begin
	SQL
	RowsRead
	Commit
	WriteHold
	NonSQL
	phaseCount
)

type (
	traceKey struct{}
	Trace    struct {
		id, operation string
		started       time.Time
		phases        [phaseCount]atomic.Int64
	}
)

// StartTrace receives an opaque correlation ID and a fixed operation name.
// It never records SQL, arguments, request bodies, user IDs, or filesystem paths.
func StartTrace(ctx context.Context, id, operation string) (context.Context, *Trace) {
	trace := &Trace{id: id, operation: operation, started: time.Now()}
	return context.WithValue(ctx, traceKey{}, trace), trace
}

func TraceID(ctx context.Context) string {
	if trace, ok := ctx.Value(traceKey{}).(*Trace); ok {
		return trace.id
	}
	return ""
}

func RecordTiming(ctx context.Context, phase TimingPhase, elapsed time.Duration) {
	if trace, ok := ctx.Value(traceKey{}).(*Trace); ok {
		trace.phases[phase].Add(int64(elapsed))
	}
}

func (trace *Trace) Duration(phase TimingPhase) time.Duration {
	return time.Duration(trace.phases[phase].Load())
}

func (trace *Trace) Report(ctx context.Context) {
	milliseconds := func(phase TimingPhase) float64 { return float64(trace.Duration(phase)) / float64(time.Millisecond) }
	slog.InfoContext(ctx, "request database timing", "request_id", trace.id, "operation", trace.operation,
		"total_ms", float64(time.Since(trace.started))/float64(time.Millisecond),
		"pool_wait_ms", milliseconds(PoolWait), "begin_ms", milliseconds(Begin),
		"reader_pool_wait_ms", milliseconds(ReaderPoolWait), "writer_pool_wait_ms", milliseconds(WriterPoolWait),
		"sql_call_ms", milliseconds(SQL), "rows_ms", milliseconds(RowsRead),
		"commit_ms", milliseconds(Commit), "write_hold_ms", milliseconds(WriteHold), "non_sql_ms", milliseconds(NonSQL))
}
