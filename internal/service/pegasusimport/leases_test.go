package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type leaseFake struct {
	candidate model.LeaseCandidate
	current   model.ExecutionSnapshot
	claimed   *model.LeaseClaim
	renewed   bool
	failure   error
}

func (fake *leaseFake) LoadLeaseCandidate(
	_ context.Context, _ int64,
) (model.LeaseCandidate, bool, error) {
	if fake.failure != nil {
		return model.LeaseCandidate{}, false, fake.failure
	}
	return fake.candidate, true, nil
}

func (fake *leaseFake) LoadCurrentLease(
	_ context.Context, _ string,
) (model.ExecutionSnapshot, error) {
	return fake.current, fake.failure
}

func (fake *leaseFake) CommitLeaseClaim(
	_ context.Context, change model.LeaseClaim,
) error {
	if fake.failure != nil {
		return fake.failure
	}
	fake.claimed = &change
	return nil
}

func (fake *leaseFake) CommitLeaseRenewal(
	_ context.Context, _ model.LeaseRenewal,
) error {
	if fake.failure != nil {
		return fake.failure
	}
	fake.renewed = true
	return nil
}

func leaseCandidate() model.LeaseCandidate {
	return model.LeaseCandidate{
		Work:       model.Work{JobID: "job", ImportID: "import", Kind: "SERVER_PEGASUS_IMPORT", ExecutionNo: 1},
		JobVersion: 1, ImportVersion: 1, ImportState: "QUEUED", MaxAttempts: 4,
	}
}

func TestLeasePreservesBudgetAndAssignsFreshOwner(t *testing.T) {
	t.Parallel()
	started, deadline := int64(2), int64(100)
	fake := &leaseFake{candidate: leaseCandidate()}
	fake.candidate.StartedAtMS, fake.candidate.DeadlineAtMS = &started, &deadline
	fake.candidate.Work.Attempt = 1
	service := NewLeases(fake, func() time.Time { return time.UnixMilli(10) })
	unit, found, err := service.Claim(t.Context())
	if err != nil || !found || unit.WorkerID == "" || unit.DeadlineAtMS != 100 || unit.Attempt != 2 {
		t.Fatalf("claim = %+v, %v, %v", unit, found, err)
	}
	if fake.claimed == nil || fake.claimed.StartedAtMS != 2 || fake.claimed.LeaseUntilMS != 100 {
		t.Fatalf("claim changed original budget: %+v", fake.claimed)
	}
}

func TestLeaseRejectsInvalidQueueWithoutWriting(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"parent", "deadline", "attempts", "partial budget"} {
		t.Run(name, func(t *testing.T) {
			fake := &leaseFake{candidate: leaseCandidate()}
			switch name {
			case "parent":
				fake.candidate.ImportState = "COMPLETED"
			case "deadline":
				start, end := int64(1), int64(10)
				fake.candidate.StartedAtMS, fake.candidate.DeadlineAtMS = &start, &end
			case "attempts":
				fake.candidate.Work.Attempt = 4
			case "partial budget":
				start := int64(1)
				fake.candidate.StartedAtMS = &start
			}
			_, found, err := NewLeases(
				fake, func() time.Time { return time.UnixMilli(10) },
			).Claim(t.Context())
			if err == nil || found || fake.claimed != nil {
				t.Fatalf("invalid queue claimed: %v %v", found, err)
			}
		})
	}
}

func TestLeaseRenewalFencesOwnerAndDeadline(t *testing.T) {
	t.Parallel()
	identity := model.ExecutionIdentity{
		JobID: "job", ImportID: "import", WorkerID: "owner",
		ExecutionNo: 2, Attempt: 3,
	}
	for _, name := range []string{
		"valid", "canceling", "worker", "execution",
		"attempt", "lease", "deadline", "parent",
	} {
		t.Run(name, func(t *testing.T) {
			fake := &leaseFake{current: model.ExecutionSnapshot{
				JobID: "job", ImportID: "import", WorkerID: "owner",
				Kind: "SERVER_PEGASUS_IMPORT", JobState: "RUNNING",
				ImportState: "RUNNING", ExecutionNo: 2, Attempt: 3,
				JobVersion: 1, ImportVersion: 1,
				LeaseUntilMS: 50, DeadlineMS: 100,
			}}
			switch name {
			case "canceling":
				fake.current.JobState = "CANCEL_REQUESTED"
				fake.current.ImportState = "CANCEL_REQUESTED"
			case "worker":
				fake.current.WorkerID = "new-owner"
			case "execution":
				fake.current.ExecutionNo++
			case "attempt":
				fake.current.Attempt++
			case "lease":
				fake.current.LeaseUntilMS = 10
			case "deadline":
				fake.current.DeadlineMS = 10
			case "parent":
				fake.current.ImportState = "QUEUED"
			}
			err := NewLeases(
				fake, func() time.Time { return time.UnixMilli(10) },
			).Renew(t.Context(), identity)
			valid := name == "valid" || name == "canceling"
			if valid != (err == nil) || fake.renewed != valid {
				t.Fatalf("renewed=%v err=%v", fake.renewed, err)
			}
		})
	}
}

func TestLeaseReadFailureKeepsCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("storage unavailable")
	fake := &leaseFake{failure: cause}
	_, _, err := NewLeases(fake, time.Now).Claim(t.Context())
	if !errors.Is(err, cause) {
		t.Fatalf("claim hid cause: %v", err)
	}
}
