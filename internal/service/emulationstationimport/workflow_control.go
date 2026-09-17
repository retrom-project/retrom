package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type WorkflowControl struct {
	repository model.WorkflowRepository
	sources    model.FrozenSources
	now        func() time.Time
}

func NewWorkflowControl(
	repository model.WorkflowRepository,
	sources model.FrozenSources,
	now func() time.Time,
) *WorkflowControl {
	return &WorkflowControl{repository: repository, sources: sources, now: now}
}

func (service *WorkflowControl) Retry(
	ctx context.Context,
	id string,
	version int64,
	actor string,
) (model.Summary, error) {
	before, err := service.repository.InspectRetry(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return model.Summary{}, model.ErrNotRetryable
	}
	if err != nil {
		return model.Summary{}, fmt.Errorf("inspect EmulationStation retry: %w", err)
	}
	if err := model.ValidateRetryEligibility(before, version); err != nil {
		return model.Summary{}, err
	}
	if err := verifyFrozenSource(ctx, service.sources, before.Summary, before.FrozenSourceSnapshot); err != nil {
		return model.Summary{}, err
	}
	plan, err := newRetryPlan(before, actor)
	if err != nil {
		return model.Summary{}, err
	}
	return service.queueRetry(ctx, plan, version)
}

func (service *WorkflowControl) queueRetry(
	ctx context.Context,
	plan model.RetryPlan,
	version int64,
) (model.Summary, error) {
	plan.NowMS = service.now().UnixMilli()
	cmd := model.RetryWorkflowCommand{Plan: plan, Version: version}
	result, err := service.repository.CommitRetryWorkflow(ctx, cmd)
	if err != nil {
		return model.Summary{}, fmt.Errorf("finish EmulationStation retry: %w", err)
	}
	return result, nil
}
