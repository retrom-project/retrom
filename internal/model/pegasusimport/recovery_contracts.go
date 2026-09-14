package pegasusimport

import (
	"context"

	library "retrom/internal/model/libraryimport"
	payload "retrom/internal/model/payloadrelease"
)

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

type RecoveryRecords interface {
	Current(context.Context, string) (RecoverySnapshot, error)
	Reviews(context.Context, string, int) ([]ReviewHandoffSnapshot, error)
	CompleteReview(context.Context, RecoveryReviewChange) error
	Apply(context.Context, RecoveryChange) error
}

type RecoveryScope struct {
	Payload  payload.ReleaseScope
	Records  RecoveryRecords
	Metadata library.MetadataScope
}

type RecoveryRepository interface {
	ExpiredExecutions(context.Context, int64, int) ([]RecoverySnapshot, error)
	WithRecovery(context.Context, func(RecoveryScope) error) error
}
