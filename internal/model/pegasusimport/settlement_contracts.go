package pegasusimport

import (
	"context"

	library "retrom/internal/model/libraryimport"
	payload "retrom/internal/model/payloadrelease"
)

type ExecutionFailure struct {
	Code      string
	Retryable bool
}

type WorkerSettlementChange struct {
	Before  ExecutionSnapshot
	State   string
	Failure ExecutionFailure
	NowMS   int64
}

type WorkerSettlementReader interface {
	Current(context.Context, string) (ExecutionSnapshot, error)
	Reviews(context.Context, string, int) ([]ReviewHandoffSnapshot, error)
}

type WorkerSettlementWriter interface {
	CompleteReview(context.Context, RecoveryReviewChange) error
	Close(context.Context, WorkerSettlementChange) error
}

type WorkerSettlementScope struct {
	Payload  payload.ReleaseScope
	Read     WorkerSettlementReader
	Write    WorkerSettlementWriter
	Metadata library.MetadataScope
}

type SettlementReviewBatchResult struct {
	Before ExecutionSnapshot
	More   bool
}

type WorkerSettlementRepository interface {
	WithSettlement(context.Context, func(WorkerSettlementScope) error) error
	CurrentSettlement(context.Context, string) (ExecutionSnapshot, error)
	CommitSettlementReviewBatch(ctx context.Context, id ExecutionIdentity, nowFunc func() int64, releaseYearMax int) (SettlementReviewBatchResult, error)
	CommitSettlement(ctx context.Context, change WorkerSettlementChange) error
}
