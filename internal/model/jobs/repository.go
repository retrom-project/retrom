package jobs

import "context"

// CancelCommand captures all inputs for a direct job cancellation.
type CancelCommand struct {
	JobID            string
	ExpectedVersion  int64
	Reason           string
	NowMS            int64
	DomainHandlerFor []string // job kinds that have domain handlers
}

// CancelResult is the outcome of a direct cancellation.
type CancelResult struct {
	Result      Result
	Pending     bool
	NeedsDomain bool
	DomainJob   Job
}

// Result is the public outcome of a job command.
type Result struct {
	Kind        string
	JobID       string
	State       string
	ExecutionNo int64
	Version     int64
}

// RetryCommand captures all inputs for a job retry.
type RetryCommand struct {
	JobID           string
	ExpectedVersion int64
	NowMS           int64
}

// Repository binds all job state, input and event changes to one transaction.
type Repository interface {
	WithRead(context.Context, func(ReadRecords) error) error
	CommitCancel(context.Context, CancelCommand) (CancelResult, error)
	CommitRetry(context.Context, RetryCommand) (Result, error)
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
