package pegasusimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/pegasusimport"
)

type (
	WorkRecovery interface{ Recover(context.Context) error }
	WorkExpiry   interface{ Expire(context.Context) error }
	Maintenance  struct {
		recovery WorkRecovery
		expiry   WorkExpiry
	}
)

func NewMaintenance(recovery WorkRecovery, expiry WorkExpiry) *Maintenance {
	return &Maintenance{recovery: recovery, expiry: expiry}
}

func (service *Maintenance) Maintain(ctx context.Context) error {
	if err := service.recovery.Recover(ctx); err != nil {
		return fmt.Errorf("recover Pegasus work: %w", err)
	}
	if err := service.expiry.Expire(ctx); err != nil {
		return fmt.Errorf("expire Pegasus plans: %w", err)
	}
	return nil
}

type (
	CancellationObserver interface {
		Cancelled(context.Context, model.ExecutionIdentity) (bool, error)
	}
	WorkerCancellationControl struct {
		observer   CancellationObserver
		settlement CancellationObserver
	}
)

func NewWorkerCancellation(observer, settlement CancellationObserver) *WorkerCancellationControl {
	return &WorkerCancellationControl{observer: observer, settlement: settlement}
}

func (control *WorkerCancellationControl) Cancelled(ctx context.Context, id model.ExecutionIdentity) (bool, error) {
	pending, err := control.observer.Cancelled(ctx, id)
	if err != nil {
		return false, fmt.Errorf("observe Pegasus cancellation: %w", err)
	}
	return pending, nil
}

func (control *WorkerCancellationControl) CloseCancelled(
	ctx context.Context,
	id model.ExecutionIdentity,
) (bool, error) {
	closed, err := control.settlement.Cancelled(ctx, id)
	if err != nil {
		return false, fmt.Errorf("settle Pegasus cancellation: %w", err)
	}
	return closed, nil
}
