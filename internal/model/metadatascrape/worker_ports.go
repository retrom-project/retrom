package metadatascrape

import (
	"context"
	"errors"
)

var (
	ErrExecutionLost     = errors.New("metadata execution ownership lost")
	ErrAttemptsExhausted = errors.New("metadata execution attempts exhausted")
)

type WorkerRun struct {
	RunID, JobID, Provider, State, JobState, Payload string
	ExecutionNo, Version, AttemptCount, MaxAttempts  int64
	Deadline, LeaseUntil, AvailableAt                int64
}
type WorkerClaim struct {
	RunID, JobID, WorkerID     string
	ExecutionNo, Now, Deadline int64
	Version, AttemptCount      int64
	Terminal                   bool
}
type WorkerStatus struct {
	State                    string
	Expired, ParentCancelled bool
}
type WorkerOutcome struct {
	Claim                 WorkerClaim
	State, RunState, Code string
	Count                 int
	Now                   int64
}
type WorkerLeases interface {
	Claim(context.Context, WorkerClaim) (bool, error)
	Requeue(context.Context, WorkerClaim, int64) (bool, error)
	Refresh(context.Context, WorkerClaim, int64) (bool, error)
	Status(context.Context, WorkerClaim, int64) (WorkerStatus, error)
}
type WorkerWriter interface {
	Finish(context.Context, WorkerOutcome) error
}
type WorkerScope struct {
	Leases  WorkerLeases
	Write   WorkerWriter
	Initial InitialReviewScope
}

type WorkerClaimCommand struct {
	Run      WorkerRun
	WorkerID string
	Now      int64
}
type WorkerClaimResult struct {
	Claim   WorkerClaim
	Claimed bool
}
type WorkerRefreshCommand struct {
	Claim WorkerClaim
	Now   int64
}
type WorkerSettleCommand struct {
	Claim  WorkerClaim
	Count  int
	Code   string
	Failed bool
	Now    int64
}
type WorkerSettleResult struct {
	Expired bool
}

type WorkerRepository interface {
	Run(context.Context, string) (WorkerRun, error)
	Recoverable(context.Context, int64) ([]string, error)
	CommitClaim(context.Context, WorkerClaimCommand) (WorkerClaimResult, error)
	CommitRefresh(context.Context, WorkerRefreshCommand) (bool, error)
	CommitSettle(context.Context, WorkerSettleCommand) (WorkerSettleResult, error)
}
type WorkerProcessor interface {
	Process(context.Context, WorkerClaim, string) (int, string, error)
}
