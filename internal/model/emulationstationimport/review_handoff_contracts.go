package emulationstationimport

import (
	"context"

	library "retrom/internal/model/libraryimport"
)

type ReviewHandoffRequest struct {
	Execution                           Execution
	ItemID, LibraryJobID, LibraryItemID string
}

type ReviewHandoffReader interface {
	Current(context.Context, string) (LeaseSnapshot, bool, error)
	Review(context.Context, string, string) (ExecutionReview, bool, error)
}

type ReviewHandoffScope struct {
	Read     ReviewHandoffReader
	Write    ExecutionReviewWriter
	Metadata library.MetadataScope
}

type ReviewHandoffRepository interface {
	WithReviewHandoff(context.Context, func(ReviewHandoffScope) error) error
}

type ReviewMetadataSeeder interface {
	SeedInScope(
		context.Context, library.MetadataScope, string, library.ServerMetadata, int,
	) (int64, []library.ServerMetadataWarning, error)
}
