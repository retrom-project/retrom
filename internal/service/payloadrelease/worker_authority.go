package payloadrelease

import (
	"context"
	"fmt"

	model "retrom/internal/model/payloadrelease"
)

// CheckInScope validates the originally claimed execution inside each effect transaction.
func (worker *Worker) CheckInScope(ctx context.Context, scope model.WorkerScope, unit model.Work) error {
	current, err := worker.current(ctx, scope, unit)
	if err != nil {
		return err
	}
	if err := scope.Write.Fence(ctx, current); err != nil {
		return fmt.Errorf("fence payload effect: %w", err)
	}
	return nil
}

func (worker *Worker) current(ctx context.Context, scope model.WorkerScope, unit model.Work) (model.Work, error) {
	before, found, err := scope.Read.Current(ctx, unit.ID)
	if err != nil {
		return model.Work{}, fmt.Errorf("read payload authority: %w", err)
	}
	now := worker.now().UnixMilli()
	if !found || !validWork(before) || !sameExecution(before, unit) || !before.Lease.Set || before.Lease.Value <= now ||
		!before.Deadline.Set || before.Deadline.Value <= now {
		return model.Work{}, model.ErrExecutionLost
	}
	return before, nil
}

func sameExecution(current, original model.Work) bool {
	return current.ID == original.ID && current.Kind == original.Kind && current.Scope == original.Scope &&
		current.State == "RUNNING" && original.State == "RUNNING" && current.WorkerID != "" &&
		current.WorkerID == original.WorkerID && current.ExecutionNo == original.ExecutionNo &&
		current.Attempt == original.Attempt && current.MaxAttempts == original.MaxAttempts &&
		current.Started == original.Started && current.Deadline == original.Deadline &&
		current.InputFound == original.InputFound &&
		current.InputJSON == original.InputJSON && current.InputDigest == original.InputDigest
}

func (worker *Worker) Renew(ctx context.Context, unit model.Work) error {
	err := worker.repository.WithWorker(ctx, func(scope model.WorkerScope) error {
		before, err := worker.current(ctx, scope, unit)
		if err != nil {
			return err
		}
		now := worker.now().UnixMilli()
		after := before
		after.Version++
		after.Heartbeat = model.WorkTime{Set: true, Value: now}
		after.Lease = model.WorkTime{Set: true, Value: min(now+workerLease.Milliseconds(), before.Deadline.Value)}
		if err := scope.Write.Change(ctx, model.WorkChange{Before: before, After: after, NowMS: now}); err != nil {
			return fmt.Errorf("renew payload worker: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("renew payload execution: %w", err)
	}
	return nil
}
