package diagnostics

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/diagnostics"
)

type Repository struct {
	database dbapi.DB
}

type records struct {
	executor dbapi.Executor
}

func New(database dbapi.DB) *Repository {
	return &Repository{database: database}
}

func (repository *Repository) WithRead(ctx context.Context, work func(application.ReadScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("diagnostics: begin read: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if err := work(records{executor: transaction}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("diagnostics: commit read: %w", err)
	}
	return nil
}
