package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/emulationstationimport"

	"github.com/google/uuid"
)

type PlanLifecycle struct {
	repository model.PlanLifecycleRepository
	now        func() time.Time
}

func NewPlanLifecycle(
	repository model.PlanLifecycleRepository,
	now func() time.Time,
) *PlanLifecycle {
	return &PlanLifecycle{repository: repository, now: now}
}

func (service *PlanLifecycle) Delete(ctx context.Context, id string, version int64, actorID string) error {
	before, err := service.repository.LoadPlanSummary(ctx, id)
	if err != nil {
		return fmt.Errorf("finish EmulationStation deletion: %w", err)
	}
	if before.Version != version || before.ImportJobID != nil ||
		before.State != "AWAITING_MAPPING" && before.State != "EXPIRED" {
		return model.ErrInvalid
	}
	if actorID == "" {
		actorID = before.CreatedBy.ID
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("finish EmulationStation deletion: %w", err)
	}
	if err := service.repository.CommitPlanDeletion(
		ctx,
		model.PlanDeletion{Before: before, ActorID: actorID, AuditID: auditID.String(), NowMS: service.now().UnixMilli()},
	); err != nil {
		return fmt.Errorf("finish EmulationStation deletion: %w", err)
	}
	return nil
}

func (service *PlanLifecycle) Expire(ctx context.Context) error {
	now := service.now().UnixMilli()
	candidates, err := service.repository.ExpiredPlans(ctx, now, 100)
	if err != nil {
		return fmt.Errorf("read expired EmulationStation plans: %w", err)
	}
	for _, candidate := range candidates {
		if err := service.expireCandidate(ctx, candidate, now); err != nil {
			return err
		}
	}
	return nil
}

func (service *PlanLifecycle) expireCandidate(ctx context.Context, candidate model.ExpiredPlan, now int64) error {
	before, err := service.repository.LoadPlanSummary(ctx, candidate.ID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("finish EmulationStation expiry: %w", err)
	}
	if before.Version != candidate.Version || before.State != "AWAITING_MAPPING" || before.ExpiresAtMS > now {
		return nil
	}
	if err := service.repository.CommitPlanExpiry(
		ctx,
		model.PlanExpiry{Before: before, NowMS: now},
	); err != nil {
		return fmt.Errorf("finish EmulationStation expiry: %w", err)
	}
	return nil
}
