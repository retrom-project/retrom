package accounts

import (
	"context"
	"log/slog"
	"time"

	"retrom/internal/telemetry"
)

// Timings describe only the request's work; no credentials or subject identifiers
// are retained. Waiting for the write scope is separate from transaction work.
type loginTiming struct {
	started                                               time.Time
	limits, credential, password, writerWait, transaction time.Duration
}

func (timing *loginTiming) report(ctx context.Context, err error) {
	outcome := "SUCCEEDED"
	if err != nil {
		outcome = "FAILED"
	}
	millis := func(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }
	slog.InfoContext(ctx, "authentication login timing",
		"request_id", telemetry.TraceID(ctx), "outcome", outcome, "total_ms", millis(time.Since(timing.started)),
		"limit_read_ms", millis(timing.limits), "credential_read_ms", millis(timing.credential),
		"password_verify_ms", millis(timing.password), "writer_wait_ms", millis(timing.writerWait),
		"transaction_ms", millis(timing.transaction))
}
