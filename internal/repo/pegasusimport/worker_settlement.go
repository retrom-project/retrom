package pegasusimport

import (
	"context"
	"database/sql"

	application "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/dbexec"
)

type WorkerSettlement struct{ database *sql.DB }

func NewWorkerSettlement(database *sql.DB) *WorkerSettlement {
	return &WorkerSettlement{database: database}
}

type workerSettlementRecords struct{ tx dbexec.Executor }

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
