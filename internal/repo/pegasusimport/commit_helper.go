package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
)

func commitResultTx(
	ctx context.Context,
	database *sql.DB,
	hook func(dbexec.Executor) error,
	label string,
	execute func(*sql.Tx) (string, error),
) (string, error) {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin %s: %w", label, err)
	}
	defer dbexec.Rollback(tx)
	result, err := execute(tx)
	if err != nil {
		return "", err
	}
	if hook != nil {
		if err := hook(tx); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit %s: %w", label, err)
	}
	return result, nil
}
