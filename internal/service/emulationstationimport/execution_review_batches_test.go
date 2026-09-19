package emulationstationimport

import (
	"context"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type executionReviewMemory struct {
	*executionMemory
	remaining, completed, transactions, maxBatch int
}

func (memory *executionReviewMemory) CurrentExecution(_ context.Context, _ string) (model.LeaseSnapshot, bool, error) {
	return memory.before, true, nil
}

func (memory *executionReviewMemory) TerminalCount(_ context.Context, _ string) (int64, error) {
	return memory.terminal, nil
}

func (memory *executionReviewMemory) CommitExecutionReviewBatch(
	_ context.Context, _ model.Execution, _ func() int64, _ int,
) (model.ExecutionReviewBatchResult, error) {
	batchSize := min(100, memory.remaining)
	memory.remaining -= batchSize
	before := memory.completed
	memory.completed += batchSize
	memory.transactions++
	memory.maxBatch = max(memory.maxBatch, memory.completed-before)
	for range batchSize {
		memory.before.ImportVersion++
	}
	more := memory.remaining > 0
	return model.ExecutionReviewBatchResult{Before: memory.before, More: more}, nil
}

func (memory *executionReviewMemory) CommitExecutionFinish(_ context.Context, change model.ExecutionFinish) error {
	memory.finish = change
	memory.committed = true
	return nil
}

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
