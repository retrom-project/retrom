package launch

import (
	"context"
	"fmt"
	"math"
	"time"
)

func (service *ProductCreator) commit(
	ctx context.Context,
	scope ProductCreationScope,
	command ProductCreateCommand,
	preparation productPreparation,
) (productAttempt, error) {
	stored, found, err := scope.Replay(ctx, command)
	if err != nil {
		return productAttempt{}, fmt.Errorf("read final product replay: %w", err)
	}
	if found {
		receipt, err := service.replay(command, stored)
		return productAttempt{receipt: receipt}, err
	}
	current, err := scope.Snapshot(ctx, command)
	if err != nil {
		return productAttempt{}, fmt.Errorf("read final product authority: %w", err)
	}
	if !sameProductInputs(preparation.snapshot, current, preparation.validation) {
		return productAttempt{}, ErrBlocked
	}
	now := service.environment.Now().UnixMilli()
	if now < 0 || now > math.MaxInt64-86_400_000 {
		return productAttempt{}, ErrBlocked
	}
	if preparation.validation {
		return service.commitValidation(ctx, scope, command, current, now)
	}
	plan := preparation.plan
	plan.NowMS, plan.BootstrapEnd, plan.HardEnd = now, now+300_000, now+86_400_000
	if err := scope.Create(ctx, plan); err != nil {
		return productAttempt{}, fmt.Errorf("persist product plan: %w", err)
	}
	result := Created{
		LaunchID:             plan.ID,
		PlayURL:              "/play/" + plan.ID,
		Warnings:             []string{},
		BootstrapExpiresAtMS: plan.BootstrapEnd,
		HardExpiresAtMS:      plan.HardEnd,
		Capability:           preparation.capability,
	}
	receipt, err := productReceipt(result, now)
	if err != nil {
		return productAttempt{}, err
	}
	if err := scope.StoreReceipt(ctx, command, receipt); err != nil {
		return productAttempt{}, fmt.Errorf("save created product receipt: %w", err)
	}
	return productAttempt{receipt: receipt}, nil
}

func (service *ProductCreator) schedule(
	ctx context.Context,
	scope ProductValidationScope,
	snapshot ProductSnapshot,
	now int64,
) (Created, bool, error) {
	source := snapshot.Source
	if source.VariantID == "" {
		id, err := checkedProductID(service.environment.NewID)
		if err != nil {
			return Created{}, false, err
		}
		source.VariantID = id
		if err := scope.CreateVariant(ctx, ProductVariantWrite{Source: source, NowMS: now}); err != nil {
			return Created{}, false, fmt.Errorf("create validation variant: %w", err)
		}
	}
	inputs, err := ProductValidationInputs(snapshot, source.VariantID)
	if err != nil {
		return Created{}, false, err
	}
	scheduler := NewValidationScheduler(
		scope,
		ValidationEnvironment{Now: func() time.Time { return time.UnixMilli(now) }, NewID: service.environment.NewID},
	)
	queued, err := scheduler.Queue(ctx, inputs)
	if err != nil {
		return Created{}, false, err
	}
	if queued.Queued {
		if err := scope.MarkPending(ctx, source.VariantID, now); err != nil {
			return Created{}, false, fmt.Errorf("update validation variant: %w", err)
		}
	} else if source.VariantStatus == "READY" {
		return Created{Status: "READY"}, true, nil
	}
	return Created{Status: "VALIDATION_PENDING", JobID: queued.JobID, RetryAfterMS: 1000}, false, nil
}

func (service *ProductCreator) commitValidation(
	ctx context.Context,
	scope ProductCreationScope,
	command ProductCreateCommand,
	current ProductSnapshot,
	now int64,
) (productAttempt, error) {
	result, ready, err := service.schedule(ctx, scope.Validation(), current, now)
	if err != nil {
		return productAttempt{}, err
	}
	if ready {
		return productAttempt{ready: true}, nil
	}
	receipt, err := productReceipt(result, now)
	if err != nil {
		return productAttempt{}, err
	}
	if err := scope.StoreReceipt(ctx, command, receipt); err != nil {
		return productAttempt{}, fmt.Errorf("save pending product receipt: %w", err)
	}
	return productAttempt{receipt: receipt, resume: result.JobID}, nil
}
