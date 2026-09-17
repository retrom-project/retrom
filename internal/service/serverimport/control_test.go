package serverimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/serverimport"
)

type controlMemory struct {
	cancelResult model.CancelResult
	retryResult  model.Summary
	cancelCmd    model.CancelCommand
	retryCmd     model.RetryCommand
	writes       int
	cancelErr    error
	retryErr     error
}

func (memory *controlMemory) CommitCancel(
	_ context.Context, cmd model.CancelCommand,
) (model.CancelResult, error) {
	memory.cancelCmd = cmd
	if memory.cancelErr != nil {
		return model.CancelResult{}, memory.cancelErr
	}
	memory.writes++
	return memory.cancelResult, nil
}

func (memory *controlMemory) CommitRetry(
	_ context.Context, cmd model.RetryCommand,
) (model.Summary, error) {
	memory.retryCmd = cmd
	if memory.retryErr != nil {
		return model.Summary{}, memory.retryErr
	}
	memory.writes++
	return memory.retryResult, nil
}

func controlFixture() (*Control, *controlMemory) {
	memory := &controlMemory{
		cancelResult: model.CancelResult{
			Summary: model.Summary{
				ID: "import", State: "CANCELLED", Version: 4,
			},
			Pending: false,
		},
		retryResult: model.Summary{
			ID: "import", State: "QUEUED", Version: 4,
		},
	}
	return NewControl(
		memory,
		map[string]string{"root": "root-digest"},
		func() time.Time { return time.UnixMilli(100) },
	), memory
}

func TestQueuedCancellationDelegatesToRepo(t *testing.T) {
	service, memory := controlFixture()
	result, pending, err := service.Cancel(
		t.Context(), "import", 3, " stop ", "actor",
	)
	if err != nil {
		t.Fatal(err)
	}
	if pending || result.ID != "import" || result.State != "CANCELLED" {
		t.Fatalf("cancel result: %+v pending=%v", result, pending)
	}
	if memory.cancelCmd.Reason != "stop" {
		t.Fatalf("reason not trimmed: %q", memory.cancelCmd.Reason)
	}
}

func TestCancellationRejectsInvalidReasons(t *testing.T) {
	service, memory := controlFixture()
	_, _, err := service.Cancel(t.Context(), "import", 3, "", "actor")
	if !errors.Is(err, model.ErrNotCancellable) || memory.writes != 0 {
		t.Fatalf("empty reason: %v", err)
	}
}

func TestRetryDelegatesToRepo(t *testing.T) {
	service, memory := controlFixture()
	result, err := service.Retry(t.Context(), "import", 3, "actor")
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 4 || memory.retryCmd.ValidRoots["root"] != "root-digest" {
		t.Fatalf("retry: %+v cmd=%+v", result, memory.retryCmd)
	}
}

func TestControlCommitFailureReturnsNoSuccessfulSummary(t *testing.T) {
	service, memory := controlFixture()
	memory.retryErr = context.Canceled
	result, err := service.Retry(t.Context(), "import", 3, "actor")
	if !errors.Is(err, context.Canceled) || result.ID != "" {
		t.Fatalf("late retry: %+v %v", result, err)
	}
	memory.cancelErr = context.Canceled
	result, pending, err := service.Cancel(
		t.Context(), "import", 3, "stop", "actor",
	)
	if !errors.Is(err, context.Canceled) || result.ID != "" || pending {
		t.Fatalf("late cancel: %+v %v", result, err)
	}
}
