package libraryimport

import (
	"context"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
)

// runWorkerClaim keeps the transaction lifecycle shared by worker claim
// adapters while each worker retains ownership of its SQL and claim model.
func runWorkerClaim[T any](
	ctx context.Context,
	database dbapi.DB,
	jobID, workerID string,
	now int64,
	name string,
	claimRecords func(context.Context, dbapi.Tx, string, string, int64) error,
	readClaim func(context.Context, dbapi.Tx, string, string) (T, error),
) (T, error) {
	var claim T
	var readErr error
	err := dbapi.RetryTransaction(ctx, database, func(tx dbapi.Tx) error {
		if err := claimRecords(ctx, tx, jobID, workerID, now); err != nil {
			return err
		}
		// Invalid business inputs still consume an attempt for recovery. SQL
		// errors must reach the retry boundary before an aborted COMMIT hides them.
		claim, readErr = readClaim(ctx, tx, jobID, workerID)
		var state interface{ SQLState() string }
		if errors.As(readErr, &state) {
			return readErr
		}
		return nil
	})
	if err != nil {
		var zero T
		return zero, fmt.Errorf("commit %s claim: %w", name, err)
	}
	return claim, readErr
}
