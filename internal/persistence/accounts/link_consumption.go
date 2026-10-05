package accounts

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/accounts"
)

func (repository *LinkRepository) WithConsumptionWrite(
	ctx context.Context, work func(accounts.LinkConsumptionScope) error,
) error {
	err := dbapi.RetryTransaction(ctx, repository.writer, func(tx dbapi.Tx) error {
		records := linkRecords{accountOperations{tx}}
		return work(accounts.LinkConsumptionScope{Read: records, Write: records})
	})
	if err != nil {
		return fmt.Errorf("commit accounts transaction: %w", err)
	}
	return nil
}

func (repository *LinkRepository) ResetState(ctx context.Context, id string) (accounts.ResetState, bool, error) {
	tx, err := repository.reader.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return accounts.ResetState{}, false, fmt.Errorf("begin reset capability snapshot: %w", err)
	}
	defer dbapi.Rollback(tx)
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
	err := dbapi.QueryRowContext(
		ctx, records.executor,

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
