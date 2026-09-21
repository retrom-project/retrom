package emulationstationimport

import (
	"context"
	"time"
)

type (
	Execution struct {
		JobID, ImportID, Kind, RootID, RootDigest, RelativePath string
		CreatedByUserID, WorkerID                               string
		ExecutionNo, Attempt, DeadlineAtMS                      int64
		ReleaseYearMax                                          int
	}
	LeaseSnapshot struct {
		Execution
		JobState, ImportState                  string
		JobVersion, ImportVersion, MaxAttempts int64
		AvailableAtMS, LeaseUntilMS            int64
		StartedAtMS                            *int64
	}
	ClaimLease struct {
		Before                      LeaseSnapshot
		Execution                   Execution
		ImportState, Phase          string
		NowMS, StartedAtMS, UntilMS int64
	}
	RenewLease struct {
		Before         LeaseSnapshot
		NowMS, UntilMS int64
	}
	LeaseReader interface {
		Next(context.Context, int64) (LeaseSnapshot, bool, error)
		Current(context.Context, string) (LeaseSnapshot, bool, error)
	}
	LeaseWriter interface {
		Claim(context.Context, ClaimLease) error
		Renew(context.Context, RenewLease) error
	}
	LeaseScope struct {
		Read  LeaseReader
		Write LeaseWriter
	}
	LeaseRepository interface {
		WithLease(context.Context, func(LeaseScope) error) error
	}
	Leases struct {
		repository LeaseRepository
		now        func() time.Time
	}
	LeaseState string
)

const (
	LeaseActive    LeaseState = "ACTIVE"
	LeaseLost      LeaseState = "LOST"
	LeaseDeadline  LeaseState = "DEADLINE"
	LeaseCancelled LeaseState = "CANCELLED"
)

func NewLeases(repository LeaseRepository, now func() time.Time) *Leases {
	return &Leases{repository: repository, now: now}
}
