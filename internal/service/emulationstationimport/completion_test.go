package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type completionMemory struct {
	before                       model.LeaseSnapshot
	counts                       model.CompletionCounts
	change                       model.CompletionChange
	readErr, writeErr, commitErr error
}

func (memory *completionMemory) CommitCompletion(_ context.Context, unit model.Execution, nowMS int64) error {
	if memory.readErr != nil {
		return memory.readErr
	}
	if !true || memory.before.Execution != unit {
		return model.ErrVersionConflict
	}
	if memory.before.Kind != "SERVER_EMULATIONSTATION_IMPORT" || model.ExecutionState(memory.before, unit, nowMS) != model.LeaseActive {
		return model.ErrVersionConflict
	}
	change, err := model.PlanCompletion(memory.before, memory.counts, nowMS)
	if err != nil {
		return err
	}
	if memory.writeErr != nil {
		return memory.writeErr
	}
	memory.change = change
	if memory.commitErr != nil {
		return memory.commitErr
	}
	return nil
}

func newCompletionMemory() *completionMemory {
	return &completionMemory{
		before: newItemWorkMemory().before.Execution,
		counts: model.CompletionCounts{ExpectedItems: 2, Terminal: model.TerminalItemCounts{ReviewPending: 1, Existing: 1}},
	}
}

func TestCompletionPolicyRejectsUnfinishedAndChoosesTerminalOutcome(t *testing.T) {
	for _, kind := range []string{"complete", "partial", "unfinished", "counts", "owner"} {
		t.Run(kind, func(t *testing.T) {
			memory := newCompletionMemory()
			unit := memory.before.Execution
			switch kind {
			case "partial":
				memory.counts.Terminal.Existing = 0
				memory.counts.Terminal.Failed = 1
				memory.counts.RetryableFailed = 1
			case "unfinished":
				memory.counts.Unfinished = 1
			case "counts":
				memory.counts.ExpectedItems = 3
			case "owner":
				memory.before.WorkerID = "other"
			}
			err := NewCompletion(memory, func() time.Time { return time.UnixMilli(2000) }).Finish(t.Context(), unit)
			if kind != "complete" && kind != "partial" {
				if err == nil || memory.change.ImportState != "" {
					t.Fatalf("invalid completion=%#v error=%v", memory.change, err)
				}
				return
			}
			expected := "COMPLETED"
			if kind == "partial" {
				expected = "PARTIAL_FAILURE"
			}
			if err != nil || memory.change.ImportState != expected || memory.change.Retryable != (kind == "partial") {
				t.Fatalf("completion=%#v error=%v", memory.change, err)
			}
		})
	}
}

func TestCompletionPreservesFailureCauses(t *testing.T) {
	cause := errors.New("completion storage failed")
	for _, stage := range []string{"read", "write", "commit"} {
		t.Run(stage, func(t *testing.T) {
			memory := newCompletionMemory()
			switch stage {
			case "read":
				memory.readErr = cause
			case "write":
				memory.writeErr = cause
			case "commit":
				memory.commitErr = cause
			}
			if err := NewCompletion(memory, func() time.Time { return time.UnixMilli(2000) }).Finish(
				t.Context(),
				memory.before.Execution,
			); !errors.Is(
				err,
				cause,
			) {
				t.Fatalf("cause=%v", err)
			}
		})
	}
}
