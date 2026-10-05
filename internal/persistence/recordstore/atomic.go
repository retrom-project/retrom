package recordstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
)

type operation func(dbapi.Executor) (sql.Result, error)

// Atomic runs an application write and its dependent SQL on one connection.
// Work must contain only replayable SQL; a DB-owned operation retries conflicts.
// A failed operation rolls back its savepoint even when an outer transaction continues.
func Atomic(
	ctx context.Context, db dbapi.Executor, work operation,
) (sql.Result, error) {
	switch value := db.(type) {
	case dbapi.Tx:
		return savepoint(ctx, value, work)
	case dbapi.DB:
		var result sql.Result
		err := dbapi.RetryTransaction(ctx, value, func(tx dbapi.Tx) error {
			var err error
			result, err = savepoint(ctx, tx, work)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("commit record transaction: %w", err)
		}
		return result, nil
	default:
		return nil, errTransactionRequired
	}
}

func savepoint(
	ctx context.Context, db dbapi.Executor, work operation,
) (sql.Result, error) {
	if _, err := db.ExecContext(ctx, "SAVEPOINT record_write"); err != nil {
		return nil, fmt.Errorf("begin record savepoint: %w", err)
	}
	result, err := work(db)
	if err != nil {
		return nil, rollbackSavepoint(ctx, db, err)
	}
	if _, err := db.ExecContext(ctx, "RELEASE record_write"); err != nil {
		return nil, rollbackSavepoint(ctx, db, fmt.Errorf("release record savepoint: %w", err))
	}
	return result, nil
}

func rollbackSavepoint(ctx context.Context, db dbapi.Executor, cause error) error {
	rollbackCtx := context.WithoutCancel(ctx)
	_, rollbackErr := db.ExecContext(rollbackCtx, "ROLLBACK TO record_write")
	_, releaseErr := db.ExecContext(rollbackCtx, "RELEASE record_write")
	return errors.Join(cause, rollbackErr, releaseErr)
}
