package emulationstationimport

import (
	"context"
	"fmt"
)

type SourceObserver interface {
	Observe(context.Context, Execution) (LeaseState, error)
}
type SourceGuard struct{ observer SourceObserver }

func NewSourceGuard(observer SourceObserver) *SourceGuard { return &SourceGuard{observer: observer} }

func (guard *SourceGuard) Check(ctx context.Context, unit Execution) error {
	state, err := guard.observer.Observe(ctx, unit)
	if err != nil {
		return fmt.Errorf("observe EmulationStation source execution: %w: %w", ErrExecutionObservation, err)
	}
	switch state {
	case LeaseActive:
		return nil
	case LeaseCancelled:
		return ErrExecutionCancelled
	case LeaseLost:
		return ErrVersionConflict
	case LeaseDeadline:
		return ErrExpired
	default:
		return ErrVersionConflict
	}
}
