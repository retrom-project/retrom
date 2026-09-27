package cleanupjobs

import (
	"context"
	"errors"
	"time"
)

var (
	ErrDeletionSnapshotChanged            = errors.New("FILE_DELETE_SNAPSHOT_CHANGED")
	ErrImmediateDeletionAuditActorMissing = errors.New("IMMEDIATE_FILE_DELETE_AUDIT_ACTOR_MISSING")
	ErrImmediateDeletionBytesOverflow     = errors.New("IMMEDIATE_FILE_DELETE_BYTES_OVERFLOW")
	ErrImmediateDeletionJobStateInvalid   = errors.New("IMMEDIATE_FILE_DELETE_JOB_STATE_INVALID")
)

type DeletionFile struct {
	ID, Digest             string
	SizeBytes              int64
	Retained, HasCandidate bool
	Candidate              DeletionCandidate
}

type DeletionCandidate struct {
	Work                            Work
	RetiredMS, ScheduledMS, Attempt int64
}

type DeletionReader interface {
	Page(context.Context, string, int) ([]DeletionFile, error)
	Selected(context.Context, []string) ([]DeletionFile, error)
	Candidates(context.Context) ([]DeletionFile, error)
}

type DeletionWriter interface {
	Fence(context.Context, []DeletionFile) error
	Queue(context.Context, DeletionQueue) error
	Advance(context.Context, DeletionAdvance) error
	Audit(context.Context, DeletionAudit) error
}

type DeletionScope struct {
	Read  DeletionReader
	Write DeletionWriter
}

type DeletionRepository interface {
	WithDeletion(context.Context, func(DeletionScope) error) error
}

type DeletionQueue struct {
	Before      DeletionFile
	Job         ScheduledJob
	AvailableMS int64
	EventJSON   string
}

type DeletionAdvance struct {
	Before             DeletionFile
	NowMS, ScheduledMS int64
	Retry              *DeletionRetry
}

type DeletionRetry struct {
	ExecutionNo                                    int64
	InputJSON, InputDigest, EventJSON, PayloadJSON string
}

type DeletionAudit struct {
	ID, ActorUserID, AfterJSON string
	NowMS                      int64
}

type ImmediateDeletionResult struct {
	FileCount, Bytes, AcceptedAtMS int64
}

type DeletionOptions struct {
	Now   func() time.Time
	NewID func() (string, error)
	Wake  func()
}

type DeletionScheduler struct {
	repository DeletionRepository
	now        func() time.Time
	newID      func() (string, error)
	wake       func()
}

func NewDeletionScheduler(repository DeletionRepository, options DeletionOptions) (*DeletionScheduler, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &DeletionScheduler{
		repository: repository, now: options.Now, newID: NewScheduler(options.NewID).identity,
		wake: options.Wake,
	}, nil
}
