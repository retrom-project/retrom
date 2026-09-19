package emulationstationimport

import (
	"context"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type recoveryReviewMemory struct {
	before                                       model.LeaseSnapshot
	remaining, completed, transactions, maxBatch int
	applied                                      int
}

func (memory *recoveryReviewMemory) Expired(context.Context, int64, int) ([]model.LeaseSnapshot, error) {
	return []model.LeaseSnapshot{memory.before}, nil
}

func (memory *recoveryReviewMemory) CurrentRecovery(_ context.Context, _ string) (model.LeaseSnapshot, bool, error) {
	return memory.before, true, nil
}

func (memory *recoveryReviewMemory) CommitRecoveryReviewBatch(_ context.Context, candidate model.LeaseSnapshot, _ int64, _ int) (model.RecoveryReviewBatchResult, error) {
	batch := min(100, memory.remaining)
	memory.remaining -= batch
	memory.completed += batch
	memory.transactions++
	memory.maxBatch = max(memory.maxBatch, batch)
	memory.before.ImportVersion += int64(batch)
	return model.RecoveryReviewBatchResult{Before: memory.before, Found: true, More: memory.remaining > 0}, nil
}

func (memory *recoveryReviewMemory) CommitRecovery(_ context.Context, change model.RecoveryChange) error {
	memory.applied++
	memory.before.JobState, memory.before.ImportState = change.JobState, change.ImportState
	return nil
}

func TestRecoveryLeavesRemainingReviewsForNextMaintenancePass(t *testing.T) {
	started := int64(1000)
	memory := &recoveryReviewMemory{
		before: model.LeaseSnapshot{
			Execution: model.Execution{
				JobID: "job", ImportID: "plan", Kind: "SERVER_EMULATIONSTATION_IMPORT", WorkerID: "owner",
				ExecutionNo: 1, Attempt: 1, DeadlineAtMS: 300000, ReleaseYearMax: 2027,
			},
			JobState: "CANCEL_REQUESTED", ImportState: "CANCEL_REQUESTED", JobVersion: 2, ImportVersion: 3, MaxAttempts: 4,
			LeaseUntilMS: 2000, StartedAtMS: &started,
		},
		remaining: 205,
	}
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
