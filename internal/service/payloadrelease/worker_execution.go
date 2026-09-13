package payloadrelease

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (worker *Worker) RunOnce(parent context.Context) (bool, error) {
	ctx, done, accepted := worker.register(parent)
	if !accepted {
		return false, ErrWorkerClosed
	}
	defer done()
	unit, found, err := worker.Claim(ctx)
	if err != nil || !found {
		return false, err
	}
	input, err := DecodeWork(unit)
	if err == nil {
		err = worker.execute(ctx, Execution{Work: unit, Input: input})
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finishErr := worker.Finish(cleanup, unit, err)
	if errors.Is(finishErr, ErrExecutionLost) {
		finishErr = errors.Join(finishErr, worker.Recover(cleanup))
	}
	return true, errors.Join(err, finishErr)
}

func (worker *Worker) execute(parent context.Context, execution Execution) error {
	remaining := execution.Work.Deadline.Value - worker.now().UnixMilli()
	if remaining <= 0 {
		return ErrExecutionTimeout
	}
	timed, timeout := context.WithTimeoutCause(parent, time.Duration(remaining)*time.Millisecond, ErrExecutionTimeout)
	defer timeout()
	ctx, cancel := context.WithCancelCause(timed)
	defer cancel(context.Canceled)
	monitorDone := make(chan error, 1)
	go func() { monitorDone <- worker.monitor(ctx, cancel, execution.Work) }()
	err := worker.executor.Execute(ctx, execution)
	cause := context.Cause(ctx)
	cancel(context.Canceled)
	monitorErr := <-monitorDone
	if failure := errors.Join(err, cause, monitorErr); failure != nil {
		return fmt.Errorf("execute payload work: %w", failure)
	}
	return nil
}

func (worker *Worker) monitor(ctx context.Context, cancel context.CancelCauseFunc, unit Work) error {
	observe := time.NewTicker(time.Second)
	heartbeat := time.NewTicker(workerHeartbeat)
	defer observe.Stop()
	defer heartbeat.Stop()
	for {
		var err error
		select {
		case <-ctx.Done():
			return nil
		case <-heartbeat.C:
			err = worker.Renew(ctx, unit)
		case <-observe.C:
			err = worker.repository.WithWorker(ctx, func(scope WorkerScope) error {
				return worker.CheckInScope(ctx, scope, unit)
			})
		}
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			failure := fmt.Errorf("observe payload worker: %w", err)
			cancel(failure)
			return failure
		}
	}
}
