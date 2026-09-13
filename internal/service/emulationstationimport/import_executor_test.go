package emulationstationimport

import (
	"context"
	"errors"
	"testing"
)

type importExecutionMemory struct {
	item                         ExecutionItem
	next, source, review, finish int
	completed                    bool
	sourceErr, finishErr         error
}

func (memory *importExecutionMemory) Next(context.Context, Execution) (ExecutionItem, bool, error) {
	memory.next++
	return memory.item, memory.finish == 0, nil
}

func (memory *importExecutionMemory) Finish(_ context.Context, _ Execution, _ string, _ ItemOutcome) error {
	memory.finish++
	return memory.finishErr
}

func (memory *importExecutionMemory) Copy(context.Context, Execution, MaterialSource, VerifiedBlob) (string, error) {
	return "copied", nil
}

func (memory *importExecutionMemory) Warning(context.Context, Execution, MaterialSource, string) error {
	return nil
}

func (memory *importExecutionMemory) SetPhase(context.Context, Execution, string) error { return nil }

func (memory *importExecutionMemory) CopyFile(context.Context, Execution, ExecutionFile) (VerifiedBlob, error) {
	memory.source++
	return VerifiedBlob{SHA256: "copy", Size: 1}, memory.sourceErr
}

func (memory *importExecutionMemory) CopyAsset(context.Context, Execution, ExecutionAsset) (VerifiedBlob, bool, error) {
	return VerifiedBlob{}, false, nil
}

func (memory *importExecutionMemory) Resume(context.Context, Execution, ExecutionItem) (bool, error) {
	return false, nil
}

func (memory *importExecutionMemory) Create(context.Context, Execution, ExecutionItem) error {
	memory.review++
	memory.finish++
	return nil
}

func (memory *importExecutionMemory) Observe(context.Context, Execution) (LeaseState, error) {
	return LeaseActive, nil
}

func (memory *importExecutionMemory) CloseCancelled(context.Context, Execution) (bool, error) {
	return false, nil
}
func (memory *importExecutionMemory) Sanitize(error) string      { return "source read failed" }
func (memory *importExecutionMemory) DatabaseCause(error) string { return "" }

type importCompletionMemory struct{ memory *importExecutionMemory }

func (completion importCompletionMemory) Finish(context.Context, Execution) error {
	completion.memory.completed = true
	return nil
}

func newImportExecutionMemory() (*importExecutionMemory, *ImportExecutor) {
	memory := &importExecutionMemory{
		item: ExecutionItem{
			ID:    "item",
			State: "COPYING",
			Files: []ExecutionFile{{Path: "game.nes", Size: 1, Facts: "frozen", State: "DISCOVERED"}},
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
	err := executor.Execute(t.Context(), Execution{})
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
		t.Context(),
		Execution{},
	); err != nil || memory.source != 1 || memory.review != 1 || memory.next != 2 || !memory.completed {
		t.Fatalf("error=%v memory=%#v", err, memory)
	}
}

func TestImportExecutorStopsOnCancelledSourceWithoutTerminalizingItem(t *testing.T) {
	memory, executor := newImportExecutionMemory()
	memory.sourceErr = context.Canceled
	err := executor.Execute(t.Context(), Execution{})
	if !errors.Is(err, context.Canceled) || memory.finish != 0 || memory.review != 0 || memory.next != 1 {
		t.Fatalf("error=%v memory=%#v", err, memory)
	}
}
