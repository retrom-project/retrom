package cleanup

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
)

// Error handles cleanup failures without leaking path-bearing error strings.
func Error(operation string, err error) {
	if err != nil {
		slog.Warn("resource cleanup failed", "operation", operation, "errorType", fmt.Sprintf("%T", err))
	}
}

// Remove handles best-effort removal without logging a host path.
func Remove(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("resource cleanup failed", "operation", "remove", "errorType", fmt.Sprintf("%T", err))
	}
}

// RemoveAll handles best-effort recursive cleanup without logging a host path.
func RemoveAll(path string) {
	if err := os.RemoveAll(path); err != nil {
		slog.Warn("resource cleanup failed", "operation", "remove-all", "errorType", fmt.Sprintf("%T", err))
	}
}
