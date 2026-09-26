package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/accounts"
)

type (
	RecoveryRepository struct{ database dbapi.DB }
	recoveryRecords    struct{ executor dbapi.Executor }
)

func NewRecovery(database dbapi.DB) *RecoveryRepository { return &RecoveryRepository{database} }
func (repository *RecoveryRepository) ByUsername(
	ctx context.Context,
	username string,
) (accounts.RecoveryTarget, bool, error) {
	return scanRecovery(dbapi.QueryRowContext(
		ctx, repository.database,

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
	defer dbapi.Rollback(tx)
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
	return scanRecovery(dbapi.QueryRowContext(
		ctx, records.executor,

		`SELECT id,username,display_name,role,status,version FROM users WHERE id=?`,
		id,
	),
	)
}

func scanRecovery(scanner dbapi.Scanner) (accounts.RecoveryTarget, bool, error) {
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
