package payloadrelease

import (
	"context"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/payloadrelease"
)

func NewWorker(repository model.WorkerRepository, executor model.WorkExecutor, options WorkerOptions) *Worker {
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

func (worker *Worker) Claim(ctx context.Context) (model.Work, bool, error) {
	now := worker.now().UnixMilli()
	before, found, err := worker.repository.LoadNextWork(ctx, now)
	if err != nil {
		return model.Work{}, false, fmt.Errorf("claim payload worker: read next: %w", err)
	}
	if !found {
		return model.Work{}, false, nil
	}
	if before.State != "QUEUED" || before.AvailableMS > now || !validWork(before) {
		return model.Work{}, false, fmt.Errorf("claim payload worker: %w", model.ErrExecutionLost)
	}
	if failure := executionBudgetFailure(before, now); failure != nil {
		change, err := worker.settlement(before, failure, now)
		if err != nil {
			return model.Work{}, false, fmt.Errorf("claim payload worker: %w", err)
		}
		if err := worker.prepareOwnerFailure(ctx, &change); err != nil {
			return model.Work{}, false, fmt.Errorf("claim payload worker: %w", err)
		}
		if err := worker.repository.CommitWorkChange(ctx, change); err != nil {
			return model.Work{}, false, fmt.Errorf("claim payload worker: %w", err)
		}
		return model.Work{}, false, nil
	}
	id, err := worker.newID()
	if err != nil {
		return model.Work{}, false, fmt.Errorf("claim payload worker: create identity: %w", err)
	}
	if id == "" {
		return model.Work{}, false, fmt.Errorf("claim payload worker: %w", model.ErrScheduleIDInvalid)
	}
	after := before
	after.State = "RUNNING"
	after.WorkerID = id
	after.Attempt++
	after.Version++
	if !after.Started.Set {
		after.Started = model.WorkTime{Set: true, Value: now}
	}
	if !after.Deadline.Set {
		after.Deadline = model.WorkTime{Set: true, Value: after.Started.Value + ExecutionTimeout.Milliseconds()}
	}
	after.Lease = model.WorkTime{Set: true, Value: min(now+workerLease.Milliseconds(), after.Deadline.Value)}
	after.Heartbeat = model.WorkTime{Set: true, Value: now}
	change := model.WorkChange{
		Before: before, After: after, NowMS: now, EventType: "STARTED",
		EventJSON: fmt.Sprintf(`{"schemaVersion":1,"executionNo":%d,"attempt":%d}`, after.ExecutionNo, after.Attempt),
	}
	if err := worker.repository.CommitWorkChange(ctx, change); err != nil {
		return model.Work{}, false, fmt.Errorf("claim payload worker: %w", err)
	}
	return after, true, nil
}

func validWork(work model.Work) bool {
	return work.ID != "" && work.Scope.ID != "" && (work.Kind == "PAYLOAD_RELEASE" || work.Kind == "BLOB_GC") &&
		work.ExecutionNo > 0 && work.Attempt >= 0 && work.MaxAttempts > 0 && work.Version > 0 && work.Version < math.MaxInt64
}

func executionBudgetFailure(work model.Work, now int64) error {
	if work.Deadline.Set && work.Deadline.Value <= now {
		return model.ErrExecutionTimeout
	}
	if work.Attempt >= work.MaxAttempts {
		return model.ErrAttemptsExhausted
	}
	return nil
}

func (worker *Worker) Recover(ctx context.Context) error {
	now := worker.now().UnixMilli()
	pending, err := worker.repository.LoadInterruptedWork(ctx, now, 100)
	if err != nil {
		return fmt.Errorf("recover payload worker: read interrupted: %w", err)
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
			cause = model.ErrExecutionLost
		}
		change, err := worker.settlement(before, cause, now)
		if err != nil {
			return fmt.Errorf("recover payload worker: %w", err)
		}
		if err := worker.prepareOwnerFailure(ctx, &change); err != nil {
			return fmt.Errorf("recover payload worker: %w", err)
		}
		if err := worker.repository.CommitWorkChange(ctx, change); err != nil {
			return fmt.Errorf("recover payload worker: %w", err)
		}
	}
	return nil
}
