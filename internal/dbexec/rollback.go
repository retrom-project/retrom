package dbexec

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// Rollback ignores the expected post-commit sentinel and reports other cleanup failures.
func Rollback(transaction *sql.Tx) {
	if err := transaction.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		slog.Warn("resource cleanup failed", "operation", "rollback", "errorType", fmt.Sprintf("%T", err))
	}
}
