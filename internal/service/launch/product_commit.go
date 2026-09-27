package launch

import (
	"context"
	"fmt"
	"math"

	gamevariant "retrom/internal/service/gamevariant"
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

func (service *ProductCreator) commitValidation(
	ctx context.Context,
	scope ProductCreationScope,
	command ProductCreateCommand,
	current ProductSnapshot,
	now int64,
) (productAttempt, error) {
	readiness, err := gamevariant.Schedule(
		ctx, scope.Validation(), current.VariantSnapshot(), now, service.environment.NewID,
	)
	if err != nil {
		return productAttempt{}, fmt.Errorf("%w: %w", ErrBlocked, err)
	}
	if readiness.Ready {
		return productAttempt{ready: true}, nil
	}
	result := Created{Status: "VALIDATION_PENDING", JobID: readiness.JobID, RetryAfterMS: readiness.RetryAfterMS}
	receipt, err := productReceipt(result, now)
	if err != nil {
		return productAttempt{}, err
	}
	if err := scope.StoreReceipt(ctx, command, receipt); err != nil {
		return productAttempt{}, fmt.Errorf("save pending product receipt: %w", err)
	}
	return productAttempt{receipt: receipt, resume: result.JobID}, nil
}
