package pegasusimport

import "context"

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

type SettlementReviewBatchResult struct {
	Before ExecutionSnapshot
	More   bool
}

type WorkerSettlementRepository interface {
	CurrentSettlement(context.Context, string) (ExecutionSnapshot, error)
	CommitSettlementReviewBatch(ctx context.Context, id ExecutionIdentity, nowFunc func() int64, releaseYearMax int) (SettlementReviewBatchResult, error)
	CommitSettlement(ctx context.Context, change WorkerSettlementChange) error
}
