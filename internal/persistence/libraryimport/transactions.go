package libraryimport

import (
	"context"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
)

var errLibraryImportDatabaseUnavailable = errors.New("library import database unavailable")

// Transactions owns the connection and transaction lifecycle for the legacy
// library-import facade. Callers receive only the shared executor contract so
// business orchestration cannot depend on a concrete database implementation.
type Transactions struct{ database dbapi.DB }

func NewTransactions(database dbapi.DB) *Transactions {
	return &Transactions{database: database}
}

func (repository *Transactions) Read(
	ctx context.Context,
	work func(dbapi.Executor) error,
) error {
	return repository.with(ctx, &dbapi.TxOptions{ReadOnly: true}, "read", work)
}

func (repository *Transactions) Write(
	ctx context.Context,
	work func(dbapi.Executor) error,
) error {
	return repository.with(ctx, nil, "write", work)
}

func (repository *Transactions) with(
	ctx context.Context,
	options *dbapi.TxOptions,
	mode string,
	work func(dbapi.Executor) error,
) error {
	if repository == nil || repository.database == nil {
		return fmt.Errorf("begin library import %s transaction: %w", mode, errLibraryImportDatabaseUnavailable)
	}
	transaction, err := repository.database.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin library import %s transaction: %w", mode, err)
	}
	defer dbapi.Rollback(transaction)
	if err := work(transaction); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit library import %s transaction: %w", mode, err)
	}
	return nil
}
