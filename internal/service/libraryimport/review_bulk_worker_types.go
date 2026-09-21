package libraryimport

import (
	"context"
	"errors"
)

// ErrReviewBulkWorkerNotRunnable means that a bulk approval no longer has the
// state or ownership required for the requested worker transition.
//
// The persistence adapter returns this error for compare-and-set misses. The
// application layer keeps the sentinel here so the legacy facade can preserve
// its existing error handling without depending on a database package.
var ErrReviewBulkWorkerNotRunnable = errors.New("review bulk approval is not runnable")

type ReviewBulkWork struct {
	BulkApprovalID string
	JobID          string
	WorkerID       string
	UserID         string
}

type ReviewBulkWorkItem struct {
	ImportItemID          string
	ValidationID          string
	SourceSnapshotID      string
	ExpectedReviewVersion int64
}

type ReviewBulkClaim struct {
	BulkApprovalID string
	WorkerID       string
	NowMS          int64
	DeadlineMS     int64
}

type ReviewBulkItemCompletion struct {
	Work          ReviewBulkWork
	Item          ReviewBulkWorkItem
	State         string
	OutcomeCode   string
	DetailsJSON   string
	NowMS         int64
	LeasedUntilMS int64
}

type ReviewBulkCancelTarget struct {
	State    string
	JobID    string
	JobState string
}

type ReviewBulkCancellationRequest struct {
	Target          ReviewBulkCancelTarget
	BulkApprovalID  string
	Reason          string
	ExpectedVersion int64
	NowMS           int64
}

type ReviewBulkRetryTarget struct {
	JobID       string
	PayloadJSON string
	ExecutionNo int64
}

type ReviewBulkRetryRequest struct {
	Target          ReviewBulkRetryTarget
	BulkApprovalID  string
	ExpectedVersion int64
	NowMS           int64
}

type ReviewBulkResumableJob struct {
	ID    string
	State string
}

// ReviewBulkWorkerClaimScope contains the claim operations bound to one
// caller-owned transaction.
type ReviewBulkWorkerClaimScope interface {
	Claim(context.Context, ReviewBulkClaim) (ReviewBulkWork, error)
	ClaimItem(context.Context, ReviewBulkWork, int64) (ReviewBulkWorkItem, error)
}

// ReviewBulkWorkerProgressScope contains item progress operations.
type ReviewBulkWorkerProgressScope interface {
	ProgressEvent(context.Context, ReviewBulkWork, int64) error
	CompleteItem(context.Context, ReviewBulkItemCompletion) error
	ItemStillFrozen(context.Context, ReviewBulkWorkItem) (bool, error)
}

// ReviewBulkWorkerTerminalScope contains terminal worker operations.
type ReviewBulkWorkerTerminalScope interface {
	Finish(context.Context, ReviewBulkWork, int64) error
	FinalizeCancellation(context.Context, string, int64) error
	CancellationRequested(context.Context, string) (bool, error)
	Fail(context.Context, ReviewBulkWork, int64) error
	FailQueued(context.Context, string, int64) error
}

// ReviewBulkWorkerOutcomeScope contains item and terminal outcome operations.
type ReviewBulkWorkerOutcomeScope interface {
	ReviewBulkWorkerProgressScope
	ReviewBulkWorkerTerminalScope
}

// ReviewBulkWorkerResumeScope contains restart coordination operations.
type ReviewBulkWorkerResumeScope interface {
	Resume(context.Context, int64) ([]ReviewBulkResumableJob, error)
	ListResumable(context.Context) ([]ReviewBulkResumableJob, error)
	Active(context.Context) (bool, error)
}

// ReviewBulkWorkerControlScope contains cancellation and retry operations.
type ReviewBulkWorkerControlScope interface {
	LoadCancelTarget(context.Context, string, int64) (ReviewBulkCancelTarget, error)
	RequestCancellation(context.Context, ReviewBulkCancellationRequest) error
	LoadRetryTarget(context.Context, string, int64) (ReviewBulkRetryTarget, error)
	QueueRetry(context.Context, ReviewBulkRetryRequest) error
}

// ReviewBulkWorkerRecoveryScope contains restart and cancellation/retry
// coordination operations.
type ReviewBulkWorkerRecoveryScope interface {
	ReviewBulkWorkerResumeScope
	ReviewBulkWorkerControlScope
}

// ReviewBulkWorkerScope is bound to one caller-owned transaction. Each method
// performs only the SQL needed for its operation; transaction boundaries stay
// with ReviewBulkWorkerRepository.WithWorker.
type ReviewBulkWorkerScope interface {
	ReviewBulkWorkerClaimScope
	ReviewBulkWorkerOutcomeScope
	ReviewBulkWorkerRecoveryScope
}

type ReviewBulkWorkerRepository interface {
	WithWorker(context.Context, func(ReviewBulkWorkerScope) error) error
}
