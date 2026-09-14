package emulationstationimport

import (
	"context"

	library "retrom/internal/model/libraryimport"
	payload "retrom/internal/model/payloadrelease"
)

type ExecutionFailure struct {
	Code      string
	Retryable bool
}

type ExecutionFinish struct {
	Before                                        LeaseSnapshot
	NowMS, AvailableAtMS                          int64
	JobState, ImportState, ItemState, Phase, Code string
	Retryable                                     bool
	ClearScan, TerminalItems, SchedulePayload     bool
	RetryFailedItems                              bool
}

type ExecutionReader interface {
	Current(context.Context, string) (LeaseSnapshot, bool, error)
	TerminalCount(context.Context, string) (int64, error)
	Reviews(context.Context, string, int) ([]ExecutionReview, error)
}

type ExecutionWriter interface {
	Finish(context.Context, ExecutionFinish) error
	Fence(context.Context, LeaseSnapshot, int64) error
	CompleteReview(context.Context, ExecutionReviewCompletion) error
}

type ExecutionScope struct {
	Payload  payload.ReleaseScope
	Read     ExecutionReader
	Write    ExecutionWriter
	Metadata library.MetadataScope
}

type ExecutionRepository interface {
	WithExecution(context.Context, func(ExecutionScope) error) error
}
