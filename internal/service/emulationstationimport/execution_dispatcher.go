package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrExecutionRootChanged = errors.New("SERVER_IMPORT_ROOT_CHANGED")

type (
	ExecutionRoots  interface{ CheckRoot(Execution) error }
	ExecutionRunner interface {
		Execute(context.Context, Execution) error
	}
)

type ExecutionFailer interface {
	Fail(context.Context, Execution, ExecutionFailure) (string, error)
}
type (
	ExecutionRecoverer              interface{ Recover(context.Context) error }
	ExecutionDispatcherDependencies struct {
		Roots          ExecutionRoots
		Scans, Imports ExecutionRunner
		Control        ExecutionFailer
		Recovery       ExecutionRecoverer
		Report         func(error)
	}
)

type ExecutionDispatcher struct {
	dependencies ExecutionDispatcherDependencies
	now          func() time.Time
}

func NewExecutionDispatcher(
	dependencies ExecutionDispatcherDependencies,
	now func() time.Time,
) *ExecutionDispatcher {
	return &ExecutionDispatcher{dependencies: dependencies, now: now}
}

func (dispatcher *ExecutionDispatcher) Execute(ctx context.Context, unit Execution) {
	if err := dispatcher.dependencies.Roots.CheckRoot(unit); err != nil {
		dispatcher.Failed(ctx, unit, err)
		return
	}
	var err error
	if unit.Kind == "SERVER_EMULATIONSTATION_SCAN" {
		err = dispatcher.dependencies.Scans.Execute(
			ctx,
			unit,
		)
	} else {
		err = dispatcher.dependencies.Imports.Execute(
			ctx,
			unit,
		)
	}
	if err != nil {
		dispatcher.Failed(ctx, unit, err)
	}
}

func (dispatcher *ExecutionDispatcher) Failed(ctx context.Context, unit Execution, cause error) {
	now := dispatcher.now().UnixMilli()
	if executionStopped(ctx, unit, now, cause) {
		if dispatcher.dependencies.Report != nil {
			dispatcher.dependencies.Report(
				fmt.Errorf("stop EmulationStation execution: %w", cause),
			)
		}
		return
	}
	failure := executionFailure(cause)
	if errors.Is(
		ctx.Err(),
		context.DeadlineExceeded,
	) || unit.DeadlineAtMS > 0 && unit.DeadlineAtMS <= now || errors.Is(
		cause,
		ErrExpired,
	) {
		failure = ExecutionFailure{Code: "EMULATIONSTATION_EXECUTION_TIMEOUT"}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_, err := dispatcher.dependencies.Control.Fail(cleanup, unit, failure)
	if errors.Is(err, ErrExpired) {
		err = dispatcher.dependencies.Recovery.Recover(cleanup)
	}
	if errors.Is(err, ErrVersionConflict) {
		err = nil
	}
	if dispatcher.dependencies.Report != nil {
		dispatcher.dependencies.Report(
			fmt.Errorf("execute EmulationStation workflow: %w", errors.Join(cause, err)),
		)
	}
}

func executionStopped(ctx context.Context, unit Execution, now int64, cause error) bool {
	return errors.Is(cause, ErrVersionConflict) || errors.Is(cause, ErrExecutionCancelled) ||
		errors.Is(cause, context.Canceled) ||
		errors.Is(ctx.Err(), context.Canceled) || ctx.Err() != nil && unit.DeadlineAtMS > now
}

func executionFailure(cause error) ExecutionFailure {
	if errors.Is(cause, ErrExecutionRootChanged) {
		return ExecutionFailure{Code: ErrExecutionRootChanged.Error()}
	}
	var failure *ExecutionError
	if errors.As(cause, &failure) {
		return failure.Failure
	}
	return ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true}
}
