package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type WorkflowControl struct {
	repository WorkflowRepository
	sources    FrozenSources
	now        func() time.Time
}

func NewWorkflowControl(repository WorkflowRepository, sources FrozenSources, now func() time.Time) *WorkflowControl {
	return &WorkflowControl{repository: repository, sources: sources, now: now}
}

func (service *WorkflowControl) Retry(ctx context.Context, id string, version int64, actor string) (Summary, error) {
	before, err := service.repository.InspectRetry(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Summary{}, ErrNotRetryable
	}
	if err != nil {
		return Summary{}, fmt.Errorf("inspect EmulationStation retry: %w", err)
	}
	if err := validateRetry(before, version); err != nil {
		return Summary{}, err
	}
	if err := verifyFrozenSource(ctx, service.sources, before.Summary, before.FrozenSourceSnapshot); err != nil {
		return Summary{}, err
	}
	plan, err := newRetryPlan(before, actor)
	if err != nil {
		return Summary{}, err
	}
	return service.queueRetry(ctx, plan, version)
}

func (service *WorkflowControl) queueRetry(ctx context.Context, plan RetryPlan, version int64) (Summary, error) {
	var result Summary
	err := service.repository.WithControl(ctx, func(scope WorkflowScope) error {
		current, err := scope.Read.RetryCurrent(ctx, plan.Before.Summary.ID)
		if errors.Is(err, ErrNotFound) {
			return ErrNotRetryable
		}
		if err != nil {
			return fmt.Errorf("reread EmulationStation retry: %w", err)
		}
		if err := validateRetry(current, version); err != nil {
			return err
		}
		if !sameRetryExecution(plan.Before, current) {
			return ErrNotRetryable
		}
		if !sameFrozenSource(
			plan.Before.Summary,
			current.Summary,
			plan.Before.FrozenSourceSnapshot,
			current.FrozenSourceSnapshot,
		) {
			return ErrSourceChanged
		}
		plan.Before = current
		plan.NowMS = service.now().UnixMilli()
		if err := scope.Write.Retry(ctx, plan); err != nil {
			return fmt.Errorf("persist EmulationStation retry: %w", err)
		}
		after, err := scope.Read.Current(ctx, current.Summary.ID)
		if err != nil {
			return fmt.Errorf("read retried EmulationStation plan: %w", err)
		}
		result = after.Summary
		return nil
	})
	if err != nil {
		return Summary{}, fmt.Errorf("finish EmulationStation retry: %w", err)
	}
	return result, nil
}
