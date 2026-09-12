package recordstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
)

type operation func(DBTX) (sql.Result, error)

// Atomic runs an application write and its dependent SQL on one connection.
// A failed operation rolls back its savepoint even when an outer transaction continues.
func Atomic(
	ctx context.Context, db DBTX, work operation,
) (sql.Result, error) {
	switch value := db.(type) {
	case *sql.Tx:
		return savepoint(ctx, value, work)
	case *sql.Conn:
		return savepoint(ctx, value, work)
	case *sql.DB:
		tx, err := value.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("begin record transaction: %w", err)
		}
		defer cleanup.Rollback(tx)
		result, err := savepoint(ctx, tx, work)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit record transaction: %w", err)
		}
		return result, nil
	default:
		return nil, errTransactionRequired
	}
}

func savepoint(
	ctx context.Context, db DBTX, work operation,
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

func rollbackSavepoint(ctx context.Context, db DBTX, cause error) error {
	rollbackCtx := context.WithoutCancel(ctx)
	_, rollbackErr := db.ExecContext(rollbackCtx, "ROLLBACK TO record_write")
	_, releaseErr := db.ExecContext(rollbackCtx, "RELEASE record_write")
	return errors.Join(cause, rollbackErr, releaseErr)
}
