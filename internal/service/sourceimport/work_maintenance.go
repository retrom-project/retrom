package sourceimport

import (
	"context"
	"fmt"
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
		return fmt.Errorf("recover Source work: %w", err)
	}
	if err := service.expiry.Expire(ctx); err != nil {
		return fmt.Errorf("expire Source plans: %w", err)
	}
	return nil
}

type (
	CancellationObserver interface {
		Cancelled(context.Context, ExecutionIdentity) (bool, error)
	}
	WorkerCancellationControl struct {
		observer   CancellationObserver
		settlement CancellationObserver
	}
)

func NewWorkerCancellation(observer, settlement CancellationObserver) *WorkerCancellationControl {
	return &WorkerCancellationControl{observer: observer, settlement: settlement}
}

func (control *WorkerCancellationControl) Cancelled(ctx context.Context, id ExecutionIdentity) (bool, error) {
	pending, err := control.observer.Cancelled(ctx, id)
	if err != nil {
		return false, fmt.Errorf("observe Source cancellation: %w", err)
	}
	return pending, nil
}

func (control *WorkerCancellationControl) CloseCancelled(ctx context.Context, id ExecutionIdentity) (bool, error) {
	closed, err := control.settlement.Cancelled(ctx, id)
	if err != nil {
		return false, fmt.Errorf("settle Source cancellation: %w", err)
	}
	return closed, nil
}
