package emulationstationimport

import (
	"context"
	"fmt"
	"testing"
	"time"

	library "retrom/internal/service/libraryimport"
)

type executionReviewMemory struct {
	*executionMemory
	remaining, completed, transactions, maxBatch int
}

func (memory *executionReviewMemory) WithExecution(_ context.Context, run func(ExecutionScope) error) error {
	before := memory.completed
	if err := run(ExecutionScope{Read: memory, Write: memory, Metadata: memory}); err != nil {
		return err
	}
	memory.transactions++
	memory.maxBatch = max(memory.maxBatch, memory.completed-before)
	return nil
}

func (memory *executionReviewMemory) Reviews(context.Context, string, int) ([]ExecutionReview, error) {
	result := make([]ExecutionReview, min(101, memory.remaining))
	for index := range result {
		result[index] = ExecutionReview{
			ItemID: fmt.Sprint(memory.completed + index), State: "VALIDATING", ReservedItemID: "ordinary", ReservedJobID: "library",
			Version: 1, MetadataJSON: `{"title":"Game"}`, WarningsJSON: "[]",
		}
	}
	return result, nil
}

func (memory *executionReviewMemory) CompleteReview(context.Context, ExecutionReviewCompletion) error {
	memory.remaining--
	memory.completed++
	memory.before.ImportVersion++
	return nil
}

func (*executionReviewMemory) CurrentMetadata(context.Context, string) (library.MetadataDraft, error) {
	return library.MetadataDraft{Version: 1, MetadataJSON: `{"description":"","developer":"","genre":"","players":null,"publisher":"","releaseYear":null,"title":"Game"}`}, nil
}

func (*executionReviewMemory) SaveMetadata(context.Context, library.MetadataChange) error { return nil }

func TestExecutionCancellationCompletesReviewsInBoundedTransactions(t *testing.T) {
	memory := &executionReviewMemory{executionMemory: newExecutionMemory(), remaining: 205}
	memory.before.Kind = "SERVER_EMULATIONSTATION_IMPORT"
	memory.before.JobState, memory.before.ImportState = "CANCEL_REQUESTED", "CANCEL_REQUESTED"
	closed, err := NewExecutionControl(memory, func() time.Time { return time.UnixMilli(2000) }).CloseCancelled(t.Context(), memory.before.Execution)
	if err != nil || !closed || memory.completed != 205 || memory.remaining != 0 || memory.transactions != 3 || memory.maxBatch != 100 {
		t.Fatalf("closed=%v completed=%d remaining=%d transactions=%d batch=%d error=%v",
			closed, memory.completed, memory.remaining, memory.transactions, memory.maxBatch, err)
	}
	if memory.finish.Before.ImportVersion != 208 {
		t.Fatalf("final scope version=%d", memory.finish.Before.ImportVersion)
	}
}
