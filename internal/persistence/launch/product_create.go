package launch

import (
	"context"
	"fmt"

	variantrepository "retrom/internal/persistence/gamevariant"
	gamevariant "retrom/internal/service/gamevariant"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/dberrors"
	application "retrom/internal/service/launch"
)

type ProductCreation struct{ database dbapi.DB }

func NewProductCreation(database dbapi.DB) *ProductCreation {
	return &ProductCreation{database: database}
}

type productCreationRecords struct {
	executor    dbapi.Executor
	transaction dbapi.Tx
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
	tx, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ProductSnapshot{}, fmt.Errorf("begin product snapshot: %w", err)
	}
	defer dbapi.Rollback(tx)
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
	// Preparation and external effects stay outside this replayable database scope.
	create := func(tx dbapi.Tx) error {
		return work(productCreationRecords{executor: tx, transaction: tx})
	}
	err := dbapi.RetryTransaction(ctx, repository.database, create)
	if dberrors.Unique(err, "idempotency_records_pkey") {
		// The winner committed the complete launch and receipt; a fresh RR snapshot
		// returns that receipt instead of leaking the unique-key race to the caller.
		err = dbapi.RetryTransaction(ctx, repository.database, create)
	}
	if err != nil {
		return fmt.Errorf("commit launch transaction: %w", err)
	}
	return nil
}

func (records productCreationRecords) Validation() gamevariant.WriteScope {
	return variantrepository.NewWriteScope(records.executor)
}
