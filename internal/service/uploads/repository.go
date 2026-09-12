package uploads

import (
	"context"
	"io"

	"retrom/internal/blobstore"
)

type BlobWriter interface {
	Put(io.Reader) (blobstore.Metadata, error)
}
type Repository interface {
	WithWrite(context.Context, func(WriteScope) error) error
	Snapshot(context.Context, string) (Session, error)
	Target(context.Context, FileKey) (PartTarget, error)
	Parts(context.Context, string) ([]Part, error)
	Candidates(context.Context, string) ([]Candidate, error)
}
type WriteScope struct {
	Sessions SessionRecords
	Files    FileRecords
	Parts    PartRecords
	Jobs     JobRecords
	Blobs    BlobRecords
}
type SessionRecords interface {
	Create(context.Context, Registration) error
	Current(context.Context, string) (SessionState, error)
	BeginFinalization(context.Context, Finalization) error
	Advance(context.Context, SessionProgress) error
	Finish(context.Context, SessionFinish) error
}
type FileRecords interface {
	Target(context.Context, FileKey) (PartTarget, error)
	AddReceived(context.Context, FileProgress) error
	MarkFinalizing(context.Context, string, int64) error
	Publish(context.Context, FilePublication) error
	FailPending(context.Context, PendingFailure) error
}
type PartRecords interface {
	Put(context.Context, PartRecord) (bool, error)
	Get(context.Context, string, int) (PartRecord, bool, error)
	DeleteForFile(context.Context, string) error
}
type JobRecords interface {
	Create(context.Context, JobCreation) error
	Get(context.Context, string) (Job, error)
	Claim(context.Context, JobClaim) (bool, error)
	RequestCancel(context.Context, JobCancellation) error
	Finish(context.Context, JobFinish) error
}
type BlobRecords interface {
	Ensure(context.Context, blobstore.Metadata, int64) (string, error)
}
type (
	FileKey    struct{ UploadID, FileID string }
	PartTarget struct {
		FileKey
		DeclaredSize, SessionVersion, ExpiresAtMS int64
		FileState, SessionState                   string
		LastErrorCode                             *string
	}
)

type SessionState struct {
	ID, State               string
	Version, FinalizationNo int64
	FinalizeJobID           *string
	Consumed                bool
}
type Registration struct {
	Session        Session
	ManifestDigest string
	AtMS           int64
}
type Part struct {
	Offset, Size int64
	Path, SHA256 string
	Number       int
}
type PartRecord struct {
	FileID string
	Part   Part
	AtMS   int64
}
type Candidate struct {
	ID   string
	Size int64
}
type FileProgress struct {
	FileID      string
	Bytes, AtMS int64
}
type SessionProgress struct {
	ID, State             string
	ExpectedVersion, AtMS int64
}
type Finalization struct {
	Run                   Run
	ExpectedVersion, AtMS int64
}
type Run struct {
	UploadID, JobID             string
	FinalizationNo, ExecutionNo int64
}
type FilePublication struct {
	Run            Run
	FileID, BlobID string
	AtMS           int64
}
type PendingFailure struct {
	UploadID, Code string
	AtMS           int64
}
type SessionFinish struct {
	Run                   Run
	State                 string
	ExpectedVersion, AtMS int64
	ExpiresAtMS           *int64
	ErrorCode             *string
}
type JobCreation struct {
	Run                               Run
	DedupeKey, InputDigest            string
	PayloadJSON, InputJSON, EventJSON []byte
	AtMS                              int64
}
type Job struct {
	ID, State   string
	ExecutionNo int64
}
type JobCancellation struct {
	ID, UploadID, ExpectedState, State string
	AtMS, ExecutionNo                  int64
	FinishedAtMS                       *int64
	EventJSON                          []byte
}
type JobFinish struct {
	Run                  Run
	ExpectedState, State string
	ErrorCode            *string
	AtMS                 int64
	EventJSON            []byte
}

type JobClaim struct {
	Run       Run
	AtMS      int64
	EventJSON []byte
}
