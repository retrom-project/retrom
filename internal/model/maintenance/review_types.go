package maintenance

import (
	"context"

	"retrom/internal/model/libraryimport"
)

type RestoredReview struct {
	Kind, ItemID, ImportID, JobID                        string
	State, ImportState, JobState                         string
	Version, ImportVersion, JobVersion, ExecutionNo      int64
	LibraryJobID, LibraryItemID                          string
	ReservedJobID, ReservedItemID, UploadID, OwnerUpload string
	OwnerKind, OwnerItemID                               string
	OrdinaryItemCount, OrdinaryVersion                   int64
	MetadataJSON, WarningsJSON                           string
	RootID, RootDigest, RelativePath, CreatorID          string
	ReleaseYearMax                                       int
	Retryable                                            bool
}

type RestoredReviewQuery struct {
	Kind, AfterID string
	Limit         int
}

type RestoredReviewChange struct {
	Before       RestoredReview
	Preparation  []string
	WarningsJSON string
	NowMS        int64
}

type RestoredReviewRecords interface {
	Pending(context.Context, RestoredReviewQuery) ([]RestoredReview, error)
	Complete(context.Context, RestoredReviewChange) error
}

type RestoredReviewScope struct {
	Records  RestoredReviewRecords
	Metadata libraryimport.MetadataScope
}
