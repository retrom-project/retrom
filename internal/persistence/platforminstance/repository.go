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
	transaction, err := repository.database.BeginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("platforminstance: begin immediate: %w", err)
	}
	defer dbapi.Rollback(transaction)
	bound := records{transaction}
	if err := work(platforminstance.WriteScope{Reader: bound, Directories: bound, Idempotency: bound}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("platforminstance: commit: %w", err)
	}
	return nil
}
