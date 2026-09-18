package payloadrelease

import (
	"context"
	"errors"
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

type GCScheduleBatch struct {
	Selected []GCBlob
	Queued   []GCQueue
}

type GCImmediateCommit struct {
	Selected []GCBlob
	Changes  []GCAdvance
	Audit    GCAudit
}

type GCCancellationBatch struct {
	Selected      []GCBlob
	Cancellations []GCCancellation
}

type GCSnapshotReader interface {
	LoadGCPage(context.Context, string, int) ([]GCBlob, error)
	LoadGCCandidates(context.Context) ([]GCBlob, error)
	LoadGCSelected(context.Context, []string) ([]GCBlob, error)
}

type GCCommitter interface {
	CommitGCSchedule(context.Context, GCScheduleBatch) error
	CommitImmediateGC(context.Context, GCImmediateCommit) error
	CommitGCCancellation(context.Context, GCCancellationBatch) error
}

type GCRepository interface {
	GCSnapshotReader
	GCCommitter
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
