package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
)

// runWorkerClaim keeps the transaction lifecycle shared by worker claim
// adapters while each worker retains ownership of its SQL and claim model.
func runWorkerClaim[T any](
	ctx context.Context,
	database *sql.DB,
	jobID, workerID string,
	now int64,
	name string,
	claimRecords func(context.Context, *sql.Tx, string, string, int64) error,
	readClaim func(context.Context, *sql.Tx, string, string) (T, error),
) (T, error) {
	var zero T
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return zero, fmt.Errorf("begin %s claim: %w", name, err)
	}
	defer dbexec.Rollback(tx)
	if err := claimRecords(ctx, tx, jobID, workerID, now); err != nil {
		return zero, err
	}
	claim, err := readClaim(ctx, tx, jobID, workerID)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, fmt.Errorf("commit %s claim: %w", name, err)
	}
	return claim, nil
}
