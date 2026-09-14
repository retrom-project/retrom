package emulationstationimport

import (
	"context"

	payload "retrom/internal/model/payloadrelease"
)

type CompletionCounts struct {
	Terminal                                                  TerminalItemCounts
	ExpectedItems, Unfinished, RetryableFailed, MediaWarnings int64
}

type CompletionChange struct {
	Before      LeaseSnapshot
	Counts      CompletionCounts
	ImportState string
	Retryable   bool
	NowMS       int64
}

type CompletionReader interface {
	Current(context.Context, string) (LeaseSnapshot, bool, error)
	Counts(context.Context, string) (CompletionCounts, error)
}

type CompletionWriter interface {
	Complete(context.Context, CompletionChange) error
}

type CompletionScope struct {
	Payload payload.ReleaseScope
	Read    CompletionReader
	Write   CompletionWriter
}

type CompletionRepository interface {
	WithCompletion(context.Context, func(CompletionScope) error) error
}
