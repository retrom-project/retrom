package serverimport

import (
	"context"
	"errors"
)

var ErrOutcomeIncomplete = errors.New("SERVER_IMPORT_ITEMS_UNFINISHED")

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

type FinishCommand struct {
	Unit Work
	Now  int64
}

type CancelOutcomeCommand struct {
	Unit Work
	Now  int64
}

type FailCommand struct {
	Unit Work
	Code string
	Now  int64
}

type FailResult struct {
	RetryAt int64
}

type OutcomeRepository interface {
	CommitItemOutcome(context.Context, ItemOutcome) error
	CommitFinish(context.Context, FinishCommand) error
	CommitCancelOutcome(context.Context, CancelOutcomeCommand) error
	CommitFail(context.Context, FailCommand) (FailResult, error)
	Recovery(context.Context, int64) (RecoveryWork, bool, error)
}
