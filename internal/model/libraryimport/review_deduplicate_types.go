package libraryimport

import "context"

type ReviewDeduplicateRepository interface {
	WithDeduplicate(context.Context, func(ReviewDeduplicateScope) error) error
}

type ReviewDeduplicateScope struct {
	Reader     ReviewDeduplicateReader
	Duplicates ContentDuplicateReader
	Discard    ReviewDiscardScope
}

type ReviewDeduplicateReader interface {
	LatestReviewItemID(context.Context) (*string, error)
	Candidates(context.Context, ReviewBulkCandidateQuery) ([]ReviewBulkCandidate, error)
}

type ReviewDeduplicateRequest struct {
	Scope         ReviewBulkScope
	AfterItemID   string
	ThroughItemID string
}

type ReviewDeduplicateResult struct {
	ScannedCount          int
	DiscardedCount        int
	AttachmentActiveCount int
	NextAfterItemID       *string
	ThroughItemID         *string
}
