package payloadrelease

import (
	"context"
	"errors"
	"time"
)

var (
	ErrGCRetentionInvalid           = errors.New("GC_RETENTION_INVALID")
	ErrGCSnapshotChanged            = errors.New("GC_SNAPSHOT_CHANGED")
	ErrImmediateGCAuditActorMissing = errors.New("IMMEDIATE_GC_AUDIT_ACTOR_MISSING")
	ErrImmediateGCBytesOverflow     = errors.New("IMMEDIATE_GC_BYTES_OVERFLOW")
	ErrImmediateGCJobStateInvalid   = errors.New("IMMEDIATE_GC_JOB_STATE_INVALID")
)

type GCBlob struct {
	ID, Digest              string
	SizeBytes               int64
	Protected, HasCandidate bool
	Candidate               GCCandidate
}

type GCCandidate struct {
	Work                                      Work
	FirstUnreferencedMS, ScheduledMS, Attempt int64
}

type GCReader interface {
	Page(context.Context, string, int) ([]GCBlob, error)
	Selected(context.Context, []string) ([]GCBlob, error)
	Candidates(context.Context) ([]GCBlob, error)
}

type GCWriter interface {
	Fence(context.Context, []GCBlob) error
	Queue(context.Context, GCQueue) error
	Advance(context.Context, GCAdvance) error
	Cancel(context.Context, GCCancellation) error
	Audit(context.Context, GCAudit) error
}

type GCScope struct {
	Read  GCReader
	Write GCWriter
}

type GCRepository interface {
	WithGC(context.Context, func(GCScope) error) error
}

type GCQueue struct {
	Before      GCBlob
	Job         ScheduledJob
	AvailableMS int64
	EventJSON   string
}

type GCAdvance struct {
	Before             GCBlob
	NowMS, ScheduledMS int64
	Retry              *GCRetry
}

type GCRetry struct {
	ExecutionNo                                    int64
	InputJSON, InputDigest, EventJSON, PayloadJSON string
}

type GCCancellation struct {
	Before    GCBlob
	NowMS     int64
	Complete  bool
	EventJSON string
}

type GCAudit struct {
	ID, ActorUserID, AfterJSON string
	NowMS                      int64
}

type ImmediateGCResult struct {
	BlobCount, Bytes, AcceptedAtMS int64
}

type GCOptions struct {
	Now       func() time.Time
	Retention time.Duration
	NewID     func() (string, error)
	Wake      func()
}

type GCScheduler struct {
	repository GCRepository
	now        func() time.Time
	newID      func() (string, error)
	retention  time.Duration
	wake       func()
}

func NewGCScheduler(repository GCRepository, options GCOptions) (*GCScheduler, error) {
	if options.Retention < 24*time.Hour || options.Retention > 30*24*time.Hour {
		return nil, ErrGCRetentionInvalid
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &GCScheduler{
		repository: repository, now: options.Now, newID: NewScheduler(options.NewID).identity,
		retention: options.Retention, wake: options.Wake,
	}, nil
}
