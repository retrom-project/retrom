package emulationstationimport

import (
	"context"

	library "retrom/internal/model/libraryimport"
	payload "retrom/internal/model/payloadrelease"
)

type RecoveryChange struct {
	Before                                               LeaseSnapshot
	JobState, ImportState, Phase, ItemState, Code, Event string
	NowMS, AvailableAtMS                                 int64
	ClearScan, TerminalItems, SchedulePayload            bool
}

type RecoveryReader interface {
	Current(context.Context, string) (LeaseSnapshot, bool, error)
	Reviews(context.Context, string, int) ([]ExecutionReview, error)
}

type RecoveryWriter interface {
	Apply(context.Context, RecoveryChange) error
	Fence(context.Context, LeaseSnapshot, int64) error
	CompleteReview(context.Context, ExecutionReviewCompletion) error
}

type RecoveryScope struct {
	Payload  payload.ReleaseScope
	Read     RecoveryReader
	Write    RecoveryWriter
	Metadata library.MetadataScope
}

type RecoveryRepository interface {
	Expired(context.Context, int64, int) ([]LeaseSnapshot, error)
	WithRecovery(context.Context, func(RecoveryScope) error) error
}
