package emulationstationimport

import (
	"context"
	"fmt"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
	library "retrom/internal/model/libraryimport"
)

type recoveryReviewMemory struct {
	before                                    model.LeaseSnapshot
	remaining, completed, transactions, maxBatch int
	applied                                    int
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

func (memory *recoveryReviewMemory) Current(_ context.Context, _ string) (model.LeaseSnapshot, bool, error) {
	return memory.before, true, nil
}

func (memory *recoveryReviewMemory) Reviews(_ context.Context, _ string, _ int) ([]model.ExecutionReview, error) {
	result := make([]model.ExecutionReview, min(101, memory.remaining))
	for index := range result {
		result[index] = model.ExecutionReview{
			ItemID: fmt.Sprint(memory.completed + index), State: "VALIDATING", ReservedItemID: "ordinary", ReservedJobID: "library",
			Version: 1, MetadataJSON: `{"title":"Game"}`, WarningsJSON: "[]",
		}
	}
	return result, nil
}

func (memory *recoveryReviewMemory) Fence(context.Context, model.LeaseSnapshot, int64) error {
	return nil
}

func (memory *recoveryReviewMemory) CompleteReview(_ context.Context, _ model.ExecutionReviewCompletion) error {
	memory.remaining--
	memory.completed++
	memory.before.ImportVersion++
	return nil
}

func (*recoveryReviewMemory) CurrentMetadata(_ context.Context, _ string) (library.MetadataDraft, error) {
	return library.MetadataDraft{Version: 1, MetadataJSON: `{"description":"","developer":"","genre":"","players":null,"publisher":"","releaseYear":null,"title":"Game"}`}, nil
}

func (*recoveryReviewMemory) SaveMetadata(_ context.Context, _ library.MetadataChange) error {
	return nil
}

func (memory *recoveryReviewMemory) Apply(_ context.Context, change model.RecoveryChange) error {
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
