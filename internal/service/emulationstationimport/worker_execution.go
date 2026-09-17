package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

var (
	errWorkerCancellation = errors.New("EmulationStation execution cancellation requested")
	errWorkerDeadline     = errors.New("EmulationStation original execution deadline reached")
)

// Run retains the persisted execution budget in the same clock domain as lease checks.
// Monitors stop I/O; settlement independently rechecks the complete current authority.
func (worker *Worker) Run(parent context.Context, unit model.Execution) {
	if parent.Err() != nil {
		return
	}
	remaining := unit.DeadlineAtMS - worker.now().UnixMilli()
	if remaining <= 0 {
		worker.finishDeadline(parent, unit)
		return
	}
	if remaining > math.MaxInt64/int64(time.Millisecond) {
		worker.report(parent, model.ErrInvalid)
		return
	}
	bounded, cancelBudget := context.WithTimeoutCause(parent, time.Duration(remaining)*time.Millisecond, errWorkerDeadline)
	defer cancelBudget()
	ctx, cancel := context.WithCancelCause(bounded)
	defer cancel(nil)
	if worker.checkExecution(ctx, unit, cancel) {
		done := make(chan struct{})
		go func() { defer close(done); worker.monitor(ctx, unit, cancel) }()
		worker.dependencies.Executor.Execute(ctx, unit)
		if ctx.Err() == nil {
			worker.checkExecution(ctx, unit, cancel)
		}
		cancel(nil)
		<-done
	}
	switch {
	case errors.Is(context.Cause(ctx), errWorkerCancellation):
		worker.finishCancellation(parent, unit)
	case errors.Is(context.Cause(ctx), errWorkerDeadline):
		worker.finishDeadline(parent, unit)
	}
}

func (worker *Worker) checkExecution(ctx context.Context, unit model.Execution, cancel context.CancelCauseFunc) bool {
	state, err := worker.dependencies.Control.Observe(ctx, unit)
	if err != nil {
		worker.report(ctx, fmt.Errorf("observe EmulationStation execution owner: %w", err))
		cancel(err)
		return false
	}
	return worker.observeState(ctx, state, cancel)
}

func (worker *Worker) observeState(ctx context.Context, state model.LeaseState, cancel context.CancelCauseFunc) bool {
	switch state {
	case model.LeaseActive:
		return ctx.Err() == nil
	case model.LeaseCancelled:
		cancel(errWorkerCancellation)
	case model.LeaseDeadline:
		cancel(errWorkerDeadline)
	case model.LeaseLost:
		cancel(model.ErrVersionConflict)
	default:
		cancel(model.ErrVersionConflict)
	}
	return false
}

func (worker *Worker) monitor(ctx context.Context, unit model.Execution, cancel context.CancelCauseFunc) {
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		renew := false
		select {
		case <-ctx.Done():
			return
		case <-worker.cancellationWake:
		case <-poll.C:
		case <-heartbeat.C:
			renew = true
		}
		if !worker.checkExecution(ctx, unit, cancel) {
			return
		}
		if renew && !worker.renew(ctx, unit, cancel) {
			return
		}
	}
}

func (worker *Worker) renew(ctx context.Context, unit model.Execution, cancel context.CancelCauseFunc) bool {
	state, err := worker.dependencies.Leases.Renew(ctx, unit)
	if err != nil {
		worker.report(ctx, fmt.Errorf("renew EmulationStation worker lease: %w", err))
		cancel(err)
		return false
	}
	return worker.observeState(ctx, state, cancel)
}

func (worker *Worker) finishCancellation(parent context.Context, unit model.Execution) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer cancel()
	_, err := worker.dependencies.Control.CloseCancelled(cleanup, unit)
	if err != nil {
		worker.report(context.WithoutCancel(parent), fmt.Errorf("settle cancelled EmulationStation worker: %w", err))
	}
}

func (worker *Worker) finishDeadline(parent context.Context, unit model.Execution) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer cancel()
	_, err := worker.dependencies.Control.Fail(cleanup, unit, model.ExecutionFailure{Code: "EMULATIONSTATION_EXECUTION_TIMEOUT"})
	if errors.Is(err, model.ErrExpired) {
		err = worker.dependencies.Maintenance.Maintain(cleanup)
	}
	if err != nil {
		worker.report(context.WithoutCancel(parent), fmt.Errorf("settle expired EmulationStation worker: %w", err))
	}
}
