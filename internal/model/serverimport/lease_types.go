package serverimport

import (
	"context"
	"errors"
)

var (
	ErrLeaseLost       = errors.New("SERVER_IMPORT_LEASE_LOST")
	ErrWorkerCancelled = errors.New("SERVER_IMPORT_WORKER_CANCELLED")
)

type Work struct {
	ImportID, JobID, RootID, RelativePath, RootDigest, CatalogDigest string
	Owner                                                            string
	Recovery                                                         bool
	Execution                                                        int64
	ReplaceIfBetter                                                  bool
	DeadlineAtMS                                                     int64
}

type LeaseSnapshot struct {
	Work                                                     Work
	State, ImportState                                       string
	JobVersion, ImportVersion, Attempt, Maximum, AvailableAt int64
	LeaseUntil, Deadline                                     *int64
}

type LeaseClaim struct {
	Before               LeaseSnapshot
	Work                 Work
	Now, LeaseUntil      int64
	Event, RecoveryEvent []byte
}

type LeaseTouch struct {
	Before          LeaseSnapshot
	Now, LeaseUntil int64
	Phase           string
	Event           []byte
}

type ClaimCommand struct {
	Now int64
}

type ClaimResult struct {
	Unit  Work
	Found bool
}

type TouchCommand struct {
	Unit  Work
	Phase string
	Event []byte
	Now   int64
}

type LeaseRepository interface {
	CommitClaim(context.Context, ClaimCommand) (ClaimResult, error)
	CommitTouch(context.Context, TouchCommand) error
}
