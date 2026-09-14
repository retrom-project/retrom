package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Recovery struct {
	repository RecoveryRepository
	now        func() time.Time
}

func NewRecovery(repository RecoveryRepository, now func() time.Time) *Recovery {
	return &Recovery{repository: repository, now: now}
}

func (service *Recovery) Recover(ctx context.Context) error {
	candidates, err := service.repository.Expired(ctx, service.now().UnixMilli(), 100)
	if err != nil {
		return fmt.Errorf("list expired EmulationStation executions: %w", err)
	}
	for _, candidate := range candidates {
		err := service.repository.WithRecovery(ctx, func(scope RecoveryScope) error {
			return service.recoverInScope(ctx, scope, candidate)
		})
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		if err != nil {
			return fmt.Errorf("recover EmulationStation execution %s: %w", candidate.JobID, err)
		}
	}
	return nil
}

func (service *Recovery) recoverInScope(ctx context.Context, scope RecoveryScope, candidate LeaseSnapshot) error {
	current, found, err := scope.Read.Current(ctx, candidate.JobID)
	if err != nil {
		return fmt.Errorf("read EmulationStation recovery candidate: %w", err)
	}
	if !found {
		return nil
	}
	if !sameRecoverySnapshot(current, candidate) {
		return ErrVersionConflict
	}
	if _, err := planRecovery(current, service.now().UnixMilli()); err != nil {
		return err
	}
	current, more, err := completeExecutionReviews(ctx, ExecutionReviewScope{
		Read: scope.Read, Write: scope.Write, Metadata: scope.Metadata,
	}, current, service.now)
	if err != nil {
		return err
	}
	if more {
		return nil
	}
	change, err := planRecovery(current, service.now().UnixMilli())
	if err != nil {
		return err
	}
	if err := scope.Write.Apply(ctx, change); err != nil {
		return fmt.Errorf("persist EmulationStation recovery: %w", err)
	}
	if change.SchedulePayload {
		return scheduleTerminalPayloads(ctx, scope.Payload, change.Before.ImportID, change.NowMS)
	}
	return nil
}

func sameRecoverySnapshot(a, b LeaseSnapshot) bool {
	if (a.StartedAtMS == nil) != (b.StartedAtMS == nil) {
		return false
	}
	if a.StartedAtMS != nil && *a.StartedAtMS != *b.StartedAtMS {
		return false
	}
	a.StartedAtMS, b.StartedAtMS = nil, nil
	return a == b
}
