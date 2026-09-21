package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type (
	ExpiredPlan struct {
		ID      string
		Version int64
	}
	PlanDeletion struct {
		Before           Summary
		ActorID, AuditID string
		NowMS            int64
	}
	PlanExpiry struct {
		Before Summary
		NowMS  int64
	}
	PlanRecords interface {
		Get(context.Context, string) (Summary, error)
		Delete(context.Context, PlanDeletion) error
		Expire(context.Context, PlanExpiry) error
	}
	PlanLifecycleRepository interface {
		WithPlanWrite(context.Context, func(PlanRecords) error) error
		ExpiredPlans(context.Context, int64, int) ([]ExpiredPlan, error)
	}
	PlanLifecycle struct {
		repository PlanLifecycleRepository
		now        func() time.Time
	}
)

func NewPlanLifecycle(
	repository PlanLifecycleRepository,
	now func() time.Time,
) *PlanLifecycle {
	return &PlanLifecycle{repository: repository, now: now}
}

func (service *PlanLifecycle) Delete(ctx context.Context, id string, version int64, actorID string) error {
	err := service.repository.WithPlanWrite(ctx, func(records PlanRecords) error {
		before, err := records.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("read Pegasus deletion plan: %w", err)
		}
		if before.Version != version || before.ImportJobID != nil ||
			before.State != "AWAITING_MAPPING" && before.State != "EXPIRED" {
			return ErrInvalid
		}
		if actorID == "" {
			actorID = before.CreatedBy.ID
		}
		auditID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generate Pegasus deletion audit: %w", err)
		}
		if err := records.Delete(
			ctx,
			PlanDeletion{Before: before, ActorID: actorID, AuditID: auditID.String(), NowMS: service.now().UnixMilli()},
		); err != nil {
			return fmt.Errorf(
				"delete Pegasus plan: %w",
				err,
			)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("finish Pegasus deletion: %w", err)
	}
	return nil
}

func (service *PlanLifecycle) Expire(ctx context.Context) error {
	now := service.now().UnixMilli()
	candidates, err := service.repository.ExpiredPlans(ctx, now, 100)
	if err != nil {
		return fmt.Errorf("read expired Pegasus plans: %w", err)
	}
	for _, candidate := range candidates {
		if err := service.expireCandidate(ctx, candidate, now); err != nil {
			return err
		}
	}
	return nil
}

func (service *PlanLifecycle) expireCandidate(ctx context.Context, candidate ExpiredPlan, now int64) error {
	err := service.repository.WithPlanWrite(ctx, func(records PlanRecords) error {
		before, err := records.Get(ctx, candidate.ID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read Pegasus expiry plan: %w", err)
		}
		if before.Version != candidate.Version || before.State != "AWAITING_MAPPING" || before.ExpiresAtMS > now {
			return nil
		}
		if err := records.Expire(
			ctx,
			PlanExpiry{Before: before, NowMS: now},
		); err != nil {
			return fmt.Errorf(
				"expire Pegasus plan: %w",
				err,
			)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("finish Pegasus expiry: %w", err)
	}
	return nil
}
