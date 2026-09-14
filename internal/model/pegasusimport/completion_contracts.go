package pegasusimport

import (
	"context"

	payload "retrom/internal/model/payloadrelease"
)

type CompletionCounts struct {
	Blocked, Failed, ReviewPending, Published, ReviewDiscarded, Existing, Cancelled, Unfinished int64
}

type CompletionChange struct {
	Before      ExecutionSnapshot
	Counts      CompletionCounts
	ImportState string
	Retryable   bool
	NowMS       int64
}

type CompletionRecords interface {
	Payload() payload.ReleaseScope
	Current(context.Context, string) (ExecutionSnapshot, error)
	Counts(context.Context, string) (CompletionCounts, error)
	Complete(context.Context, CompletionChange) error
}

type CompletionRepository interface {
	WithCompletion(context.Context, func(CompletionRecords) error) error
}
