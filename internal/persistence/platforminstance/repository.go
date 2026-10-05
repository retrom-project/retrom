package platforminstance

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/platforminstance"
)

type (
	Repository struct{ database dbapi.DB }
	records    struct{ database dbapi.Executor }
)

func New(database dbapi.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) WithRead(ctx context.Context, work func(platforminstance.Reader) error) error {
	transaction, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("platforminstance: begin read: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if err := work(records{transaction}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("platforminstance: commit read: %w", err)
	}
	return nil
}

func (repository *Repository) WithWrite(ctx context.Context, work func(platforminstance.WriteScope) error) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		bound := records{tx}
		return work(platforminstance.WriteScope{Reader: bound, Directories: bound, Idempotency: bound})
	})
	if err != nil {
		return fmt.Errorf("commit platforminstance transaction: %w", err)
	}
	return nil
}
