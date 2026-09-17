package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type executionDispatchMemory struct {
	rootErr, runErr, failErr error
	ran, failed, recovered   int
	failure                  model.ExecutionFailure
	reported                 error
}

func (memory *executionDispatchMemory) CheckRoot(model.Execution) error { return memory.rootErr }
func (memory *executionDispatchMemory) Execute(context.Context, model.Execution) error {
	memory.ran++
	return memory.runErr
}

func (memory *executionDispatchMemory) Fail(
	_ context.Context,
	_ model.Execution,
	failure model.ExecutionFailure,
) (string, error) {
	memory.failed++
	memory.failure = failure
	return "FAILED", memory.failErr
}

func (memory *executionDispatchMemory) Recover(context.Context) error { memory.recovered++; return nil }

func (memory *executionDispatchMemory) dispatcher() *ExecutionDispatcher {
	return NewExecutionDispatcher(ExecutionDispatcherDependencies{Roots: memory, Scans: memory, Imports: memory, Control: memory, Recovery: memory, Report: func(err error) { memory.reported = err }}, func() time.Time { return time.UnixMilli(2000) })
}

func TestExecutionDispatcherKeepsStoppingAuthorityWithoutFailureWrite(t *testing.T) {
	for _, cause := range []error{
		context.Canceled,
		model.ErrVersionConflict,
		ErrExecutionCancelled,
	} {
		t.Run(
			cause.Error(),
			func(t *testing.T) {
				memory := &executionDispatchMemory{runErr: cause}
				memory.dispatcher().Execute(t.Context(), model.Execution{Kind: "SERVER_EMULATIONSTATION_IMPORT", DeadlineAtMS: 5000})
				if memory.failed != 0 || memory.ran != 1 {
					t.Fatalf("run=%d failures=%d", memory.ran, memory.failed)
				}
			},
		)
	}
}

func TestExecutionDispatcherPreservesScanFailureAndRecoversExpiredOwner(t *testing.T) {
	cause := errors.New("scan unavailable")
	memory := &executionDispatchMemory{
		runErr:  &ExecutionError{Failure: model.ExecutionFailure{Code: "TEST_READ", Retryable: true}, Cause: cause},
		failErr: model.ErrExpired,
	}
	memory.dispatcher().Execute(t.Context(), model.Execution{Kind: "SERVER_EMULATIONSTATION_SCAN", DeadlineAtMS: 5000})
	if memory.failed != 1 || memory.failure.Code != "TEST_READ" || !memory.failure.Retryable || memory.recovered != 1 || !errors.Is(
		memory.reported,
		cause,
	) {
		t.Fatalf(
			"memory=%#v",
			memory,
		)
	}
}

func TestExecutionDispatcherRejectsRootAndDeadlineWithStableReasons(t *testing.T) {
	for _, kind := range []string{"root", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			memory := &executionDispatchMemory{}
			unit := model.Execution{Kind: "SERVER_EMULATIONSTATION_IMPORT", DeadlineAtMS: 5000}
			expected := "SERVER_IMPORT_ROOT_CHANGED"
			if kind == "root" {
				memory.rootErr = ErrExecutionRootChanged
			} else {
				unit.DeadlineAtMS = 2000
				memory.runErr = model.ErrExpired
				expected = "EMULATIONSTATION_EXECUTION_TIMEOUT"
			}
			memory.dispatcher().Execute(t.Context(), unit)
			if memory.failed != 1 || memory.failure.Code != expected || memory.failure.Retryable {
				t.Fatalf("memory=%#v", memory)
			}
		})
	}
}
