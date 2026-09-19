package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type itemWorkMemory struct {
	before                       model.OwnedItem
	claimed, resumed, finished   int
	outcome                      model.ItemOutcome
	readErr, writeErr, commitErr error
}

func (memory *itemWorkMemory) ClaimNextItem(ctx context.Context, unit model.Execution, nowMS int64) (model.ClaimNextItemResult, error) {
	if memory.readErr != nil {
		return model.ClaimNextItemResult{}, memory.readErr
	}
	execution := memory.before.Execution
	if execution.Execution != unit {
		return model.ClaimNextItemResult{}, model.ErrVersionConflict
	}
	if err := model.ValidateImportExecution(execution, unit, nowMS); err != nil {
		return model.ClaimNextItemResult{}, err
	}
	item := memory.before.Item
	if item.ImportID != unit.ImportID || !model.ValidItemVersion(item.Version) || !model.WorkingItemState(item.State) {
		return model.ClaimNextItemResult{}, model.ErrVersionConflict
	}
	if item.State != "PENDING" {
		return model.ClaimNextItemResult{Found: true, Item: item}, memory.commitErr
	}
	if memory.writeErr != nil {
		return model.ClaimNextItemResult{}, memory.writeErr
	}
	memory.claimed++
	item.State, item.Version = "COPYING", item.Version+1
	return model.ClaimNextItemResult{Found: true, Item: item}, memory.commitErr
}

func (memory *itemWorkMemory) CommitItemResume(ctx context.Context, unit model.Execution, itemID, jobID, ordinaryID string, nowMS int64) error {
	if memory.readErr != nil {
		return memory.readErr
	}
	if err := model.ValidateImportExecution(memory.before.Execution, unit, nowMS); err != nil {
		return err
	}
	if memory.before.Item.ID != itemID || memory.before.Item.ImportID != unit.ImportID || !model.ValidItemVersion(memory.before.Item.Version) {
		return model.ErrVersionConflict
	}
	if jobID == "" || ordinaryID == "" || memory.before.Item.LibraryImportJobID != jobID || memory.before.Item.LibraryImportItemID != ordinaryID {
		return model.ErrVersionConflict
	}
	if memory.before.Item.State == "VALIDATING" || memory.before.Item.State == "REVIEW_PENDING" {
		return memory.commitErr
	}
	if memory.before.Item.State != "COPYING" {
		return model.ErrVersionConflict
	}
	if memory.writeErr != nil {
		return memory.writeErr
	}
	memory.resumed++
	return memory.commitErr
}

func (memory *itemWorkMemory) CommitItemFinish(ctx context.Context, unit model.Execution, itemID string, outcome model.ItemOutcome, nowMS int64) error {
	if memory.readErr != nil {
		return memory.readErr
	}
	if err := model.ValidateImportExecution(memory.before.Execution, unit, nowMS); err != nil {
		return err
	}
	if memory.before.Item.ID != itemID || memory.before.Item.ImportID != unit.ImportID || !model.ValidItemVersion(memory.before.Item.Version) {
		return model.ErrVersionConflict
	}
	if memory.before.Item.State != "COPYING" && memory.before.Item.State != "VALIDATING" {
		return model.ErrVersionConflict
	}
	if memory.writeErr != nil {
		return memory.writeErr
	}
	memory.finished++
	memory.outcome = outcome
	return memory.commitErr
}

func newItemWorkMemory() *itemWorkMemory {
	execution := newExecutionMemory().before
	execution.Kind, execution.ImportState = "SERVER_EMULATIONSTATION_IMPORT", "RUNNING"
	return &itemWorkMemory{
		before: model.OwnedItem{
			Execution: execution,
			Item:      model.ExecutionItem{ID: "item", ImportID: "plan", State: "PENDING", Version: 1},
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
	outcome := model.ItemOutcome{State: "COMMIT_FAILED", Code: "INTERNAL_ERROR", Retryable: true}
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
		model.ItemOutcome{State: "CANCELLED", Code: "CANCELLED"},
	); !errors.Is(
		err,
		model.ErrInvalid,
	) {
		t.Fatalf("inline cancellation=%v", err)
	}
}
