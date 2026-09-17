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
	err := service.repository.WithPlanWrite(ctx, func(records model.PlanRecords) error {
		before, err := records.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("read EmulationStation deletion plan: %w", err)
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
			return fmt.Errorf("generate EmulationStation deletion audit: %w", err)
		}
		if err := records.Delete(
			ctx,
			model.PlanDeletion{Before: before, ActorID: actorID, AuditID: auditID.String(), NowMS: service.now().UnixMilli()},
		); err != nil {
			return fmt.Errorf(
				"delete EmulationStation plan: %w",
				err,
			)
		}
		return nil
	})
	if err != nil {
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
	err := service.repository.WithPlanWrite(ctx, func(records model.PlanRecords) error {
		before, err := records.Get(ctx, candidate.ID)
		if errors.Is(err, model.ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read EmulationStation expiry plan: %w", err)
		}
		if before.Version != candidate.Version || before.State != "AWAITING_MAPPING" || before.ExpiresAtMS > now {
			return nil
		}
		if err := records.Expire(
			ctx,
			model.PlanExpiry{Before: before, NowMS: now},
		); err != nil {
			return fmt.Errorf(
				"expire EmulationStation plan: %w",
				err,
			)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("finish EmulationStation expiry: %w", err)
	}
	return nil
}
