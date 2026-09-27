package libraryimport

import (
	"context"
	"errors"
)

var (
	ErrReviewBulkActive      = errors.New("REVIEW_BULK_APPROVAL_ACTIVE")
	ErrReviewBulkTooLarge    = errors.New("REVIEW_BULK_SCOPE_TOO_LARGE")
	ErrReviewBulkEmpty       = errors.New("REVIEW_BULK_SCOPE_EMPTY")
	ErrReviewBulkConflict    = errors.New("REVIEW_BULK_VERSION_CONFLICT")
	ErrReviewBulkNotRunnable = errors.New("review bulk worker no longer owns task")
)

type ReviewBulkScanItem struct {
	ID                                                             string
	ReviewVersion, ReviewUpdatedAtMS, ItemUpdatedAtMS, CreatedAtMS int64
}

type ReviewBulkWorker interface {
	Claim(context.Context, string, string, int64) (string, string, error)
	Next(context.Context, string, string) (ReviewBulkScanItem, bool, error)
	Skip(context.Context, string, string, string, string, string, int64) error
	Finish(context.Context, string, string, string, int64) error
	Fail(context.Context, string, string, string, int64) error
}

type ReviewBulkRecovery interface {
	Resume(context.Context, int64) ([]string, error)
}

type ReviewBulkWrites interface {
	CreateGlobal(context.Context, string, string, string, int64) (ReviewBulkSummary, error)
}

type ReviewBulkCandidates interface {
	CandidateByID(context.Context, string) (ReviewBulkCandidate, bool, error)
}

type ReviewBulkStep struct {
	Worker     ReviewBulkWorker
	Recovery   ReviewBulkRecovery
	Writes     ReviewBulkWrites
	Candidates ReviewBulkCandidates
	Approval   ReviewApprovalScope
}

type ReviewBulkStore interface {
	WithStep(context.Context, func(ReviewBulkStep) error) error
	Summary(context.Context, string) (ReviewBulkSummary, error)
	ActiveSummary(context.Context) (ReviewBulkSummary, bool, error)
}
