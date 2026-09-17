package serverimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/serverimport"
)

type leaseMemory struct {
	claimResult model.ClaimResult
	touchCmd    model.TouchCommand
	writes      int
	claimErr    error
	touchErr    error
}

func (memory *leaseMemory) CommitClaim(
	_ context.Context, _ model.ClaimCommand,
) (model.ClaimResult, error) {
	if memory.claimErr != nil {
		return model.ClaimResult{}, memory.claimErr
	}
	memory.writes++
	return memory.claimResult, nil
}

func (memory *leaseMemory) CommitTouch(
	_ context.Context, cmd model.TouchCommand,
) error {
	memory.touchCmd = cmd
	if memory.touchErr != nil {
		return memory.touchErr
	}
	memory.writes++
	return nil
}

func TestClaimDelegatesToRepoAndReturnsWork(t *testing.T) {
	memory := &leaseMemory{claimResult: model.ClaimResult{
		Unit: model.Work{
			ImportID: "import", JobID: "job",
			Owner: "new-owner", Execution: 2,
			DeadlineAtMS: 500,
		},
		Found: true,
	}}
	service := NewLeases(
		memory, func() time.Time { return time.UnixMilli(100) },
	)
	unit, found, err := service.Claim(t.Context())
	if err != nil || !found ||
		unit.Owner != "new-owner" || unit.DeadlineAtMS != 500 {
		t.Fatalf("claim: %+v %v %v", unit, found, err)
	}
}

func TestLeaseTouchDelegatesToRepo(t *testing.T) {
	memory := &leaseMemory{}
	unit := model.Work{
		ImportID: "import", JobID: "job",
		Owner: "worker", Execution: 1,
	}
	service := NewLeases(
		memory, func() time.Time { return time.UnixMilli(100) },
	)
	if err := service.Progress(
		t.Context(), unit, "INSTALLING", 0, 1,
	); err != nil {
		t.Fatal(err)
	}
	if memory.touchCmd.Phase != "INSTALLING" ||
		memory.touchCmd.Unit.Owner != "worker" {
		t.Fatalf("touch: %+v", memory.touchCmd)
	}
}

func TestLeaseTouchRejectsMismatch(t *testing.T) {
	memory := &leaseMemory{touchErr: model.ErrLeaseLost}
	unit := model.Work{
		ImportID: "import", JobID: "job",
		Owner: "worker", Execution: 1,
	}
	err := NewLeases(
		memory, func() time.Time { return time.UnixMilli(100) },
	).Progress(t.Context(), unit, "INSTALLING", 0, 1)
	if !errors.Is(err, model.ErrLeaseLost) || memory.writes != 0 {
		t.Fatalf("stale progress: %v writes=%d", err, memory.writes)
	}
}

func TestClaimCommitFailureDoesNotReleaseWork(t *testing.T) {
	memory := &leaseMemory{claimErr: context.Canceled}
	unit, found, err := NewLeases(memory, time.Now).Claim(t.Context())
	if !errors.Is(err, context.Canceled) || found ||
		unit.Owner != "" {
		t.Fatalf(
			"uncommitted work escaped: %+v %v %v",
			unit, found, err,
		)
	}
}
