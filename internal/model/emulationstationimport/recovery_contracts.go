package emulationstationimport

import "context"

type RecoveryChange struct {
	Before                                               LeaseSnapshot
	JobState, ImportState, Phase, ItemState, Code, Event string
	NowMS, AvailableAtMS                                 int64
	ClearScan, TerminalItems, SchedulePayload            bool
}

type RecoveryReviewBatchResult struct {
	Before LeaseSnapshot
	Found  bool
	More   bool
}

type RecoveryRepository interface {
	Expired(context.Context, int64, int) ([]LeaseSnapshot, error)
	CurrentRecovery(ctx context.Context, jobID string) (LeaseSnapshot, bool, error)
	CommitRecoveryReviewBatch(ctx context.Context, candidate LeaseSnapshot, nowMS int64, releaseYearMax int) (RecoveryReviewBatchResult, error)
	CommitRecovery(ctx context.Context, change RecoveryChange) error
}
