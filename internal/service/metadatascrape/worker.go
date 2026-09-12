package metadatascrape

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const metadataExecutionTimeout = time.Hour

type Worker struct {
	repository WorkerRepository
	processor  WorkerProcessor
	now        func() time.Time
}

func NewWorker(repository WorkerRepository, processor WorkerProcessor, now func() time.Time) *Worker {
	return &Worker{repository: repository, processor: processor, now: now}
}

func (worker *Worker) Run(parent context.Context, runID string) error {
	run, err := worker.repository.Run(parent, runID)
	if err != nil {
		return fmt.Errorf("read metadata execution: %w", err)
	}
	if run.Provider == "NONE" || run.State != "RUNNING" {
		return nil
	}
	claim := WorkerClaim{RunID: runID, JobID: run.JobID, ExecutionNo: run.ExecutionNo}
	if run.JobState == "CANCELLED" {
		return worker.settle(parent, claim, 0, "", nil)
	}
	workerID, err := scheduleID()
	if err != nil {
		return err
	}
	claim.WorkerID = workerID
	claimed := false
	err = worker.repository.WithWrite(parent, func(scope WorkerScope) error {
		claim.Now = worker.now().UnixMilli()
		claim.Deadline = claim.Now + metadataExecutionTimeout.Milliseconds()
		var err error
		claimed, err = scope.Leases.Claim(parent, claim)
		if err != nil {
			return fmt.Errorf("claim scrape lease: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("claim metadata execution: %w", err)
	}
	if !claimed {
		return nil
	}
	ctx, deadlineCancel := context.WithTimeout(parent, metadataExecutionTimeout)
	defer deadlineCancel()
	ctx, cancel := context.WithCancelCause(ctx)
	stopped := make(chan struct{})
	go worker.heartbeat(ctx, cancel, claim, stopped)
	defer func() { cancel(nil); <-stopped }()
	count, code, cause := worker.processor.Process(ctx, claim, run.Payload)
	if context.Cause(ctx) != nil {
		cause = errors.Join(cause, context.Cause(ctx))
		code = "METADATA_EXECUTION_INTERRUPTED"
	}
	return worker.settle(parent, claim, count, code, cause)
}

func (worker *Worker) heartbeat(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	claim WorkerClaim,
	stopped chan<- struct{},
) {
	defer close(stopped)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := false
			err := worker.repository.WithWrite(ctx, func(scope WorkerScope) error {
				var err error
				current, err = scope.Leases.Refresh(ctx, claim, worker.now().UnixMilli())
				if err != nil {
					return fmt.Errorf("renew scrape lease: %w", err)
				}
				return nil
			})
			if err != nil {
				cancel(fmt.Errorf("refresh metadata lease: %w", err))
				return
			}
			if !current {
				cancel(ErrExecutionLost)
				return
			}
		}
	}
}
