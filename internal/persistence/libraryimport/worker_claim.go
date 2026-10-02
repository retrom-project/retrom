package libraryimport

import (
	"context"
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
	var zero T
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return zero, fmt.Errorf("begin %s claim: %w", name, err)
	}
	defer dbapi.Rollback(tx)
	if err := claimRecords(ctx, tx, jobID, workerID, now); err != nil {
		return zero, err
	}
	// A broken input must still consume an attempt. Recovery owns the lease
	// and deadline even when the worker cannot construct its input.
	claim, readErr := readClaim(ctx, tx, jobID, workerID)
	if err := tx.Commit(); err != nil {
		return zero, fmt.Errorf("commit %s claim: %w", name, err)
	}
	return claim, readErr
}
