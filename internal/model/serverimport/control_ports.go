package serverimport

import (
	"context"
	"errors"
	"math"
)

var (
	ErrNotCancellable = errors.New("SERVER_IMPORT_NOT_CANCELLABLE")
	ErrNotRetryable   = errors.New("SERVER_IMPORT_NOT_RETRYABLE")
)

type ControlSnapshot struct {
	OtherActive                         bool
	Summary                             Summary
	RootDigest, CatalogDigest, JobState string
	JobVersion, Execution, PendingItems int64
}
type ControlEvidence struct {
	ActorID, AuditID string
	Event            []byte
	Now              int64
}
type Cancellation struct {
	Before         ControlSnapshot
	Pending        bool
	State, Reason  string
	CompletedAt    *int64
	CancelledItems int64
	Evidence       ControlEvidence
}
type ManualRetry struct {
	Before         ControlSnapshot
	Execution      int64
	Input, Payload []byte
	InputDigest    string
	Evidence       ControlEvidence
}

type CancelCommand struct {
	ID      string
	Version int64
	Reason  string
	ActorID string
	Now     int64
}

type CancelResult struct {
	Summary Summary
	Pending bool
}

type RetryCommand struct {
	ID         string
	Version    int64
	ActorID    string
	Now        int64
	ValidRoots map[string]string
}

type ControlRepository interface {
	CommitCancel(context.Context, CancelCommand) (CancelResult, error)
	CommitRetry(context.Context, RetryCommand) (Summary, error)
}

// CancelValid checks whether the snapshot allows cancellation at the given
// version.
func CancelValid(before ControlSnapshot, version int64) bool {
	return before.Summary.Version == version &&
		version != math.MaxInt64 &&
		before.JobState == before.Summary.State &&
		(before.Summary.State == "QUEUED" ||
			before.Summary.State == "RUNNING")
}

// Retryable checks whether the snapshot allows a manual retry at the given
// version, using validRoots to confirm the root configuration is still current.
func Retryable(
	before ControlSnapshot,
	version int64,
	validRoots map[string]string,
) bool {
	summary := before.Summary
	if before.OtherActive ||
		summary.Version != version ||
		version == math.MaxInt64 ||
		before.Execution < 1 ||
		before.Execution == math.MaxInt64 ||
		summary.State != "FAILED" ||
		before.JobState != "FAILED" ||
		summary.LastErrorCode == nil {
		return false
	}
	if *summary.LastErrorCode != "SERVER_IMPORT_ROOT_UNAVAILABLE" &&
		*summary.LastErrorCode != "INTERNAL_ERROR" {
		return false
	}
	digest, found := validRoots[summary.Root.ID]
	return found && digest == before.RootDigest
}
