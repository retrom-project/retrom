package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type itemWorkMemory struct {
	before                       OwnedItem
	claimed, resumed, finished   int
	outcome                      ItemOutcome
	readErr, writeErr, commitErr error
}

func (memory *itemWorkMemory) WithItemWork(_ context.Context, run func(ItemWorkScope) error) error {
	if err := run(ItemWorkScope{Read: memory, Write: memory, Payload: payloadItemScope(memory)}); err != nil {
		return err
	}
	return memory.commitErr
}

func (memory *itemWorkMemory) Current(context.Context, string) (LeaseSnapshot, bool, error) {
	return memory.before.Execution, true, memory.readErr
}

func (memory *itemWorkMemory) Next(context.Context, string) (ExecutionItem, bool, error) {
	return memory.before.Item, true, memory.readErr
}

func (memory *itemWorkMemory) Item(context.Context, string) (OwnedItem, error) {
	return memory.before, memory.readErr
}

func (memory *itemWorkMemory) Claim(context.Context, ItemClaim) error {
	memory.claimed++
	return memory.writeErr
}

func (memory *itemWorkMemory) Resume(context.Context, ItemResume) error {
	memory.resumed++
	return memory.writeErr
}

func (memory *itemWorkMemory) Finish(_ context.Context, change ItemFinish) error {
	memory.finished++
	memory.outcome = change.Outcome
	return memory.writeErr
}

func newItemWorkMemory() *itemWorkMemory {
	execution := newExecutionMemory().before
	execution.Kind, execution.ImportState = "SERVER_EMULATIONSTATION_IMPORT", "RUNNING"
	return &itemWorkMemory{
		before: OwnedItem{
			Execution: execution,
			Item:      ExecutionItem{ID: "item", ImportID: "plan", State: "PENDING", Version: 1},
		},
	}
}

func itemWorkService(memory *itemWorkMemory) *ItemWork {
	return NewItemWork(memory, func() time.Time { return time.UnixMilli(2000) })
}

func TestItemWorkResumesExistingWorkingItemBeforeNewClaim(t *testing.T) {
	for _, state := range []string{"PENDING", "COPYING", "VALIDATING"} {
		t.Run(state, func(t *testing.T) {
			memory := newItemWorkMemory()
			memory.before.Item.State = state
			item, found, err := itemWorkService(memory).Next(t.Context(), memory.before.Execution.Execution)
			wantVersion, wantState, wantClaims := int64(1), state, 0
			if state == "PENDING" {
				wantVersion, wantState, wantClaims = 2, "COPYING", 1
			}
			if err != nil || !found || item.Version != wantVersion || item.State != wantState || memory.claimed != wantClaims {
				t.Fatalf("item=%#v found=%v error=%v claims=%d", item, found, err, memory.claimed)
			}
		})
	}
}

func TestItemWorkRejectsOriginalOwnerAndBudgetDrift(t *testing.T) {
	for _, mutation := range []string{"worker", "lease", "deadline", "item", "state"} {
		t.Run(mutation, func(t *testing.T) {
			memory := newItemWorkMemory()
			unit := memory.before.Execution.Execution
			switch mutation {
			case "worker":
				memory.before.Execution.WorkerID = "other"
			case "lease":
				memory.before.Execution.LeaseUntilMS = 2000
			case "deadline":
				memory.before.Execution.DeadlineAtMS = 2000
			case "item":
				memory.before.Item.ImportID = "other"
			case "state":
				memory.before.Execution.JobState = "CANCEL_REQUESTED"
			}
			if _, _, err := itemWorkService(memory).Next(t.Context(), unit); err == nil || memory.claimed != 0 {
				t.Fatalf("accepted mutation=%s error=%v", mutation, err)
			}
		})
	}
}

func TestItemWorkPreservesStorageCausesAndNoUncommittedClaim(t *testing.T) {
	cause := errors.New("item scope failed")
	for _, stage := range []string{"read", "write", "commit"} {
		t.Run(stage, func(t *testing.T) {
			memory := newItemWorkMemory()
			switch stage {
			case "read":
				memory.readErr = cause
			case "write":
				memory.writeErr = cause
			case "commit":
				memory.commitErr = cause
			}
			item, found, err := itemWorkService(memory).Next(t.Context(), memory.before.Execution.Execution)
			if !errors.Is(err, cause) || found || item.ID != "" {
				t.Fatalf("uncommitted item=%#v found=%v error=%v", item, found, err)
			}
		})
	}
}

func TestItemWorkOnlyFinishesOwnedOutcomeAndBoundReview(t *testing.T) {
	memory := newItemWorkMemory()
	memory.before.Item.State = "COPYING"
	service := itemWorkService(memory)
	unit := memory.before.Execution.Execution
	outcome := ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR", Retryable: true}
	if err := service.Finish(t.Context(), unit, "item", outcome); err != nil || memory.finished != 1 {
		t.Fatalf("finish=%v", err)
	}
	if err := service.Resume(
		t.Context(),
		unit,
		"item",
		"ordinary-job",
		"ordinary-item",
	); err == nil || memory.resumed != 0 {
		t.Fatalf("unbound resume=%v", err)
	}
	memory.before.Item.LibraryImportJobID, memory.before.Item.LibraryImportItemID = "ordinary-job", "ordinary-item"
	if err := service.Resume(
		t.Context(),
		unit,
		"item",
		"ordinary-job",
		"ordinary-item",
	); err != nil || memory.resumed != 1 {
		t.Fatalf("bound resume=%v", err)
	}
	if err := service.Finish(
		t.Context(),
		unit,
		"item",
		ItemOutcome{State: "CANCELLED", Code: "CANCELLED"},
	); !errors.Is(
		err,
		ErrInvalid,
	) {
		t.Fatalf("inline cancellation=%v", err)
	}
}
