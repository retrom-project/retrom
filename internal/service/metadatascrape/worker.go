package metadatascrape

import (
	"context"
	"errors"
	"fmt"
	"time"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

type Worker struct {
	repository metadatascrapemodel.WorkerRepository
	processor  metadatascrapemodel.WorkerProcessor
	now        func() time.Time
}

func NewWorker(
	repository metadatascrapemodel.WorkerRepository,
	processor metadatascrapemodel.WorkerProcessor,
	now func() time.Time,
) *Worker {
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
	claim := metadatascrapemodel.WorkerClaim{RunID: runID, JobID: run.JobID, ExecutionNo: run.ExecutionNo}
	if run.JobState == "CANCELLED" {
		return worker.settle(parent, claim, 0, "", nil)
	}
	claim, claimed, err := worker.claim(parent, run)
	if err != nil || !claimed {
		return err
	}
	if claim.Terminal {
		if run.JobState == "CANCEL_REQUESTED" {
			return worker.settle(parent, claim, 0, "", nil)
		}
		code := "METADATA_EXECUTION_EXPIRED"
		cause := context.DeadlineExceeded
		if run.Deadline == 0 || run.Deadline > claim.Now {
			code = "METADATA_ATTEMPTS_EXHAUSTED"
			cause = metadatascrapemodel.ErrAttemptsExhausted
		}
		return worker.settle(parent, claim, 0, code, cause)
	}
	remaining := time.Duration(claim.Deadline-worker.now().UnixMilli()) * time.Millisecond
	ctx, deadlineCancel := context.WithTimeout(parent, remaining)
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
	claim metadatascrapemodel.WorkerClaim, stopped chan<- struct{},
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
			err := worker.repository.WithWrite(ctx, func(scope metadatascrapemodel.WorkerScope) error {
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
				cancel(metadatascrapemodel.ErrExecutionLost)
				return
			}
		}
	}
}
