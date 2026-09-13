package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"retrom/internal/capability/security/authn"
)

func (worker *ImportWorker) Run(parent context.Context, work ImportWork) error {
	ctx, done, err := worker.register(parent, work.Execution.JobID)
	if err != nil {
		return err
	}
	return worker.runRegistered(ctx, work, done)
}

func (worker *ImportWorker) runClaimed(parent context.Context, work ImportWork) error {
	ctx, done, err := worker.register(parent, work.Execution.JobID)
	if err != nil {
		if errors.Is(err, ErrImportWorkerClosed) {
			return worker.settle(parent, work, err)
		}
		return err
	}
	return worker.runRegistered(ctx, work, done)
}

func (worker *ImportWorker) runRegistered(parent context.Context, work ImportWork, done func()) error {
	ctx := parent
	defer done()
	remaining := time.Duration(work.Execution.DeadlineMS-worker.settings.Now().UnixMilli()) * time.Millisecond
	bounded, cancelDeadline := context.WithTimeout(ctx, max(remaining, 0))
	defer cancelDeadline()
	execution, cancel := context.WithCancelCause(bounded)
	monitorDone := make(chan struct{})
	go func() { defer close(monitorDone); worker.monitor(execution, cancel, work.Execution) }()
	err := worker.execute(execution, work)
	if err != nil && context.Cause(execution) != nil {
		err = context.Cause(execution)
	}
	cancel(nil)
	<-monitorDone
	if err != nil {
		return worker.settle(parent, work, err)
	}
	return nil
}

func (worker *ImportWorker) execute(ctx context.Context, work ImportWork) error {
	if work.Execution.ActorUserID != "" {
		ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: work.Execution.ActorUserID})
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start import execution: %w", err)
	}
	plan, err := worker.dependencies.Preparation.Prepare(ctx, work.Request)
	if err != nil {
		return fmt.Errorf("prepare import execution: %w", err)
	}
	if err := worker.dependencies.Control.Progress(ctx, work.Execution, len(plan.Groups)); err != nil {
		return fmt.Errorf("record prepared import: %w", err)
	}
	result, err := worker.dependencies.Creations.CommitPrepared(ctx, plan, ImportCreationOptions{Queued: &work.Execution})
	if err != nil {
		return fmt.Errorf("commit import execution: %w", err)
	}
	if result.Created.JobID != work.Execution.JobID || result.Created.ImportJobID != work.Execution.ImportID {
		return ErrInvalid
	}
	return nil
}

func (worker *ImportWorker) monitor(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	execution QueuedImportExecution,
) {
	ticker := time.NewTicker(ImportExecutionHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cancelled, err := worker.dependencies.Control.Renew(ctx, execution)
			if err != nil {
				cancel(err)
				return
			}
			if cancelled {
				cancel(context.Canceled)
				return
			}
		}
	}
}

func (worker *ImportWorker) settle(parent context.Context, work ImportWork, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer cancel()
	if err := worker.dependencies.Control.Fail(ctx, work.Execution, cause); err != nil {
		return errors.Join(cause, fmt.Errorf("settle import execution: %w", err))
	}
	return fmt.Errorf("import execution stopped: %w", cause)
}
