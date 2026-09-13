package accounts

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
	"retrom/internal/service/accounts"
)

func (repository *LinkRepository) WithConsumptionWrite(
	ctx context.Context,
	work func(accounts.LinkConsumptionScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin account link consumption: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := linkRecords{accountOperations{tx}}
	if err := work(accounts.LinkConsumptionScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account link consumption: %w", err)
	}
	return nil
}

func (repository *LinkRepository) ResetState(ctx context.Context, id string) (accounts.ResetState, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return accounts.ResetState{}, false, fmt.Errorf("begin reset capability snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := linkRecords{accountOperations{tx}}
	link, found, err := records.Current(ctx, id)
	if err != nil {
		return accounts.ResetState{}, false, err
	}
	if !found || link.Link.TargetUserID == nil {
		return accounts.ResetState{}, false, nil
	}
	target, found, err := records.Target(ctx, *link.Link.TargetUserID)
	if err != nil {
		return accounts.ResetState{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return accounts.ResetState{}, false, fmt.Errorf("finish reset capability snapshot: %w", err)
	}
	return accounts.ResetState{Link: link, Target: target}, found, nil
}

func (records linkRecords) UsernameExists(ctx context.Context, username string) (bool, error) {
	var exists bool
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE username=?)`,
		username,
	).Scan(
		&exists,
	)
	if err != nil {
		return false, fmt.Errorf("query invited username: %w", err)
	}
	return exists, nil
}
