package payloadrelease

import (
	"context"
	"fmt"
	"math"
	"time"
)

func NewWorker(repository WorkerRepository, executor WorkExecutor, options WorkerOptions) *Worker {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = NewScheduler(nil).Identity
	}
	return &Worker{
		repository: repository, executor: executor, now: options.Now, newID: options.NewID,
		report: options.Report, maintain: options.Maintain,
		active: make(map[*workerRun]struct{}), wake: make(chan struct{}, 1),
	}
}

func (worker *Worker) Claim(ctx context.Context) (Work, bool, error) {
	var claimed Work
	err := worker.repository.WithWorker(ctx, func(scope WorkerScope) error {
		now := worker.now().UnixMilli()
		before, found, err := scope.Read.Next(ctx, now)
		if err != nil {
			return fmt.Errorf("read next release work: %w", err)
		}
		if !found {
			return nil
		}
		if before.State != "QUEUED" || before.AvailableMS > now || !validWork(before) {
			return ErrExecutionLost
		}
		if failure := executionBudgetFailure(before, now); failure != nil {
			return worker.settle(ctx, scope, before, failure, now)
		}
		id, err := worker.newID()
		if err != nil {
			return fmt.Errorf("create payload worker identity: %w", err)
		}
		if id == "" {
			return ErrScheduleIDInvalid
		}
		after := before
		after.State = "RUNNING"
		after.WorkerID = id
		after.Attempt++
		after.Version++
		if !after.Started.Set {
			after.Started = WorkTime{Set: true, Value: now}
		}
		if !after.Deadline.Set {
			after.Deadline = WorkTime{Set: true, Value: after.Started.Value + ExecutionTimeout.Milliseconds()}
		}
		after.Lease = WorkTime{Set: true, Value: min(now+workerLease.Milliseconds(), after.Deadline.Value)}
		after.Heartbeat = WorkTime{Set: true, Value: now}
		change := WorkChange{
			Before: before, After: after, NowMS: now, EventType: "STARTED",
			EventJSON: fmt.Sprintf(`{"schemaVersion":1,"executionNo":%d,"attempt":%d}`, after.ExecutionNo, after.Attempt),
		}
		if err := scope.Write.Change(ctx, change); err != nil {
			return fmt.Errorf("claim release work: %w", err)
		}
		claimed = after
		return nil
	})
	if err != nil {
		return Work{}, false, fmt.Errorf("claim payload worker: %w", err)
	}
	return claimed, claimed.ID != "", nil
}

func validWork(work Work) bool {
	return work.ID != "" && work.Scope.ID != "" && (work.Kind == "PAYLOAD_RELEASE" || work.Kind == "BLOB_GC") &&
		work.ExecutionNo > 0 && work.Attempt >= 0 && work.MaxAttempts > 0 && work.Version > 0 && work.Version < math.MaxInt64
}

func executionBudgetFailure(work Work, now int64) error {
	if work.Deadline.Set && work.Deadline.Value <= now {
		return ErrExecutionTimeout
	}
	if work.Attempt >= work.MaxAttempts {
		return ErrAttemptsExhausted
	}
	return nil
}

func (worker *Worker) Recover(ctx context.Context) error {
	err := worker.repository.WithWorker(ctx, func(scope WorkerScope) error {
		now := worker.now().UnixMilli()
		pending, err := scope.Read.Interrupted(ctx, now, 100)
		if err != nil {
			return fmt.Errorf("read interrupted release work: %w", err)
		}
		for _, before := range pending {
			if before.State != "RUNNING" {
				continue
			}
			if before.Lease.Set && before.Lease.Value > now && before.Deadline.Set && before.Deadline.Value > now {
				continue
			}
			cause := executionBudgetFailure(before, now)
			if cause == nil {
				cause = ErrExecutionLost
			}
			if err := worker.settle(ctx, scope, before, cause, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("recover payload worker: %w", err)
	}
	return nil
}
