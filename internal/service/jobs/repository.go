package jobs

import "context"

// Repository binds all job state, input and event changes to one transaction.
type Repository interface {
	WithWrite(context.Context, func(Records) error) error
	WithRead(context.Context, func(ReadRecords) error) error
}

type Records interface {
	Get(context.Context, string) (Job, error)
	Input(context.Context, string, int64) ([]byte, error)
	Cancel(context.Context, Cancellation) error
	Retry(context.Context, RetryWrite) error
	CancelServerImport(context.Context, Cancellation) error
}

type Job struct {
	Kind, ScopeType, ScopeID, State string
	Cancellable, Retryable          bool
	ExecutionNo, Version            int64
}

type Cancellation struct {
	JobID, State, Reason  string
	ExpectedVersion, AtMS int64
	FinishedAtMS          *int64
	Event                 []byte
}

type RetryWrite struct {
	JobID                              string
	ExpectedVersion, ExecutionNo, AtMS int64
	Input                              []byte
	InputDigest                        string
	Payload, Event                     []byte
}
