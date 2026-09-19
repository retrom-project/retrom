package pegasusimport

import "context"

type RecoverySnapshot = ExecutionSnapshot

type RecoveryChange struct {
	Before                                                  RecoverySnapshot
	JobState, ImportState, ItemState, Code, ItemCode, Event string
	NowMS                                                   int64
}

type RecoveryReviewChange struct {
	Execution RecoverySnapshot
	Handoff   ReviewHandoffChange
}

type RecoveryReviewBatchResult struct {
	Before RecoverySnapshot
	More   bool
}

type RecoveryRepository interface {
	ExpiredExecutions(context.Context, int64, int) ([]RecoverySnapshot, error)
	CurrentRecovery(ctx context.Context, jobID string) (RecoverySnapshot, error)
	CommitRecoveryReviewBatch(ctx context.Context, id ExecutionIdentity, nowMS int64, releaseYearMax int) (RecoveryReviewBatchResult, error)
	CommitRecovery(ctx context.Context, change RecoveryChange) error
}
