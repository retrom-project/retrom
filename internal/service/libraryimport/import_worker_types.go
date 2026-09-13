package libraryimport

import (
	"context"
	"time"

	"retrom/internal/service/importprogress"
	"retrom/internal/service/payloadrelease"
)

const (
	ImportExecutionBudget    = 6 * time.Hour
	ImportExecutionLease     = time.Minute
	ImportExecutionHeartbeat = 15 * time.Second
)

type ImportExecutionRepository interface {
	WithExecution(context.Context, func(ImportExecutionScope) error) error
	Queued(context.Context, int64) ([]string, error)
	Recoverable(context.Context, int64) ([]string, error)
}
type ImportExecutionScope struct {
	Records ImportExecutionRecords
	Facts   ImportFactsReader
	Payload payloadrelease.SchedulingScope
}
type ImportExecutionRecords interface {
	Current(context.Context, string) (ImportWorkerSnapshot, bool, error)
	Transition(context.Context, ImportWorkerTransition) error
}

type ImportWorkerSnapshot struct {
	StartedAtMS, DeadlineAtMS                      *int64
	Creation                                       CreationQueuedSnapshot
	HasRequest, Cancellable                        bool
	AvailableAtMS                                  int64
	LeaseUntilMS, HeartbeatAtMS, FinishedAtMS      *int64
	ErrorCode                                      *string
	Retryable                                      *bool
	CancelRequestedAtMS                            *int64
	CancelReason                                   *string
	ParentCompletedAtMS, ParentCancelRequestedAtMS *int64
	ParentCancelReason, ParentErrorCode            *string
	Counts                                         importprogress.Counts
	ItemCount, ResolvedFiles                       int64
}
type ImportWorkerProjection struct {
	StartedAtMS, DeadlineAtMS                 *int64
	Execution                                 QueuedImportExecution
	State                                     string
	AvailableAtMS                             int64
	LeaseUntilMS, HeartbeatAtMS, FinishedAtMS *int64
	ErrorCode                                 *string
	Retryable                                 *bool
	CancelRequestedAtMS                       *int64
	CancelReason                              *string
}
type ImportWorkerParent struct {
	State                              string
	CompletedAtMS, CancelRequestedAtMS *int64
	CancelReason, ErrorCode            *string
}
type ImportWorkerTransition struct {
	Before                       ImportWorkerSnapshot
	Job                          ImportWorkerProjection
	Parent                       *ImportWorkerParent
	Event                        *CreationEvent
	AtMS                         int64
	RequireLiveLease, ParentOnly bool
}
type ImportWork struct {
	Execution QueuedImportExecution
	Request   ImportRequest
}
type ImportWorkerSettings struct {
	Now    func() time.Time
	Report func(error)
}
type ImportJobCancellation struct {
	JobID, ImportID string
	ExpectedVersion int64
	Reason          string
}

type ImportBatchCancellationRequest struct {
	ImportID        string
	ExpectedVersion int64
	Reason          string
	PreserveReviews bool
}

type ImportBatchCancellationResult struct {
	ImportID   string
	GroupJobID string
	State      string
	Version    int64
	Pending    bool
}

type ImportBatchCancellationRepository interface {
	Cancel(context.Context, ImportBatchCancellationRequest, int64) (ImportBatchCancellationResult, error)
}
type ImportCancellationResult struct {
	JobID, State         string
	ExecutionNo, Version int64
	Pending              bool
}
type ImportWorkerPreparation interface {
	Prepare(context.Context, ImportRequest) (PreparedImport, error)
}
type ImportWorkerCreations interface {
	CommitPrepared(context.Context, PreparedImport, ImportCreationOptions) (ImportCreationResult, error)
}

type ImportExecutionQueue interface {
	Queued(context.Context) ([]string, error)
	Claim(context.Context, string) (ImportWork, bool, error)
}
type ImportExecutionControl interface {
	Renew(context.Context, QueuedImportExecution) (bool, error)
	Progress(context.Context, QueuedImportExecution, int) error
	Fail(context.Context, QueuedImportExecution, error) error
}
type (
	ImportExecutionRecovery  interface{ Recover(context.Context) error }
	ImportWorkerDependencies struct {
		Queue       ImportExecutionQueue
		Control     ImportExecutionControl
		Recovery    ImportExecutionRecovery
		Preparation ImportWorkerPreparation
		Creations   ImportWorkerCreations
	}
)
