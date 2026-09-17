package serverimport

import "context"

type WorkerAccess int

const (
	RunningWorker WorkerAccess = iota
	CancelledWorker
	ExhaustedWorker
)

type (
	RetryBudget  struct{ Attempt, Maximum, Deadline int64 }
	RecoveryWork struct {
		Unit      Work
		Cancelled bool
	}
)

type ItemOutcome struct {
	Unit                        Work
	RequirementID, State, Code  string
	Method                      *string
	Details, Event              []byte
	CandidateID, CandidateState *string
	Now                         int64
}
type (
	TerminalCounts struct {
		Matched, Warning, Missing, NotFound                             int64
		SkippedExisting, SkippedNotBetter, SameBytes, Failed, Cancelled int64
	}
	FinalOutcome struct {
		Unit                       Work
		State, JobState, EventType string
		Phase, Code                *string
		Retryable                  *bool
		HeartbeatAt                *int64
		PendingState, PendingCode  string
		Counts                     TerminalCounts
		Event                      []byte
		Now                        int64
	}
)

type AutomaticRetry struct {
	Unit             Work
	AvailableAt, Now int64
	Event            []byte
}
type OutcomeReader interface {
	Budget(context.Context, Work) (RetryBudget, error)
	Counts(context.Context, Work) (map[string]int64, error)
}
type OutcomeWriter interface {
	Lock(context.Context, Work, int64, WorkerAccess) error
	Item(context.Context, ItemOutcome) error
	Final(context.Context, FinalOutcome) error
	Retry(context.Context, AutomaticRetry) error
}
type OutcomeScope struct {
	Read  OutcomeReader
	Write OutcomeWriter
}
type OutcomeRepository interface {
	CommitWrite(context.Context, func(OutcomeScope) error) error
	Recovery(context.Context, int64) (RecoveryWork, bool, error)
}
