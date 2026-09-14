package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/dbexec"
)

type (
	RecoveryRepository struct{ database *sql.DB }
	recoveryRecords    struct{ executor dbexec.Executor }
)

func NewRecovery(database *sql.DB) *RecoveryRepository { return &RecoveryRepository{database} }
func (repository *RecoveryRepository) ByUsername(
	ctx context.Context,
	username string,
) (accounts.RecoveryTarget, bool, error) {
	return scanRecovery(
		repository.database.QueryRowContext(
			ctx,
			`SELECT id,username,display_name,role,status,version FROM users WHERE username=?`,
			username,
		),
	)
}

func (repository *RecoveryRepository) WithWrite(ctx context.Context, work func(accounts.RecoveryScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin offline recovery: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := recoveryRecords{tx}
	if err := work(accounts.RecoveryScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit offline recovery: %w", err)
	}
	return nil
}

func (records recoveryRecords) Current(ctx context.Context, id string) (accounts.RecoveryTarget, bool, error) {
	return scanRecovery(
		records.executor.QueryRowContext(
			ctx,
			`SELECT id,username,display_name,role,status,version FROM users WHERE id=?`,
			id,
		),
	)
}

func scanRecovery(scanner dbexec.Scanner) (accounts.RecoveryTarget, bool, error) {
	var target accounts.RecoveryTarget
	err := scanner.Scan(
		&target.UserID,
		&target.Username,
		&target.DisplayName,
		&target.Role,
		&target.Status,
		&target.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return target, false, nil
	}
	if err != nil {
		return target, false, fmt.Errorf("scan recovery target: %w", err)
	}
	return target, true, nil
}
