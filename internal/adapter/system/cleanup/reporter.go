// Package cleanup owns the output boundary for nonfatal resource diagnostics.
package cleanup

import (
	"context"
	"log/slog"

	"retrom/internal/model/diagnostics"
)

type Reporter struct {
	logger *slog.Logger
}

var _ diagnostics.ErrorReporter = (*Reporter)(nil)

// NewReporter receives the Bootstrap-owned logger instead of looking up global state.
func NewReporter(logger *slog.Logger) *Reporter {
	if logger == nil {
		panic("cleanup reporter requires a logger")
	}
	return &Reporter{logger: logger}
}

func (reporter *Reporter) Report(ctx context.Context, event diagnostics.DiagnosticEvent) {
	// The interface can receive an event literal; enforce the same policy again
	// at the output boundary. Invalid fields fall back without dropping the event.
	event = diagnostics.CleanupFailure(event.Operation, event.RequestID, event.Message)
	reporter.logger.WarnContext(ctx, "resource cleanup failed",
		"operation", event.Operation, "errorType", event.Message,
		"code", event.Code, "requestID", event.RequestID)
}
