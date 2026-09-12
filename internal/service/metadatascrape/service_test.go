package metadatascrape

import (
	"context"
	"errors"
	"testing"
	"time"
)

type serviceRunner struct {
	runID string
	cause error
}

func (runner *serviceRunner) Run(_ context.Context, id string) error {
	runner.runID = id
	return runner.cause
}

func TestServiceDelegatesExecutionAndPreservesWorkerFailure(t *testing.T) {
	runner := &serviceRunner{cause: context.Canceled}
	service := New(nil, runner, time.Now)
	if err := service.Run(t.Context(), "run"); !errors.Is(err, context.Canceled) {
		t.Fatalf("worker cause: %v", err)
	}
	if runner.runID != "run" {
		t.Fatalf("executed %q", runner.runID)
	}
}
