package launch

import (
	"context"
	"time"

	"retrom/internal/capability/content/corevalidation"
)

type ValidationWork struct {
	ID, Kind, ScopeType, ScopeID, State, WorkerID           string
	SnapshotJSON, InputDigest                               string
	Version, ExecutionNo, Attempt, MaxAttempts, AvailableMS int64
	StartedMS, DeadlineMS, LeaseMS                          *int64
}

type ValidationClaim struct {
	Job      ValidationWork
	Snapshot ValidationSnapshot
}

type ValidationClaimWrite struct {
	Before                                ValidationWork
	WorkerID                              string
	NowMS, StartedMS, DeadlineMS, LeaseMS int64
}

type ValidationRecovery struct {
	Before   ValidationWork
	NowMS    int64
	Terminal bool
}

type ValidationTerminal struct {
	Evaluated   bool
	Claim       ValidationClaim
	State, Code string
	Retryable   bool
	NowMS       int64
}

type ValidationOutcome struct {
	Status, Code, DependencyJSON string
	BIOS                         corevalidation.Snapshot
}

type ValidationFacts struct {
	VariantVersion                           int64
	Found, BindingFound, RelationshipEnabled bool
	Content                                  ProductSnapshot
	Classification                           string
}

type ValidationVariantWrite struct {
	Inputs  ValidationInputs
	Outcome ValidationOutcome
	NowMS   int64
}

type ValidationWorkerReader interface {
	LoadValidationWork(context.Context, string) (ValidationWork, bool, error)
	LoadValidationFacts(context.Context, ValidationInputs) (ValidationFacts, error)
	LoadValidationCandidates(context.Context, int64) ([]string, error)
}

type ValidationWorkerCommitter interface {
	CommitValidationClaim(context.Context, ValidationClaimWrite) error
	CommitValidationRenewal(context.Context, ValidationClaim, int64, int64) error
	CommitValidationFinish(context.Context, ValidationTerminal) error
	CommitValidationRecovery(context.Context, ValidationRecovery) error
	CommitValidationSettlement(context.Context, ValidationSettlement) error
}

type ValidationWorkerRepository interface {
	ValidationWorkerReader
	ValidationWorkerCommitter
}

type ValidationSettlement struct {
	Variant ValidationVariantWrite
	Finish  ValidationTerminal
}

type ValidationTicker interface {
	Ticks() <-chan time.Time
	Stop()
}

type ValidationWorkerEnvironment struct {
	Now       func() time.Time
	NewID     func() (string, error)
	NewTicker func(time.Duration) ValidationTicker
}
