package pegasusimport

import (
	"context"

	library "retrom/internal/model/libraryimport"
)

type ReviewHandoffRequest struct {
	ItemID, ImportID, JobID, LibraryJobID, LibraryItemID, WorkerID string
	ExecutionNo, Attempt                                           int64
}

type ReviewHandoffSnapshot struct {
	Identity                             ReviewHandoffRequest
	State, ImportState, JobState         string
	Version, ImportVersion, LeaseUntilMS int64
	DeadlineMS                           int64
	Metadata                             library.ServerMetadata
	Warnings                             []map[string]any
}

type ReviewHandoffChange struct {
	Before   ReviewHandoffSnapshot
	Warnings []map[string]any
	NowMS    int64
}

type ReviewHandoffRecords interface {
	CurrentReviewHandoff(context.Context, string) (ReviewHandoffSnapshot, error)
	FinishReviewHandoff(context.Context, ReviewHandoffChange) error
}

type ReviewHandoffScope struct {
	Records  ReviewHandoffRecords
	Metadata library.MetadataScope
}

type ReviewHandoffRepository interface {
	CommitReviewHandoff(ctx context.Context, request ReviewHandoffRequest, nowMS int64,
		auditID, actorKind string, actorUserID, actorLabel *string, maximumYear int) error
}

type ReviewMetadataSeeder interface {
	SeedInScope(
		context.Context, library.MetadataScope, string, library.ServerMetadata, int,
	) (int64, []library.ServerMetadataWarning, error)
}
