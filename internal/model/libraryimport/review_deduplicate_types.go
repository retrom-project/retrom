package libraryimport

import "context"

type DeduplicateCommand struct {
	Request  ReviewDeduplicateRequest
	Discards []DeduplicateDiscardSlot
	NowMS    int64
	Actor    ReviewActor
}

type DeduplicateDiscardSlot struct {
	EventID string
}

type ReviewDeduplicateRepository interface {
	CommitDeduplicate(context.Context, DeduplicateCommand) (ReviewDeduplicateResult, error)
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
