package pegasusimport

import "context"

type ExecutionIdentity struct {
	JobID, ImportID, WorkerID string
	ExecutionNo, Attempt      int64
}

type Work struct {
	JobID, ImportID, Kind, RootID, RootDigest, RelativePath string
	CreatedByUserID, WorkerID                               string
	ExecutionNo, Attempt, DeadlineAtMS                      int64
}

func (unit Work) Identity() ExecutionIdentity {
	return ExecutionIdentity{
		JobID: unit.JobID, ImportID: unit.ImportID, WorkerID: unit.WorkerID,
		ExecutionNo: unit.ExecutionNo, Attempt: unit.Attempt,
	}
}

type LeaseCandidate struct {
	Work                                   Work
	ImportState                            string
	JobVersion, ImportVersion, MaxAttempts int64
	StartedAtMS, DeadlineAtMS              *int64
}

type LeaseClaim struct {
	Before                           LeaseCandidate
	Work                             Work
	ImportState, Phase               string
	NowMS, StartedAtMS, LeaseUntilMS int64
}

type LeaseRenewal struct {
	Before              ExecutionSnapshot
	NowMS, LeaseUntilMS int64
}

type LeaseRepository interface {
	LoadLeaseCandidate(context.Context, int64) (LeaseCandidate, bool, error)
	LoadCurrentLease(context.Context, string) (ExecutionSnapshot, error)
	CommitLeaseClaim(context.Context, LeaseClaim) error
	CommitLeaseRenewal(context.Context, LeaseRenewal) error
}
