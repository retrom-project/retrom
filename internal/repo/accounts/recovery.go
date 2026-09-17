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

func (repository *RecoveryRepository) CommitRecovery(
	ctx context.Context, cmd accounts.RecoveryCommand,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin offline recovery: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := recoveryRecords{tx}
	current, found, err := records.Current(ctx, cmd.UserID)
	if err != nil {
		return fmt.Errorf("recheck offline recovery target: %w", err)
	}
	if !found || !accounts.Recoverable(current) || current.Version != cmd.Version {
		return accounts.ErrOfflineAdmin
	}
	plan := accounts.RecoveryPlan{
		Target: current, PasswordHash: cmd.PasswordHash, AuditID: cmd.AuditID,
		ClearTestDefault: current.Username == "test", Now: cmd.NowMS,
		BeforeJSON: fmt.Sprintf(
			`{"status":%q,"version":%d}`,
			current.Status, current.Version,
		),
		AfterJSON: fmt.Sprintf(
			`{"status":"ENABLED","version":%d}`,
			current.Version+1,
		),
	}
	if err := records.Reset(ctx, plan); err != nil {
		return fmt.Errorf("reset offline admin security: %w", err)
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
