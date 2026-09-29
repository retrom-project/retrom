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
	arcade *ArcadePreparation,
	reload func(context.Context, string, string) (Snapshot, error),
) (Result, error) {
	source, snapshot, err := prepareScheduleSource(ctx, scope, snapshot, now, nextID, arcade, reload)
	if err != nil {
		return Result{}, err
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

func prepareScheduleSource(
	ctx context.Context, scope WriteScope, snapshot Snapshot, now int64,
	nextID func() (string, error), arcade *ArcadePreparation,
	reload func(context.Context, string, string) (Snapshot, error),
) (Source, Snapshot, error) {
	source := snapshot.Source
	if source.VariantID != "" {
		return source, snapshot, nil
	}
	id, err := createValidationVariant(ctx, scope, source, now, nextID, arcade)
	if err != nil {
		return Source{}, Snapshot{}, err
	}
	source.VariantID = id
	if arcade == nil {
		return source, snapshot, nil
	}
	refreshed, err := reloadArcadeValidation(ctx, source, reload)
	return source, refreshed, err
}

func createValidationVariant(
	ctx context.Context, scope WriteScope, source Source, now int64,
	nextID func() (string, error), arcade *ArcadePreparation,
) (string, error) {
	id, err := checkedVariantID(nextID)
	if err != nil {
		return "", err
	}
	source.VariantID = id
	if err := scope.CreateVariant(ctx, VariantWrite{Source: source, Arcade: arcade, NowMS: now}); err != nil {
		return "", fmt.Errorf("create validation variant: %w", err)
	}
	return id, nil
}

func reloadArcadeValidation(
	ctx context.Context, source Source, reload func(context.Context, string, string) (Snapshot, error),
) (Snapshot, error) {
	if reload == nil {
		return Snapshot{}, ErrBlocked
	}
	snapshot, err := reload(ctx, source.GameID, source.CoreID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("reload alternate arcade BIOS: %w", err)
	}
	if !snapshot.Found || snapshot.Source.VariantID != source.VariantID {
		return Snapshot{}, ErrBlocked
	}
	return snapshot, nil
}
