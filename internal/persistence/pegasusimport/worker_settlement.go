package pegasusimport

import (
	"context"
	"database/sql"
	"fmt"
	payload "retrom/internal/persistence/payloadrelease"

	"retrom/internal/dbexec"
	library "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/pegasusimport"
)

type WorkerSettlement struct{ database *sql.DB }

func NewWorkerSettlement(database *sql.DB) *WorkerSettlement {
	return &WorkerSettlement{database: database}
}

func (repository *WorkerSettlement) WithSettlement(
	ctx context.Context,
	work func(application.WorkerSettlementScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus settlement: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := workerSettlementRecords{tx: tx}
	scope := application.WorkerSettlementScope{Payload: payload.BindReleases(tx), Read: records, Write: records, Metadata: library.BindMetadata(tx)}
	if err := work(scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus settlement: %w", err)
	}
	return nil
}

type workerSettlementRecords struct{ tx *sql.Tx }

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
