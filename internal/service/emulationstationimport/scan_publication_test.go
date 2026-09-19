package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type scanMemory struct {
	snapshot     model.LeaseSnapshot
	writes       []string
	batches      []int
	transactions int
	failure      error
	stage        string
}

func (memory *scanMemory) WithScan(_ context.Context, work func(model.ScanScope) error) error {
	memory.transactions++
	if err := work(model.ScanScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	if memory.stage == "commit" {
		return memory.failure
	}
	return nil
}

func (memory *scanMemory) Current(context.Context, string) (model.LeaseSnapshot, bool, error) {
	if memory.stage == "read" {
		return model.LeaseSnapshot{}, false, memory.failure
	}
	return memory.snapshot, true, nil
}

func (memory *scanMemory) Clear(context.Context, model.ScanMutation) error {
	return memory.write("clear")
}

func (memory *scanMemory) Headers(context.Context, model.ScanMutation, model.ScanProjection) error {
	return memory.write("headers")
}

func (memory *scanMemory) Items(_ context.Context, _ model.ScanMutation, items []model.ScanItem) error {
	memory.batches = append(memory.batches, len(items))
	return memory.write("items")
}

func (memory *scanMemory) Complete(context.Context, model.ScanMutation, model.ScanProjection) error {
	return memory.write("complete")
}

func (memory *scanMemory) Reject(context.Context, model.ScanMutation, model.ScanProjection) error {
	return memory.write("reject")
}

func (memory *scanMemory) write(value string) error {
	if memory.stage == value {
		return memory.failure
	}
	memory.writes = append(memory.writes, value)
	return nil
}

func scanMemoryFixture() *scanMemory {
	snapshot := leaseFixture().snapshot
	snapshot.JobState = "RUNNING"
	snapshot.WorkerID = "worker"
	snapshot.Attempt = 1
	snapshot.DeadlineAtMS = 10000
	snapshot.LeaseUntilMS = 5000
	return &scanMemory{snapshot: snapshot}
}

func TestScanPublicationRejectsLostAndCancelledOwners(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"worker", "cancel", "lease", "deadline"} {
		memory := scanMemoryFixture()
		unit := memory.snapshot.Execution
		switch scenario {
		case "worker":
			memory.snapshot.WorkerID = "replacement"
		case "cancel":
			memory.snapshot.JobState = "CANCEL_REQUESTED"
			memory.snapshot.ImportState = "CANCEL_REQUESTED"
		case "lease":
			memory.snapshot.LeaseUntilMS = 1000
		case "deadline":
			memory.snapshot.DeadlineAtMS = 1000
			unit.DeadlineAtMS = 1000
		}
		err := NewScanPublication(memory, func() time.Time { return time.UnixMilli(1000) }).Headers(t.Context(), unit, model.ScanProjection{})
		if err == nil || len(memory.writes) != 0 {
			t.Fatalf("%s error=%v writes=%v", scenario, err, memory.writes)
		}
	}
}

func TestScanPublicationPreservesReadWriteCommitCauses(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"read", "headers", "commit"} {
		memory := scanMemoryFixture()
		memory.failure = errors.New("scan storage failed")
		memory.stage = stage
		err := NewScanPublication(memory, func() time.Time { return time.UnixMilli(1000) }).Headers(t.Context(), memory.snapshot.Execution, model.ScanProjection{})
		if !errors.Is(err, memory.failure) {
			t.Fatalf("%s cause=%v", stage, err)
		}
	}
}

func TestScanPublicationUsesBoundedItemTransactions(t *testing.T) {
	t.Parallel()
	memory := scanMemoryFixture()
	service := NewScanPublication(memory, func() time.Time { return time.UnixMilli(1000) })
	if err := service.Items(t.Context(), memory.snapshot.Execution, make([]model.ScanItem, 1001)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(memory.batches, []int{500, 500, 1}) || memory.transactions != 3 {
		t.Fatalf("batches=%v transactions=%d", memory.batches, memory.transactions)
	}
	if err := service.Items(t.Context(), memory.snapshot.Execution, nil); err != nil {
		t.Fatal(err)
	}
	if memory.transactions != 3 {
		t.Fatal("empty scan batch opened a transaction")
	}
}
