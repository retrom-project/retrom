package gamevariant

import (
	"context"
	"fmt"
	"time"
)

type Result struct {
	Ready        bool
	JobID        string
	RetryAfterMS int64
}

func Schedule(
	ctx context.Context,
	scope WriteScope,
	snapshot Snapshot,
	now int64,
	nextID func() (string, error),
) (Result, error) {
	source := snapshot.Source
	if source.VariantID == "" {
		id, err := checkedVariantID(nextID)
		if err != nil {
			return Result{}, err
		}
		source.VariantID = id
		if err := scope.CreateVariant(ctx, VariantWrite{Source: source, NowMS: now}); err != nil {
			return Result{}, fmt.Errorf("create validation variant: %w", err)
		}
	}
	inputs, err := Inputs(snapshot, source.VariantID)
	if err != nil {
		return Result{}, err
	}
	scheduler := NewValidationScheduler(
		scope,
		ValidationEnvironment{Now: func() time.Time { return time.UnixMilli(now) }, NewID: nextID},
	)
	queued, err := scheduler.Queue(ctx, inputs)
	if err != nil {
		return Result{}, err
	}
	if queued.Queued {
		if err := scope.MarkPending(ctx, source.VariantID, now); err != nil {
			return Result{}, fmt.Errorf("update validation variant: %w", err)
		}
	} else if source.VariantStatus == "READY" {
		return Result{Ready: true}, nil
	}
	return Result{JobID: queued.JobID, RetryAfterMS: 1000}, nil
}
