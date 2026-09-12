package launch

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/launch"
)

type ProductCreation struct{ database *sql.DB }

func NewProductCreation(database *sql.DB) *ProductCreation {
	return &ProductCreation{database: database}
}

type productCreationRecords struct {
	executor    dbexec.Executor
	transaction *sql.Tx
}

func (repository *ProductCreation) Replay(
	ctx context.Context,
	command application.ProductCreateCommand,
) (application.ProductReceipt, bool, error) {
	return (productCreationRecords{executor: repository.database}).Replay(ctx, command)
}

func (repository *ProductCreation) Snapshot(
	ctx context.Context,
	command application.ProductCreateCommand,
) (application.ProductSnapshot, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ProductSnapshot{}, fmt.Errorf("begin product snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	result, err := (productCreationRecords{executor: tx}).Snapshot(ctx, command)
	if err != nil {
		return application.ProductSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.ProductSnapshot{}, fmt.Errorf("commit product snapshot: %w", err)
	}
	return result, nil
}

func (repository *ProductCreation) WithCreation(
	ctx context.Context,
	work func(application.ProductCreationScope) error,
) error {
	// The shared store supplies one writer connection. Competing receipts are
	// serialized before their final lookup and all ownership/creation writes.
	// A different database adapter must retain that transaction guarantee.
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin product creation: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(productCreationRecords{executor: tx, transaction: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit product creation: %w", err)
	}
	return nil
}

func (records productCreationRecords) Validation() application.ProductValidationScope {
	return productValidationRecords{ValidationJobs: NewValidationJobs(records.executor), executor: records.executor}
}
