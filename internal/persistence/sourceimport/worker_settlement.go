package sourceimport

import (
	"context"
	"fmt"

	payload "retrom/internal/persistence/sourceimport/sourcerelease"

	dbapi "retrom/internal/database"
	library "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/sourceimport"
)

type WorkerSettlement struct{ database dbapi.DB }

func NewWorkerSettlement(database dbapi.DB) *WorkerSettlement {
	return &WorkerSettlement{database: database}
}

func (repository *WorkerSettlement) WithSettlement(
	ctx context.Context,
	work func(application.WorkerSettlementScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Source settlement: %w", err)
	}
	defer dbapi.Rollback(tx)
	records := workerSettlementRecords{tx: tx}
	scope := application.WorkerSettlementScope{
		Payload: payload.BindReleases(tx), Read: records, Write: records,
		Metadata: library.BindMetadata(tx),
	}
	if err := work(scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Source settlement: %w", err)
	}
	return nil
}

type workerSettlementRecords struct{ tx dbapi.Tx }

func (records workerSettlementRecords) Current(ctx context.Context, id string) (application.ExecutionSnapshot, error) {
	return leaseRecords(records).Current(ctx, id)
}

func (records workerSettlementRecords) Reviews(
	ctx context.Context,
	id string,
	limit int,
) ([]application.ReviewHandoffSnapshot, error) {
	return recoveryRecords(records).Reviews(ctx, id, limit)
}
