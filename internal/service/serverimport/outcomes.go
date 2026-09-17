package serverimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/serverimport"
)

type Outcomes struct {
	repository model.OutcomeRepository
	now        func() time.Time
}

func NewOutcomes(
	repository model.OutcomeRepository, now func() time.Time,
) *Outcomes {
	return &Outcomes{repository, now}
}

func (service *Outcomes) Finish(
	ctx context.Context, unit model.Work,
) error {
	err := service.repository.CommitFinish(ctx, model.FinishCommand{
		Unit: unit, Now: service.now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("finish server import: %w", err)
	}
	return nil
}

func (service *Outcomes) Cancel(
	ctx context.Context, unit model.Work,
) error {
	err := service.repository.CommitCancelOutcome(
		ctx,
		model.CancelOutcomeCommand{
			Unit: unit, Now: service.now().UnixMilli(),
		},
	)
	if err != nil {
		return fmt.Errorf("cancel import execution: %w", err)
	}
	return nil
}

func (service *Outcomes) Fail(
	ctx context.Context, unit model.Work, code string,
) (int64, error) {
	result, err := service.repository.CommitFail(ctx, model.FailCommand{
		Unit: unit, Code: code, Now: service.now().UnixMilli(),
	})
	if err != nil {
		return 0, fmt.Errorf("fail import execution: %w", err)
	}
	return result.RetryAt, nil
}

func (service *Outcomes) Reconcile(
	ctx context.Context,
) (bool, error) {
	pending, found, err := service.repository.Recovery(
		ctx, service.now().UnixMilli(),
	)
	if err != nil {
		return false, fmt.Errorf("find import recovery: %w", err)
	}
	if !found {
		return false, nil
	}
	if pending.Cancelled {
		if err := service.Cancel(ctx, pending.Unit); err != nil {
			return false, err
		}
	} else {
		if _, err := service.Fail(
			ctx, pending.Unit, "INTERNAL_ERROR",
		); err != nil {
			return false, err
		}
	}
	return true, nil
}
