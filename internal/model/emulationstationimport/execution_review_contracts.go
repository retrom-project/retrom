package emulationstationimport

import (
	"context"

	library "retrom/internal/model/libraryimport"
)

type ExecutionReview struct {
	ItemID, State, LibraryJobID, LibraryItemID, ReservedJobID, ReservedItemID string
	MetadataJSON, WarningsJSON                                                string
	Version                                                                   int64
	Retryable                                                                 bool
}

type ExecutionReviewCompletion struct {
	Before       LeaseSnapshot
	Review       ExecutionReview
	WarningsJSON string
	Preparation  []string
	NowMS        int64
}

type ExecutionSnapshotReader interface {
	Current(context.Context, string) (LeaseSnapshot, bool, error)
}

type ExecutionReviewReader interface {
	Current(context.Context, string) (LeaseSnapshot, bool, error)
	Reviews(context.Context, string, int) ([]ExecutionReview, error)
}

type ExecutionReviewWriter interface {
	Fence(context.Context, LeaseSnapshot, int64) error
	CompleteReview(context.Context, ExecutionReviewCompletion) error
}

type ExecutionReviewScope struct {
	Read     ExecutionReviewReader
	Write    ExecutionReviewWriter
	Metadata library.MetadataScope
}
