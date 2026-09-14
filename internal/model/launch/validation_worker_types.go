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

type ValidationWorkerRepository interface {
	WithWorker(context.Context, func(ValidationWorkerScope) error) error
	Facts(context.Context, ValidationInputs) (ValidationFacts, error)
	Candidates(context.Context, int64) ([]string, error)
}

type ValidationWorkerScope struct {
	Jobs     ValidationWorkerJobs
	Facts    ValidationWorkerFacts
	Variants ValidationWorkerVariants
}

type ValidationWorkerJobs interface {
	Read(context.Context, string) (ValidationWork, bool, error)
	Claim(context.Context, ValidationClaimWrite) error
	Renew(context.Context, ValidationClaim, int64, int64) error
	Finish(context.Context, ValidationTerminal) error
	Recover(context.Context, ValidationRecovery) error
}

type ValidationWorkerFacts interface {
	Facts(context.Context, ValidationInputs) (ValidationFacts, error)
}

type ValidationWorkerVariants interface {
	Apply(context.Context, ValidationVariantWrite) error
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
