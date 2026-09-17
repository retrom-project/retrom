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

type LeaseRecords interface {
	Next(context.Context, int64) (LeaseSnapshot, bool, error)
	Current(context.Context, string) (LeaseSnapshot, error)
	Claim(context.Context, LeaseClaim) error
	Touch(context.Context, LeaseTouch) error
}

type LeaseRepository interface {
	CommitWrite(context.Context, func(LeaseRecords) error) error
}
