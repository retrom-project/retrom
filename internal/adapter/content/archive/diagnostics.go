package archive

import (
	"context"
	"fmt"
	"io"

	"retrom/internal/model/diagnostics"
)

// closeResource observes only the concrete error type. The resource error text
// may contain archive paths or payloads and must never enter diagnostics.
func closeResource(ctx context.Context, reporter diagnostics.ErrorReporter, resource io.Closer) {
	if err := resource.Close(); err != nil {
		reporter.Report(ctx, diagnostics.CleanupFailure("close", "", fmt.Sprintf("%T", err)))
	}
}
