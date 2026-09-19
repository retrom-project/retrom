package emulationstationimport

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/emulationstationimport"
)

type importExecutionMemory struct {
	item                         model.ExecutionItem
	next, source, review, finish int
	completed                    bool
	sourceErr, finishErr         error
}

func (memory *importExecutionMemory) Next(context.Context, model.Execution) (model.ExecutionItem, bool, error) {
	memory.next++
	return memory.item, memory.finish == 0, nil
}

func (memory *importExecutionMemory) Finish(_ context.Context, _ model.Execution, _ string, _ model.ItemOutcome) error {
	memory.finish++
	return memory.finishErr
}

func (memory *importExecutionMemory) Copy(context.Context, model.Execution, model.MaterialSource, model.VerifiedBlob) (string, error) {
	return "copied", nil
}

func (memory *importExecutionMemory) Warning(context.Context, model.Execution, model.MaterialSource, string) error {
	return nil
}

func (memory *importExecutionMemory) SetPhase(context.Context, model.Execution, string) error {
	return nil
}

func (memory *importExecutionMemory) CopyFile(context.Context, model.Execution, model.ExecutionFile) (model.VerifiedBlob, error) {
	memory.source++
	return model.VerifiedBlob{SHA256: "copy", Size: 1}, memory.sourceErr
}

func (memory *importExecutionMemory) CopyAsset(context.Context, model.Execution, model.ExecutionAsset) (model.VerifiedBlob, bool, error) {
	return model.VerifiedBlob{}, false, nil
}

func (memory *importExecutionMemory) Resume(context.Context, model.Execution, model.ExecutionItem) (bool, error) {
	return false, nil
}

func (memory *importExecutionMemory) Create(context.Context, model.Execution, model.ExecutionItem) error {
	memory.review++
	memory.finish++
	return nil
}

func (memory *importExecutionMemory) Observe(context.Context, model.Execution) (model.LeaseState, error) {
	return model.LeaseActive, nil
}

func (memory *importExecutionMemory) CloseCancelled(context.Context, model.Execution) (bool, error) {
	return false, nil
}
func (memory *importExecutionMemory) Sanitize(error) string      { return "source read failed" }
func (memory *importExecutionMemory) DatabaseCause(error) string { return "" }

type importCompletionMemory struct{ memory *importExecutionMemory }

func (completion importCompletionMemory) Finish(context.Context, model.Execution) error {
	completion.memory.completed = true
	return nil
}

func newImportExecutionMemory() (*importExecutionMemory, *ImportExecutor) {
	memory := &importExecutionMemory{
		item: model.ExecutionItem{
			ID:    "item",
			State: "COPYING",
			Files: []model.ExecutionFile{{Path: "game.nes", Size: 1, Facts: "frozen", State: "DISCOVERED"}},
		},
	}
	return memory, NewImportExecutor(
		ImportExecutorDependencies{
			Items:       memory,
			Materials:   memory,
			Sources:     memory,
			Reviews:     memory,
			Control:     memory,
			Completion:  importCompletionMemory{memory: memory},
			Diagnostics: memory,
		},
	)
}

func TestImportExecutorStopsBeforeNextClaimWhenOutcomeWriteFails(t *testing.T) {
	memory, executor := newImportExecutionMemory()
	sourceCause := errors.New("source unreadable")
	writeCause := errors.New("outcome write failed")
	memory.sourceErr = sourceCause
	memory.finishErr = writeCause
	err := executor.Execute(t.Context(), model.Execution{})
	if !errors.Is(
		err,
		writeCause,
	) || !errors.Is(
		err,
		sourceCause,
	) || memory.next != 1 || memory.review != 0 || memory.completed {
		t.Fatalf("error=%v next=%d review=%d complete=%v", err, memory.next, memory.review, memory.completed)
	}
}

func TestImportExecutorCopiesHandsOffAndCompletes(t *testing.T) {
	memory, executor := newImportExecutionMemory()
	if err := executor.Execute(
		t.Context(), model.Execution{},
	); err != nil || memory.source != 1 || memory.review != 1 || memory.next != 2 || !memory.completed {
		t.Fatalf("error=%v memory=%#v", err, memory)
	}
}

func TestImportExecutorStopsOnCancelledSourceWithoutTerminalizingItem(t *testing.T) {
	memory, executor := newImportExecutionMemory()
	memory.sourceErr = context.Canceled
	err := executor.Execute(t.Context(), model.Execution{})
	if !errors.Is(err, context.Canceled) || memory.finish != 0 || memory.review != 0 || memory.next != 1 {
		t.Fatalf("error=%v memory=%#v", err, memory)
	}
}
