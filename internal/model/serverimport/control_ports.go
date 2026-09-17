package serverimport

import (
	"context"
	"errors"
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
type ControlReader interface {
	Current(context.Context, string) (ControlSnapshot, error)
}
type ControlWriter interface {
	Cancel(context.Context, Cancellation) error
	Retry(context.Context, ManualRetry) error
}
type ControlScope struct {
	Read  ControlReader
	Write ControlWriter
}
type ControlRepository interface {
	CommitWrite(context.Context, func(ControlScope) error) error
}
