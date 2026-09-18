package launch

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
)

func readOnlyTx[T any](
	ctx context.Context,
	database *sql.DB,
	label string,
	read func(dbexec.Executor) (T, bool, error),
) (T, bool, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		var zero T
		return zero, false, fmt.Errorf("begin %s: %w", label, err)
	}
	defer dbexec.Rollback(tx)
	result, found, err := read(tx)
	if err != nil {
		var zero T
		return zero, false, err
	}
	if err := tx.Commit(); err != nil {
		var zero T
		return zero, false, fmt.Errorf("commit %s: %w", label, err)
	}
	return result, found, nil
}
