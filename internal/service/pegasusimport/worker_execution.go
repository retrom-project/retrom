package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/pegasusimport"
)

var errWorkerCancellation = errors.New("pegasus execution cancellation requested")

// Run bounds an already claimed execution by its original persisted deadline.
// Cancellation observation does not authorize settlement; the settlement port must recheck ownership.
func (worker *Worker) Run(parent context.Context, unit model.Work) {
	ctx := parent
	if unit.DeadlineAtMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(parent, time.UnixMilli(unit.DeadlineAtMS))
		defer cancel()
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	if ctx.Err() != nil {
		return
	}
	if worker.checkCancellation(ctx, unit.Identity(), cancel) {
		done := make(chan struct{})
		go func() { defer close(done); worker.monitor(ctx, unit.Identity(), cancel) }()
		worker.dependencies.Executor.Execute(ctx, unit)
		cancel(nil)
		<-done
	}
	if errors.Is(context.Cause(ctx), errWorkerCancellation) {
		cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
		defer cancelCleanup()
		_, err := worker.dependencies.Cancellation.CloseCancelled(cleanup, unit.Identity())
		if err != nil {
			worker.report(context.WithoutCancel(parent), fmt.Errorf("settle interrupted Pegasus execution: %w", err))
		}
	}
}

func (worker *Worker) checkCancellation(
	ctx context.Context, id model.ExecutionIdentity, cancel context.CancelCauseFunc,
) bool {
	pending, err := worker.dependencies.Cancellation.Cancelled(ctx, id)
	if err != nil {
		worker.report(ctx, fmt.Errorf("observe Pegasus execution owner: %w", err))
		cancel(err)
		return false
	}
	if pending {
		cancel(errWorkerCancellation)
		return false
	}
	return ctx.Err() == nil
}

func (worker *Worker) monitor(ctx context.Context, id model.ExecutionIdentity, cancel context.CancelCauseFunc) {
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
		if !worker.checkCancellation(ctx, id, cancel) {
			return
		}
		if renew {
			if err := worker.dependencies.Leases.Renew(ctx, id); err != nil {
				worker.report(ctx, fmt.Errorf("renew Pegasus execution lease: %w", err))
				cancel(err)
				return
			}
		}
	}
}
