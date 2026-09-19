package emulationstationimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/emulationstationimport"
)

type SourceObserver interface {
	Observe(context.Context, model.Execution) (model.LeaseState, error)
}
type SourceGuard struct{ observer SourceObserver }

func NewSourceGuard(observer SourceObserver) *SourceGuard { return &SourceGuard{observer: observer} }

func (guard *SourceGuard) Check(ctx context.Context, unit model.Execution) error {
	state, err := guard.observer.Observe(ctx, unit)
	if err != nil {
		return fmt.Errorf("observe EmulationStation source execution: %w: %w", ErrExecutionObservation, err)
	}
	switch state {
	case model.LeaseActive:
		return nil
	case model.LeaseCancelled:
		return ErrExecutionCancelled
	case model.LeaseLost:
		return model.ErrVersionConflict
	case model.LeaseDeadline:
		return model.ErrExpired
	default:
		return model.ErrVersionConflict
	}
}
