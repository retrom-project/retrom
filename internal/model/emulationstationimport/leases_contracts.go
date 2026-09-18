package emulationstationimport

import "context"

type Execution struct {
	JobID, ImportID, Kind, RootID, RootDigest, RelativePath string
	CreatedByUserID, WorkerID                               string
	ExecutionNo, Attempt, DeadlineAtMS                      int64
	ReleaseYearMax                                          int
}

type LeaseSnapshot struct {
	Execution
	JobState, ImportState                  string
	JobVersion, ImportVersion, MaxAttempts int64
	AvailableAtMS, LeaseUntilMS            int64
	StartedAtMS                            *int64
}

type ClaimLease struct {
	Before                      LeaseSnapshot
	Execution                   Execution
	ImportState, Phase          string
	NowMS, StartedAtMS, UntilMS int64
}

type RenewLease struct {
	Before         LeaseSnapshot
	NowMS, UntilMS int64
}

type LeaseRepository interface {
	LoadLeaseCandidate(context.Context, int64) (LeaseSnapshot, bool, error)
	LoadCurrentLease(context.Context, string) (LeaseSnapshot, bool, error)
	CommitLeaseClaim(context.Context, ClaimLease) error
	CommitLeaseRenewal(context.Context, RenewLease) error
}

type LeaseState string

const (
	LeaseActive    LeaseState = "ACTIVE"
	LeaseLost      LeaseState = "LOST"
	LeaseDeadline  LeaseState = "DEADLINE"
	LeaseCancelled LeaseState = "CANCELLED"
)
