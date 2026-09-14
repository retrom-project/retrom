package diagnostics

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/diagnostics"
	"retrom/internal/repo/dbexec"
)

type Repository struct {
	database *sql.DB
}

type records struct {
	executor dbexec.Executor
}

func New(database *sql.DB) *Repository {
	return &Repository{database: database}
}

func (repository *Repository) WithRead(ctx context.Context, work func(application.ReadScope) error) error {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
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
