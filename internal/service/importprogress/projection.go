package importprogress

import "errors"

var ErrInvalid = errors.New("invalid import progress")

type Counts struct {
	Queued, Running, ReviewPending, Failed, Cancelled int64
	Rejected, ResolvedRejected                        int64
}

type Snapshot struct {
	Counts                             Counts
	Started                            bool
	State                              string
	CancelRequestedAtMS, CompletedAtMS *int64
}

type Projection struct {
	State         string
	CompletedAtMS *int64
}

// Project derives item-driven progress without overriding cancellation or a task-level failure.
func Project(snapshot Snapshot, now int64) (Projection, error) {
	if !validCounts(snapshot.Counts) {
		return Projection{}, ErrInvalid
	}
	if snapshot.CancelRequestedAtMS != nil || snapshot.State == "FAILED" {
		return Projection{State: snapshot.State, CompletedAtMS: snapshot.CompletedAtMS}, nil
	}
	if snapshot.Counts.Cancelled > 0 {
		return Projection{}, ErrInvalid
	}
	return activeProjection(snapshot, now), nil
}

func validCounts(counts Counts) bool {
	for _, count := range []int64{
		counts.Queued, counts.Running, counts.ReviewPending,
		counts.Failed, counts.Cancelled, counts.Rejected, counts.ResolvedRejected,
	} {
		if count < 0 {
			return false
		}
	}
	return counts.ResolvedRejected <= counts.Rejected
}

func activeProjection(snapshot Snapshot, now int64) Projection {
	counts := snapshot.Counts
	switch {
	case !snapshot.Started:
		return Projection{State: "QUEUED"}
	case counts.Queued > 0 || counts.Running > 0:
		return Projection{State: "RUNNING"}
	case counts.Failed > 0 || counts.Rejected > counts.ResolvedRejected:
		return Projection{State: "PARTIAL_FAILURE"}
	case counts.ReviewPending > 0:
		return Projection{State: "REVIEW_PENDING"}
	default:
		return Projection{State: "COMPLETED", CompletedAtMS: &now}
	}
}
