package emulationstationimport

import (
	"context"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type recoveryReviewMemory struct {
	*executionReviewMemory
	applied int
}

func (memory *recoveryReviewMemory) Expired(context.Context, int64, int) ([]model.LeaseSnapshot, error) {
	return []model.LeaseSnapshot{memory.before}, nil
}

func (memory *recoveryReviewMemory) WithRecovery(_ context.Context, run func(model.RecoveryScope) error) error {
	before := memory.completed
	if err := run(model.RecoveryScope{Payload: emptyPayloadScope(), Read: memory, Write: memory, Metadata: memory}); err != nil {
		return err
	}
	memory.transactions++
	memory.maxBatch = max(memory.maxBatch, memory.completed-before)
	return nil
}

func (memory *recoveryReviewMemory) Apply(_ context.Context, change model.RecoveryChange) error {
	memory.applied++
	memory.before.JobState, memory.before.ImportState = change.JobState, change.ImportState
	return nil
}

func TestRecoveryLeavesRemainingReviewsForNextMaintenancePass(t *testing.T) {
	memory := &recoveryReviewMemory{executionReviewMemory: &executionReviewMemory{executionMemory: newExecutionMemory(), remaining: 205}}
	memory.before.Kind = "SERVER_EMULATIONSTATION_IMPORT"
	memory.before.JobState, memory.before.ImportState = "CANCEL_REQUESTED", "CANCEL_REQUESTED"
	memory.before.LeaseUntilMS = 2000
	service := NewRecovery(memory, func() time.Time { return time.UnixMilli(2000) })
	for pass := 1; pass <= 3; pass++ {
		if err := service.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		want := min(pass*100, 205)
		if memory.completed != want || memory.transactions != pass || memory.maxBatch > 100 {
			t.Fatalf("pass=%d completed=%d tx=%d batch=%d", pass, memory.completed, memory.transactions, memory.maxBatch)
		}
		if pass < 3 && memory.applied != 0 {
			t.Fatal("closed recovery while reviews remained")
		}
	}
	if memory.applied != 1 || memory.before.ImportState != "CANCELLED" || memory.before.JobState != "CANCELLED" {
		t.Fatalf("terminal recovery=%#v", memory)
	}
}
